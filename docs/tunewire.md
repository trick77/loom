# loom-tunewire: Apple Music

`loom-tunewire` (`backend/cmd/loom-tunewire`, image `loom-tunewire`) gives loom two MCP servers:

| loom server | path | tools | who |
|---|---|---|---|
| `tunewire` | `/mcp/library` | `list_playlists`, `get_playlist`, `recently_played`, `new_music`, `new_releases` | `BACKEND_TUNEWIRE_USERS` only |
| `applemusic` | `/mcp/catalog` | `search_catalog` (songs, albums, artists with links and cover art) | every user |

Both are best-effort: loom boots without the sidecar and offers the tools while it answers.

Listening tools (owner's storefront, all GET):

| tool | reads |
|---|---|
| `recently_played` | `kind=recent`: `/v1/me/recent/played/tracks` (50 newest); `kind=heavy_rotation`: `/v1/me/history/heavy-rotation` |
| `new_music` | `section=recently_added`: `/v1/me/library/recently-added`; `for_you`: `/v1/me/recommendations` plus the New Music Mix's tracks; default both |
| `new_releases` | up to 50 artists, most listened first, then library order (`/v1/me/library/artists?include=catalog`); per artist `/v1/catalog/{sf}/artists/{id}/view/latest-release`, 4 at a time. `days` 1-120 (30); pre-releases always shown |

`new_releases` keeps each artist's latest release in memory for 3 h (at most 1000) and the library's artist list for
30 min, so a follow-up costs no lookups. Out of time (~20 s), it answers with what it has and says the list is partial.

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
BACKEND_TUNEWIRE_USERS=<loom user ids, comma-separated: the "id" of /api/me>
```

Only those accounts see the settings section and the playlist tools; they share the one Apple Music token. An
`mcp.json` entry named `tunewire` may replace the endpoint, never these users.

Then sign in to loom, open Settings → Apple Music and paste the cookie value: music.apple.com → developer tools →
Application (Safari: Storage) → Cookies → `https://music.apple.com` → `media-user-token`.

## When the cookie expires

Apple doesn't document how long the cookie lasts; expect months. A password change or a lapsed subscription ends it
early. The tools then answer "Apple Music sign-in expired" and the settings section shows *Expired*: paste a new one.
Catalog search keeps working, since it needs only the developer token.

## Sidecar routes

`POST /mcp/library`, `POST /mcp/catalog`, `GET /status[?check=1]`, `PUT /token` and `DELETE /token` need
`Authorization: Bearer <TUNEWIRE_TOKEN>`. `GET /healthz` and `/readyz` are kit's probes: no token, but they answer only
a caller on loopback (the container's own `loom-tunewire healthcheck`), 404 to anyone else.
