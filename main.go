package main

import (
	"embed"
	"encoding/json"
	"log"
	"net/http"
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
	Protocol     string `json:"protocol"`      // "HTTP" or "POSTGRES"
	Method       string `json:"method"`        // HTTP only
	URL          string `json:"url"`           // HTTP only
	Query        string `json:"query"`         // POSTGRES only
	StatusCode   int    `json:"statusCode"`    // HTTP only
	LatencyMs    int    `json:"latencyMs"`     // HTTP only
	ResponseBody string `json:"responseBody"`
	State        Status `json:"state"`
}

func httpKey(method, url string) string {
	return "HTTP " + method + " " + url
}

func postgresKey(sql string) string {
	return "POSTGRES " + sql
}

// ---- State ---------------------------------------------------------------

var (
	mocks   = map[string]*MockDefinition{}
	mocksMu sync.RWMutex
)

const stateFile = "veritaserum.json"

func loadState() {
	data, err := os.ReadFile(stateFile)
	if err != nil {
		return
	}
	mocksMu.Lock()
	defer mocksMu.Unlock()
	if err := json.Unmarshal(data, &mocks); err != nil {
		log.Printf("warn: could not parse %s: %v", stateFile, err)
	}
}

// ---- Embed ---------------------------------------------------------------

//go:embed ui
var uiFiles embed.FS

// ---- Entry Point ---------------------------------------------------------

func main() {
	loadState()

	go func() {
		log.Println("Proxy listening on :9999")
		if err := http.ListenAndServe(":9999", http.HandlerFunc(proxyHandler)); err != nil {
			log.Fatalf("proxy: %v", err)
		}
	}()

	go StartPostgresMock("54320")

	StartAPIServer("8080") // blocks; Gin runs the main goroutine
}
