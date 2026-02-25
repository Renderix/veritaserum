# Veritaserum

A lightweight service virtualization tool for local development. Intercept outbound HTTP calls and mock database queries through a web UI — no downstream dependencies required.

## What It Does

- **HTTP mocking** — forward proxy on `:9999`. Route your service's outbound HTTP calls through it, intercept them, and configure mock responses.
- **Database mocking** — wire-protocol mocks for MySQL (`:33060`) and Postgres (`:54320`). Point your JDBC/driver at them, intercept queries, configure mock result sets.
- **On-demand MySQL provisioning** — spin up a real MySQL 8 Docker container with your schema and seed data on demand. Get back a JDBC URL ready to connect to.
- **Web UI + REST API** — manage everything at `http://localhost:8080`.

## Getting Started

```bash
go build -o veritaserum .
./veritaserum
```

```
Proxy      listening on :9999
MySQL mock listening on :33060
Postgres   listening on :54320
UI + API   listening on :8080  →  http://localhost:8080
```

## HTTP Mocking

Point your service at the forward proxy:

```bash
HTTP_PROXY=http://localhost:9999 curl http://api.example.com/users
# → 503: veritaserum: intercepted, configure mock in UI
```

The request appears in the **Pending** section of the UI at `http://localhost:8080`. Fill in the status code, latency, and response body, then click **Save Mock**. Hit the same request again — it replays your configured response.

You can also skip the interception step and configure a mock directly:

```bash
curl -X POST http://localhost:8080/api/mocks \
  -H 'Content-Type: application/json' \
  -d '{
    "protocol": "HTTP",
    "method": "GET",
    "url": "http://api.example.com/users",
    "statusCode": 200,
    "latencyMs": 150,
    "responseBody": "[{\"id\":1,\"name\":\"Alice\"}]"
  }'
```

To edit a configured mock, click the **Edit** button on the card in the UI and save again.

### How It Works

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

## Database Mocking (MySQL / Postgres)

Point your JDBC driver at the mock servers instead of your real database:

```
jdbc:mysql://localhost:33060/app
jdbc:postgresql://localhost:54320/app
```

On first query, the query is registered as pending in the UI. Configure a JSON response body (the rows to return), save, and subsequent executions of the same query return the mock result set.

## On-Demand MySQL Provisioning

Spin up a real MySQL 8 container with your schema and seed data:

```bash
curl -X POST http://localhost:8080/api/databases \
  -H 'Content-Type: application/json' \
  -d '{
    "type": "MYSQL",
    "schema": "CREATE TABLE users (id INT, name VARCHAR(255));",
    "hydrate": "INSERT INTO users VALUES (1, '\''Alice'\''), (2, '\''Bob'\'');"
  }'
# → {"id":"...","type":"MYSQL","status":"provisioning"}
```

Poll until ready (MySQL takes 20–60s to start):

```bash
curl http://localhost:8080/api/databases
# → [{"status":"ready","jdbcUrl":"jdbc:mysql://localhost:XXXXX/app?user=root&password=veritaserum"}]
```

Connect directly once ready:

```bash
mysql -h 127.0.0.1 -P XXXXX -uroot -pveritaserum app -e "SELECT * FROM users;"
```

You can also provision from the UI — use the **Provision Database** section at the top of the page.

> Requires Docker to be running. Each provisioned instance is an independent container on a random host port.

## REST API

| Method | Endpoint | Description |
|--------|----------|-------------|
| `GET` | `/api/pending` | List mocks awaiting configuration |
| `GET` | `/api/mocks` | List all configured mocks |
| `POST` | `/api/mocks` | Create or update a mock |
| `POST` | `/api/databases` | Provision a MySQL Docker container |
| `GET` | `/api/databases` | List provisioned DB instances |
| `POST` | `/api/export` | Persist state to `veritaserum.json` |

## Persistence

Click **Export JSON** in the UI (or `POST /api/export`) to save mock state to `veritaserum.json`. State is reloaded from this file automatically on next startup.

Provisioned Docker containers are not persisted — they are tracked in memory only for the lifetime of the process.

## Requirements

- Go 1.21+
- Docker (for on-demand MySQL provisioning only)
