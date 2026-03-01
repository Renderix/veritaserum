# Veritaserum Redesign Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Rebuild Veritaserum from a flat stub server into a proper test management sidecar with protocol-aware capture, named test cases, a React+Bun+TS multi-tab UI, and a headless CLI replay mode.

**Architecture:** A single Go binary runs five mock servers (HTTP proxy, MySQL, Postgres, Redis wire mocks, and the API/UI server). All captured traffic is stored as `Interaction` records in a flat global store. The React frontend (built by Bun, embedded in the binary) organises interactions into named `TestCase` groups via a multi-tab UI. A `--replay --suite=file.json` flag starts the binary in headless mode for CI.

**Tech Stack:** Go 1.21+, Gin, React 18, TypeScript, Bun

---

## Task 1: Rewrite the store

**Files:**
- Rewrite: `src/store/store.go`

Replace all existing types (`Host`, `Endpoint`, `Scenario`, `MockDefinition`, `ProvisionedDB`) and their global vars with the new model. Keep `SaveState`/`LoadState` so persistence survives the redesign.

**Step 1: Replace `src/store/store.go` entirely**

```go
package store

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"sync"
	"time"
)

// ---- Protocols & States --------------------------------------------------

const (
	ProtoHTTP     = "HTTP"
	ProtoMySQL    = "MYSQL"
	ProtoPostgres = "POSTGRES"
	ProtoRedis    = "REDIS"
	ProtoDynamoDB = "DYNAMODB"

	StatePending    = "pending"
	StateConfigured = "configured"
)

// ---- Interaction ---------------------------------------------------------

type InteractionRequest struct {
	// HTTP + DynamoDB (DynamoDB is HTTP under the hood)
	Method   string            `json:"method,omitempty"`
	Host     string            `json:"host,omitempty"`
	Path     string            `json:"path,omitempty"`
	Headers  map[string]string `json:"headers,omitempty"`
	Body     string            `json:"body,omitempty"`
	BodyHash string            `json:"bodyHash,omitempty"`

	// DynamoDB-specific (parsed from body)
	Operation string `json:"operation,omitempty"` // GetItem, PutItem, Query, …
	Table     string `json:"table,omitempty"`
	KeyJSON   string `json:"keyJSON,omitempty"`

	// MySQL / Postgres
	Query string `json:"query,omitempty"`

	// Redis
	Command string   `json:"command,omitempty"`
	Args    []string `json:"args,omitempty"`
}

type InteractionResponse struct {
	// HTTP
	StatusCode int               `json:"statusCode,omitempty"`
	Headers    map[string]string `json:"headers,omitempty"`
	Body       string            `json:"body,omitempty"`
	LatencyMs  int               `json:"latencyMs,omitempty"`

	// MySQL / Postgres — SELECT
	Rows []map[string]interface{} `json:"rows,omitempty"`
	// MySQL / Postgres — INSERT/UPDATE/DELETE
	AffectedRows int `json:"affectedRows,omitempty"`

	// DynamoDB
	ItemJSON string `json:"itemJSON,omitempty"`

	// Redis
	Value string `json:"value,omitempty"`
}

type Interaction struct {
	ID         string               `json:"id"`
	Protocol   string               `json:"protocol"`
	Key        string               `json:"key"`   // routing key
	Name       string               `json:"name"`  // user-assigned label
	Request    InteractionRequest   `json:"request"`
	Response   *InteractionResponse `json:"response,omitempty"`
	State      string               `json:"state"`      // pending | configured
	TestCaseID string               `json:"testCaseId"` // empty until grouped
	CapturedAt time.Time            `json:"capturedAt"`
}

// ---- TestCase ------------------------------------------------------------

type TestCase struct {
	ID             string    `json:"id"`
	Name           string    `json:"name"`
	Description    string    `json:"description,omitempty"`
	InteractionIDs []string  `json:"interactionIds"`
	CreatedAt      time.Time `json:"createdAt"`
}

// ---- Schema (per DB table) ----------------------------------------------

type Schema struct {
	TableName       string `json:"tableName"`
	Protocol        string `json:"protocol"` // MYSQL or POSTGRES
	CreateStatement string `json:"createStatement"`
}

// ---- Global store --------------------------------------------------------

var (
	mu           sync.RWMutex
	interactions = map[string]*Interaction{}
	testCases    = map[string]*TestCase{}
	schemas      = map[string]*Schema{} // keyed by "MYSQL:tablename"
)

// ---- Interaction helpers -------------------------------------------------

// BodyHash returns the first 8 hex chars of SHA-256(body), or "" for empty body.
func BodyHash(body []byte) string {
	if len(body) == 0 {
		return ""
	}
	h := sha256.Sum256(body)
	return fmt.Sprintf("%x", h[:8])
}

// HTTPKey builds the routing key for an HTTP/DynamoDB interaction.
func HTTPKey(method, host, path, bodyHash string) string {
	return fmt.Sprintf("%s %s %s %s", method, host, path, bodyHash)
}

// DBKey builds the routing key for MySQL or Postgres.
func DBKey(protocol, query string) string {
	return fmt.Sprintf("%s %s", protocol, query)
}

// RedisKey builds the routing key for Redis.
func RedisKey(command string, args []string) string {
	key := command
	for _, a := range args {
		key += " " + a
	}
	return key
}

// RegisterInteraction records a new pending interaction. Idempotent — returns
// the existing one if the key is already known.
func RegisterInteraction(protocol, key string, req InteractionRequest) *Interaction {
	mu.Lock()
	defer mu.Unlock()
	for _, i := range interactions {
		if i.Protocol == protocol && i.Key == key {
			return i
		}
	}
	id := fmt.Sprintf("%d", time.Now().UnixNano())
	i := &Interaction{
		ID:         id,
		Protocol:   protocol,
		Key:        key,
		Request:    req,
		State:      StatePending,
		CapturedAt: time.Now(),
	}
	interactions[id] = i
	return i
}

// LookupConfigured returns a configured interaction for the given key, or nil.
func LookupConfigured(protocol, key string) *Interaction {
	mu.RLock()
	defer mu.RUnlock()
	for _, i := range interactions {
		if i.Protocol == protocol && i.Key == key && i.State == StateConfigured {
			return i
		}
	}
	return nil
}

// IsPending returns true if the key exists but is not yet configured.
func IsPending(protocol, key string) bool {
	mu.RLock()
	defer mu.RUnlock()
	for _, i := range interactions {
		if i.Protocol == protocol && i.Key == key && i.State == StatePending {
			return true
		}
	}
	return false
}

// ConfigureInteraction saves the user-supplied response for an interaction.
func ConfigureInteraction(id, name string, resp InteractionResponse) error {
	mu.Lock()
	defer mu.Unlock()
	i, ok := interactions[id]
	if !ok {
		return fmt.Errorf("interaction %s not found", id)
	}
	i.Name = name
	i.Response = &resp
	i.State = StateConfigured
	return nil
}

func GetAllInteractions() []*Interaction {
	mu.RLock()
	defer mu.RUnlock()
	out := make([]*Interaction, 0, len(interactions))
	for _, i := range interactions {
		out = append(out, i)
	}
	return out
}

func GetPendingInteractions() []*Interaction {
	mu.RLock()
	defer mu.RUnlock()
	out := make([]*Interaction, 0)
	for _, i := range interactions {
		if i.State == StatePending {
			out = append(out, i)
		}
	}
	return out
}

// ---- TestCase helpers ----------------------------------------------------

func CreateTestCase(name, description string) *TestCase {
	mu.Lock()
	defer mu.Unlock()
	id := fmt.Sprintf("tc-%d", time.Now().UnixNano())
	tc := &TestCase{
		ID:             id,
		Name:           name,
		Description:    description,
		InteractionIDs: []string{},
		CreatedAt:      time.Now(),
	}
	testCases[id] = tc
	return tc
}

func GetAllTestCases() []*TestCase {
	mu.RLock()
	defer mu.RUnlock()
	out := make([]*TestCase, 0, len(testCases))
	for _, tc := range testCases {
		out = append(out, tc)
	}
	return out
}

func GetTestCase(id string) (*TestCase, bool) {
	mu.RLock()
	defer mu.RUnlock()
	tc, ok := testCases[id]
	return tc, ok
}

func UpdateTestCase(id, name, description string, interactionIDs []string) error {
	mu.Lock()
	defer mu.Unlock()
	tc, ok := testCases[id]
	if !ok {
		return fmt.Errorf("test case %s not found", id)
	}
	if name != "" {
		tc.Name = name
	}
	tc.Description = description
	if interactionIDs != nil {
		tc.InteractionIDs = interactionIDs
		// Sync TestCaseID on interactions
		for _, i := range interactions {
			i.TestCaseID = ""
		}
		for _, iid := range interactionIDs {
			if i, ok := interactions[iid]; ok {
				i.TestCaseID = id
			}
		}
	}
	return nil
}

func DeleteTestCase(id string) error {
	mu.Lock()
	defer mu.Unlock()
	tc, ok := testCases[id]
	if !ok {
		return fmt.Errorf("test case %s not found", id)
	}
	for _, iid := range tc.InteractionIDs {
		if i, ok := interactions[iid]; ok {
			i.TestCaseID = ""
		}
	}
	delete(testCases, id)
	return nil
}

// ---- Schema helpers ------------------------------------------------------

func schemaKey(protocol, tableName string) string {
	return protocol + ":" + tableName
}

func UpsertSchema(protocol, tableName, createStatement string) {
	mu.Lock()
	defer mu.Unlock()
	schemas[schemaKey(protocol, tableName)] = &Schema{
		TableName:       tableName,
		Protocol:        protocol,
		CreateStatement: createStatement,
	}
}

func GetSchema(protocol, tableName string) (*Schema, bool) {
	mu.RLock()
	defer mu.RUnlock()
	s, ok := schemas[schemaKey(protocol, tableName)]
	return s, ok
}

func GetAllSchemas() []*Schema {
	mu.RLock()
	defer mu.RUnlock()
	out := make([]*Schema, 0, len(schemas))
	for _, s := range schemas {
		out = append(out, s)
	}
	return out
}

// ---- Persistence ---------------------------------------------------------

const StateFileName = "veritaserum.json"

type stateFile struct {
	Interactions map[string]*Interaction `json:"interactions"`
	TestCases    map[string]*TestCase    `json:"testCases"`
	Schemas      map[string]*Schema      `json:"schemas"`
}

func LoadState() {
	data, err := os.ReadFile(StateFileName)
	if err != nil {
		return
	}
	var sf stateFile
	if err := json.Unmarshal(data, &sf); err != nil {
		log.Printf("warn: could not parse %s: %v", StateFileName, err)
		return
	}
	mu.Lock()
	defer mu.Unlock()
	if sf.Interactions != nil {
		interactions = sf.Interactions
	}
	if sf.TestCases != nil {
		testCases = sf.TestCases
	}
	if sf.Schemas != nil {
		schemas = sf.Schemas
	}
}

func SaveState() error {
	mu.RLock()
	sf := stateFile{
		Interactions: interactions,
		TestCases:    testCases,
		Schemas:      schemas,
	}
	data, err := json.MarshalIndent(sf, "", "  ")
	mu.RUnlock()
	if err != nil {
		return err
	}
	return os.WriteFile(StateFileName, data, 0644)
}

// LoadSuite loads interactions from a suite JSON file (--replay mode).
// It only loads configured interactions; pending ones are ignored.
func LoadSuite(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read suite: %w", err)
	}
	var suite struct {
		TestCase     string         `json:"testCase"`
		Interactions []*Interaction `json:"interactions"`
	}
	if err := json.Unmarshal(data, &suite); err != nil {
		return fmt.Errorf("parse suite: %w", err)
	}
	mu.Lock()
	defer mu.Unlock()
	for _, i := range suite.Interactions {
		if i.State == StateConfigured {
			interactions[i.ID] = i
		}
	}
	return nil
}
```

**Step 2: Verify compilation**

```bash
cd /path/to/veritaserum && go vet ./src/store/...
```

Expected: no errors (other packages will fail until updated — that is fine).

**Step 3: Commit**

```bash
git add src/store/store.go
git commit -m "feat: rewrite store with Interaction/TestCase/Schema model"
```

---

## Task 2: Update the HTTP proxy

**Files:**
- Rewrite: `src/http/proxy.go`

The proxy must now:
1. Build an `HTTPKey` from method + host + path + bodyHash
2. Detect DynamoDB calls (host contains `.dynamodb.`) and set protocol to `DYNAMODB`
3. Call `store.LookupConfigured` / `store.RegisterInteraction`

**Step 1: Replace `src/http/proxy.go`**

```go
package proxy

import (
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"veritaserum/src/store"
)

// isDynamoDB returns true when the host looks like an AWS DynamoDB endpoint.
func isDynamoDB(host string) bool {
	return strings.Contains(host, ".dynamodb.")
}

// parseDynamoDB extracts the DynamoDB operation and table name from the
// X-Amz-Target header (format: "DynamoDB_20120810.GetItem").
func parseDynamoDB(r *http.Request, body []byte) (operation, table string) {
	target := r.Header.Get("X-Amz-Target")
	if idx := strings.Index(target, "."); idx != -1 {
		operation = target[idx+1:]
	}
	// Table name lives in the JSON body under "TableName"
	// Simple extraction without full JSON parse to stay lightweight
	if i := strings.Index(string(body), `"TableName"`); i != -1 {
		rest := string(body)[i+11:]
		rest = strings.TrimSpace(rest)
		rest = strings.TrimPrefix(rest, ":")
		rest = strings.TrimSpace(rest)
		rest = strings.TrimPrefix(rest, `"`)
		if end := strings.IndexByte(rest, '"'); end != -1 {
			table = rest[:end]
		}
	}
	return
}

func Handler(w http.ResponseWriter, r *http.Request) {
	targetURL := r.RequestURI
	if targetURL == "" || targetURL == "/" {
		http.Error(w, "bad request: missing absolute URI", http.StatusBadRequest)
		return
	}

	parsed, err := url.Parse(targetURL)
	if err != nil {
		http.Error(w, "bad request: invalid URI", http.StatusBadRequest)
		return
	}

	host := parsed.Host
	path := parsed.Path
	if path == "" {
		path = "/"
	}

	rawBody, _ := io.ReadAll(r.Body)
	bodyHash := store.BodyHash(rawBody)

	protocol := store.ProtoHTTP
	var req store.InteractionRequest

	if isDynamoDB(host) {
		protocol = store.ProtoDynamoDB
		op, table := parseDynamoDB(r, rawBody)
		req = store.InteractionRequest{
			Method:    r.Method,
			Host:      host,
			Path:      path,
			BodyHash:  bodyHash,
			Body:      string(rawBody),
			Operation: op,
			Table:     table,
		}
	} else {
		req = store.InteractionRequest{
			Method:   r.Method,
			Host:     host,
			Path:     path,
			BodyHash: bodyHash,
			Body:     string(rawBody),
		}
	}

	key := store.HTTPKey(r.Method, host, path, bodyHash)

	if i := store.LookupConfigured(protocol, key); i != nil {
		if i.Response.LatencyMs > 0 {
			time.Sleep(time.Duration(i.Response.LatencyMs) * time.Millisecond)
		}
		for k, v := range i.Response.Headers {
			w.Header().Set(k, v)
		}
		if w.Header().Get("Content-Type") == "" {
			w.Header().Set("Content-Type", "application/json")
		}
		w.WriteHeader(i.Response.StatusCode)
		io.WriteString(w, i.Response.Body)
		log.Printf("PLAYBACK  %s %s  →  %d", r.Method, targetURL, i.Response.StatusCode)
		return
	}

	if store.IsPending(protocol, key) {
		http.Error(w, "veritaserum: mock pending configuration", http.StatusServiceUnavailable)
		log.Printf("PENDING   %s %s", r.Method, targetURL)
		return
	}

	// New capture
	store.RegisterInteraction(protocol, key, req)
	http.Error(w, "veritaserum: intercepted, configure mock in UI", http.StatusServiceUnavailable)
	log.Printf("INTERCEPT %s %s → registered as pending", r.Method, targetURL)
}
```

**Step 2: Verify**

```bash
go vet ./src/http/...
```

**Step 3: Commit**

```bash
git add src/http/proxy.go
git commit -m "feat: update HTTP proxy to use new Interaction store, add DynamoDB detection"
```

---

## Task 3: Update MySQL mock

**Files:**
- Modify: `src/dbs/mysql.go`

Replace the two `store.Mocks` / `store.MocksMu` call sites in `handleMySQLQuery` with the new store API. Everything else (wire protocol) stays untouched.

**Step 1: Replace `handleMySQLQuery` in `src/dbs/mysql.go`**

Find the existing function and replace it:

```go
func handleMySQLQuery(mc *mysqlConn, sql string) {
	key := store.DBKey(store.ProtoMySQL, sql)

	if i := store.LookupConfigured(store.ProtoMySQL, key); i != nil {
		log.Printf("MYSQL PLAYBACK: %s", sql)
		// Build JSON from rows for the existing wire encoder
		rowsJSON := "[]"
		if i.Response != nil && len(i.Response.Rows) > 0 {
			if b, err := json.Marshal(i.Response.Rows); err == nil {
				rowsJSON = string(b)
			}
		}
		if err := sendResultSet(mc, rowsJSON); err != nil {
			log.Printf("mysql: sendResultSet error: %v", err)
		}
		return
	}

	if !store.IsPending(store.ProtoMySQL, key) {
		req := store.InteractionRequest{Query: sql}
		store.RegisterInteraction(store.ProtoMySQL, key, req)
		log.Printf("MYSQL INTERCEPT: %s → registered as pending", sql)
	}

	sendOK(mc)
}
```

Also add `"encoding/json"` to the import block if not already present (it is).

**Step 2: Verify**

```bash
go vet ./src/dbs/...
```

**Step 3: Commit**

```bash
git add src/dbs/mysql.go
git commit -m "feat: update MySQL mock to use new Interaction store"
```

---

## Task 4: Update Postgres mock

**Files:**
- Modify: `src/dbs/postgres.go`

Same change as Task 3 but for `handlePostgresQuery`.

**Step 1: Replace `handlePostgresQuery` in `src/dbs/postgres.go`**

```go
func handlePostgresQuery(conn net.Conn, sql string) {
	key := store.DBKey(store.ProtoPostgres, sql)

	if i := store.LookupConfigured(store.ProtoPostgres, key); i != nil {
		log.Printf("POSTGRES PLAYBACK: %s", sql)
		rowsJSON := "[]"
		if i.Response != nil && len(i.Response.Rows) > 0 {
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
```

Add `"encoding/json"` to the import block in `postgres.go`.

**Step 2: Verify**

```bash
go vet ./src/dbs/...
```

**Step 3: Commit**

```bash
git add src/dbs/postgres.go
git commit -m "feat: update Postgres mock to use new Interaction store"
```

---

## Task 5: Add Redis wire mock

**Files:**
- Create: `src/dbs/redis.go`

Implement a RESP (Redis Serialisation Protocol) server. Supports GET, SET, HGET, HSET, LPUSH, LRANGE, DEL, EXISTS, PING. Unknown commands return an empty null bulk string. Non-configured keys go to pending.

**Step 1: Create `src/dbs/redis.go`**

```go
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
		protocol := store.ProtoRedis

		if i := store.LookupConfigured(protocol, key); i != nil {
			log.Printf("REDIS PLAYBACK: %s", key)
			writeBulkString(conn, i.Response.Value)
			continue
		}

		if !store.IsPending(protocol, key) {
			req := store.InteractionRequest{
				Command: cmd,
				Args:    args[1:],
			}
			store.RegisterInteraction(protocol, key, req)
			log.Printf("REDIS INTERCEPT: %s → registered as pending", key)
		}

		// Return null bulk string so the client doesn't crash
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
		// Array: *<count>\r\n
		count, err := strconv.Atoi(line[1:])
		if err != nil || count <= 0 {
			return nil, fmt.Errorf("invalid array count: %s", line)
		}
		args := make([]string, 0, count)
		for i := 0; i < count; i++ {
			// Bulk string: $<len>\r\n<data>\r\n
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
		// Inline command (some clients send these)
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
```

**Step 2: Verify**

```bash
go vet ./src/dbs/...
```

**Step 3: Commit**

```bash
git add src/dbs/redis.go
git commit -m "feat: add Redis RESP wire mock on :6380"
```

---

## Task 6: Rewrite the REST API

**Files:**
- Rewrite: `src/messaging/api.go`

Remove all old endpoints. Implement the new API. The MySQL provisioner (`/api/databases`) is dropped for now — it can be re-added later. Static file serving switches from `ui/index.html` to `dist/`.

**Step 1: Replace `src/messaging/api.go`**

```go
package messaging

import (
	"encoding/json"
	"io"
	"io/fs"
	"log"
	"net/http"
	"os"

	"github.com/gin-gonic/gin"
	"veritaserum/src/store"
)

func StartAPIServer(port string, staticFiles fs.FS) {
	r := gin.Default()

	// Serve embedded React build
	r.GET("/", func(c *gin.Context) {
		f, err := staticFiles.Open("dist/index.html")
		if err != nil {
			c.Status(http.StatusNotFound)
			return
		}
		defer f.Close()
		c.DataFromReader(http.StatusOK, -1, "text/html; charset=utf-8", f, nil)
	})
	r.GET("/assets/*filepath", func(c *gin.Context) {
		fp := "dist/assets" + c.Param("filepath")
		f, err := staticFiles.Open(fp)
		if err != nil {
			c.Status(http.StatusNotFound)
			return
		}
		defer f.Close()
		http.ServeContent(c.Writer, c.Request, fp, zeroTime, f.(io.ReadSeeker))
	})

	// ---- Interactions --------------------------------------------------------

	r.GET("/api/interactions", func(c *gin.Context) {
		c.JSON(http.StatusOK, store.GetAllInteractions())
	})

	r.GET("/api/interactions/pending", func(c *gin.Context) {
		c.JSON(http.StatusOK, store.GetPendingInteractions())
	})

	r.POST("/api/interactions/:id/configure", func(c *gin.Context) {
		id := c.Param("id")
		var req struct {
			Name     string                  `json:"name"`
			Response store.InteractionResponse `json:"response"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		if err := store.ConfigureInteraction(id, req.Name, req.Response); err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}
		c.Status(http.StatusNoContent)
	})

	// ---- Test Cases ----------------------------------------------------------

	r.GET("/api/testcases", func(c *gin.Context) {
		c.JSON(http.StatusOK, store.GetAllTestCases())
	})

	r.POST("/api/testcases", func(c *gin.Context) {
		var req struct {
			Name        string `json:"name"`
			Description string `json:"description"`
		}
		if err := c.ShouldBindJSON(&req); err != nil || req.Name == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "name is required"})
			return
		}
		tc := store.CreateTestCase(req.Name, req.Description)
		c.JSON(http.StatusCreated, tc)
	})

	r.PUT("/api/testcases/:id", func(c *gin.Context) {
		id := c.Param("id")
		var req struct {
			Name           string   `json:"name"`
			Description    string   `json:"description"`
			InteractionIDs []string `json:"interactionIds"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		if err := store.UpdateTestCase(id, req.Name, req.Description, req.InteractionIDs); err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}
		c.Status(http.StatusNoContent)
	})

	r.DELETE("/api/testcases/:id", func(c *gin.Context) {
		if err := store.DeleteTestCase(c.Param("id")); err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}
		c.Status(http.StatusNoContent)
	})

	r.GET("/api/testcases/:id/export", func(c *gin.Context) {
		tc, ok := store.GetTestCase(c.Param("id"))
		if !ok {
			c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
			return
		}
		all := store.GetAllInteractions()
		idSet := map[string]bool{}
		for _, id := range tc.InteractionIDs {
			idSet[id] = true
		}
		kept := make([]*store.Interaction, 0)
		for _, i := range all {
			if idSet[i.ID] {
				kept = append(kept, i)
			}
		}
		payload := map[string]interface{}{
			"version":      "2",
			"testCase":     tc.Name,
			"interactions": kept,
		}
		c.Header("Content-Disposition", "attachment; filename=\""+tc.Name+".json\"")
		c.JSON(http.StatusOK, payload)
	})

	// ---- Import --------------------------------------------------------------

	r.POST("/api/import", func(c *gin.Context) {
		body, err := io.ReadAll(c.Request.Body)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		var suite struct {
			TestCase     string              `json:"testCase"`
			Interactions []*store.Interaction `json:"interactions"`
		}
		if err := json.Unmarshal(body, &suite); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		tc := store.CreateTestCase(suite.TestCase, "imported")
		ids := make([]string, 0)
		for _, i := range suite.Interactions {
			if i.State == store.StateConfigured {
				existing := store.RegisterInteraction(i.Protocol, i.Key, i.Request)
				if i.Response != nil {
					store.ConfigureInteraction(existing.ID, i.Name, *i.Response)
				}
				ids = append(ids, existing.ID)
			}
		}
		store.UpdateTestCase(tc.ID, tc.Name, tc.Description, ids)
		c.JSON(http.StatusCreated, tc)
	})

	// ---- Schemas -------------------------------------------------------------

	r.GET("/api/schemas", func(c *gin.Context) {
		c.JSON(http.StatusOK, store.GetAllSchemas())
	})

	r.POST("/api/schemas", func(c *gin.Context) {
		var req struct {
			Protocol        string `json:"protocol"`
			TableName       string `json:"tableName"`
			CreateStatement string `json:"createStatement"`
		}
		if err := c.ShouldBindJSON(&req); err != nil || req.TableName == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "protocol and tableName are required"})
			return
		}
		store.UpsertSchema(req.Protocol, req.TableName, req.CreateStatement)
		c.Status(http.StatusNoContent)
	})

	// ---- Persist -------------------------------------------------------------

	r.POST("/api/state/save", func(c *gin.Context) {
		if err := store.SaveState(); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		log.Printf("STATE saved to %s", store.StateFileName)
		c.Status(http.StatusNoContent)
	})

	// ---- Health (used in CLI replay mode) ------------------------------------

	r.GET("/healthz", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	log.Printf("API/UI listening on :%s  →  http://localhost:%s/", port, port)
	if err := r.Run(":" + port); err != nil {
		log.Fatalf("api: %v", err)
	}
}

// zeroTime is used as the modtime for static assets (no caching needed for a dev tool).
var zeroTime = struct{ Read func([]byte) (int, error) }{}
```

> Note: `zeroTime` trick won't compile. Replace the `http.ServeContent` call with a simpler approach — just stream the file directly using `c.DataFromReader`. Fix:

```go
r.GET("/assets/*filepath", func(c *gin.Context) {
    fp := "dist/assets" + c.Param("filepath")
    f, err := staticFiles.Open(fp)
    if err != nil {
        c.Status(http.StatusNotFound)
        return
    }
    defer f.Close()
    // Detect content type from extension
    ct := "application/octet-stream"
    if strings.HasSuffix(fp, ".js") {
        ct = "application/javascript"
    } else if strings.HasSuffix(fp, ".css") {
        ct = "text/css"
    }
    c.DataFromReader(http.StatusOK, -1, ct, f, nil)
})
```

Add `"strings"` to imports.

**Step 2: Verify**

```bash
go vet ./src/messaging/...
```

**Step 3: Commit**

```bash
git add src/messaging/api.go
git commit -m "feat: rewrite REST API for new Interaction/TestCase/Schema endpoints"
```

---

## Task 7: Update main.go

**Files:**
- Rewrite: `main.go`

Add `--replay` and `--suite` flags. Start the Redis server. Switch embed path from `ui` to `dist`.

**Step 1: Replace `main.go`**

```go
package main

import (
	"embed"
	"flag"
	"log"
	"net/http"
	"time"

	proxy "veritaserum/src/http"
	"veritaserum/src/dbs"
	"veritaserum/src/messaging"
	"veritaserum/src/store"
)

//go:embed dist
var distFiles embed.FS

func main() {
	replay  := flag.Bool("replay", false, "headless replay mode — no UI, loads suite JSON")
	suite   := flag.String("suite", "", "path to suite JSON file (required with --replay)")
	timeout := flag.Duration("timeout", 0, "auto-exit after duration, e.g. 120s (replay mode only)")
	flag.Parse()

	if *replay {
		if *suite == "" {
			log.Fatal("--suite is required in --replay mode")
		}
		if err := store.LoadSuite(*suite); err != nil {
			log.Fatalf("load suite: %v", err)
		}
		log.Printf("Replay mode: loaded suite %s", *suite)
	} else {
		store.LoadState()
	}

	go func() {
		log.Println("Proxy      listening on :9999")
		if err := http.ListenAndServe(":9999", http.HandlerFunc(proxy.Handler)); err != nil {
			log.Fatalf("proxy: %v", err)
		}
	}()

	go dbs.StartPostgresMock("54320")
	go dbs.StartMySQLMock("33060")
	go dbs.StartRedisMock("6380")

	if *replay && *timeout > 0 {
		go func() {
			time.Sleep(*timeout)
			log.Printf("Replay timeout (%s) reached, exiting", *timeout)
			// os.Exit is fine here — all goroutines are daemon-like
			log.Fatal("timeout")
		}()
	}

	messaging.StartAPIServer("8080", distFiles) // blocks
}
```

**Step 2: Verify (will fail until dist/ exists — that is expected)**

```bash
go vet ./...
```

The embed will fail with "pattern dist: directory prefix dist does not exist". This is resolved in Task 8 when we scaffold the frontend.

**Step 3: Commit**

```bash
git add main.go
git commit -m "feat: add --replay/--suite/--timeout CLI flags, start Redis mock, switch embed to dist/"
```

---

## Task 8: Scaffold the React+Bun+TypeScript frontend

**Files:**
- Create: `ui/package.json`
- Create: `ui/tsconfig.json`
- Create: `ui/index.html`
- Create: `ui/src/main.tsx`
- Create: `ui/src/App.tsx`

**Step 1: Create `ui/package.json`**

```json
{
  "name": "veritaserum-ui",
  "private": true,
  "scripts": {
    "dev": "vite",
    "build": "vite build --outDir ../dist --emptyOutDir"
  },
  "dependencies": {
    "react": "^18.3.1",
    "react-dom": "^18.3.1"
  },
  "devDependencies": {
    "@types/react": "^18.3.1",
    "@types/react-dom": "^18.3.1",
    "@vitejs/plugin-react": "^4.3.1",
    "typescript": "^5.5.3",
    "vite": "^5.4.1"
  }
}
```

**Step 2: Create `ui/tsconfig.json`**

```json
{
  "compilerOptions": {
    "target": "ES2020",
    "useDefineForClassFields": true,
    "lib": ["ES2020", "DOM", "DOM.Iterable"],
    "module": "ESNext",
    "skipLibCheck": true,
    "moduleResolution": "bundler",
    "allowImportingTsExtensions": true,
    "isolatedModules": true,
    "moduleDetection": "force",
    "noEmit": true,
    "jsx": "react-jsx",
    "strict": true
  },
  "include": ["src"]
}
```

**Step 3: Create `ui/vite.config.ts`**

```ts
import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

export default defineConfig({
  plugins: [react()],
  build: {
    outDir: '../dist',
    emptyOutDir: true,
  },
})
```

**Step 4: Create `ui/index.html`**

```html
<!doctype html>
<html lang="en">
  <head>
    <meta charset="UTF-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1.0" />
    <title>Veritaserum</title>
    <style>
      * { box-sizing: border-box; margin: 0; padding: 0; }
      body { font-family: system-ui, sans-serif; background: #0f172a; color: #e2e8f0; }
    </style>
  </head>
  <body>
    <div id="root"></div>
    <script type="module" src="/src/main.tsx"></script>
  </body>
</html>
```

**Step 5: Create `ui/src/main.tsx`**

```tsx
import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import App from './App'

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <App />
  </StrictMode>
)
```

**Step 6: Create `ui/src/App.tsx`** (tab shell, placeholder tabs)

```tsx
import { useState } from 'react'

type Tab = 'pending' | 'mocks' | 'testcases' | 'import'

export default function App() {
  const [tab, setTab] = useState<Tab>('pending')

  return (
    <div style={{ minHeight: '100vh', display: 'flex', flexDirection: 'column' }}>
      <header style={{ background: '#1e293b', padding: '12px 24px', display: 'flex', gap: 24, alignItems: 'center' }}>
        <span style={{ fontWeight: 700, fontSize: 18, color: '#7c3aed' }}>⚗ Veritaserum</span>
        {(['pending', 'mocks', 'testcases', 'import'] as Tab[]).map(t => (
          <button
            key={t}
            onClick={() => setTab(t)}
            style={{
              background: 'none', border: 'none', cursor: 'pointer',
              color: tab === t ? '#a78bfa' : '#94a3b8',
              fontWeight: tab === t ? 700 : 400,
              fontSize: 14, padding: '4px 8px',
              borderBottom: tab === t ? '2px solid #7c3aed' : '2px solid transparent',
            }}
          >
            {t === 'testcases' ? 'Test Cases' : t.charAt(0).toUpperCase() + t.slice(1)}
          </button>
        ))}
      </header>
      <main style={{ flex: 1, padding: 24 }}>
        {tab === 'pending' && <div>Pending tab — coming next</div>}
        {tab === 'mocks' && <div>Mocks tab — coming next</div>}
        {tab === 'testcases' && <div>Test Cases tab — coming next</div>}
        {tab === 'import' && <div>Import tab — coming next</div>}
      </main>
    </div>
  )
}
```

**Step 7: Install dependencies and do a first build**

```bash
cd ui
bun install
bun run build
```

Expected: `dist/` directory created at the repo root with `index.html` and `assets/`.

**Step 8: Verify Go compiles with the embed**

```bash
cd ..
go build ./...
```

Expected: binary builds cleanly.

**Step 9: Add .gitignore entry for dist**

Append to `.gitignore` (create it if it doesn't exist):

```
dist/
ui/node_modules/
```

**Step 10: Commit**

```bash
git add ui/ dist/ .gitignore
git commit -m "feat: scaffold React+Bun+TypeScript frontend, wire Go embed to dist/"
```

---

## Task 9: Shared types and API client

**Files:**
- Create: `ui/src/types/index.ts`
- Create: `ui/src/api/client.ts`

**Step 1: Create `ui/src/types/index.ts`**

```ts
export type Protocol = 'HTTP' | 'MYSQL' | 'POSTGRES' | 'REDIS' | 'DYNAMODB'
export type InteractionState = 'pending' | 'configured'

export interface InteractionRequest {
  method?: string
  host?: string
  path?: string
  headers?: Record<string, string>
  body?: string
  bodyHash?: string
  // DynamoDB
  operation?: string
  table?: string
  keyJSON?: string
  // DB
  query?: string
  // Redis
  command?: string
  args?: string[]
}

export interface InteractionResponse {
  // HTTP
  statusCode?: number
  headers?: Record<string, string>
  body?: string
  latencyMs?: number
  // DB
  rows?: Record<string, unknown>[]
  affectedRows?: number
  // DynamoDB
  itemJSON?: string
  // Redis
  value?: string
}

export interface Interaction {
  id: string
  protocol: Protocol
  key: string
  name: string
  request: InteractionRequest
  response?: InteractionResponse
  state: InteractionState
  testCaseId: string
  capturedAt: string
}

export interface TestCase {
  id: string
  name: string
  description?: string
  interactionIds: string[]
  createdAt: string
}

export interface Schema {
  tableName: string
  protocol: 'MYSQL' | 'POSTGRES'
  createStatement: string
}
```

**Step 2: Create `ui/src/api/client.ts`**

```ts
import type { Interaction, InteractionResponse, TestCase, Schema } from '../types'

const BASE = ''

async function json<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(BASE + path, init)
  if (!res.ok) throw new Error(`${res.status} ${res.statusText}`)
  if (res.status === 204) return undefined as T
  return res.json()
}

export const api = {
  interactions: {
    all: () => json<Interaction[]>('/api/interactions'),
    pending: () => json<Interaction[]>('/api/interactions/pending'),
    configure: (id: string, name: string, response: InteractionResponse) =>
      json<void>(`/api/interactions/${id}/configure`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ name, response }),
      }),
  },
  testcases: {
    all: () => json<TestCase[]>('/api/testcases'),
    create: (name: string, description?: string) =>
      json<TestCase>('/api/testcases', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ name, description }),
      }),
    update: (id: string, payload: { name?: string; description?: string; interactionIds?: string[] }) =>
      json<void>(`/api/testcases/${id}`, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(payload),
      }),
    delete: (id: string) => json<void>(`/api/testcases/${id}`, { method: 'DELETE' }),
    exportUrl: (id: string) => `/api/testcases/${id}/export`,
  },
  schemas: {
    all: () => json<Schema[]>('/api/schemas'),
    upsert: (protocol: string, tableName: string, createStatement: string) =>
      json<void>('/api/schemas', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ protocol, tableName, createStatement }),
      }),
  },
  import: (file: string) =>
    json<TestCase>('/api/import', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: file,
    }),
  saveState: () => json<void>('/api/state/save', { method: 'POST' }),
}
```

**Step 3: Build to verify types**

```bash
cd ui && bun run build
```

**Step 4: Commit**

```bash
git add ui/src/types/ ui/src/api/
git commit -m "feat: add shared TypeScript types and typed API client"
```

---

## Task 10: Protocol-specific forms

**Files:**
- Create: `ui/src/forms/HttpForm.tsx`
- Create: `ui/src/forms/MySqlForm.tsx`
- Create: `ui/src/forms/PostgresForm.tsx`
- Create: `ui/src/forms/DynamoDbForm.tsx`
- Create: `ui/src/forms/RedisForm.tsx`

Each form receives an `Interaction` and an `onSave(name, response)` callback.

**Step 1: Create `ui/src/forms/HttpForm.tsx`**

```tsx
import { useState } from 'react'
import type { Interaction, InteractionResponse } from '../types'

interface Props {
  interaction: Interaction
  onSave: (name: string, response: InteractionResponse) => void
}

export default function HttpForm({ interaction: i, onSave }: Props) {
  const [name, setName]           = useState(i.name || '')
  const [statusCode, setStatus]   = useState(i.response?.statusCode ?? 200)
  const [latencyMs, setLatency]   = useState(i.response?.latencyMs ?? 0)
  const [body, setBody]           = useState(i.response?.body ?? '')
  const [headersRaw, setHeaders]  = useState(
    i.response?.headers ? JSON.stringify(i.response.headers, null, 2) : '{}'
  )

  function handleSave() {
    let headers: Record<string, string> = {}
    try { headers = JSON.parse(headersRaw) } catch { /* ignore */ }
    onSave(name, { statusCode, latencyMs, body, headers })
  }

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
      <label>
        <small style={{ color: '#94a3b8' }}>Endpoint</small>
        <div style={{ fontFamily: 'monospace', fontSize: 13, color: '#7c3aed' }}>
          {i.request.method} {i.request.host}{i.request.path}
        </div>
      </label>
      <Field label="Name / label" value={name} onChange={setName} />
      <Row>
        <NumberField label="Status code" value={statusCode} onChange={setStatus} />
        <NumberField label="Latency (ms)" value={latencyMs} onChange={setLatency} />
      </Row>
      <TextArea label="Response headers (JSON)" value={headersRaw} onChange={setHeaders} rows={3} />
      <TextArea label="Response body" value={body} onChange={setBody} rows={8} />
      <SaveButton onClick={handleSave} />
    </div>
  )
}

// ---- Shared small components (inlined to keep files self-contained) -----

function Field({ label, value, onChange }: { label: string; value: string; onChange: (v: string) => void }) {
  return (
    <label style={{ display: 'flex', flexDirection: 'column', gap: 4 }}>
      <small style={{ color: '#94a3b8' }}>{label}</small>
      <input
        value={value} onChange={e => onChange(e.target.value)}
        style={{ background: '#1e293b', border: '1px solid #334155', color: '#e2e8f0', padding: '6px 8px', borderRadius: 4 }}
      />
    </label>
  )
}

function NumberField({ label, value, onChange }: { label: string; value: number; onChange: (v: number) => void }) {
  return (
    <label style={{ display: 'flex', flexDirection: 'column', gap: 4, flex: 1 }}>
      <small style={{ color: '#94a3b8' }}>{label}</small>
      <input
        type="number" value={value} onChange={e => onChange(Number(e.target.value))}
        style={{ background: '#1e293b', border: '1px solid #334155', color: '#e2e8f0', padding: '6px 8px', borderRadius: 4 }}
      />
    </label>
  )
}

function TextArea({ label, value, onChange, rows }: { label: string; value: string; onChange: (v: string) => void; rows: number }) {
  return (
    <label style={{ display: 'flex', flexDirection: 'column', gap: 4 }}>
      <small style={{ color: '#94a3b8' }}>{label}</small>
      <textarea
        rows={rows} value={value} onChange={e => onChange(e.target.value)}
        style={{ background: '#1e293b', border: '1px solid #334155', color: '#e2e8f0', padding: '6px 8px', borderRadius: 4, fontFamily: 'monospace', fontSize: 12, resize: 'vertical' }}
      />
    </label>
  )
}

function Row({ children }: { children: React.ReactNode }) {
  return <div style={{ display: 'flex', gap: 12 }}>{children}</div>
}

function SaveButton({ onClick }: { onClick: () => void }) {
  return (
    <button
      onClick={onClick}
      style={{ background: '#7c3aed', color: '#fff', border: 'none', padding: '8px 16px', borderRadius: 4, cursor: 'pointer', fontWeight: 600, alignSelf: 'flex-start' }}
    >
      Save mock
    </button>
  )
}
```

**Step 2: Create `ui/src/forms/MySqlForm.tsx`**

```tsx
import { useState } from 'react'
import type { Interaction, InteractionResponse } from '../types'

interface Props {
  interaction: Interaction
  existingSchema?: string
  onSave: (name: string, response: InteractionResponse) => void
  onSaveSchema: (tableName: string, createStatement: string) => void
}

export default function MySqlForm({ interaction: i, existingSchema, onSave, onSaveSchema }: Props) {
  const isSelect = i.request.query?.trim().toUpperCase().startsWith('SELECT')
  const [name, setName]             = useState(i.name || '')
  const [rows, setRows]             = useState(i.response?.rows ? JSON.stringify(i.response.rows, null, 2) : '[]')
  const [affectedRows, setAffected] = useState(i.response?.affectedRows ?? 1)
  const [schema, setSchema]         = useState(existingSchema ?? '')
  const [tableName, setTableName]   = useState('')
  const needsSchema = !existingSchema

  function handleSave() {
    if (needsSchema && tableName && schema) {
      onSaveSchema(tableName, schema)
    }
    const response: InteractionResponse = isSelect
      ? { rows: JSON.parse(rows) }
      : { affectedRows }
    onSave(name, response)
  }

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
      <ReadOnly label="SQL query" value={i.request.query ?? ''} />
      {needsSchema && (
        <>
          <Field label="Table name" value={tableName} onChange={setTableName} />
          <TextArea label="CREATE TABLE statement (for schema reference)" value={schema} onChange={setSchema} rows={4} />
        </>
      )}
      <Field label="Name / label" value={name} onChange={setName} />
      {isSelect
        ? <TextArea label="Rows to return (JSON array)" value={rows} onChange={setRows} rows={8} />
        : <NumberField label="Affected rows" value={affectedRows} onChange={setAffected} />
      }
      <SaveButton onClick={handleSave} />
    </div>
  )
}

function ReadOnly({ label, value }: { label: string; value: string }) {
  return (
    <label style={{ display: 'flex', flexDirection: 'column', gap: 4 }}>
      <small style={{ color: '#94a3b8' }}>{label}</small>
      <div style={{ background: '#0f172a', border: '1px solid #1e293b', color: '#7c3aed', padding: '6px 8px', borderRadius: 4, fontFamily: 'monospace', fontSize: 12 }}>{value}</div>
    </label>
  )
}
function Field({ label, value, onChange }: { label: string; value: string; onChange: (v: string) => void }) {
  return (
    <label style={{ display: 'flex', flexDirection: 'column', gap: 4 }}>
      <small style={{ color: '#94a3b8' }}>{label}</small>
      <input value={value} onChange={e => onChange(e.target.value)} style={{ background: '#1e293b', border: '1px solid #334155', color: '#e2e8f0', padding: '6px 8px', borderRadius: 4 }} />
    </label>
  )
}
function NumberField({ label, value, onChange }: { label: string; value: number; onChange: (v: number) => void }) {
  return (
    <label style={{ display: 'flex', flexDirection: 'column', gap: 4 }}>
      <small style={{ color: '#94a3b8' }}>{label}</small>
      <input type="number" value={value} onChange={e => onChange(Number(e.target.value))} style={{ background: '#1e293b', border: '1px solid #334155', color: '#e2e8f0', padding: '6px 8px', borderRadius: 4 }} />
    </label>
  )
}
function TextArea({ label, value, onChange, rows }: { label: string; value: string; onChange: (v: string) => void; rows: number }) {
  return (
    <label style={{ display: 'flex', flexDirection: 'column', gap: 4 }}>
      <small style={{ color: '#94a3b8' }}>{label}</small>
      <textarea rows={rows} value={value} onChange={e => onChange(e.target.value)} style={{ background: '#1e293b', border: '1px solid #334155', color: '#e2e8f0', padding: '6px 8px', borderRadius: 4, fontFamily: 'monospace', fontSize: 12, resize: 'vertical' }} />
    </label>
  )
}
function SaveButton({ onClick }: { onClick: () => void }) {
  return <button onClick={onClick} style={{ background: '#7c3aed', color: '#fff', border: 'none', padding: '8px 16px', borderRadius: 4, cursor: 'pointer', fontWeight: 600, alignSelf: 'flex-start' }}>Save mock</button>
}
```

**Step 3: Create `ui/src/forms/PostgresForm.tsx`**

Identical to `MySqlForm.tsx` with the label text "POSTGRES" in the schema section. Copy the file and change "MySQL" to "Postgres" in labels — the logic is the same.

**Step 4: Create `ui/src/forms/DynamoDbForm.tsx`**

```tsx
import { useState } from 'react'
import type { Interaction, InteractionResponse } from '../types'

interface Props {
  interaction: Interaction
  onSave: (name: string, response: InteractionResponse) => void
}

export default function DynamoDbForm({ interaction: i, onSave }: Props) {
  const [name, setName]       = useState(i.name || '')
  const [itemJSON, setItem]   = useState(i.response?.itemJSON ?? '{}')

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
      <ReadOnly label="Operation" value={`${i.request.operation ?? 'Unknown'} on ${i.request.table ?? 'unknown table'}`} />
      <ReadOnly label="Key" value={i.request.keyJSON ?? ''} />
      <Field label="Name / label" value={name} onChange={setName} />
      <TextArea label="Item JSON to return" value={itemJSON} onChange={setItem} rows={10} />
      <button onClick={() => onSave(name, { itemJSON })} style={{ background: '#7c3aed', color: '#fff', border: 'none', padding: '8px 16px', borderRadius: 4, cursor: 'pointer', fontWeight: 600, alignSelf: 'flex-start' }}>Save mock</button>
    </div>
  )
}

function ReadOnly({ label, value }: { label: string; value: string }) {
  return <label style={{ display: 'flex', flexDirection: 'column', gap: 4 }}><small style={{ color: '#94a3b8' }}>{label}</small><div style={{ background: '#0f172a', border: '1px solid #1e293b', color: '#7c3aed', padding: '6px 8px', borderRadius: 4, fontFamily: 'monospace', fontSize: 12 }}>{value}</div></label>
}
function Field({ label, value, onChange }: { label: string; value: string; onChange: (v: string) => void }) {
  return <label style={{ display: 'flex', flexDirection: 'column', gap: 4 }}><small style={{ color: '#94a3b8' }}>{label}</small><input value={value} onChange={e => onChange(e.target.value)} style={{ background: '#1e293b', border: '1px solid #334155', color: '#e2e8f0', padding: '6px 8px', borderRadius: 4 }} /></label>
}
function TextArea({ label, value, onChange, rows }: { label: string; value: string; onChange: (v: string) => void; rows: number }) {
  return <label style={{ display: 'flex', flexDirection: 'column', gap: 4 }}><small style={{ color: '#94a3b8' }}>{label}</small><textarea rows={rows} value={value} onChange={e => onChange(e.target.value)} style={{ background: '#1e293b', border: '1px solid #334155', color: '#e2e8f0', padding: '6px 8px', borderRadius: 4, fontFamily: 'monospace', fontSize: 12, resize: 'vertical' }} /></label>
}
```

**Step 5: Create `ui/src/forms/RedisForm.tsx`**

```tsx
import { useState } from 'react'
import type { Interaction, InteractionResponse } from '../types'

interface Props {
  interaction: Interaction
  onSave: (name: string, response: InteractionResponse) => void
}

export default function RedisForm({ interaction: i, onSave }: Props) {
  const [name, setName]   = useState(i.name || '')
  const [value, setValue] = useState(i.response?.value ?? '')

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
      <label>
        <small style={{ color: '#94a3b8' }}>Command</small>
        <div style={{ fontFamily: 'monospace', fontSize: 13, color: '#7c3aed' }}>
          {i.request.command} {(i.request.args ?? []).join(' ')}
        </div>
      </label>
      <label style={{ display: 'flex', flexDirection: 'column', gap: 4 }}>
        <small style={{ color: '#94a3b8' }}>Name / label</small>
        <input value={name} onChange={e => setName(e.target.value)} style={{ background: '#1e293b', border: '1px solid #334155', color: '#e2e8f0', padding: '6px 8px', borderRadius: 4 }} />
      </label>
      <label style={{ display: 'flex', flexDirection: 'column', gap: 4 }}>
        <small style={{ color: '#94a3b8' }}>Return value</small>
        <textarea rows={4} value={value} onChange={e => setValue(e.target.value)} style={{ background: '#1e293b', border: '1px solid #334155', color: '#e2e8f0', padding: '6px 8px', borderRadius: 4, fontFamily: 'monospace', fontSize: 12, resize: 'vertical' }} />
      </label>
      <button onClick={() => onSave(name, { value })} style={{ background: '#7c3aed', color: '#fff', border: 'none', padding: '8px 16px', borderRadius: 4, cursor: 'pointer', fontWeight: 600, alignSelf: 'flex-start' }}>Save mock</button>
    </div>
  )
}
```

**Step 6: Build and verify**

```bash
cd ui && bun run build
```

**Step 7: Commit**

```bash
git add ui/src/forms/
git commit -m "feat: add protocol-specific form components (HTTP, MySQL, Postgres, DynamoDB, Redis)"
```

---

## Task 11: PendingTab component

**Files:**
- Create: `ui/src/components/PendingTab.tsx`

Polls `/api/interactions/pending` every 2 seconds. Shows a list on the left, selected interaction's form on the right.

**Step 1: Create `ui/src/components/PendingTab.tsx`**

```tsx
import { useEffect, useState } from 'react'
import { api } from '../api/client'
import type { Interaction, InteractionResponse, Schema } from '../types'
import HttpForm from '../forms/HttpForm'
import MySqlForm from '../forms/MySqlForm'
import PostgresForm from '../forms/PostgresForm'
import DynamoDbForm from '../forms/DynamoDbForm'
import RedisForm from '../forms/RedisForm'

const PROTOCOL_COLORS: Record<string, string> = {
  HTTP: '#0ea5e9', MYSQL: '#f59e0b', POSTGRES: '#6366f1',
  REDIS: '#ef4444', DYNAMODB: '#10b981',
}

export default function PendingTab() {
  const [interactions, setInteractions] = useState<Interaction[]>([])
  const [schemas, setSchemas]           = useState<Schema[]>([])
  const [selected, setSelected]         = useState<string | null>(null)

  useEffect(() => {
    const load = () => {
      api.interactions.pending().then(setInteractions).catch(() => {})
      api.schemas.all().then(setSchemas).catch(() => {})
    }
    load()
    const t = setInterval(load, 2000)
    return () => clearInterval(t)
  }, [])

  async function handleSave(id: string, name: string, response: InteractionResponse) {
    await api.interactions.configure(id, name, response)
    setInteractions(prev => prev.filter(i => i.id !== id))
    setSelected(null)
  }

  async function handleSaveSchema(tableName: string, createStatement: string, protocol: string) {
    await api.schemas.upsert(protocol, tableName, createStatement)
    const updated = await api.schemas.all()
    setSchemas(updated)
  }

  const active = interactions.find(i => i.id === selected)

  return (
    <div style={{ display: 'flex', height: 'calc(100vh - 80px)', gap: 0 }}>
      {/* Left: list */}
      <div style={{ width: 340, borderRight: '1px solid #1e293b', overflowY: 'auto' }}>
        {interactions.length === 0
          ? <div style={{ padding: 24, color: '#475569', fontSize: 13 }}>No pending interactions. Proxy some traffic to see captures here.</div>
          : interactions.map(i => (
            <div
              key={i.id}
              onClick={() => setSelected(i.id)}
              style={{
                padding: '12px 16px', cursor: 'pointer', borderBottom: '1px solid #1e293b',
                background: selected === i.id ? '#1e293b' : 'transparent',
              }}
            >
              <div style={{ display: 'flex', gap: 8, alignItems: 'center', marginBottom: 4 }}>
                <span style={{ background: PROTOCOL_COLORS[i.protocol] ?? '#64748b', color: '#fff', fontSize: 10, fontWeight: 700, padding: '2px 6px', borderRadius: 3 }}>{i.protocol}</span>
                {i.protocol === 'HTTP' || i.protocol === 'DYNAMODB'
                  ? <span style={{ fontSize: 12, fontFamily: 'monospace', color: '#94a3b8' }}>{i.request.method} {i.request.host}</span>
                  : <span style={{ fontSize: 12, fontFamily: 'monospace', color: '#94a3b8' }}>{i.request.query ?? i.request.command}</span>
                }
              </div>
              {(i.protocol === 'HTTP' || i.protocol === 'DYNAMODB') &&
                <div style={{ fontSize: 11, color: '#64748b', fontFamily: 'monospace' }}>{i.request.path}</div>
              }
            </div>
          ))
        }
      </div>

      {/* Right: form */}
      <div style={{ flex: 1, padding: 24, overflowY: 'auto' }}>
        {!active
          ? <div style={{ color: '#475569', fontSize: 13 }}>Select an interaction to configure its mock response.</div>
          : renderForm(active, schemas, handleSave, handleSaveSchema)
        }
      </div>
    </div>
  )
}

function renderForm(
  i: Interaction,
  schemas: Schema[],
  onSave: (id: string, name: string, resp: InteractionResponse) => void,
  onSaveSchema: (tableName: string, createStatement: string, protocol: string) => void
) {
  const save = (name: string, resp: InteractionResponse) => onSave(i.id, name, resp)

  switch (i.protocol) {
    case 'HTTP':
      return <HttpForm interaction={i} onSave={save} />
    case 'MYSQL': {
      const schema = schemas.find(s => s.protocol === 'MYSQL' && i.request.query?.includes(s.tableName))
      return <MySqlForm interaction={i} existingSchema={schema?.createStatement} onSave={save} onSaveSchema={(t, c) => onSaveSchema(t, c, 'MYSQL')} />
    }
    case 'POSTGRES': {
      const schema = schemas.find(s => s.protocol === 'POSTGRES' && i.request.query?.includes(s.tableName))
      return <PostgresForm interaction={i} existingSchema={schema?.createStatement} onSave={save} onSaveSchema={(t, c) => onSaveSchema(t, c, 'POSTGRES')} />
    }
    case 'DYNAMODB':
      return <DynamoDbForm interaction={i} onSave={save} />
    case 'REDIS':
      return <RedisForm interaction={i} onSave={save} />
    default:
      return <div style={{ color: '#ef4444' }}>Unknown protocol: {i.protocol}</div>
  }
}
```

**Step 2: Build and verify**

```bash
cd ui && bun run build
```

**Step 3: Commit**

```bash
git add ui/src/components/PendingTab.tsx
git commit -m "feat: add PendingTab with protocol-aware form dispatch and 2s polling"
```

---

## Task 12: MocksTab component

**Files:**
- Create: `ui/src/components/MocksTab.tsx`

Shows all configured interactions, grouped by protocol then host/query prefix. Allows re-editing.

**Step 1: Create `ui/src/components/MocksTab.tsx`**

```tsx
import { useEffect, useState } from 'react'
import { api } from '../api/client'
import type { Interaction, InteractionResponse, Schema } from '../types'
import HttpForm from '../forms/HttpForm'
import MySqlForm from '../forms/MySqlForm'
import PostgresForm from '../forms/PostgresForm'
import DynamoDbForm from '../forms/DynamoDbForm'
import RedisForm from '../forms/RedisForm'

const PROTOCOL_COLORS: Record<string, string> = {
  HTTP: '#0ea5e9', MYSQL: '#f59e0b', POSTGRES: '#6366f1',
  REDIS: '#ef4444', DYNAMODB: '#10b981',
}

export default function MocksTab() {
  const [interactions, setInteractions] = useState<Interaction[]>([])
  const [schemas, setSchemas]           = useState<Schema[]>([])
  const [selected, setSelected]         = useState<string | null>(null)
  const [editing, setEditing]           = useState(false)

  useEffect(() => {
    api.interactions.all().then(list => setInteractions(list.filter(i => i.state === 'configured'))).catch(() => {})
    api.schemas.all().then(setSchemas).catch(() => {})
  }, [])

  async function handleSave(id: string, name: string, response: InteractionResponse) {
    await api.interactions.configure(id, name, response)
    const updated = await api.interactions.all()
    setInteractions(updated.filter(i => i.state === 'configured'))
    setEditing(false)
  }

  const active = interactions.find(i => i.id === selected)

  // Group by protocol
  const byProtocol: Record<string, Interaction[]> = {}
  for (const i of interactions) {
    ;(byProtocol[i.protocol] ??= []).push(i)
  }

  return (
    <div style={{ display: 'flex', height: 'calc(100vh - 80px)', gap: 0 }}>
      <div style={{ width: 340, borderRight: '1px solid #1e293b', overflowY: 'auto' }}>
        {interactions.length === 0
          ? <div style={{ padding: 24, color: '#475569', fontSize: 13 }}>No configured mocks yet.</div>
          : Object.entries(byProtocol).map(([proto, list]) => (
            <div key={proto}>
              <div style={{ padding: '8px 16px', background: '#0f172a', fontSize: 11, fontWeight: 700, color: PROTOCOL_COLORS[proto] ?? '#64748b', textTransform: 'uppercase', letterSpacing: 1 }}>{proto}</div>
              {list.map(i => (
                <div
                  key={i.id}
                  onClick={() => { setSelected(i.id); setEditing(false) }}
                  style={{ padding: '10px 16px', cursor: 'pointer', borderBottom: '1px solid #1e293b', background: selected === i.id ? '#1e293b' : 'transparent' }}
                >
                  <div style={{ fontSize: 13, color: '#e2e8f0', marginBottom: 2 }}>{i.name || i.key}</div>
                  <div style={{ fontSize: 11, color: '#64748b', fontFamily: 'monospace' }}>{i.key.slice(0, 60)}</div>
                </div>
              ))}
            </div>
          ))
        }
      </div>
      <div style={{ flex: 1, padding: 24, overflowY: 'auto' }}>
        {!active
          ? <div style={{ color: '#475569', fontSize: 13 }}>Select a mock to view or edit it.</div>
          : editing
            ? renderForm(active, schemas, (id, name, resp) => handleSave(id, name, resp))
            : <Detail interaction={active} onEdit={() => setEditing(true)} />
        }
      </div>
    </div>
  )
}

function Detail({ interaction: i, onEdit }: { interaction: Interaction; onEdit: () => void }) {
  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
        <h3 style={{ color: '#e2e8f0' }}>{i.name || i.key}</h3>
        <button onClick={onEdit} style={{ background: '#334155', color: '#e2e8f0', border: 'none', padding: '6px 12px', borderRadius: 4, cursor: 'pointer' }}>Edit</button>
      </div>
      <pre style={{ background: '#0f172a', padding: 12, borderRadius: 4, fontSize: 12, color: '#94a3b8', overflowX: 'auto' }}>
        {JSON.stringify(i.response, null, 2)}
      </pre>
    </div>
  )
}

function renderForm(
  i: Interaction,
  schemas: Schema[],
  onSave: (id: string, name: string, resp: InteractionResponse) => void
) {
  const save = (name: string, resp: InteractionResponse) => onSave(i.id, name, resp)
  switch (i.protocol) {
    case 'HTTP': return <HttpForm interaction={i} onSave={save} />
    case 'MYSQL': {
      const schema = schemas.find(s => s.protocol === 'MYSQL' && i.request.query?.includes(s.tableName))
      return <MySqlForm interaction={i} existingSchema={schema?.createStatement} onSave={save} onSaveSchema={() => {}} />
    }
    case 'POSTGRES': {
      const schema = schemas.find(s => s.protocol === 'POSTGRES' && i.request.query?.includes(s.tableName))
      return <PostgresForm interaction={i} existingSchema={schema?.createStatement} onSave={save} onSaveSchema={() => {}} />
    }
    case 'DYNAMODB': return <DynamoDbForm interaction={i} onSave={save} />
    case 'REDIS': return <RedisForm interaction={i} onSave={save} />
    default: return null
  }
}
```

**Step 2: Build and verify**

```bash
cd ui && bun run build
```

**Step 3: Commit**

```bash
git add ui/src/components/MocksTab.tsx
git commit -m "feat: add MocksTab — grouped configured mocks with inline edit"
```

---

## Task 13: TestCasesTab and ImportTab

**Files:**
- Create: `ui/src/components/TestCasesTab.tsx`
- Create: `ui/src/components/ImportTab.tsx`

**Step 1: Create `ui/src/components/TestCasesTab.tsx`**

```tsx
import { useEffect, useState } from 'react'
import { api } from '../api/client'
import type { TestCase, Interaction } from '../types'

export default function TestCasesTab() {
  const [testCases, setTestCases]       = useState<TestCase[]>([])
  const [interactions, setInteractions] = useState<Interaction[]>([])
  const [selected, setSelected]         = useState<string | null>(null)
  const [newName, setNewName]           = useState('')
  const [newDesc, setNewDesc]           = useState('')
  const [creating, setCreating]         = useState(false)

  useEffect(() => {
    api.testcases.all().then(setTestCases).catch(() => {})
    api.interactions.all().then(list => setInteractions(list.filter(i => i.state === 'configured'))).catch(() => {})
  }, [])

  async function handleCreate() {
    if (!newName.trim()) return
    const tc = await api.testcases.create(newName.trim(), newDesc.trim())
    setTestCases(prev => [...prev, tc])
    setNewName(''); setNewDesc(''); setCreating(false)
    setSelected(tc.id)
  }

  async function handleDelete(id: string) {
    await api.testcases.delete(id)
    setTestCases(prev => prev.filter(tc => tc.id !== id))
    if (selected === id) setSelected(null)
  }

  async function toggleInteraction(tcId: string, iid: string, currentIds: string[]) {
    const next = currentIds.includes(iid) ? currentIds.filter(x => x !== iid) : [...currentIds, iid]
    await api.testcases.update(tcId, { interactionIds: next })
    setTestCases(prev => prev.map(tc => tc.id === tcId ? { ...tc, interactionIds: next } : tc))
  }

  async function handleSaveState() {
    await api.saveState()
  }

  const activeTc = testCases.find(tc => tc.id === selected)

  return (
    <div style={{ display: 'flex', height: 'calc(100vh - 80px)', gap: 0 }}>
      {/* Left: list */}
      <div style={{ width: 280, borderRight: '1px solid #1e293b', display: 'flex', flexDirection: 'column' }}>
        <div style={{ padding: 12, borderBottom: '1px solid #1e293b', display: 'flex', gap: 8 }}>
          <button onClick={() => setCreating(true)} style={{ flex: 1, background: '#7c3aed', color: '#fff', border: 'none', padding: '6px 10px', borderRadius: 4, cursor: 'pointer', fontSize: 13 }}>+ New test case</button>
        </div>
        <div style={{ flex: 1, overflowY: 'auto' }}>
          {testCases.map(tc => (
            <div key={tc.id} onClick={() => setSelected(tc.id)} style={{ padding: '10px 16px', cursor: 'pointer', borderBottom: '1px solid #1e293b', background: selected === tc.id ? '#1e293b' : 'transparent', display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
              <div>
                <div style={{ fontSize: 13, color: '#e2e8f0' }}>{tc.name}</div>
                <div style={{ fontSize: 11, color: '#64748b' }}>{tc.interactionIds.length} interactions</div>
              </div>
              <button onClick={e => { e.stopPropagation(); handleDelete(tc.id) }} style={{ background: 'none', border: 'none', color: '#ef4444', cursor: 'pointer', fontSize: 16 }}>×</button>
            </div>
          ))}
        </div>
        <div style={{ padding: 12, borderTop: '1px solid #1e293b' }}>
          <button onClick={handleSaveState} style={{ width: '100%', background: '#1e293b', color: '#94a3b8', border: '1px solid #334155', padding: '6px 10px', borderRadius: 4, cursor: 'pointer', fontSize: 12 }}>Save state to disk</button>
        </div>
      </div>

      {/* Right: detail */}
      <div style={{ flex: 1, padding: 24, overflowY: 'auto' }}>
        {creating && (
          <div style={{ display: 'flex', flexDirection: 'column', gap: 12, maxWidth: 400, marginBottom: 24 }}>
            <h3 style={{ color: '#e2e8f0' }}>New test case</h3>
            <input placeholder="Name" value={newName} onChange={e => setNewName(e.target.value)} style={{ background: '#1e293b', border: '1px solid #334155', color: '#e2e8f0', padding: '6px 8px', borderRadius: 4 }} />
            <input placeholder="Description (optional)" value={newDesc} onChange={e => setNewDesc(e.target.value)} style={{ background: '#1e293b', border: '1px solid #334155', color: '#e2e8f0', padding: '6px 8px', borderRadius: 4 }} />
            <div style={{ display: 'flex', gap: 8 }}>
              <button onClick={handleCreate} style={{ background: '#7c3aed', color: '#fff', border: 'none', padding: '6px 14px', borderRadius: 4, cursor: 'pointer' }}>Create</button>
              <button onClick={() => setCreating(false)} style={{ background: '#334155', color: '#e2e8f0', border: 'none', padding: '6px 14px', borderRadius: 4, cursor: 'pointer' }}>Cancel</button>
            </div>
          </div>
        )}
        {!activeTc
          ? <div style={{ color: '#475569', fontSize: 13 }}>Select or create a test case.</div>
          : <TestCaseDetail tc={activeTc} interactions={interactions} onToggle={toggleInteraction} />
        }
      </div>
    </div>
  )
}

function TestCaseDetail({ tc, interactions, onToggle }: { tc: TestCase; interactions: Interaction[]; onToggle: (tcId: string, iid: string, current: string[]) => void }) {
  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 16 }}>
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
        <h3 style={{ color: '#e2e8f0' }}>{tc.name}</h3>
        <a href={`/api/testcases/${tc.id}/export`} download style={{ background: '#0ea5e9', color: '#fff', border: 'none', padding: '6px 12px', borderRadius: 4, textDecoration: 'none', fontSize: 13 }}>Export JSON</a>
      </div>
      {tc.description && <p style={{ color: '#94a3b8', fontSize: 13 }}>{tc.description}</p>}
      <div>
        <h4 style={{ color: '#94a3b8', fontSize: 12, textTransform: 'uppercase', letterSpacing: 1, marginBottom: 8 }}>Interactions ({tc.interactionIds.length})</h4>
        {interactions.map(i => {
          const inTc = tc.interactionIds.includes(i.id)
          return (
            <div key={i.id} style={{ display: 'flex', alignItems: 'center', gap: 12, padding: '8px 0', borderBottom: '1px solid #1e293b' }}>
              <input type="checkbox" checked={inTc} onChange={() => onToggle(tc.id, i.id, tc.interactionIds)} style={{ cursor: 'pointer' }} />
              <span style={{ fontSize: 11, fontWeight: 700, color: '#64748b', minWidth: 70 }}>{i.protocol}</span>
              <span style={{ fontSize: 13, color: '#e2e8f0' }}>{i.name || i.key}</span>
            </div>
          )
        })}
      </div>
    </div>
  )
}
```

**Step 2: Create `ui/src/components/ImportTab.tsx`**

```tsx
import { useState } from 'react'
import { api } from '../api/client'
import type { TestCase } from '../types'

export default function ImportTab() {
  const [content, setContent] = useState('')
  const [result, setResult]   = useState<TestCase | null>(null)
  const [error, setError]     = useState('')

  async function handleImport() {
    setError(''); setResult(null)
    try {
      const tc = await api.import(content)
      setResult(tc)
    } catch (e) {
      setError(String(e))
    }
  }

  function handleFile(e: React.ChangeEvent<HTMLInputElement>) {
    const file = e.target.files?.[0]
    if (!file) return
    const reader = new FileReader()
    reader.onload = ev => setContent(ev.target?.result as string ?? '')
    reader.readAsText(file)
  }

  return (
    <div style={{ maxWidth: 680, padding: '24px 0', display: 'flex', flexDirection: 'column', gap: 16 }}>
      <h3 style={{ color: '#e2e8f0' }}>Import a test suite</h3>
      <p style={{ color: '#94a3b8', fontSize: 13 }}>Load a previously exported <code>.json</code> file to restore its test case and interactions.</p>
      <label style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
        <input type="file" accept=".json" onChange={handleFile} style={{ color: '#94a3b8' }} />
      </label>
      <textarea
        rows={16}
        value={content}
        onChange={e => setContent(e.target.value)}
        placeholder="…or paste the JSON here"
        style={{ background: '#1e293b', border: '1px solid #334155', color: '#e2e8f0', padding: '8px', borderRadius: 4, fontFamily: 'monospace', fontSize: 12, resize: 'vertical' }}
      />
      <button
        onClick={handleImport}
        disabled={!content.trim()}
        style={{ background: '#7c3aed', color: '#fff', border: 'none', padding: '8px 16px', borderRadius: 4, cursor: 'pointer', fontWeight: 600, alignSelf: 'flex-start' }}
      >
        Import
      </button>
      {result && <div style={{ color: '#10b981', fontSize: 13 }}>Imported test case: <strong>{result.name}</strong> ({result.interactionIds.length} interactions)</div>}
      {error  && <div style={{ color: '#ef4444', fontSize: 13 }}>{error}</div>}
    </div>
  )
}
```

**Step 3: Build and verify**

```bash
cd ui && bun run build
```

**Step 4: Commit**

```bash
git add ui/src/components/TestCasesTab.tsx ui/src/components/ImportTab.tsx
git commit -m "feat: add TestCasesTab and ImportTab"
```

---

## Task 14: Wire up App.tsx and final build

**Files:**
- Modify: `ui/src/App.tsx`

Replace the placeholder tab bodies with the real components.

**Step 1: Replace `ui/src/App.tsx`**

```tsx
import { useState } from 'react'
import PendingTab    from './components/PendingTab'
import MocksTab      from './components/MocksTab'
import TestCasesTab  from './components/TestCasesTab'
import ImportTab     from './components/ImportTab'

type Tab = 'pending' | 'mocks' | 'testcases' | 'import'

const TAB_LABELS: Record<Tab, string> = {
  pending:   'Pending',
  mocks:     'Mocks',
  testcases: 'Test Cases',
  import:    'Import',
}

export default function App() {
  const [tab, setTab] = useState<Tab>('pending')

  return (
    <div style={{ minHeight: '100vh', display: 'flex', flexDirection: 'column' }}>
      <header style={{ background: '#1e293b', padding: '0 24px', display: 'flex', gap: 24, alignItems: 'center', height: 52, borderBottom: '1px solid #0f172a', flexShrink: 0 }}>
        <span style={{ fontWeight: 700, fontSize: 16, color: '#7c3aed', marginRight: 8 }}>⚗ Veritaserum</span>
        {(Object.keys(TAB_LABELS) as Tab[]).map(t => (
          <button
            key={t}
            onClick={() => setTab(t)}
            style={{
              background: 'none', border: 'none', cursor: 'pointer',
              color: tab === t ? '#a78bfa' : '#94a3b8',
              fontWeight: tab === t ? 700 : 400,
              fontSize: 14, padding: '0 4px', height: '100%',
              borderBottom: tab === t ? '2px solid #7c3aed' : '2px solid transparent',
            }}
          >
            {TAB_LABELS[t]}
          </button>
        ))}
      </header>
      <main style={{ flex: 1, overflow: 'hidden' }}>
        {tab === 'pending'   && <PendingTab />}
        {tab === 'mocks'     && <MocksTab />}
        {tab === 'testcases' && <TestCasesTab />}
        {tab === 'import'    && <ImportTab />}
      </main>
    </div>
  )
}
```

**Step 2: Final build**

```bash
cd ui && bun run build
```

**Step 3: Final Go build**

```bash
cd .. && go build -o veritaserum .
```

Expected: binary builds cleanly.

**Step 4: Smoke test**

```bash
./veritaserum
# Open http://localhost:8080 — should show the 4-tab UI
# In another terminal:
HTTP_PROXY=http://localhost:9999 curl http://api.example.com/users
# → 503 (intercepted, appears in Pending tab)
```

**Step 5: Commit**

```bash
git add ui/src/App.tsx dist/
git commit -m "feat: wire all tab components into App.tsx, final build"
```

---

## Task 15: Clean up and delete obsolete files

**Files:**
- Delete: `stage-2.md`
- Delete: `ui/index.html` (the old single-file UI — replaced by `ui/src/`)

**Step 1: Delete old files**

```bash
git rm stage-2.md
git rm ui/index.html   # only if it still exists
```

**Step 2: Update .gitignore**

Ensure `dist/` and `ui/node_modules/` are in `.gitignore`.

**Step 3: Commit**

```bash
git add .gitignore
git commit -m "chore: remove obsolete stage-2.md and old single-file UI"
```

---

## Task 16: Verify the complete binary end-to-end

**Step 1: Cold start test**

```bash
rm -f veritaserum.json
./veritaserum
```

Expected log output:
```
Proxy      listening on :9999
MySQL mock listening on :33060
Postgres   mock listening on :54320
Redis mock listening on :6380
API/UI     listening on :8080  →  http://localhost:8080/
```

**Step 2: HTTP intercept**

```bash
HTTP_PROXY=http://localhost:9999 curl http://httpbin.org/get
# → 503
# Appears in Pending tab. Configure a mock. Re-trigger. Get the mock response.
```

**Step 3: CLI replay test**

```bash
# Export a test case from UI first, then:
./veritaserum --replay --suite=my-test-case.json
HTTP_PROXY=http://localhost:9999 curl http://httpbin.org/get
# → should return the configured mock response
```

**Step 4: Final commit**

```bash
git add -A
git commit -m "feat: complete Veritaserum redesign — Interaction/TestCase model, React+Bun UI, Redis mock, CLI replay"
```
