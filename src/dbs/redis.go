package dbs

import (
	"bufio"
	"fmt"
	"log"
	"net"
	"strconv"
	"strings"

	"veritaserum/src/store"
)

func StartRedisMock(port string) {
	ln, err := net.Listen("tcp", ":"+port)
	if err != nil {
		log.Fatalf("redis: listen error: %v", err)
	}
	log.Printf("Redis mock listening on :%s", port)
	for {
		conn, err := ln.Accept()
		if err != nil {
			log.Printf("redis: accept error: %v", err)
			continue
		}
		go handleRedisConn(conn)
	}
}

func handleRedisConn(conn net.Conn) {
	defer conn.Close()
	r := bufio.NewReader(conn)

	for {
		args, err := readRESP(r)
		if err != nil || len(args) == 0 {
			return
		}

		cmd := strings.ToUpper(args[0])

		if cmd == "PING" {
			conn.Write([]byte("+PONG\r\n"))
			continue
		}

		key := store.RedisKey(cmd, args[1:])

		if i := store.LookupConfigured(store.ProtoRedis, key); i != nil && i.Response != nil {
			log.Printf("REDIS PLAYBACK: %s", key)
			writeBulkString(conn, i.Response.Value)
			continue
		}

		if !store.IsPending(store.ProtoRedis, key) {
			req := store.InteractionRequest{
				Command: cmd,
				Args:    args[1:],
			}
			store.RegisterInteraction(store.ProtoRedis, key, req)
			log.Printf("REDIS INTERCEPT: %s → registered as pending", key)
		}

		// Return null bulk string so the client does not crash
		conn.Write([]byte("$-1\r\n"))
	}
}

// readRESP reads one RESP array command from the reader.
func readRESP(r *bufio.Reader) ([]string, error) {
	line, err := r.ReadString('\n')
	if err != nil {
		return nil, err
	}
	line = strings.TrimRight(line, "\r\n")
	if len(line) == 0 {
		return nil, nil
	}

	switch line[0] {
	case '*':
		// Array: *<count>\r\n followed by bulk strings
		count, err := strconv.Atoi(line[1:])
		if err != nil || count <= 0 {
			return nil, fmt.Errorf("invalid array count: %s", line)
		}
		args := make([]string, 0, count)
		for i := 0; i < count; i++ {
			lenLine, err := r.ReadString('\n')
			if err != nil {
				return nil, err
			}
			lenLine = strings.TrimRight(lenLine, "\r\n")
			if len(lenLine) == 0 || lenLine[0] != '$' {
				return nil, fmt.Errorf("expected bulk string, got: %s", lenLine)
			}
			n, err := strconv.Atoi(lenLine[1:])
			if err != nil {
				return nil, err
			}
			buf := make([]byte, n+2) // +2 for \r\n
			if _, err := r.Read(buf); err != nil {
				return nil, err
			}
			args = append(args, string(buf[:n]))
		}
		return args, nil

	default:
		// Inline command (some clients use this format)
		return strings.Fields(line), nil
	}
}

func writeBulkString(conn net.Conn, s string) {
	if s == "" {
		conn.Write([]byte("$-1\r\n"))
		return
	}
	conn.Write([]byte(fmt.Sprintf("$%d\r\n%s\r\n", len(s), s)))
}
