package store

import (
	"encoding/json"
	"log"
	"os"
	"sync"
)

// ---- Provisioned DBs --------------------------------------------------------

type ProvisionedDB struct {
	ID          string `json:"id"`
	Type        string `json:"type"`
	ContainerID string `json:"containerId,omitempty"`
	Port        int    `json:"port,omitempty"`
	JDBCUrl     string `json:"jdbcUrl,omitempty"`
	Status      string `json:"status"` // "provisioning" | "ready" | "error"
	Error       string `json:"error,omitempty"`
}

var (
	ProvisionedDBs   []*ProvisionedDB
	ProvisionedDBsMu sync.RWMutex
)

func FindProvisionedDB(id string) *ProvisionedDB {
	ProvisionedDBsMu.RLock()
	defer ProvisionedDBsMu.RUnlock()
	for _, db := range ProvisionedDBs {
		if db.ID == id {
			return db
		}
	}
	return nil
}

func UpdateProvisionedDB(id, containerID string, port int) {
	ProvisionedDBsMu.Lock()
	defer ProvisionedDBsMu.Unlock()
	for _, db := range ProvisionedDBs {
		if db.ID == id {
			db.ContainerID = containerID
			db.Port = port
			return
		}
	}
}

func ReadyProvisionedDB(id, jdbcURL string) {
	ProvisionedDBsMu.Lock()
	defer ProvisionedDBsMu.Unlock()
	for _, db := range ProvisionedDBs {
		if db.ID == id {
			db.JDBCUrl = jdbcURL
			db.Status = "ready"
			return
		}
	}
}

func FailProvisionedDB(id, errMsg string) {
	ProvisionedDBsMu.Lock()
	defer ProvisionedDBsMu.Unlock()
	for _, db := range ProvisionedDBs {
		if db.ID == id {
			db.Status = "error"
			db.Error = errMsg
			return
		}
	}
}

// ---- Types ---------------------------------------------------------------

type Status string

const (
	StatusPending    Status = "pending"
	StatusConfigured Status = "configured"
)

type MockDefinition struct {
	Protocol     string `json:"protocol"`
	Method       string `json:"method"`
	URL          string `json:"url"`
	Query        string `json:"query"`
	StatusCode   int    `json:"statusCode"`
	LatencyMs    int    `json:"latencyMs"`
	ResponseBody string `json:"responseBody"`
	State        Status `json:"state"`
}

func HttpKey(method, url string) string {
	return "HTTP " + method + " " + url
}

func PostgresKey(sql string) string {
	return "POSTGRES " + sql
}

func MysqlKey(sql string) string {
	return "MYSQL " + sql
}

// ---- State ---------------------------------------------------------------

var (
	Mocks   = map[string]*MockDefinition{}
	MocksMu sync.RWMutex
)

const StateFile = "veritaserum.json"

func LoadState() {
	data, err := os.ReadFile(StateFile)
	if err != nil {
		return
	}
	MocksMu.Lock()
	defer MocksMu.Unlock()
	if err := json.Unmarshal(data, &Mocks); err != nil {
		log.Printf("warn: could not parse %s: %v", StateFile, err)
	}
}
