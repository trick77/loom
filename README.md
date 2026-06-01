# spark

Self-hosted, multi-user LLM chat app with a Go backend, embedded React frontend, SQLite, SSE, and
authentik-backed authentication.

## Development

Required for local backend startup:

- Go 1.25
- Node.js/npm for frontend builds
- `SPARK_SESSION_SECRET`
- OIDC configuration for authentik, or local-only development auth

Common commands:

```bash
make test
make fe-test
make fe-build
make build
make dev
```

### Local development without OIDC

For local UI/API work, Spark can sign in as one fixed admin user without contacting authentik.
Start both the backend and Vite frontend with:

```bash
make dev
```

The script binds both services to IPv4 loopback so Vite proxies `/api` to Spark predictably.

To run only the backend from `backend/`:

```bash
SPARK_SESSION_SECRET=dev-secret \
SPARK_AUTH_MODE=dev \
SPARK_ADDR=127.0.0.1:8080 \
SPARK_PUBLIC_URL=http://localhost:8080 \
SPARK_DB_PATH=/tmp/spark-dev.db \
go run ./cmd/spark
```

This mode is guarded at startup. `SPARK_AUTH_MODE=dev` is rejected unless `SPARK_ADDR` binds to
`localhost`, `127.0.0.1`, or `::1`, and `SPARK_PUBLIC_URL` is empty or loopback. It always creates an
admin session for the fixed local development user; there is no user switcher.

## Authentik OIDC Setup

Spark delegates sign-in to authentik through OpenID Connect. Authentik owns credentials, MFA, user
lifecycle, and group membership. Spark stores only app-local users and opaque app sessions.

### 1. Create an authentik provider

In authentik, create an **OAuth2/OpenID Provider** for Spark.

Use these settings:

- Flow: the default explicit consent or authorization flow used by your authentik installation.
- Client type: confidential.
- Redirect URI: `https://spark.example.com/api/auth/callback`
- Signing key: your normal authentik signing key.
- Scopes: include `openid`, `profile`, and `email`.
- Subject mode: stable per-user subject.

Record:

- Issuer URL, for example `https://auth.example.com/application/o/spark/`
- Client ID
- Client Secret

### 2. Create an authentik application

Create an authentik **Application** that uses the provider.

Suggested values:

- Name: `Spark`
- Slug: `spark`
- Launch URL: `https://spark.example.com/`

### 3. Configure admin group mapping

Create or choose an authentik group for Spark administrators, for example:

```text
spark-admins
```

Ensure authentik includes a `groups` claim in the ID token for the Spark provider. Spark maps users
to the local `admin` role when the configured admin group appears in that claim. Everyone else is
mapped to `user`.

### 4. Configure Spark environment

Set these variables for Spark:

```bash
SPARK_AUTH_MODE=oidc
SPARK_PUBLIC_URL=https://spark.example.com
SPARK_SESSION_SECRET=replace-with-a-long-random-secret
SPARK_OIDC_ISSUER=https://auth.example.com/application/o/spark/
SPARK_OIDC_CLIENT_ID=replace-with-authentik-client-id
SPARK_OIDC_CLIENT_SECRET=replace-with-authentik-client-secret
SPARK_OIDC_REDIRECT_URL=https://spark.example.com/api/auth/callback
SPARK_OIDC_POST_LOGOUT_REDIRECT_URL=https://spark.example.com/
SPARK_OIDC_ADMIN_GROUP=spark-admins
SPARK_CHAT_BASE_URL=http://your-mimo-host/v1
SPARK_CHAT_API_KEY=replace-with-chat-api-key
SPARK_CHAT_MODEL=MiMo
SPARK_CHAT_REASONING_EFFORT=high
```

Keep secrets in environment variables or an uncommitted `.env` file. Do not commit client secrets or
session secrets.

### 5. Reverse proxy notes

Spark OIDC does not require authentik ForwardAuth headers. Your reverse proxy only needs to route
normal HTTPS traffic to Spark.

Required externally reachable paths:

- `/`
- `/api/auth/login`
- `/api/auth/callback`
- `/api/auth/logout`
- `/api/me`
- `/api/projects`
- `/api/threads`

The callback URL configured in authentik must exactly match `SPARK_OIDC_REDIRECT_URL`.

## Chat Setup

Spark supports project-less chats, projects, threads, message persistence, starred/recents, SSE
streaming, first-exchange thread naming, and MCP-backed tool calls.

Spark uses an OpenAI-compatible chat endpoint:

```bash
SPARK_CHAT_BASE_URL=http://your-mimo-host/v1
SPARK_CHAT_API_KEY=replace-with-chat-api-key
SPARK_CHAT_MODEL=MiMo
SPARK_CHAT_REASONING_EFFORT=high
```

The backend calls `POST <SPARK_CHAT_BASE_URL>/chat/completions` with OpenAI-compatible
`messages`, `model`, `stream`, and `reasoning_effort` fields. `SPARK_CHAT_REASONING_EFFORT`
defaults to `high` when unset. If `SPARK_CHAT_BASE_URL` is empty, the authenticated shell still loads
but sending a chat message returns a service-unavailable error.

### MCP Tools

Spark exposes built-in SearXNG web search when `SPARK_SEARXNG_URL` is set. The bundled
`compose.yaml` points this at the `searxng` service directly. SearXNG uses
`searxng/settings.yaml`, which enables JSON output required by Spark's search adapter.

Spark also reads `SPARK_MCP_CONFIG` at startup for external MCP tools. The file defaults to
`/config/mcp.json`; if it is missing, Spark starts with only built-in tools. Each configured external
server is discovered once at boot. Servers that cannot be reached are logged and skipped, so one
unavailable MCP server does not block startup.

Remote MCP servers should expose a Streamable HTTP endpoint:

```json
{
  "servers": {
    "search": {
      "transport": "streamable-http",
      "url": "http://search-mcp:8080/mcp"
    }
  }
}
```

Local stdio servers are also supported:

```json
{
  "servers": {
    "fetch": {
      "transport": "stdio",
      "command": "docker",
      "args": ["run", "-i", "--rm", "mcp/fetch"]
    }
  }
}
```

Built-in and discovered MCP tools are exposed to the chat model as OpenAI-compatible function tools
named `<server>__<tool>`, such as `searxng__web_search`. During a streamed response, Spark pauses
when the model emits `tool_calls`, runs the requested tools, streams tool status events to the UI,
appends tool results to the model history, and resumes the assistant stream.

### Current Phase 4 Scope

Implemented now:

- Authenticated project/thread/message API.
- OpenAI-compatible streaming chat.
- Project-less new chats.
- Starred and recent thread lists.
- Automatic thread naming after the first completed assistant response.
- MCP config loading for Streamable HTTP and stdio servers.
- MCP tool discovery and tool execution.
- OpenAI-compatible tool schemas and streamed tool-call parsing.
- Tool progress display while an answer is streaming.

Still planned for later phases:

- Document upload, RAG, citations, and Sources population.
- Artifacts file browser.
- Memory extraction, storage, and injection.

### Smoke Test

1. Start Spark with the environment above.
2. Open `https://spark.example.com/`.
3. Click **Sign in**.
4. Complete authentik login.
5. Confirm Spark opens the authenticated app shell.
6. Sign in as a member of `SPARK_OIDC_ADMIN_GROUP` and confirm admin features appear.
7. Create a new chat.
8. Send a message and confirm the assistant response streams into the conversation.
9. If MCP servers are configured and reachable, ask a question that requires a configured tool and
   confirm the tool status appears before the final answer.
10. Confirm the thread title changes from **New chat** after the first completed response.
11. Sign out and confirm returning to Spark requires a new authenticated session.

### Logout behavior

Spark logout revokes the local Spark session and redirects to
`SPARK_OIDC_POST_LOGOUT_REDIRECT_URL`. It does not currently perform RP-initiated logout against
authentik's `end_session_endpoint`. If the browser still has an active authentik SSO session, clicking
**Sign in** again can immediately create a new Spark session without showing the authentik login form.
