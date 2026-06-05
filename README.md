# slop

Self-hosted, multi-user LLM chat app with a Go backend, embedded React frontend, SQLite, SSE, and
authentik-backed authentication.

## Development

Required for local backend startup:

- Go 1.25
- Node.js/npm for frontend builds
- `SLOP_SESSION_SECRET`
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

For local UI/API work, Slop can sign in as one fixed admin user without contacting authentik.
Start both the backend and Vite frontend with:

```bash
make dev
```

The script binds both services to IPv4 loopback so Vite proxies `/api` to Slop predictably.

To run only the backend from `backend/`:

```bash
SLOP_SESSION_SECRET=dev-secret \
SLOP_AUTH_MODE=dev \
SLOP_ADDR=127.0.0.1:8080 \
SLOP_PUBLIC_URL=http://localhost:8080 \
SLOP_DB_PATH=/tmp/slop-dev.db \
go run ./cmd/slop
```

This mode is guarded at startup. `SLOP_AUTH_MODE=dev` is rejected unless `SLOP_ADDR` binds to
`localhost`, `127.0.0.1`, or `::1`, and `SLOP_PUBLIC_URL` is empty or loopback. It always creates an
admin session for the fixed local development user; there is no user switcher.

## Authentik OIDC Setup

Slop delegates sign-in to authentik through OpenID Connect. Authentik owns credentials, MFA, user
lifecycle, and group membership. Slop stores only app-local users and opaque app sessions.

### 1. Create an authentik provider

In authentik, create an **OAuth2/OpenID Provider** for Slop.

Use these settings:

- Flow: the default explicit consent or authorization flow used by your authentik installation.
- Client type: confidential.
- Redirect URI: `https://slop.example.com/api/auth/callback`
- Signing key: your normal authentik signing key.
- Scopes: include `openid`, `profile`, and `email`.
- Subject mode: stable per-user subject.

Record:

- Issuer URL, for example `https://auth.example.com/application/o/slop/`
- Client ID
- Client Secret

### 2. Create an authentik application

Create an authentik **Application** that uses the provider.

Suggested values:

- Name: `Slop`
- Slug: `slop`
- Launch URL: `https://slop.example.com/`

### 3. Configure admin group mapping

Create or choose an authentik group for Slop administrators, for example:

```text
slop-admins
```

Ensure authentik includes a `groups` claim in the ID token for the Slop provider. Slop maps users
to the local `admin` role when the configured admin group appears in that claim. Everyone else is
mapped to `user`.

### 4. Configure Slop environment

Set these variables for Slop:

```bash
SLOP_AUTH_MODE=oidc
SLOP_PUBLIC_URL=https://slop.example.com
SLOP_SESSION_SECRET=replace-with-a-long-random-secret
SLOP_OIDC_ISSUER=https://auth.example.com/application/o/slop/
SLOP_OIDC_CLIENT_ID=replace-with-authentik-client-id
SLOP_OIDC_CLIENT_SECRET=replace-with-authentik-client-secret
SLOP_OIDC_REDIRECT_URL=https://slop.example.com/api/auth/callback
SLOP_OIDC_POST_LOGOUT_REDIRECT_URL=https://slop.example.com/
SLOP_OIDC_ADMIN_GROUP=slop-admins
SLOP_CHAT_BASE_URL=http://your-mimo-host/v1
SLOP_CHAT_API_KEY=replace-with-chat-api-key
SLOP_CHAT_MODEL=MiMo
SLOP_CHAT_REASONING_EFFORT=high
```

Keep secrets in environment variables or an uncommitted `.env` file. Do not commit client secrets or
session secrets.

### 5. Reverse proxy notes

Slop OIDC does not require authentik ForwardAuth headers. Your reverse proxy only needs to route
normal HTTPS traffic to Slop.

Required externally reachable paths:

- `/`
- `/api/auth/login`
- `/api/auth/callback`
- `/api/auth/logout`
- `/api/me`
- `/api/projects`
- `/api/threads`

The callback URL configured in authentik must exactly match `SLOP_OIDC_REDIRECT_URL`.

## Chat Setup

Slop supports project-less chats, projects, threads, message persistence, starred/recents, SSE
streaming, first-exchange thread naming, and MCP-backed tool calls.

Slop uses an OpenAI-compatible chat endpoint:

```bash
SLOP_CHAT_BASE_URL=http://your-mimo-host/v1
SLOP_CHAT_API_KEY=replace-with-chat-api-key
SLOP_CHAT_MODEL=MiMo
SLOP_CHAT_REASONING_EFFORT=high
```

The backend calls `POST <SLOP_CHAT_BASE_URL>/chat/completions` with OpenAI-compatible
`messages`, `model`, `stream`, and `reasoning_effort` fields. `SLOP_CHAT_REASONING_EFFORT`
defaults to `high` when unset. If `SLOP_CHAT_BASE_URL` is empty, the authenticated shell still loads
but sending a chat message returns a service-unavailable error.

### MCP Tools

Slop exposes built-in Tavily web search when `SLOP_TAVILY_API_KEY` is set (web search is opt-in;
without a key there is no built-in search tool). Slop connects to Tavily's hosted MCP server at
`SLOP_TAVILY_URL` (default `https://mcp.tavily.com/mcp/`), authenticating via the `tavilyApiKey`
query parameter, and exposes only the `tavily_search` tool.

Slop can also expose Context7 documentation tools when `SLOP_CONTEXT7_API_KEY` is set. It uses
the remote Streamable HTTP endpoint `https://mcp.context7.com/mcp` by default; override it with
`SLOP_CONTEXT7_MCP_URL` if needed. Slop sends the key as the `CONTEXT7_API_KEY` request header
and exposes the remote tools as `context7__resolve-library-id` and `context7__query-docs`.

Slop also reads `SLOP_MCP_CONFIG` at startup for external MCP tools. The file defaults to
`/config/mcp.json`; if it is missing, Slop starts with only built-in tools. Each configured external
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
named `<server>__<tool>`, such as `tavily__tavily_search`. During a streamed response, Slop pauses
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

1. Start Slop with the environment above.
2. Open `https://slop.example.com/`.
3. Click **Sign in**.
4. Complete authentik login.
5. Confirm Slop opens the authenticated app shell.
6. Sign in as a member of `SLOP_OIDC_ADMIN_GROUP` and confirm admin features appear.
7. Create a new chat.
8. Send a message and confirm the assistant response streams into the conversation.
9. If MCP servers are configured and reachable, ask a question that requires a configured tool and
   confirm the tool status appears before the final answer.
10. Confirm the thread title changes from **New chat** after the first completed response.
11. Sign out and confirm returning to Slop requires a new authenticated session.

### Logout behavior

Slop logout revokes the local Slop session and redirects to
`SLOP_OIDC_POST_LOGOUT_REDIRECT_URL`. It does not currently perform RP-initiated logout against
authentik's `end_session_endpoint`. If the browser still has an active authentik SSO session, clicking
**Sign in** again can immediately create a new Slop session without showing the authentik login form.
