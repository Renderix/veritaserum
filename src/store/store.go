package store

import (
	"encoding/json"
	"log"
	"os"
	"sync"
)

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
