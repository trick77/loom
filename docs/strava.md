# loom-strava: Strava

`loom-strava` (`backend/cmd/loom-strava`, image `loom-strava`) gives loom one MCP server, `strava`, over each user's
own Strava data: profile and zones, totals, every activity (Only Me ones too), laps, splits, best and segment efforts,
streams turned into metrics, gear, own routes, starred segments. Zwift rides arrive through Strava's own upload.

| loom server | path | who |
|---|---|---|
| `strava` | `/mcp` | `BACKEND_STRAVA_USERS` only, each their own Strava account |

Offered in `training` and `health` turns. Best-effort: loom boots without the sidecar and offers the tools while it
answers.

## Own account, own data

Every listed loom user connects their own Strava account; nothing is shared. Every call carries the caller's loom id
(`X-Loom-User`) and reads that user's connection only; a tool call without one reads nothing. The header is trusted
because only loom holds the bearer token. Only the user's own data is read: no clubs, other athletes, kudoers or
comment text, segment explore or leaderboards, and no write scope.

## Tools

| tool | reads |
|---|---|
| `profile` | athlete, FTP, weight, bikes and shoes, HR and power zones |
| `totals` | recent, year-to-date and all-time totals (Strava counts only Everyone-visible activities here) |
| `list_activities` | activities by date range and sport, with totals per sport and week; attaches `strava_activities.csv` |
| `get_activity` | one activity in full: laps, splits, best efforts, segment efforts, gear, device |
| `activity_analysis` | metrics from the streams (NP, IF, TSS, power curve, zones, decoupling, climbs, intervals, per-km rows), always compared with the user's same-sport activities of the last `weeks` (default 6); attaches the CSVs below |
| `compare_activities` | 2-5 activities side by side |
| `hr_zones` | max HR, threshold HR (LTHR) and Friel zones per sport (cycling, running) from the user's own streams of the last 365 days, next to Strava's zones |
| `gear`, `routes`, `segments` | gear details, own routes, starred segments and own efforts (efforts need a Strava subscription) |

Each activity named comes with a link to it on Strava (citation and attribution).

### Charts

`activity_analysis` and `list_activities` attach CSV files (`strava_<id>_streams.csv`, `_climbs`, `_power_curve`,
`_baseline`, `_same_route`, `strava_weeks.csv`, `strava_activities.csv`; columns in `sandbox/loomcharts/__init__.py`).
loom keeps them for the running turn only and hands them to `run_python` by name; they are never stored, shown or put
into the prompt. `loomcharts` in the sandbox image draws them in loom's style. A follow-up turn asks the tool again:
the sidecar answers from its cache.

## Limits and storage

Strava allows 100 reads per 15 minutes and 1000 a day, per app. The sidecar reads the rate headers and refuses past
90 % with the reset time. Activity details and streams are cached in memory for 24 h; list pages 5 min; profile and
gear 1 h. Stream-derived metrics are kept per user in `/data/metrics/<loom id>.json` (no raw streams), so each
activity's streams are fetched once; a first comparison stops at half the 15-minute budget and says what is still
missing.

### Heart-rate zones

Strava's default zones (`custom_zones: false`) are mostly 220 minus age: about a zone off for trained athletes, whose
threshold sits higher relative to max. `hr_zones` derives them instead from two stored values per activity, `hr_max10`
(best 10 s mean HR) and `hr_20m` (best 20 min): max HR is the highest `hr_max10` a second activity comes within 5 bpm
of (a strap spike or cadence lock is one activity's), LTHR is 95 % of the best `hr_20m` (Friel's 20-minute test), on
rides checked against FTP via `hr_20m_w`, the power over those same 20 minutes (a ride's best 20-min power can be a
different stretch). Families are explicit: cycling = Ride, GravelRide,
MountainBikeRide, VirtualRide; running = Run, TrailRun, VirtualRun (5-10 bpm higher); e-bike, walk, hike are out. It
reads streams only for the hardest activities not read yet (top max HR and top 20-min-plus average HR, in turns),
within the same budget; a later call reads more.

The result is stored per family in the user's metrics file (`hr_zones`, with `calculated_at` and the activities it
rests on) and recalculated from the stored entries alone, no Strava call, on every write of them (an analysis that
reads a harder effort moves the zones) and on every `hr_zones` call. Entries carry sport and name for that; ones
stored before get both from `hr_zones`' list. While Strava's zones are its defaults, `activity_analysis` buckets time
in zones (and the streams CSV) with the stored zones of the activity's sport and says so; custom Strava zones always
win, a sport without stored zones keeps Strava's, labelled as defaults. `profile` shows them with their date.

New stored keys bump the per-entry `entryRev`, never `metricsVersion`: a `metricsVersion` bump drops every user's
whole metrics file and re-reads every stream against the rate budget. Key presence alone can't tell "stored before
the key" from "activity has no HR"; it re-read every HR-less activity on every call.

Tokens live in `/data/state.json` (0600), one entry per loom user. Access tokens last 6 h; refresh rotates the refresh
token and the sidecar stores the new one before using it. Disconnect revokes the token at Strava and deletes the
user's entry and metrics.

## Setup

1. Create a Strava API app at <https://www.strava.com/settings/api>. Authorization Callback Domain: your `PUBLIC_FQDN`
   (`localhost` for dev). A new app may connect 1 athlete; raise it to 10 there (no review) when more listed users
   connect.
2. In `.env` (see `.env.example`):

   ```
   COMPOSE_PROFILES=strava        # with others: tunewire,strava
   BACKEND_STRAVA_URL=http://strava:8050
   BACKEND_STRAVA_TOKEN=<openssl rand -hex 32>
   BACKEND_STRAVA_USERS=<loom user ids, comma-separated: the "id" of /api/me>
   STRAVA_CLIENT_ID=<the app's client id>
   STRAVA_CLIENT_SECRET=<the app's client secret>
   ```

3. Each listed user signs in to loom, opens Settings → Strava and clicks Connect with Strava, leaving every permission
   ticked (unticked ones show up in Settings: private activities or details then stay hidden).

An `mcp.json` entry named `strava` may replace the endpoint, never these users or the per-user accounts.

## Sidecar routes

All need `Authorization: Bearer <STRAVA_TOKEN>`; all but `/mcp` discovery need `X-Loom-User`.
`POST /mcp`, `GET /status[?check=1]`, `GET /authorize?redirect_uri=&state=`, `PUT /connection` (`{code, scope}`),
`DELETE /connection`. `GET /healthz` and `/readyz` are kit's probes: no token, loopback only. The browser never talks to
the sidecar: Strava sends it back to loom's `/api/strava/callback`, which checks the state cookie and hands the code on.
