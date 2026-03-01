# Veritaserum

A test management sidecar for local development and CI. Proxy all outbound calls from your service through Veritaserum — HTTP, MySQL, Postgres, Redis, and DynamoDB. Unknown requests return 503 and appear as **Pending** in the UI. Configure a mock response, re-trigger, and it replays. Group interactions into named test cases and export them to JSON for headless CI replay.

## Start

```bash
go build -o veritaserum .
./veritaserum
```

```
HTTP proxy     :9999
MySQL mock     :33060
Postgres mock  :54320
Redis mock     :6380
UI + API       :8080  →  http://localhost:8080
```

## Core Flow

```
Your service makes any outbound call
      │
      ▼
Veritaserum intercepts
      │
      ├── known mock  →  replay stored response
      │
      └── unknown  →  503 back to your service
                   →  appears as PENDING in UI at :8080
                   →  fill in response, re-trigger
                   →  replays from now on
```

---

## Java

### HTTP (Spring / RestTemplate / HttpClient)

Set the JVM proxy system properties before your application starts:

```java
System.setProperty("http.proxyHost", "localhost");
System.setProperty("http.proxyPort", "9999");
System.setProperty("https.proxyHost", "localhost");
System.setProperty("https.proxyPort", "9999");
```

Or pass them on the command line:

```bash
java -Dhttp.proxyHost=localhost -Dhttp.proxyPort=9999 \
     -Dhttps.proxyHost=localhost -Dhttps.proxyPort=9999 \
     -jar your-service.jar
```

RestTemplate picks this up automatically. For `java.net.http.HttpClient` (Java 11+):

```java
HttpClient client = HttpClient.newBuilder()
    .proxy(ProxySelector.of(new InetSocketAddress("localhost", 9999)))
    .build();
```

### MySQL (JDBC)

```java
String url = "jdbc:mysql://localhost:33060/app";
Connection conn = DriverManager.getConnection(url, "root", "");
```

### Postgres (JDBC)

```java
String url = "jdbc:postgresql://localhost:54320/app";
Connection conn = DriverManager.getConnection(url, "postgres", "");
```

### Redis (Jedis / Lettuce)

```java
// Jedis
Jedis jedis = new Jedis("localhost", 6380);

// Lettuce
RedisClient client = RedisClient.create("redis://localhost:6380");
```

### Spring Boot — all at once

```yaml
# application-dev.yml
spring:
  datasource:
    url: jdbc:mysql://localhost:33060/app
    username: root
    password: ""
  data:
    redis:
      host: localhost
      port: 6380

# JVM args (add to your run config or Dockerfile CMD)
# -Dhttp.proxyHost=localhost -Dhttp.proxyPort=9999
```

---

## Go

### HTTP

```go
proxyURL, _ := url.Parse("http://localhost:9999")
client := &http.Client{
    Transport: &http.Transport{Proxy: http.ProxyURL(proxyURL)},
}

resp, err := client.Get("http://api.example.com/users")
```

Or via environment variable (works with the default `http.DefaultClient`):

```bash
HTTP_PROXY=http://localhost:9999 go run ./cmd/your-service
```

### MySQL (database/sql + go-sql-driver)

```go
import _ "github.com/go-sql-driver/mysql"

db, err := sql.Open("mysql", "root@tcp(localhost:33060)/app")
```

### Postgres (pgx / lib/pq)

```go
// pgx
conn, err := pgx.Connect(ctx, "postgres://postgres@localhost:54320/app")

// lib/pq
db, err := sql.Open("postgres", "host=localhost port=54320 dbname=app sslmode=disable")
```

### Redis (go-redis)

```go
rdb := redis.NewClient(&redis.Options{
    Addr: "localhost:6380",
})
```

### DynamoDB (AWS SDK v2)

```go
cfg, _ := config.LoadDefaultConfig(ctx,
    config.WithRegion("us-east-1"),
    config.WithHTTPClient(&http.Client{
        Transport: &http.Transport{
            Proxy: http.ProxyURL(func() *url.URL {
                u, _ := url.Parse("http://localhost:9999")
                return u
            }()),
        },
    }),
)
client := dynamodb.NewFromConfig(cfg)
```

---

## Node.js

### HTTP (axios)

```bash
HTTP_PROXY=http://localhost:9999 node your-service.js
```

Or configure per-client using `https-proxy-agent`:

```bash
npm install https-proxy-agent
```

```js
const axios = require('axios');
const { HttpsProxyAgent } = require('https-proxy-agent');

const client = axios.create({
  httpAgent: new HttpsProxyAgent('http://localhost:9999'),
  httpsAgent: new HttpsProxyAgent('http://localhost:9999'),
});

const res = await client.get('http://api.example.com/users');
```

### HTTP (node-fetch / undici)

```js
import { fetch, ProxyAgent } from 'undici';

const dispatcher = new ProxyAgent('http://localhost:9999');
const res = await fetch('http://api.example.com/users', { dispatcher });
```

### MySQL (mysql2)

```js
const mysql = require('mysql2/promise');

const conn = await mysql.createConnection({
  host: 'localhost',
  port: 33060,
  user: 'root',
  password: '',
  database: 'app',
});
```

### Postgres (pg)

```js
const { Pool } = require('pg');

const pool = new Pool({
  host: 'localhost',
  port: 54320,
  database: 'app',
  user: 'postgres',
  password: '',
});
```

### Redis (ioredis)

```js
const Redis = require('ioredis');

const redis = new Redis({ host: 'localhost', port: 6380 });
```

### DynamoDB (AWS SDK v3)

```js
const { DynamoDBClient } = require('@aws-sdk/client-dynamodb');
const { NodeHttpHandler } = require('@smithy/node-http-handler');
const { HttpProxyAgent } = require('http-proxy-agent');

const client = new DynamoDBClient({
  region: 'us-east-1',
  requestHandler: new NodeHttpHandler({
    httpAgent: new HttpProxyAgent('http://localhost:9999'),
  }),
});
```

---

## CI / Headless Replay

Export a test case from the UI, then use it in CI:

```bash
# Replay mode — no UI, loads suite JSON, starts all mocks
./veritaserum --replay --suite=testdata/create-order.json

# With a timeout (for CI jobs)
./veritaserum --replay --suite=testdata/create-order.json --timeout=120s
```

Example GitHub Actions step:

```yaml
- name: Start Veritaserum
  run: ./veritaserum --replay --suite=testdata/create-order.json &

- name: Run integration tests
  run: go test ./... -tags=integration
  # or: mvn verify  /  npm test
```

---

## REST API

| Method | Endpoint | Description |
|--------|----------|-------------|
| `GET` | `/api/interactions` | All interactions (pending + configured) |
| `GET` | `/api/interactions/pending` | Only pending |
| `POST` | `/api/interactions/:id/configure` | Save a mock response |
| `GET` | `/api/testcases` | List test cases |
| `POST` | `/api/testcases` | Create a test case |
| `PUT` | `/api/testcases/:id` | Rename / update interaction list |
| `DELETE` | `/api/testcases/:id` | Delete |
| `GET` | `/api/testcases/:id/export` | Download as JSON |
| `POST` | `/api/import` | Load a JSON suite |
| `GET` | `/api/schemas` | List stored DB schemas |
| `POST` | `/api/schemas` | Save a schema |
| `POST` | `/api/state/save` | Persist state to `veritaserum.json` |
| `GET` | `/healthz` | Health check |

## Requirements

- Go 1.21+
- Bun (to rebuild the UI — not needed for the pre-built binary)
