package dbs

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"strings"

	"veritaserum/src/store"
)

type mysqlConn struct {
	conn   net.Conn
	stmts  map[uint32]string
	nextID uint32
	seq    byte
}

func StartMySQLMock(port string) {
	ln, err := net.Listen("tcp", ":"+port)
	if err != nil {
		log.Fatalf("mysql: listen error: %v", err)
	}
	log.Printf("MySQL mock listening on :%s", port)
	for {
		conn, err := ln.Accept()
		if err != nil {
			log.Printf("mysql: accept error: %v", err)
			continue
		}
		go handleMySQLConn(conn)
	}
}

func handleMySQLConn(conn net.Conn) {
	defer conn.Close()

	mc := &mysqlConn{
		conn:   conn,
		stmts:  make(map[uint32]string),
		nextID: 1,
		seq:    0,
	}

	sendHandshake(mc)

	// Read client HandshakeResponse — ignore content, just send OK
	if _, err := readPacket(mc); err != nil {
		return
	}
	sendOK(mc)

	// Reset sequence for command phase
	mc.seq = 0

	for {
		payload, err := readPacket(mc)
		if err != nil {
			return
		}
		if len(payload) == 0 {
			return
		}

		cmd := payload[0]
		data := payload[1:]

		switch cmd {
		case 0x03: // COM_QUERY
			sql := string(data)
			log.Printf("MYSQL QUERY: %s", sql)
			handleMySQLQuery(mc, sql)
		case 0x16: // COM_STMT_PREPARE
			sql := string(data)
			log.Printf("MYSQL STMT_PREPARE: %s", sql)
			handleStmtPrepare(mc, sql)
		case 0x17: // COM_STMT_EXECUTE
			handleStmtExecute(mc, data)
		case 0x19: // COM_STMT_CLOSE
			handleStmtClose(mc, data)
		case 0x01: // COM_QUIT
			return
		}
	}
}

func sendHandshake(mc *mysqlConn) {
	var p bytes.Buffer

	// Protocol version
	p.WriteByte(0x0a)
	// Server version
	p.WriteString("8.0.0-veritaserum\x00")
	// Connection ID
	binary.Write(&p, binary.LittleEndian, uint32(1))
	// Auth data part 1 (8 bytes) + filler
	p.Write(make([]byte, 8))
	p.WriteByte(0x00)
	// Capability flags lower 2 bytes: CLIENT_LONG_PASSWORD(1) | CLIENT_PROTOCOL_41(0x200) | CLIENT_SECURE_CONNECTION(0x8000)
	binary.Write(&p, binary.LittleEndian, uint16(0x8201))
	// Charset: utf8
	p.WriteByte(0x21)
	// Status flags
	binary.Write(&p, binary.LittleEndian, uint16(0x0002))
	// Capability flags upper 2 bytes
	binary.Write(&p, binary.LittleEndian, uint16(0x0000))
	// Auth plugin data length
	p.WriteByte(21)
	// Reserved 10 bytes
	p.Write(make([]byte, 10))
	// Auth data part 2 (13 bytes)
	p.Write(make([]byte, 13))
	// Auth plugin name
	p.WriteString("mysql_native_password\x00")

	writePacket(mc, p.Bytes())
}

func handleMySQLQuery(mc *mysqlConn, sql string) {
	key := store.MysqlKey(sql)

	store.MocksMu.RLock()
	entry, found := store.Mocks[key]
	store.MocksMu.RUnlock()

	if found && entry.State == store.StatusConfigured {
		log.Printf("MYSQL PLAYBACK: %s", sql)
		if err := sendResultSet(mc, entry.ResponseBody); err != nil {
			log.Printf("mysql: sendResultSet error: %v", err)
		}
		return
	}

	if !found {
		store.MocksMu.Lock()
		store.Mocks[key] = &store.MockDefinition{
			Protocol: "MYSQL",
			Query:    sql,
			State:    store.StatusPending,
		}
		store.MocksMu.Unlock()
		log.Printf("MYSQL INTERCEPT: %s → registered as pending", sql)
	}

	sendOK(mc)
}

func handleStmtPrepare(mc *mysqlConn, sql string) {
	stmtID := mc.nextID
	mc.stmts[stmtID] = sql
	mc.nextID++

	numParams := uint16(strings.Count(sql, "?"))

	// COM_STMT_PREPARE_OK
	var p bytes.Buffer
	p.WriteByte(0x00)                                          // OK
	binary.Write(&p, binary.LittleEndian, stmtID)             // stmt_id 4B
	binary.Write(&p, binary.LittleEndian, uint16(0))          // num_columns
	binary.Write(&p, binary.LittleEndian, numParams)          // num_params
	p.WriteByte(0x00)                                          // reserved
	binary.Write(&p, binary.LittleEndian, uint16(0))          // warning_count
	writePacket(mc, p.Bytes())

	// Send dummy param definitions + EOF if there are params
	if numParams > 0 {
		for i := uint16(0); i < numParams; i++ {
			writePacket(mc, dummyColumnDef())
		}
		sendEOF(mc)
	}
}

func handleStmtExecute(mc *mysqlConn, payload []byte) {
	if len(payload) < 4 {
		sendErr(mc, "malformed COM_STMT_EXECUTE")
		return
	}
	stmtID := binary.LittleEndian.Uint32(payload[0:4])
	sql, ok := mc.stmts[stmtID]
	if !ok {
		sendErr(mc, fmt.Sprintf("unknown stmt_id %d", stmtID))
		return
	}
	log.Printf("MYSQL STMT_EXECUTE stmtID=%d sql=%s", stmtID, sql)
	handleMySQLQuery(mc, sql)
}

func handleStmtClose(mc *mysqlConn, payload []byte) {
	if len(payload) < 4 {
		return
	}
	stmtID := binary.LittleEndian.Uint32(payload[0:4])
	delete(mc.stmts, stmtID)
	// No response for COM_STMT_CLOSE
}

func sendResultSet(mc *mysqlConn, jsonStr string) error {
	var rows []map[string]interface{}
	if err := json.Unmarshal([]byte(jsonStr), &rows); err != nil {
		sendOK(mc)
		return fmt.Errorf("parse json: %w", err)
	}

	if len(rows) == 0 {
		sendOK(mc)
		return nil
	}

	// Derive ordered column names from first row
	cols := make([]string, 0, len(rows[0]))
	for k := range rows[0] {
		cols = append(cols, k)
	}

	// Column count as length-encoded integer
	var countPkt bytes.Buffer
	writeLengthEncodedInt(&countPkt, len(cols))
	writePacket(mc, countPkt.Bytes())

	// Column definitions
	for _, col := range cols {
		writePacket(mc, columnDef(col))
	}
	sendEOF(mc)

	// Data rows
	for _, row := range rows {
		var rowPkt bytes.Buffer
		for _, col := range cols {
			val := row[col]
			if val == nil {
				rowPkt.WriteByte(0xfb) // NULL
			} else {
				writeLengthEncodedString(&rowPkt, fmt.Sprintf("%v", val))
			}
		}
		writePacket(mc, rowPkt.Bytes())
	}
	sendEOF(mc)

	return nil
}

func columnDef(name string) []byte {
	var p bytes.Buffer
	writeLengthEncodedString(&p, "def")    // catalog
	writeLengthEncodedString(&p, "")       // schema
	writeLengthEncodedString(&p, "")       // table
	writeLengthEncodedString(&p, "")       // org_table
	writeLengthEncodedString(&p, name)     // name
	writeLengthEncodedString(&p, name)     // org_name
	p.WriteByte(0x0c)                      // length of fixed fields
	binary.Write(&p, binary.LittleEndian, uint16(0x21)) // charset utf8
	binary.Write(&p, binary.LittleEndian, uint32(0))    // column length
	p.WriteByte(0xfd)                      // type: VAR_STRING
	binary.Write(&p, binary.LittleEndian, uint16(0))    // flags
	p.WriteByte(0x00)                      // decimals
	binary.Write(&p, binary.LittleEndian, uint16(0))    // filler
	return p.Bytes()
}

func dummyColumnDef() []byte {
	return columnDef("?")
}

// readPacket reads a MySQL packet and advances mc.seq.
func readPacket(mc *mysqlConn) ([]byte, error) {
	header := make([]byte, 4)
	if _, err := io.ReadFull(mc.conn, header); err != nil {
		return nil, err
	}
	length := int(header[0]) | int(header[1])<<8 | int(header[2])<<16
	mc.seq = header[3] + 1
	if length == 0 {
		return []byte{}, nil
	}
	payload := make([]byte, length)
	if _, err := io.ReadFull(mc.conn, payload); err != nil {
		return nil, err
	}
	return payload, nil
}

// writePacket writes a MySQL packet with the current sequence number, then increments seq.
func writePacket(mc *mysqlConn, payload []byte) {
	length := len(payload)
	header := []byte{
		byte(length),
		byte(length >> 8),
		byte(length >> 16),
		mc.seq,
	}
	mc.seq++
	mc.conn.Write(header)
	mc.conn.Write(payload)
}

func sendOK(mc *mysqlConn) {
	// OK packet: 0x00 affected_rows=0 last_insert_id=0 status=0x0002 warnings=0
	writePacket(mc, []byte{0x00, 0x00, 0x00, 0x02, 0x00, 0x00, 0x00})
}

func sendEOF(mc *mysqlConn) {
	// EOF packet: 0xfe warnings=0 status=0x0002
	writePacket(mc, []byte{0xfe, 0x00, 0x00, 0x02, 0x00})
}

func sendErr(mc *mysqlConn, msg string) {
	var p bytes.Buffer
	p.WriteByte(0xff)                                        // ERR
	binary.Write(&p, binary.LittleEndian, uint16(1064))     // error code
	p.WriteByte('#')                                         // SQL state marker
	p.WriteString("42000")                                   // SQL state
	p.WriteString(msg)
	writePacket(mc, p.Bytes())
}

func writeLengthEncodedString(b *bytes.Buffer, s string) {
	writeLengthEncodedInt(b, len(s))
	b.WriteString(s)
}

func writeLengthEncodedInt(b *bytes.Buffer, n int) {
	switch {
	case n < 251:
		b.WriteByte(byte(n))
	case n < 0x10000:
		b.WriteByte(0xfc)
		binary.Write(b, binary.LittleEndian, uint16(n))
	case n < 0x1000000:
		b.WriteByte(0xfd)
		b.WriteByte(byte(n))
		b.WriteByte(byte(n >> 8))
		b.WriteByte(byte(n >> 16))
	default:
		b.WriteByte(0xfe)
		binary.Write(b, binary.LittleEndian, uint64(n))
	}
}
