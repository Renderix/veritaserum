package main

import (
	"io"
	"log"
	"net/http"
	"time"
)

func proxyHandler(w http.ResponseWriter, r *http.Request) {
	targetURL := r.RequestURI
	if targetURL == "" || targetURL == "/" {
		http.Error(w, "bad request: missing absolute URI", http.StatusBadRequest)
		return
	}

	key := httpKey(r.Method, targetURL)

	mocksMu.RLock()
	entry, found := mocks[key]
	mocksMu.RUnlock()

	// ---- Playback (configured) ------------------------------------------
	if found && entry.State == StatusConfigured {
		if entry.LatencyMs > 0 {
			time.Sleep(time.Duration(entry.LatencyMs) * time.Millisecond)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(entry.StatusCode)
		io.WriteString(w, entry.ResponseBody)
		log.Printf("PLAYBACK  %s %s  →  %d", r.Method, targetURL, entry.StatusCode)
		return
	}

	// ---- Pending (already registered but not yet configured) ------------
	if found && entry.State == StatusPending {
		http.Error(w, "veritaserum: mock pending configuration", http.StatusServiceUnavailable)
		log.Printf("PENDING   %s %s", r.Method, targetURL)
		return
	}

	// ---- Cache miss: register as pending --------------------------------
	mocksMu.Lock()
	mocks[key] = &MockDefinition{
		Protocol: "HTTP",
		Method:   r.Method,
		URL:      targetURL,
		State:    StatusPending,
	}
	mocksMu.Unlock()

	http.Error(w, "veritaserum: intercepted, configure mock in UI", http.StatusServiceUnavailable)
	log.Printf("INTERCEPT %s %s  →  registered as pending", r.Method, targetURL)
}
