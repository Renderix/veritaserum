package dbs

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"

	"veritaserum/src/store"
)

func StartPostgresMock(port string) {
	ln, err := net.Listen("tcp", ":"+port)
	if err != nil {
		log.Fatalf("postgres: listen error: %v", err)
	}
	log.Printf("Postgres mock listening on :%s", port)
	for {
		conn, err := ln.Accept()
		if err != nil {
			log.Printf("postgres: accept error: %v", err)
			continue
		}
		go handlePostgresConn(conn)
	}
}

func handlePostgresConn(conn net.Conn) {
	defer conn.Close()

	// --- Startup message ---
	// First 4 bytes = total length (big-endian int32)
	var msgLen int32
	if err := binary.Read(conn, binary.BigEndian, &msgLen); err != nil {
		return
	}
	// Read remaining bytes of startup message (msgLen - 4 already consumed)
	remaining := make([]byte, msgLen-4)
	if _, err := io.ReadFull(conn, remaining); err != nil {
		return
	}
	// We don't need to parse params — just accept all connections

	// --- AuthenticationOk ---
	conn.Write([]byte{'R', 0, 0, 0, 8, 0, 0, 0, 0})

	// --- ReadyForQuery ---
	conn.Write([]byte{'Z', 0, 0, 0, 5, 'I'})

	// --- Query loop ---
	for {
		// Read message type
		msgType := make([]byte, 1)
		if _, err := io.ReadFull(conn, msgType); err != nil {
			return
		}

		// Read 4-byte message length
		var length int32
		if err := binary.Read(conn, binary.BigEndian, &length); err != nil {
			return
		}

		// Read message body (length includes the 4-byte length field itself)
		body := make([]byte, length-4)
		if _, err := io.ReadFull(conn, body); err != nil {
			return
		}

		switch msgType[0] {
		case 'Q': // Simple Query
			// Body is null-terminated SQL string
			sql := string(bytes.TrimRight(body, "\x00"))
			log.Printf("POSTGRES QUERY: %s", sql)
			handlePostgresQuery(conn, sql)

		case 'X': // Terminate
			return
		}
	}
}

func handlePostgresQuery(conn net.Conn, sql string) {
	key := store.DBKey(store.ProtoPostgres, sql)

	if i := store.LookupConfigured(store.ProtoPostgres, key); i != nil && i.Response != nil {
		log.Printf("POSTGRES PLAYBACK: %s", sql)
		rowsJSON := "[]"
		if len(i.Response.Rows) > 0 {
			if b, err := json.Marshal(i.Response.Rows); err == nil {
				rowsJSON = string(b)
			}
		}
		if err := sendMockedRows(conn, rowsJSON); err != nil {
			log.Printf("postgres: sendMockedRows error: %v", err)
		}
		return
	}

	if !store.IsPending(store.ProtoPostgres, key) {
		req := store.InteractionRequest{Query: sql}
		store.RegisterInteraction(store.ProtoPostgres, key, req)
		log.Printf("POSTGRES INTERCEPT: %s → registered as pending", sql)
	}

	sendCommandComplete(conn, "SELECT 0")
	sendReadyForQuery(conn)
}

// sendMockedRows parses a JSON array and writes RowDescription + DataRow(s) + CommandComplete.
// Example jsonStr: [{"id":1,"name":"Alice"},{"id":2,"name":"Bob"}]
func sendMockedRows(conn net.Conn, jsonStr string) error {
	var rows []map[string]interface{}
	if err := json.Unmarshal([]byte(jsonStr), &rows); err != nil {
		// Fallback: empty result
		sendCommandComplete(conn, "SELECT 0")
		sendReadyForQuery(conn)
		return fmt.Errorf("parse json: %w", err)
	}

	if len(rows) == 0 {
		sendCommandComplete(conn, "SELECT 0")
		sendReadyForQuery(conn)
		return nil
	}

	// Derive ordered column names from the first row
	cols := make([]string, 0)
	for k := range rows[0] {
		cols = append(cols, k)
	}

	// RowDescription ('T')
	var rowDesc bytes.Buffer
	binary.Write(&rowDesc, binary.BigEndian, int16(len(cols)))
	for _, col := range cols {
		rowDesc.WriteString(col)
		rowDesc.WriteByte(0) // null terminator
		binary.Write(&rowDesc, binary.BigEndian, int32(0))  // table OID
		binary.Write(&rowDesc, binary.BigEndian, int16(0))  // column attr
		binary.Write(&rowDesc, binary.BigEndian, int32(25)) // type OID = text
		binary.Write(&rowDesc, binary.BigEndian, int16(-1)) // type size = variable
		binary.Write(&rowDesc, binary.BigEndian, int32(-1)) // type modifier
		binary.Write(&rowDesc, binary.BigEndian, int16(0))  // format = text
	}
	writeMessage(conn, 'T', rowDesc.Bytes())

	// DataRow ('D') for each row
	for _, row := range rows {
		var dataRow bytes.Buffer
		binary.Write(&dataRow, binary.BigEndian, int16(len(cols)))
		for _, col := range cols {
			val := fmt.Sprintf("%v", row[col])
			binary.Write(&dataRow, binary.BigEndian, int32(len(val)))
			dataRow.WriteString(val)
		}
		writeMessage(conn, 'D', dataRow.Bytes())
	}

	sendCommandComplete(conn, fmt.Sprintf("SELECT %d", len(rows)))
	sendReadyForQuery(conn)
	return nil
}

// writeMessage writes a Postgres backend message: type byte + int32 length + body.
// Length = 4 (for itself) + len(body).
func writeMessage(conn net.Conn, msgType byte, body []byte) {
	var buf bytes.Buffer
	buf.WriteByte(msgType)
	binary.Write(&buf, binary.BigEndian, int32(4+len(body)))
	buf.Write(body)
	conn.Write(buf.Bytes())
}

func sendCommandComplete(conn net.Conn, tag string) {
	var body bytes.Buffer
	body.WriteString(tag)
	body.WriteByte(0)
	writeMessage(conn, 'C', body.Bytes())
}

func sendReadyForQuery(conn net.Conn) {
	conn.Write([]byte{'Z', 0, 0, 0, 5, 'I'})
}
