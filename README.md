# Veritaserum

A lightweight HTTP service virtualization tool for local development. Point your services at it as a forward proxy, intercept outbound calls, and configure mock responses through a web UI — no downstream dependencies required.

## How It Works

Veritaserum runs two servers:

- **Proxy (`:9999`)** — an HTTP forward proxy. Set `HTTP_PROXY=http://localhost:9999` in your service and all outbound HTTP calls route through it.
- **UI + API (`:8080`)** — a web dashboard to configure mock responses, and a REST API used by that dashboard.

On first encounter, a request is registered as **pending**. Open the UI, fill in a status code, latency, and response body, then click **Save Mock**. On subsequent requests to the same `METHOD + URL`, Veritaserum replays the configured response immediately (with optional simulated latency).

```
Your Service  →  HTTP_PROXY=http://localhost:9999  →  Veritaserum
                                                          │
                                                    [known mock?]
                                                     yes  │  no
                                                          │
                                               replay  ←──┘──→  register as pending
                                                              │
                                                         configure in UI at :8080
```

## Getting Started

```bash
# Build and run
go build -o veritaserum .
./veritaserum
```

```
Proxy listening on :9999
UI listening on :8080  →  http://localhost:8080/ui/index.html
```

Point your service at the proxy:

```bash
HTTP_PROXY=http://localhost:9999 curl http://some-downstream-service/api/users
# → 503 veritaserum: intercepted, configure mock in UI
```

Open `http://localhost:8080/ui/index.html`, configure the mock, and retry the request — it will now return your configured response.

## API

### `GET /api/intercepts`
Returns all known intercepts (pending and configured).

```bash
curl http://localhost:8080/api/intercepts
```

### `POST /api/intercepts`
Configure or update a mock.

```bash
curl -X POST http://localhost:8080/api/intercepts \
  -H 'Content-Type: application/json' \
  -d '{
    "method": "GET",
    "url": "http://some-downstream-service/api/users",
    "statusCode": 200,
    "latencyMs": 150,
    "responseBody": "{\"users\": []}"
  }'
```

### `POST /api/export`
Saves current in-memory state to `veritaserum.json`.

```bash
curl -X POST http://localhost:8080/api/export
```

State is loaded from `veritaserum.json` automatically on startup if the file exists.

## Intercept States

| State        | Meaning                                              |
|--------------|------------------------------------------------------|
| `pending`    | First seen — returns 503 until configured            |
| `configured` | Mock is set — replays response with optional latency |

## Requirements

- Go 1.21+
- No external dependencies (standard library only)
