Self-hosted, multi-user LLM chat app with a Go backend, React frontend, SQLite, SSE, and
authentik-backed authentication.

## Development

Required for local backend startup:

- Go 1.25
- Node.js/npm for frontend builds
- `BACKEND_SESSION_SECRET`
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

For local UI/API work, Loom can sign in as one fixed admin user without contacting authentik.
Start both the backend and Vite frontend with:

```bash
make dev
```

The script binds both services to IPv4 loopback so Vite proxies `/api` to Loom predictably.

To run only the backend from `backend/`:

```bash
BACKEND_SESSION_SECRET=dev-secret \
BACKEND_AUTH_MODE=dev \
BACKEND_ADDR=127.0.0.1:8080 \
BACKEND_PUBLIC_URL=http://localhost:8080 \
BACKEND_DB_PATH=/tmp/loom-dev.db \
go run ./cmd/loom
```

This mode is guarded at startup. `BACKEND_AUTH_MODE=dev` is rejected unless `BACKEND_ADDR` binds to
`localhost`, `127.0.0.1`, or `::1`, and `BACKEND_PUBLIC_URL` is empty or loopback. It always creates an
admin session for the fixed local development user; there is no user switcher.

## Authentik OIDC Setup

Loom delegates sign-in to authentik through OpenID Connect. Authentik owns credentials, MFA, user
lifecycle, and group membership. Loom stores only app-local users and opaque app sessions.

### 1. Create an authentik provider

In authentik, create an **OAuth2/OpenID Provider** for Loom.

Use these settings:

- Flow: the default explicit consent or authorization flow used by your authentik installation.
- Client type: confidential.
- Redirect URI: `https://loom.example.com/api/auth/callback`
- Signing key: your normal authentik signing key.
- Scopes: include `openid`, `profile`, and `email`.
- Subject mode: stable per-user subject.

Record:

- Issuer URL, for example `https://auth.example.com/application/o/loom/`
- Client ID
- Client Secret

### 2. Create an authentik application

Create an authentik **Application** that uses the provider.

Suggested values:

- Name: `Loom`
- Slug: `loom`
- Launch URL: `https://loom.example.com/`

The application **Name** and **Description** you set here are rendered by authentik on its
login/consent page — the screen users land on after clicking "Sign in". Loom does not control
that text, so set them to match your branding (e.g. Name `Loom`, Description
"Where your ideas come together.").

### 3. Configure admin group mapping

Create or choose an authentik group for Loom administrators, for example:

```text
loom-admins
```

Ensure authentik includes a `groups` claim in the ID token for the Loom provider. Loom maps users
to the local `admin` role when the configured admin group appears in that claim. Everyone else is
mapped to `user`.

### 4. Configure Loom environment

Set these variables for Loom:

```bash
BACKEND_AUTH_MODE=oidc
BACKEND_PUBLIC_URL=https://loom.example.com
BACKEND_SESSION_SECRET=replace-with-a-long-random-secret
BACKEND_OIDC_ISSUER=https://auth.example.com/application/o/loom/
BACKEND_OIDC_CLIENT_ID=replace-with-authentik-client-id
BACKEND_OIDC_CLIENT_SECRET=replace-with-authentik-client-secret
BACKEND_OIDC_REDIRECT_URL=https://loom.example.com/api/auth/callback
BACKEND_OIDC_POST_LOGOUT_REDIRECT_URL=https://loom.example.com/
BACKEND_OIDC_ADMIN_GROUP=loom-admins
BACKEND_CHAT_BASE_URL=http://your-mimo-host/v1
BACKEND_CHAT_API_KEY=replace-with-chat-api-key
BACKEND_CHAT_MAX_COMPLETION_TOKENS=2048
BACKEND_CHAT_TIMEOUT=2m
BACKEND_CHAT_IDLE_TIMEOUT=60s
```

Keep secrets in environment variables or an uncommitted `.env` file. Do not commit client secrets or
session secrets.

### 5. Reverse proxy notes

Loom OIDC does not require authentik ForwardAuth headers. In the production Compose stack, the
nginx UI container is the only externally reachable service and proxies `/api/*` to the backend over
the internal Compose network. Any outer reverse proxy only needs to route normal HTTPS traffic to
the UI container.

Required externally reachable paths:

- `/`
- `/api/auth/login`
- `/api/auth/callback`
- `/api/auth/logout`
- `/api/me`
- `/api/projects`
- `/api/threads`

The callback URL configured in authentik must exactly match `BACKEND_OIDC_REDIRECT_URL`.

## Chat Setup

Loom supports project-less threads, projects, threads, message persistence, starred/recents, SSE
streaming, first-exchange thread naming, and MCP-backed tool calls.

Loom uses an OpenAI-compatible chat endpoint:

```bash
BACKEND_CHAT_BASE_URL=http://your-mimo-host/v1
BACKEND_CHAT_API_KEY=replace-with-chat-api-key
BACKEND_CHAT_MAX_COMPLETION_TOKENS=2048
BACKEND_CHAT_TIMEOUT=2m
BACKEND_CHAT_IDLE_TIMEOUT=60s
```

Loom targets MiMo specifically: the model and reasoning effort are hardcoded and no longer
configurable. Text-only turns use `mimo-v2.5-pro`; turns that include an image attachment are
routed to the omnimodal `mimo-v2.5` (the text-only Pro model rejects image input). Both are served
from the same `BACKEND_CHAT_BASE_URL`, selected per request via the `model` field, and
`reasoning_effort` is fixed at `high`.

The backend calls `POST <BACKEND_CHAT_BASE_URL>/chat/completions` with OpenAI-compatible
`messages`, `model`, `stream`, `reasoning_effort`, and `max_completion_tokens` fields.
`BACKEND_CHAT_MAX_COMPLETION_TOKENS`
defaults to `2048`, and `BACKEND_CHAT_TIMEOUT` defaults to `2m`, so long-running reasoning streams are
bounded even at high reasoning effort. `BACKEND_CHAT_IDLE_TIMEOUT`
defaults to `60s` and aborts a stream that goes silent mid-turn (a stalled upstream) far sooner than
the total timeout; set it to `0` to disable the idle watchdog. If `BACKEND_CHAT_BASE_URL` is
empty, the authenticated shell still loads but sending a thread message returns a service-unavailable
error.

### MCP Tools

Loom exposes built-in Tavily web search when `BACKEND_TAVILY_API_KEY` is set (web search is opt-in;
without a key there is no built-in search tool). Loom connects to Tavily's hosted MCP server at
`BACKEND_TAVILY_URL` (default `https://mcp.tavily.com/mcp/`), authenticating via the `tavilyApiKey`
query parameter, and exposes only the `tavily_search` tool.

Loom can also expose Context7 documentation tools when `BACKEND_CONTEXT7_API_KEY` is set. It uses
the remote Streamable HTTP endpoint `https://mcp.context7.com/mcp` by default; override it with
`BACKEND_CONTEXT7_MCP_URL` if needed. Loom sends the key as the `CONTEXT7_API_KEY` request header
and exposes the remote tools as `context7__resolve-library-id` and `context7__query-docs`.

The default Compose setup also includes two MCP sidecars configured with first-class env vars:

- `fetch` exposes the reference `mcp-server-fetch` URL/document reader as `fetch__fetch`. Use it for
  normal URL reading, article/document extraction, summarization, and quoting. Configure its endpoint
  with `BACKEND_FETCH_MCP_URL`.
- `obscura` exposes browser automation tools as `obscura__<tool>`. Use it only when a page needs
  JavaScript rendering, visual inspection, navigation, screenshots, or interaction. Compose runs the
  official Obscura image with native MCP HTTP bound to `0.0.0.0:8090`. Configure its endpoint with
  `BACKEND_OBSCURA_MCP_URL`.

Beyond these first-class integrations, you can register **additional MCP servers from a JSON file**
without rebuilding. Point `BACKEND_MCP_SERVERS_FILE` at a file in the standard `mcpServers` format
(default `/data/mcp.json`); its servers are discovered at startup and merged on top of the built-ins,
overriding any built-in of the same name. Keep secrets out of the file — every string value supports
`${VAR}` interpolation resolved from the backend environment, so tokens stay in env vars. A missing
referenced variable, an unknown `type`, or malformed JSON fails fast at startup; an absent file is a
no-op. For example, to add ipverse-lens whois search:

```json
{
  "mcpServers": {
    "ipverse": {
      "type": "http",
      "url": "https://gateway.ipverse.net/mcp",
      "headers": { "Authorization": "Bearer ${IPVERSE_API_KEY}" }
    }
  }
}
```

with `IPVERSE_API_KEY` set in the loom container's environment. See `mcp.json.example`. `type` accepts
`http` (Streamable HTTP, the default) or `stdio`.

Configured MCP tools are exposed to the chat model as OpenAI-compatible function tools
named `<server>__<tool>`, such as `tavily__tavily_search`. During a streamed response, Loom pauses
when the model emits `tool_calls`, runs the requested tools, streams tool status events to the UI,
appends tool results to the model history, and resumes the assistant stream.

### Current Phase 4 Scope

Implemented now:

- Authenticated project/thread/message API.
- OpenAI-compatible streaming chat.
- Project-less new threads.
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

1. Start Loom with the environment above.
2. Open `https://loom.example.com/`.
3. Click **Sign in**.
4. Complete authentik login.
5. Confirm Loom opens the authenticated app shell.
6. Sign in as a member of `BACKEND_OIDC_ADMIN_GROUP` and confirm admin features appear.
7. Create a new thread.
8. Send a message and confirm the assistant response streams into the conversation.
9. If MCP servers are configured and reachable, ask a question that requires a configured tool and
   confirm the tool status appears before the final answer.
10. Confirm the thread title changes from **New thread** after the first completed response.
11. Sign out and confirm returning to Loom requires a new authenticated session.

### Logout behavior

Loom logout revokes the local Loom session and redirects to
`BACKEND_OIDC_POST_LOGOUT_REDIRECT_URL`. It does not currently perform RP-initiated logout against
authentik's `end_session_endpoint`. If the browser still has an active authentik SSO session, clicking
**Sign in** again can immediately create a new Loom session without showing the authentik login form.
