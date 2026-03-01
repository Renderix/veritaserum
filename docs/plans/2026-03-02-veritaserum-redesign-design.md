# Veritaserum Redesign — Design Document

**Date:** 2026-03-02
**Status:** Approved

---

## Problem

Veritaserum currently works as a flat stub server: requests land as "pending", the user manually fills in a response, and the same request replays next time. There is no concept of grouping related interactions, no protocol-aware UI prompts, no named test cases, and no CI/CD export.

The goal is to redesign Veritaserum into a **test management sidecar**: a tool that any service (xyz-service, order-service, anything) can proxy all outbound calls through, capturing and organizing those calls into named, replayable test cases.

---

## Core Flow

```
xyz-service makes any outbound call
      │
      ▼
Veritaserum intercepts (HTTP proxy / DB wire mock)
      │
      ├── known mock → replay stored response (with configured latency)
      │
      └── unknown → 503 back to xyz-service
                  → captured as PENDING in UI
                  → user fills in response via protocol-specific form
                  → user re-triggers request → replays
```

There is no blocking/long-poll. The service gets a 503 on first hit. The user configures the mock in the UI, then re-triggers.

---

## Data Model

### Interaction

The atomic unit. One captured request/response pair for one dependency call.

```go
type Interaction struct {
    ID         string          // UUID
    Protocol   Protocol        // HTTP | MYSQL | POSTGRES | REDIS | DYNAMODB
    Key        string          // routing key (see below)
    Name       string          // user-assigned label
    Request    InteractionRequest
    Response   *InteractionResponse // nil when state == pending
    State      InteractionState    // pending | configured
    TestCaseID string          // empty until user assigns to a test case
    CapturedAt time.Time
}

type InteractionRequest struct {
    // HTTP
    Method   string
    Host     string
    Path     string
    Headers  map[string]string
    Body     string
    BodyHash string

    // DB (MySQL / Postgres)
    Query  string
    Params []string

    // DynamoDB (detected via HTTP host pattern)
    Operation string // GetItem, PutItem, Query, Scan, etc.
    Table     string
    KeyJSON   string // the DynamoDB key as JSON

    // Redis
    Command string
    Args    []string
}

type InteractionResponse struct {
    // HTTP
    StatusCode int
    Headers    map[string]string
    Body       string
    LatencyMs  int

    // DB (MySQL / Postgres)
    Rows         []map[string]interface{}
    AffectedRows int

    // DynamoDB
    ItemJSON string // JSON of the returned Item or Items

    // Redis
    Value string
}

type Protocol        = string // "HTTP" | "MYSQL" | "POSTGRES" | "REDIS" | "DYNAMODB"
type InteractionState = string // "pending" | "configured"
```

### Routing Keys

Used to match an incoming request against a stored interaction:

| Protocol  | Key format |
|-----------|-----------|
| HTTP      | `"GET api.catalog.com /products/123 <bodyHash>"` |
| MySQL     | `"SELECT * FROM users WHERE id = ?"` |
| Postgres  | `"SELECT * FROM users WHERE id = $1"` |
| Redis     | `"GET session:abc123"` |
| DynamoDB  | `"GetItem orders {\"orderId\":\"123\"}"` |

DynamoDB is detected from the HTTP host pattern `*.dynamodb.*.amazonaws.com` — no separate server needed. It is routed through the HTTP proxy and identified by host.

### TestCase

A named grouping of configured interactions.

```go
type TestCase struct {
    ID             string
    Name           string
    Description    string
    InteractionIDs []string
    CreatedAt      time.Time
}
```

### Schema

Per-table CREATE TABLE statement, shared across all interactions that touch that table. Stored once, referenced by table name.

```go
type Schema struct {
    TableName       string
    Protocol        Protocol // MYSQL or POSTGRES
    CreateStatement string
}
```

### Global Store

```go
var (
    Interactions map[string]*Interaction // keyed by ID
    TestCases    map[string]*TestCase    // keyed by ID
    Schemas      map[string]*Schema      // keyed by "MYSQL:tablename" or "POSTGRES:tablename"
    mu           sync.RWMutex
)
```

No "active test case" concept in V1. The proxy matches against ALL configured interactions regardless of test case grouping. Test cases are purely an organizational layer.

---

## Servers

| Server        | Port  | Change from current |
|---------------|-------|---------------------|
| HTTP proxy    | :9999 | Update store lookup; add DynamoDB host detection |
| MySQL mock    | :33060 | Update store lookup; schema-aware responses |
| Postgres mock | :54320 | Update store lookup; schema-aware responses |
| Redis mock    | :6380 | **New** — RESP wire protocol |
| UI + API      | :8080 | Rewrite |

---

## Protocol-Specific UI Forms

Each form is a separate React component. `PendingTab` renders the correct form based on `interaction.protocol`.

### HTTP Form (`HttpForm.tsx`)
- Displays: method, host, path, body hash (read-only)
- User fills in: name (label), status code, latency ms, response headers (key-value pairs), response body

### MySQL / Postgres Form (`MySqlForm.tsx`, `PostgresForm.tsx`)
- Displays: SQL query (read-only)
- If the query references a table not in Schemas: shows a CREATE TABLE textarea first
- User fills in:
  - For SELECT: rows to return as a JSON array (e.g. `[{"id":1,"name":"Alice"}]`)
  - For INSERT/UPDATE/DELETE: affected row count

### DynamoDB Form (`DynamoDbForm.tsx`)
- Displays: operation (GetItem, Query, etc.), table name, key JSON (read-only)
- No schema step
- User fills in: item/items as JSON to return

### Redis Form (`RedisForm.tsx`)
- Displays: command + args (read-only)
- User fills in: return value (string; for lists/hashes the UI shows appropriate input)

---

## UI Architecture

**Stack:** React + Bun + TypeScript
**Build output:** `dist/` — embedded into the Go binary via `//go:embed dist`

```
ui/
├── src/
│   ├── App.tsx                  ← tab shell
│   ├── main.tsx
│   ├── types/index.ts           ← Interaction, TestCase, Schema, Protocol types
│   ├── api/client.ts            ← typed fetch wrappers for REST API
│   ├── components/
│   │   ├── PendingTab.tsx       ← pending interactions + form dispatch
│   │   ├── MocksTab.tsx         ← all configured interactions, grouped by protocol/host
│   │   ├── TestCasesTab.tsx     ← create/view/export test cases
│   │   └── ImportTab.tsx        ← load a JSON suite file
│   └── forms/
│       ├── HttpForm.tsx
│       ├── MySqlForm.tsx
│       ├── PostgresForm.tsx
│       ├── DynamoDbForm.tsx
│       └── RedisForm.tsx
├── index.html
├── package.json
└── tsconfig.json

dist/                            ← built output (gitignored, embedded by Go)
```

**Tabs:**

| Tab | Purpose |
|-----|---------|
| Pending | Unconfigured interactions with protocol-specific fill-in forms |
| Mocks | All configured interactions, grouped by protocol then host/db |
| Test Cases | Create named groupings, assign interactions, export to JSON |
| Import | Load a JSON suite to hydrate interactions + test case |

The UI polls `/api/pending` every 2 seconds to surface new captures in real time.

---

## REST API (new endpoints)

| Method | Endpoint | Description |
|--------|----------|-------------|
| `GET` | `/api/interactions` | All interactions (pending + configured) |
| `GET` | `/api/interactions/pending` | Only pending |
| `POST` | `/api/interactions/:id/configure` | Save a response for an interaction |
| `GET` | `/api/testcases` | List test cases |
| `POST` | `/api/testcases` | Create a test case |
| `PUT` | `/api/testcases/:id` | Update (rename, add/remove interaction IDs) |
| `DELETE` | `/api/testcases/:id` | Delete |
| `GET` | `/api/testcases/:id/export` | Download test case as JSON |
| `POST` | `/api/import` | Import a JSON suite |
| `GET` | `/api/schemas` | List stored DB schemas |
| `POST` | `/api/schemas` | Save a schema |

Old endpoints (`/api/mocks`, `/api/pending`) are removed.

---

## Export JSON Format

```json
{
  "version": "2",
  "testCase": "create order - happy path",
  "exportedAt": "2026-03-02T10:00:00Z",
  "interactions": [
    {
      "protocol": "HTTP",
      "key": "GET api.catalog.com /products/123 ",
      "name": "get catalog item",
      "response": {
        "statusCode": 200,
        "latencyMs": 45,
        "headers": { "Content-Type": "application/json" },
        "body": "{\"id\":123,\"name\":\"Widget\"}"
      }
    },
    {
      "protocol": "MYSQL",
      "key": "SELECT * FROM users WHERE id = ?",
      "name": "fetch user by id",
      "schema": "CREATE TABLE users (id INT, name VARCHAR(255));",
      "response": {
        "rows": [{ "id": 1, "name": "Alice" }]
      }
    },
    {
      "protocol": "DYNAMODB",
      "key": "GetItem orders {\"orderId\":\"123\"}",
      "name": "get order record",
      "response": {
        "itemJSON": "{\"orderId\":\"123\",\"status\":\"pending\"}"
      }
    },
    {
      "protocol": "REDIS",
      "key": "GET session:abc123",
      "name": "get user session",
      "response": {
        "value": "{\"userId\":1,\"role\":\"admin\"}"
      }
    }
  ]
}
```

---

## CLI Replay Mode

```bash
# Headless replay — loads suite JSON, starts all mock servers, no UI
./veritaserum --replay --suite=create-order-happy-path.json

# Optional: timeout after N seconds (for CI use)
./veritaserum --replay --suite=create-order-happy-path.json --timeout=120s
```

Behaviour in replay mode:
- No UI served (`:8080` serves a minimal `/healthz` endpoint only)
- HTTP proxy on `:9999`, DB mocks on their ports, Redis on `:6380`
- All interactions from the JSON are loaded as configured
- Unknown request → `503` (same as normal mode)
- Exits cleanly on timeout or SIGTERM

**Typical CI step (docker-compose or GitHub Actions):**
```yaml
- name: Start Veritaserum mock server
  run: ./veritaserum --replay --suite=testdata/create-order-happy-path.json &

- name: Run integration tests
  run: go test ./... -tags=integration
```

---

## Build Process

```bash
# 1. Build frontend
cd ui && bun run build   # outputs → ../dist/

# 2. Build Go binary (embeds dist/)
cd .. && go build -o veritaserum .
```

`dist/` is gitignored. CI must run both steps. The Go binary is fully self-contained after build.

---

## Files Changed

| File | Change |
|------|--------|
| `src/store/store.go` | Full rewrite — Interaction / TestCase / Schema model |
| `src/http/proxy.go` | New store lookup; DynamoDB host detection |
| `src/dbs/mysql.go` | New store lookup; schema-aware result encoding |
| `src/dbs/postgres.go` | New store lookup; schema-aware result encoding |
| `src/dbs/redis.go` | **New** — RESP wire protocol mock |
| `src/messaging/api.go` | New REST API endpoints |
| `main.go` | `--replay` / `--suite` / `--timeout` flags; start Redis server |
| `ui/` | **New** — React + Bun + TypeScript app |
| `dist/` | Built frontend output (gitignored, embedded by Go) |
| `stage-2.md` | Delete (superseded) |

---

## What Is NOT Changing

- HTTP forward proxy mechanism (the `HTTP_PROXY=http://localhost:9999` usage)
- MySQL and Postgres wire protocol implementations
- The `503 on miss` behaviour
- Single Go binary deployment model
- JSON file persistence (format changes but concept stays)
