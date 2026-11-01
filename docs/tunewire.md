# loom-tunewire: Apple Music

`loom-tunewire` (`backend/cmd/loom-tunewire`, image `loom-tunewire`) gives loom two MCP servers:

| loom server | path | tools | who |
|---|---|---|---|
| `tunewire` | `/mcp/library` | `list_playlists`, `get_playlist` | `BACKEND_TUNEWIRE_USERS` only |
| `applemusic` | `/mcp/catalog` | `search_catalog` (songs, albums, artists with links and cover art) | every user |

Both are best-effort: loom boots without the sidecar and offers the tools while it answers.

## How it reaches Apple Music

There is no paid Apple developer account, so the sidecar reads the API the way music.apple.com's web player does:

- **Developer token**: a JWT (`iss: AMPWebPlay`) in the player's `/assets/index~<hash>.js`. The sidecar fetches the
  player page, finds the bundle, pulls the token out and renews it a day before it expires, or when a probe without the
  user token shows Apple refusing it. It keeps the token in `/data/state.json` across restarts.
- **User token**: the `media-user-token` cookie of a signed-in music.apple.com session, pasted in loom's settings
  (Settings → Apple Music). Loom passes it to the sidecar (`PUT /token`). The sidecar checks it with Apple
  (`/v1/me/storefront`) before storing it in `/data/state.json` (0600). Loom never stores or logs it.

Requests go to `amp-api.music.apple.com` with the web player's `Origin`. Only GET requests exist. This is unofficial:
when Apple changes the page or the API, the sidecar breaks until it is fixed. The developer token's errors name the step
that failed (player page, bundle, no token).

## Setup

In `.env` (see `.env.example`):

```
COMPOSE_PROFILES=tunewire
BACKEND_TUNEWIRE_URL=http://tunewire:8060
BACKEND_TUNEWIRE_TOKEN=<openssl rand -hex 32>
BACKEND_TUNEWIRE_USERS=<your loom user id>
```

Then sign in to loom, open Settings → Apple Music and paste the cookie value: music.apple.com → developer tools →
Application (Safari: Storage) → Cookies → `https://music.apple.com` → `media-user-token`.

## When the cookie expires

Apple doesn't document how long the cookie lasts; expect months. A password change or a lapsed subscription ends it
early. The tools then answer "Apple Music sign-in expired" and the settings section shows *Expired*: paste a new one.
Catalog search keeps working, since it needs only the developer token.

## Sidecar routes

All need `Authorization: Bearer <TUNEWIRE_TOKEN>`: `GET /healthz`, `POST /mcp/library`, `POST /mcp/catalog`,
`GET /status[?check=1]`, `PUT /token`, `DELETE /token`.
