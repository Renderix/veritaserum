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

type Intercept struct {
	Method       string `json:"method"`
	URL          string `json:"url"`
	StatusCode   int    `json:"statusCode"`
	LatencyMs    int    `json:"latencyMs"`
	ResponseBody string `json:"responseBody"`
	State        Status `json:"state"`
}

func interceptKey(method, url string) string {
	return method + " " + url
}

// ---- State ---------------------------------------------------------------

var (
	mocks   = map[string]*Intercept{}
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

	// Proxy server
	go func() {
		log.Println("Proxy listening on :9999")
		if err := http.ListenAndServe(":9999", http.HandlerFunc(proxyHandler)); err != nil {
			log.Fatalf("proxy: %v", err)
		}
	}()

	// UI + API server
	mux := http.NewServeMux()
	mux.HandleFunc("/api/intercepts", interceptsHandler)  // GET list, POST configure
	mux.HandleFunc("/api/intercepts/", interceptsHandler) // with trailing path (key)
	mux.HandleFunc("/api/export", exportHandler)
	mux.Handle("/", http.FileServer(http.FS(uiFiles)))

	log.Println("UI listening on :8080  →  http://localhost:8080/ui/index.html")
	if err := http.ListenAndServe(":8080", mux); err != nil {
		log.Fatalf("ui: %v", err)
	}
}
