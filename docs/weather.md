# weather: the weather contract

loom gets weather through two MCP tools on the server `weather`: `forecast` (`weather__forecast`) and `geocode`
(`weather__geocode`). The `loom-weather` sidecar (`backend/cmd/loom-weather`, image `loom-weather`) serves them with
Open-Meteo; an `mcp.json` entry named `weather` replaces the sidecar with any server that answers this contract. loom
keeps the policy, never the provider:

- the start page asks `forecast` for the user's place and `geocode` to turn a place name from the user's memory into
  coordinates; in a chat turn the tools are offered on weather questions only (classifier category `weather`);
- loom reads the current temperature from the hourly values itself and computes sunrise and sunset locally.

## Tools

Both answer with one JSON object as text.

`forecast` takes `latitude`, `longitude` (required), `timezone` (IANA, default the place's own) and `days` (1-3,
default 2, today first):

```json
{"latitude": 47.38, "longitude": 8.54, "timezone": "Europe/Zurich",
 "hourly": [{"time": "2026-11-09T09:00", "temperature": 6.2, "code": 3, "precipitationProbability": 10, "windSpeed": 9}],
 "daily": [{"date": "2026-11-09", "high": 11.2, "low": 3.1, "code": 3, "precipitationProbability": 10}],
 "attribution": {"text": "Weather data by Open-Meteo.com", "url": "https://open-meteo.com/"}}
```

Temperatures °C, wind km/h, probabilities %, times local to `timezone` without an offset, `code` a WMO weather
interpretation code. `attribution` is the credit the data's licence requires: loom shows it next to the data.

`geocode` takes `query` (name or postal code, required), `country` (ISO 3166-1 alpha-2) and `language` (ISO 639-1,
default `en`), and answers up to 5 matches, best first:

```json
{"places": [{"name": "Baden", "region": "Aargau", "country": "Schweiz", "countryCode": "CH",
             "latitude": 47.47, "longitude": 8.3, "timezone": "Europe/Zurich"}]}
```

## Caching

The sidecar keeps a forecast per point (rounded to 3 decimals, about 100 m), timezone and days until the top of the
next hour, and a lookup for 7 days, in memory. Concurrent misses on one key make one provider request: every user
whose place is the same city centre shares it. Errors are never kept. A restart starts empty.

## Provider

Open-Meteo's free API: no key, non-commercial use, 10,000 calls a day per IP, data under CC BY 4.0 (hence
`attribution`). The sidecar holds every fact about it.

## Setup (sidecar)

`.env`:

```sh
BACKEND_WEATHER_TOKEN=         # openssl rand -hex 32; loom and the sidecar share it, both refuse to start without it
```

`BACKEND_WEATHER_URL` defaults to `http://weather:8100` in `compose.yaml`; outside compose, unset or `off` keeps the
weather off. The sidecar reads `WEATHER_TOKEN`, `WEATHER_ADDR` (`:8100`), `WEATHER_FORECAST_URL`
(`https://api.open-meteo.com`) and `WEATHER_GEOCODING_URL` (`https://geocoding-api.open-meteo.com`).

## Replacing it

An `mcp.json` server named `weather` answering both tools with the shapes above replaces the sidecar (it overrides the
built-in by name). It must send `attribution` when its data's licence asks for one.
