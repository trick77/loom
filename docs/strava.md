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
| `get_activity` | one activity in full: laps, splits, best efforts, segment efforts, gear, device; `refresh` reads it from Strava again |
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
the sidecar answers from its store.

## Limits and storage

Strava allows 100 reads per 15 minutes and 1000 a day, per app. The sidecar reads the rate headers and refuses past
90 % with the reset time.

Activities barely change after upload, so every activity read is kept per user in `/data/activities/<loom id>/`
(0600, gzipped Strava answers) and read from there first: memory, then disk, then Strava. A restart costs no reread.

- `index.json.gz`: every activity summary ever listed. The history is read newest first in pages of 200, only as far
  back as a call needs (a window to its start, a sport filter 3 pages, the newest rows as many pages as the limit),
  and at most until half the 15-minute budget: Strava's budget is the app's, shared by every user. Later calls go on
  from there; `list_activities` says when older ones are still missing. Every 5 min at most, the 30 days before the
  newest activity are read again: uploads arrive and deletions or retags there go. An older change shows up only
  when that activity is read itself.
- `<id>.detail.json.gz`, `.streams.json.gz`, `.zones.json.gz`: what Strava answered for one activity, replaced by
  every newer answer. A detail is stored only an hour after the activity ended and an hour after a list first
  brought it (Strava matches segments after the upload, which can be a day late); streams at once. Streams are
  capped at 1 GB per user, least recently read first out.
- `refresh: true` on `get_activity` or `activity_analysis` reads the activity from Strava again: for an activity
  the user changed there (cropped, corrected, retagged). Not twice within 10 min. A refreshed detail drops the stored
  streams and metrics, so the next analysis reads them again.
- Nothing stored expires: kept for good, by design (no age limit, no knob). Activities do not change, and every
  reread costs the shared rate budget. What does remove or replace stored data: a newer read or a refresh, Strava's
  404, a head read no longer listing it, the stream cap, a corrupt file, disconnect, another athlete.

Stream-derived metrics are kept per user in `/data/metrics/<loom id>.json`; IF and TSS are derived from them with the
current FTP. A first comparison stops at half the 15-minute budget and says what is still missing. Profile, zones,
gear, routes and segments are cached in memory only (1 h).

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
rests on) and recalculated from the stored entries alone, no Strava call: on every `hr_zones` call, and on any write
touching that family's entries once `hr_zones` has created it (an analysis that reads a harder effort moves the
zones; one easy ride before anyone asked makes none). Entries carry sport and name, set under the file lock from any
list at hand (older entries lack them, a sport can be corrected); an activity deleted on Strava leaves the index with
its entry, and the zones of its sport are recalculated right there. A `hr_zones` call for one sport never creates the
other's zones. While Strava's zones are its defaults, `activity_analysis` buckets time
in zones (and the streams CSV) with the stored zones of the activity's sport and says so; custom Strava zones always
win, a sport without stored zones keeps Strava's, labelled as defaults. `profile` shows them with their date.

New stored keys bump the per-entry `entryRev`, never `metricsVersion`: a `metricsVersion` bump drops every user's
whole metrics file and re-reads every stream against the rate budget. Key presence alone can't tell "stored before
the key" from "activity has no HR"; it re-read every HR-less activity on every call.

Tokens live in `/data/state.json` (0600), one entry per loom user. Access tokens last 6 h; refresh rotates the refresh
token and the sidecar stores the new one before using it. Disconnect revokes the token at Strava and deletes the
user's entry, metrics and activities; a reconnect to another Strava athlete starts empty.

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
