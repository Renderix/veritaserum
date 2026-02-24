package proxy

import (
	"io"
	"log"
	"net/http"
	"time"

	"veritaserum/src/store"
)

func Handler(w http.ResponseWriter, r *http.Request) {
	targetURL := r.RequestURI
	if targetURL == "" || targetURL == "/" {
		http.Error(w, "bad request: missing absolute URI", http.StatusBadRequest)
		return
	}

	key := store.HttpKey(r.Method, targetURL)

	store.MocksMu.RLock()
	entry, found := store.Mocks[key]
	store.MocksMu.RUnlock()

	// ---- Playback (configured) ------------------------------------------
	if found && entry.State == store.StatusConfigured {
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
	if found && entry.State == store.StatusPending {
		http.Error(w, "veritaserum: mock pending configuration", http.StatusServiceUnavailable)
		log.Printf("PENDING   %s %s", r.Method, targetURL)
		return
	}

	// ---- Cache miss: register as pending --------------------------------
	store.MocksMu.Lock()
	store.Mocks[key] = &store.MockDefinition{
		Protocol: "HTTP",
		Method:   r.Method,
		URL:      targetURL,
		State:    store.StatusPending,
	}
	store.MocksMu.Unlock()

	http.Error(w, "veritaserum: intercepted, configure mock in UI", http.StatusServiceUnavailable)
	log.Printf("INTERCEPT %s %s  →  registered as pending", r.Method, targetURL)
}
