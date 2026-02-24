package main

import (
	"encoding/json"
	"net/http"
	"os"
)

// interceptsHandler handles GET /api/intercepts and POST /api/intercepts
func interceptsHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	switch r.Method {

	case http.MethodGet:
		mocksMu.RLock()
		list := make([]*Intercept, 0, len(mocks))
		for _, v := range mocks {
			list = append(list, v)
		}
		mocksMu.RUnlock()
		json.NewEncoder(w).Encode(list)

	case http.MethodPost:
		var req struct {
			Method       string `json:"method"`
			URL          string `json:"url"`
			StatusCode   int    `json:"statusCode"`
			LatencyMs    int    `json:"latencyMs"`
			ResponseBody string `json:"responseBody"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		key := interceptKey(req.Method, req.URL)
		mocksMu.Lock()
		if _, exists := mocks[key]; !exists {
			mocks[key] = &Intercept{Method: req.Method, URL: req.URL}
		}
		mocks[key].StatusCode = req.StatusCode
		mocks[key].LatencyMs = req.LatencyMs
		mocks[key].ResponseBody = req.ResponseBody
		mocks[key].State = StatusConfigured
		mocksMu.Unlock()
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"configured"}`))

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func exportHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	mocksMu.RLock()
	data, err := json.MarshalIndent(mocks, "", "  ")
	mocksMu.RUnlock()
	if err != nil {
		http.Error(w, "marshal error", http.StatusInternalServerError)
		return
	}
	if err := os.WriteFile(stateFile, data, 0644); err != nil {
		http.Error(w, "write error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"status":"saved"}`))
}
