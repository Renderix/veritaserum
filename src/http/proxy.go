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

// parseDynamoDB extracts the DynamoDB operation and table name from the request.
// Operation comes from the X-Amz-Target header (e.g. "DynamoDB_20120810.GetItem").
// Table comes from the JSON body field "TableName".
func parseDynamoDB(r *http.Request, body []byte) (operation, table string) {
	target := r.Header.Get("X-Amz-Target")
	if idx := strings.Index(target, "."); idx != -1 {
		operation = target[idx+1:]
	}
	bodyStr := string(body)
	if i := strings.Index(bodyStr, `"TableName"`); i != -1 {
		rest := strings.TrimSpace(bodyStr[i+10:])
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

	store.RegisterInteraction(protocol, key, req)
	http.Error(w, "veritaserum: intercepted, configure mock in UI", http.StatusServiceUnavailable)
	log.Printf("INTERCEPT %s %s → registered as pending", r.Method, targetURL)
}
