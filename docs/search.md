# search: the web search contract

loom searches the web through one MCP tool, `web` on the server `search` (`search__web`). The `loom-search` sidecar
(`backend/cmd/loom-search`, image `loom-search`) serves it with Tavily; an `mcp.json` entry named `search` replaces the
sidecar with any server that answers this contract. loom keeps the policy around the tool, never the provider:

- it counts each call as `web_search` usage and runs a round's calls side by side;
- a scheduled run makes at most 10 calls of the server's tools; its `web` calls ask for at most 5 results, `basic`
  depth;
- it numbers the results as citations from the structured sources, in their order.

The sidecar holds the Tavily key. It is stateless (no volume) and always started by `compose.yaml`; without a key it
answers but serves no tool, so web search is off and loom boots as usual. loom discovers it best-effort: a dead
sidecar costs web search, never boot.

## Setup (sidecar)

`.env`:

```sh
BACKEND_SEARCH_TOKEN=          # openssl rand -hex 32; loom and the sidecar share it, both refuse to start without it
BACKEND_TAVILY_API_KEY=        # Tavily key; compose hands it to the sidecar, loom only sees whether it is set
```

`BACKEND_SEARCH_URL` defaults to `http://search:8030` in `compose.yaml`; outside compose, unset or `off` keeps web
search off. The sidecar reads `SEARCH_TOKEN`,
`SEARCH_TAVILY_API_KEY`, `SEARCH_ADDR` (`:8030`) and `SEARCH_TAVILY_URL` (`https://api.tavily.com`).

## Replacing it

```json
"search": { "type": "http", "url": "http://my-search:8080/mcp",
  "headers": { "Authorization": "Bearer ${MY_SEARCH_TOKEN}" } }
```

Every tool of a server named `search` counts against a scheduled run's search limit, so an extract or crawl tool added
next to `web` is bounded too.

## Input

| argument | type | |
|---|---|---|
| `query` | string, required | |
| `max_results` | integer 1-10 | default 5; more is lowered to 10 |
| `topic` | `general` \| `news` | default `general` |
| `time_range` | `day` \| `week` \| `month` \| `year` | only results published within it |
| `depth` | `basic` \| `advanced` | default `basic`; `advanced` is slower and more relevant |

An unknown argument or a value outside these is an error result.

## Output

Text, one block per result, unnumbered (loom numbers them):

```
Title: …
URL: …
Published: …        (when the provider knows it)
Content: …
```

plus the same results, in the same order, as sources:

```json
"structuredContent": { "sources": [ { "url": "https://…", "title": "…", "snippet": "…" } ] }
```

`label` stays empty: loom names a source by its host. No results is a text saying so with an empty `sources` list. A
failure (provider unreachable, out of credits) is `isError` with the reason as text.
