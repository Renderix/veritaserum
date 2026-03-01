# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Build & Run

```bash
go build -o veritaserum .
./veritaserum
```

No test suite exists yet. To verify the binary works:

```bash
go vet ./...
go build ./...
```

## Architecture

Veritaserum is a single Go binary that starts four concurrent servers:

| Server | Port | Package |
|--------|------|---------|
| HTTP forward proxy | `:9999` | `src/http` |
| MySQL wire-protocol mock | `:33060` | `src/dbs` |
| Postgres wire-protocol mock | `:54320` | `src/dbs` |
| Web UI + REST API (Gin) | `:8080` | `src/messaging` |

**Entry point:** `main.go` — starts all four servers, loading persisted state first via `store.LoadState()`.

**State layer (`src/store/store.go`):** All shared mutable state lives here as package-level vars protected by `sync.RWMutex`. Two separate stores:
- `HttpMocks map[string]*Host` — HTTP mocks, keyed by hostname → endpoint key (`"METHOD /path"`) → bodyHash
- `Mocks map[string]*MockDefinition` — DB mocks (Postgres + MySQL), keyed by `"POSTGRES <sql>"` or `"MYSQL <sql>"`
- `ProvisionedDBs []*ProvisionedDB` — in-memory only (not persisted), tracks Docker-provisioned MySQL containers

**HTTP mock hierarchy (3 levels):** `Host` → `Endpoint` → `Scenario`. Scenarios are keyed by `BodyHash` (SHA-256[:8] of request body; `""` for empty body). An endpoint can pin a specific scenario via `ActiveScenario`; otherwise it auto-routes by bodyHash.

**HTTP proxy flow (`src/http/proxy.go`):**
1. Extract hostname, path, bodyHash from request
2. `lookupScenario()` → returns `"configured"` (replay mock) / `"pending"` (return 503) / `"miss"` (register as pending, return 503)

**DB mock flow (same pattern for Postgres and MySQL):**
- On query: look up `store.Mocks[key]`
- If configured → send mock result set (parsed from JSON array of objects)
- If not found → register as pending, return empty OK/CommandComplete

**MySQL provisioner (`src/dbs/provisioner.go`):** Runs `docker run mysql:8` asynchronously, polls `mysqladmin ping`, applies schema + hydrate SQL via `docker exec`, updates `store.ProvisionedDB` status.

**UI (`ui/index.html`):** Single-file vanilla JS UI embedded into the binary via `//go:embed ui`. Served at `/`.

**Persistence:** `POST /api/export` writes `veritaserum.json` to disk. `store.LoadState()` reads it on startup. Only `HttpMocks` and `DbMocks` are persisted; provisioned Docker containers are not.

## Key Design Decisions

- DB mocks (Postgres/MySQL) use a flat `map[string]*MockDefinition` keyed by protocol+SQL. HTTP mocks use the 3-level Host→Endpoint→Scenario hierarchy to support body-based routing.
- The `POST /api/mocks` endpoint is DB-only (Postgres/MySQL). HTTP mocks are managed via `POST /api/http/:hostname/scenarios`.
- Column ordering in result sets is derived from `range` over Go maps — non-deterministic between calls.
- The MySQL wire mock supports prepared statements (`COM_STMT_PREPARE`/`EXECUTE`/`CLOSE`) but maps them back to the same query-keyed mock store.
