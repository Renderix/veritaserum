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
	// HTTP + DynamoDB
	Method   string            `json:"method,omitempty"`
	Host     string            `json:"host,omitempty"`
	Path     string            `json:"path,omitempty"`
	Headers  map[string]string `json:"headers,omitempty"`
	Body     string            `json:"body,omitempty"`
	BodyHash string            `json:"bodyHash,omitempty"`

	// DynamoDB-specific (parsed from body)
	Operation string `json:"operation,omitempty"`
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

	// MySQL / Postgres SELECT
	Rows []map[string]interface{} `json:"rows,omitempty"`
	// MySQL / Postgres INSERT/UPDATE/DELETE
	AffectedRows int `json:"affectedRows,omitempty"`

	// DynamoDB
	ItemJSON string `json:"itemJSON,omitempty"`

	// Redis
	Value string `json:"value,omitempty"`
}

type Interaction struct {
	ID         string               `json:"id"`
	Protocol   string               `json:"protocol"`
	Key        string               `json:"key"`
	Name       string               `json:"name"`
	Request    InteractionRequest   `json:"request"`
	Response   *InteractionResponse `json:"response,omitempty"`
	State      string               `json:"state"`
	TestCaseID string               `json:"testCaseId"`
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

// ---- Schema (per DB table) -----------------------------------------------

type Schema struct {
	TableName       string `json:"tableName"`
	Protocol        string `json:"protocol"`
	CreateStatement string `json:"createStatement"`
}

// ---- Global store --------------------------------------------------------

var (
	mu           sync.RWMutex
	interactions = map[string]*Interaction{}
	testCases    = map[string]*TestCase{}
	schemas      = map[string]*Schema{}
)

// ---- Key builders --------------------------------------------------------

func BodyHash(body []byte) string {
	if len(body) == 0 {
		return ""
	}
	h := sha256.Sum256(body)
	return fmt.Sprintf("%x", h[:8])
}

func HTTPKey(method, host, path, bodyHash string) string {
	return fmt.Sprintf("%s %s %s %s", method, host, path, bodyHash)
}

func DBKey(protocol, query string) string {
	return fmt.Sprintf("%s %s", protocol, query)
}

func RedisKey(command string, args []string) string {
	key := command
	for _, a := range args {
		key += " " + a
	}
	return key
}

// ---- Interaction helpers -------------------------------------------------

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
