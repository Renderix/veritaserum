🤖 LLM Prompt / System Context Start
Project Name: Veritaserum
Current Task: Phase 2 - Gin API Migration & PostgreSQL Database Interception
Context:
We are building Veritaserum, a Go-based Service Virtualization tool. In Phase 1, we built an asynchronous HTTP proxy with a backfill UI. Now, in Phase 2, we are expanding the tool.
We are migrating the UI/REST API server to use the Gin Web Framework (github.com/gin-gonic/gin).
We are adding a TCP PostgreSQL Mock Server. This server will listen on port 54320, speak the PostgreSQL v3 Wire Protocol, and intercept SQL queries using the same "backfill" mechanism.
Please act as a Senior Go Developer and generate the complete code for this phase based on the exact specifications below.
🏗️ Architecture & Specifications
1. The Tri-Server Setup (Running continuously in goroutines)
HTTP Proxy (Port 9999): (Carried over from Phase 1). Intercepts HTTP traffic.
Gin API / UI Server (Port 8080): Serves the embedded HTML UI and handles REST APIs (/api/mocks, /api/pending).
TCP Postgres Server (Port 54320): Listens for raw TCP connections. Handles Postgres Startup messages, Authentication (trust/ok), and Simple Query (Q) messages.
2. Unified State Management
Update the MockDefinition and PendingRequest structs to include a Protocol field ("HTTP" or "POSTGRES").
For HTTP, the identifier is Method + URL.
For POSTGRES, the identifier is the exact SQL Query string.
If a Postgres query is not mocked, add it to pendingMocks and return a valid, empty Postgres CommandComplete (C) message to the client so it doesn't crash, just returns 0 rows.
3. Postgres Wire Protocol (Simplified MVP)
You must implement a basic TCP reader/writer for Postgres v3.
Handle StartupMessage (length and protocol version).
Respond with AuthenticationOk (R with 0) and ReadyForQuery (Z with 'I').
When receiving a Query (Q), extract the SQL string.
If mock found: The ResponseBody will be a JSON array (e.g., [{"id":1, "name":"Test"}]). Dynamically generate the RowDescription (T) based on the JSON keys, send DataRow (D) for each JSON object, and end with CommandComplete (C).
If not found: Log to pending, return CommandComplete (C).
🛠️ Implementation Steps Required
Please provide robust, production-like Go code. Use sync.RWMutex for state.
File 1: main.go
Update data structs with Protocol fields.
Use sync.WaitGroup or simple go statements to launch the three servers (HTTP Proxy, Gin API, Postgres TCP) so they run continuously without blocking each other.
File 2: api.go (Gin Migration)
Implement the Gin router (gin.Default()).
Create routes: GET /api/pending, GET /api/mocks, POST /api/mocks, POST /api/export.
Serve the index.html file directly from the //go:embed directive using Gin.
File 3: proxy.go (HTTP)
Keep the Phase 1 logic, but ensure it sets Protocol: "HTTP" when adding to pendingMocks.
File 4: postgres.go (The Wire Protocol Engine)
Implement func StartPostgresMock(port string).
Listen on TCP. Spawn a goroutine for each connection.
Write the protocol parser for Startup, Auth, and Query.
Write a helper function sendMockedRows(conn net.Conn, jsonStr string) that parses the user's JSON string, derives the column names, constructs the binary RowDescription and DataRow messages, and writes them to the TCP connection.
File 5: ui/index.html
Update the frontend to display a badge/icon indicating if a mock is HTTP or POSTGRES.
For POSTGRES mocks, the right-side editor should hide the "Status Code" input (not applicable to SQL) and show the SQL query as the read-only identifier.
Keep the 2-second polling logic using fetch().
Please output the code file by file. For postgres.go, you do not need to implement every Postgres data type (OIDs); treating all values as strings (Text format) in the RowDescription is perfectly fine for this mock MVP.
