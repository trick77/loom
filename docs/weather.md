# weather: the weather contract

loom gets weather through MCP tools on the server `weather`: `forecast` (`weather__forecast`), `geocode`
(`weather__geocode`) and, optional, `air_quality` (`weather__air_quality`). The `loom-weather` sidecar (`backend/cmd/loom-weather`, image `loom-weather`) serves them with
Open-Meteo; an `mcp.json` entry named `weather` replaces the sidecar with any server that answers this contract. loom
keeps the policy, never the provider: the start page (`GET /api/today`) asks `forecast` for the user's place, which
`geocode` found from the place their memory names; in a chat turn the tools are offered on weather questions only (classifier
category `weather`, kept by an `mcp.json` override that sets no categories of its own). The forecast carries no
"now": a reader takes it from the hourly values. When to go out for a run or a ride is loom's own (Outdoor windows).

## Tools

Both answer with one JSON object as text.

`forecast` takes `latitude`, `longitude` (required), `timezone` (IANA, default the place's own) and `days` (1-7,
default 2, today first). `daily` covers `days` days, `hourly` the first 3 of them at most, from the current hour:

```json
{"latitude": 47.38, "longitude": 8.54, "timezone": "Europe/Zurich",
 "hourly": [{"time": "2026-11-09T09:00", "temperature": 6.2, "code": 3, "precipitationProbability": 10,
             "precipitation": 0.1, "windSpeed": 9, "windGusts": 21, "windDirection": 250,
             "apparentTemperature": 3.8, "humidity": 87, "sunshineMinutes": 25}],
 "daily": [{"date": "2026-11-09", "high": 11.2, "low": 3.1, "code": 3, "precipitationProbability": 10,
            "precipitationSum": 0.3, "uvIndexMax": 1.6, "sunshineHours": 4.5, "windSpeedMax": 12.5,
            "windGustsMax": 31, "windDirection": 248}],
 "attribution": {"text": "Weather data by Open-Meteo.com", "url": "https://open-meteo.com/"}}
```

Temperatures °C, wind km/h, wind directions degrees (where it blows from; a day's is its dominant one), precipitation
mm, probabilities and humidity %, an hour's `sunshineMinutes` 0-60, times local to `timezone` without an offset, `code` a
WMO weather interpretation code. The hours carry sunshine, not the UV index (a day still has `uvIndexMax`): the
provider's 10 variables per request were all spent, and the outdoor windows need the sun hour by hour.
Only `time`/`date`, `temperature`/`high`/`low` and `code` are always there: a reader shows what it gets and leaves out
the rest. A day's `high`/`low` take in its hours' temperatures (the two parts are fetched apart). When the days cannot
be had, `daily` is empty and the hours still answer. `attribution` is the credit the data's licence requires next to the data; the result also cites it as a
source, so a chat answer carries it.

`geocode` takes `query` (name or postal code, required), `country` (ISO 3166-1 alpha-2) and `language` (ISO 639-1,
default `en`), and answers up to 5 matches, best first:

```json
{"places": [{"name": "Baden", "region": "Aargau", "country": "Schweiz", "countryCode": "CH",
             "latitude": 47.47, "longitude": 8.3, "timezone": "Europe/Zurich"}]}
```

`air_quality` (`weather__air_quality`, optional: loom shows the air only where a server has it) takes `latitude`,
`longitude`, `timezone` and `days` (1-4, default 2), and answers the European air quality index (EEA bands: good to
20, fair to 40, moderate to 60, poor to 80, very poor to 100, extremely poor above) hour by hour for 24 hours from the
current one, and each day's highest index and pollen in grains/m³ as the day's mean (the measure load classes are
defined on). Pollen kinds: `alder`, `birch`, `grass`, `mugwort`, `olive`, `ragweed`; a kind without values is left out
(the provider has pollen in Europe only, in season):

```json
{"latitude": 47.38, "longitude": 8.54, "timezone": "Europe/Zurich",
 "hourly": [{"time": "2026-11-09T09:00", "aqi": 18}],
 "daily": [{"date": "2026-11-09", "aqiMax": 33, "pollen": {"birch": 42.4, "grass": 3}}],
 "attribution": {"text": "Air quality by Open-Meteo.com, CAMS data (Copernicus Atmosphere Monitoring Service)",
                 "url": "https://open-meteo.com/en/docs/air-quality-api"}}
```

loom classes the pollen itself (MeteoSwiss load classes; olive has none and is not shown).

## Outdoor windows

"When can I go for a run / a ride (today, tomorrow, in Luzern)?" is loom's own tool `outdoor_windows`
(`internal/weather/windows.go`), offered on weather, training and health turns while `forecast` is there, to accounts
the weather server serves; its calls carry the user like any MCP call. A model
reading two days of hours proposed times after dark and misread the rain, so loom ranks and the model only words it.
It plans where the user is (the start page's place) or at a place it looks up with `geocode`, in that place's zone,
from the same `forecast` (7 days) and `air_quality` calls as the start page: the sidecar's cache, no extra provider call.
The sun is `internal/sun`. `day` is `today`, `tomorrow` or `week` (the next 7 days ranked, not their hours).

- A window lies wholly in daylight: never before sunrise, past sunset or already begun. Starts: the earliest (now,
  on a quarter hour), each full hour, the latest that still ends at sunset. Polar night: none; polar day: all day.
- An hour is wet from code 51 (drizzle and worse), a 50 % chance or 0.2 mm. Dry windows only, when there are any;
  else the two driest.
- Then sunnier (in quarters of the window; without `sunshineMinutes` the code stands in; an hour already past the
  band's top draws nothing, or a July noon would beat a mild morning), then nearer the feels-like band (run
  12-20 °C, ride 15-24 °C) and calmer (km/h past the windy limits, weighed double on a ride; past a strong limit a
  window sinks below any merely windy one), then earlier.
- Wind limits (`weather.Limits`, km/h as forecast at 10 m; at body height about a third less in the open), windy /
  strong: ride wind 15 / 39, gusts 30 / 45; run wind 20 / 39, gusts 40 / 55. A rider feels 10 km/h and works hard
  at 20 (felt); a runner's cost grows with the square of the wind and barely shows below Beaufort 4 (20); both find
  Beaufort 6 (39) the edge of bearable. Gusts are the danger on a bike (a side gust throws it across the lane);
  the DWD calls 63 stormy, the gust limits stay well below. A windy window names the wind, a strong one warns and
  ranks below every other, sun or not; the week's days use the day's strongest wind, night included.
- A week day is dry under 0.5 mm and 40 %, wet from 3 mm (or 70 % with 1 mm), showers between; ranked: a day
  without time for the activity (polar night, too short, today already over) last, then strong wind, then drier,
  calmer, sunnier, milder (the high stands in for feels-like). Today and tomorrow take their time left and wind
  from their own windows, so the week never contradicts the day. Times are planned for today and tomorrow only
  (the hours reach 3 days).
- A run is 60 minutes, a ride 120 by default. The air is a note on the best window (index from moderate, pollen
  from a moderate load), never a rank: pollen is a day's mean, it cannot tell one hour from another.
- The forecast's (and the noted air's) `attribution` sources are cited like an MCP result's.
- With run_python on the turn the result hands it `outdoor_hours/windows/days/limits.csv` (today and tomorrow
  planned whichever was asked; columns in `sandbox/loomcharts/__init__.py`, a contract with
  `internal/weather/charts.go`). `loomcharts` draws `outdoor_day`, `outdoor_compare` (today against tomorrow) and
  `outdoor_week`; the model offers one and draws it when asked.

## Caching

The sidecar keeps a forecast in two parts per point (rounded to 3 decimals, about 100 m) and timezone, each fetched
a day past the most a call can ask (hours 4 days, days 8; a part kept past the place's midnight still holds every day
asked) and cut to each call at the place's clock, so `days` never splits the cache:

- the hours until the top of the next hour: "now" is read off them;
- the days until the next three-hour mark (UTC): the models behind them run every 3 h (Switzerland's) or 6 h (the
  global one), an hourly fetch would only spend the daily call limit.

The air is kept per point and timezone until the next six-hour mark (UTC), fetched for 5 days: its models run once
or twice a day (Europe's) and every 12 h (the global one).

Each request asks at most 10 variables: past that the provider counts it as more than one call. Per place that is 24
hourly, 8 daily and 4 air requests a day, the free tier's 10,000 enough for some 270 places. A lookup is kept for 7
days.
All in memory; concurrent misses on one key make one provider request: every user whose place is the same city centre
shares it. Errors are never kept. A restart starts empty.

## Provider

Open-Meteo's free API: no key, non-commercial use, 10,000 calls a day per IP, data under CC BY 4.0 (hence
`attribution`). The sidecar holds every fact about it.

## Setup (sidecar)

`.env`:

```sh
BACKEND_WEATHER_TOKEN=         # openssl rand -hex 32; loom and the sidecar share it; empty: the weather is off
```

`BACKEND_WEATHER_URL` defaults to `http://weather:8100` in `compose.yaml`; outside compose, unset or `off` keeps the
weather off. Unlike search, a missing token never fails boot: compose starts the sidecar on every install, which
without a token answers its health probe and refuses every call. The sidecar reads `WEATHER_TOKEN`, `WEATHER_ADDR` (`:8100`), `WEATHER_FORECAST_URL`
(`https://api.open-meteo.com`), `WEATHER_GEOCODING_URL` (`https://geocoding-api.open-meteo.com`) and
`WEATHER_AIR_QUALITY_URL` (`https://air-quality-api.open-meteo.com`).

## Replacing it

An `mcp.json` server named `weather` answering `forecast` and `geocode` (and `air_quality`, if it has the air) with
the shapes above replaces the sidecar (it overrides the built-in by name). It must send `attribution` when its data's
licence asks for one.
