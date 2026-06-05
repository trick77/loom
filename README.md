<img src="frontend/src/assets/sloppy-slopr.png" alt="Slopr" width="360">

Self-hosted, multi-user LLM chat app with a Go backend, embedded React frontend, SQLite, SSE, and
authentik-backed authentication.

## Development

Required for local backend startup:

- Go 1.25
- Node.js/npm for frontend builds
- `SLOPR_SESSION_SECRET`
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

For local UI/API work, Slopr can sign in as one fixed admin user without contacting authentik.
Start both the backend and Vite frontend with:

```bash
make dev
```

The script binds both services to IPv4 loopback so Vite proxies `/api` to Slopr predictably.

To run only the backend from `backend/`:

```bash
SLOPR_SESSION_SECRET=dev-secret \
SLOPR_AUTH_MODE=dev \
SLOPR_ADDR=127.0.0.1:8080 \
SLOPR_PUBLIC_URL=http://localhost:8080 \
SLOPR_DB_PATH=/tmp/slopr-dev.db \
go run ./cmd/slopr
```

This mode is guarded at startup. `SLOPR_AUTH_MODE=dev` is rejected unless `SLOPR_ADDR` binds to
`localhost`, `127.0.0.1`, or `::1`, and `SLOPR_PUBLIC_URL` is empty or loopback. It always creates an
admin session for the fixed local development user; there is no user switcher.

## Authentik OIDC Setup

Slopr delegates sign-in to authentik through OpenID Connect. Authentik owns credentials, MFA, user
lifecycle, and group membership. Slopr stores only app-local users and opaque app sessions.

### 1. Create an authentik provider

In authentik, create an **OAuth2/OpenID Provider** for Slopr.

Use these settings:

- Flow: the default explicit consent or authorization flow used by your authentik installation.
- Client type: confidential.
- Redirect URI: `https://slopr.example.com/api/auth/callback`
- Signing key: your normal authentik signing key.
- Scopes: include `openid`, `profile`, and `email`.
- Subject mode: stable per-user subject.

Record:

- Issuer URL, for example `https://auth.example.com/application/o/slopr/`
- Client ID
- Client Secret

### 2. Create an authentik application

Create an authentik **Application** that uses the provider.

Suggested values:

- Name: `Slopr`
- Slug: `slopr`
- Launch URL: `https://slopr.example.com/`

### 3. Configure admin group mapping

Create or choose an authentik group for Slopr administrators, for example:

```text
slopr-admins
```

Ensure authentik includes a `groups` claim in the ID token for the Slopr provider. Slopr maps users
to the local `admin` role when the configured admin group appears in that claim. Everyone else is
mapped to `user`.

### 4. Configure Slopr environment

Set these variables for Slopr:

```bash
SLOPR_AUTH_MODE=oidc
SLOPR_PUBLIC_URL=https://slopr.example.com
SLOPR_SESSION_SECRET=replace-with-a-long-random-secret
SLOPR_OIDC_ISSUER=https://auth.example.com/application/o/slopr/
SLOPR_OIDC_CLIENT_ID=replace-with-authentik-client-id
SLOPR_OIDC_CLIENT_SECRET=replace-with-authentik-client-secret
SLOPR_OIDC_REDIRECT_URL=https://slopr.example.com/api/auth/callback
SLOPR_OIDC_POST_LOGOUT_REDIRECT_URL=https://slopr.example.com/
SLOPR_OIDC_ADMIN_GROUP=slopr-admins
SLOPR_CHAT_BASE_URL=http://your-mimo-host/v1
SLOPR_CHAT_API_KEY=replace-with-chat-api-key
SLOPR_CHAT_MODEL=MiMo
SLOPR_CHAT_REASONING_EFFORT=high
```

Keep secrets in environment variables or an uncommitted `.env` file. Do not commit client secrets or
session secrets.

### 5. Reverse proxy notes

Slopr OIDC does not require authentik ForwardAuth headers. Your reverse proxy only needs to route
normal HTTPS traffic to Slopr.

Required externally reachable paths:

- `/`
- `/api/auth/login`
- `/api/auth/callback`
- `/api/auth/logout`
- `/api/me`
- `/api/projects`
- `/api/threads`

The callback URL configured in authentik must exactly match `SLOPR_OIDC_REDIRECT_URL`.

## Chat Setup

Slopr supports project-less chats, projects, threads, message persistence, starred/recents, SSE
streaming, first-exchange thread naming, and MCP-backed tool calls.

Slopr uses an OpenAI-compatible chat endpoint:

```bash
SLOPR_CHAT_BASE_URL=http://your-mimo-host/v1
SLOPR_CHAT_API_KEY=replace-with-chat-api-key
SLOPR_CHAT_MODEL=MiMo
SLOPR_CHAT_REASONING_EFFORT=high
```

The backend calls `POST <SLOPR_CHAT_BASE_URL>/chat/completions` with OpenAI-compatible
`messages`, `model`, `stream`, and `reasoning_effort` fields. `SLOPR_CHAT_REASONING_EFFORT`
defaults to `high` when unset. If `SLOPR_CHAT_BASE_URL` is empty, the authenticated shell still loads
but sending a chat message returns a service-unavailable error.

### MCP Tools

Slopr exposes built-in Tavily web search when `SLOPR_TAVILY_API_KEY` is set (web search is opt-in;
without a key there is no built-in search tool). Slopr connects to Tavily's hosted MCP server at
`SLOPR_TAVILY_URL` (default `https://mcp.tavily.com/mcp/`), authenticating via the `tavilyApiKey`
query parameter, and exposes only the `tavily_search` tool.

Slopr can also expose Context7 documentation tools when `SLOPR_CONTEXT7_API_KEY` is set. It uses
the remote Streamable HTTP endpoint `https://mcp.context7.com/mcp` by default; override it with
`SLOPR_CONTEXT7_MCP_URL` if needed. Slopr sends the key as the `CONTEXT7_API_KEY` request header
and exposes the remote tools as `context7__resolve-library-id` and `context7__query-docs`.

Slopr also reads `SLOPR_MCP_CONFIG` at startup for external MCP tools. The file defaults to
`/config/mcp.json`; if it is missing, Slopr starts with only built-in tools. Each configured external
server is discovered once at boot. Servers that cannot be reached are logged and skipped, so one
unavailable MCP server does not block startup.

The default Compose setup includes two external MCP sidecars:

- `fetch` exposes the reference `mcp-server-fetch` URL/document reader as `fetch__fetch`. Use it for
  normal URL reading, article/document extraction, summarization, and quoting.
- `obscura` exposes browser automation tools as `obscura__<tool>`. Use it only when a page needs
  JavaScript rendering, visual inspection, navigation, screenshots, or interaction.

Remote MCP servers should expose a Streamable HTTP endpoint:

```json
{
  "servers": {
    "fetch": {
      "transport": "streamable-http",
      "url": "http://fetch:8090/mcp",
      "tools": ["fetch"]
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
named `<server>__<tool>`, such as `tavily__tavily_search`. During a streamed response, Slopr pauses
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

1. Start Slopr with the environment above.
2. Open `https://slopr.example.com/`.
3. Click **Sign in**.
4. Complete authentik login.
5. Confirm Slopr opens the authenticated app shell.
6. Sign in as a member of `SLOPR_OIDC_ADMIN_GROUP` and confirm admin features appear.
7. Create a new chat.
8. Send a message and confirm the assistant response streams into the conversation.
9. If MCP servers are configured and reachable, ask a question that requires a configured tool and
   confirm the tool status appears before the final answer.
10. Confirm the thread title changes from **New chat** after the first completed response.
11. Sign out and confirm returning to Slopr requires a new authenticated session.

### Logout behavior

Slopr logout revokes the local Slopr session and redirects to
`SLOPR_OIDC_POST_LOGOUT_REDIRECT_URL`. It does not currently perform RP-initiated logout against
authentik's `end_session_endpoint`. If the browser still has an active authentik SSO session, clicking
**Sign in** again can immediately create a new Slopr session without showing the authentik login form.
