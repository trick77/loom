# shop: the product search contract

loom searches a shop's catalog through one MCP tool, `search_products` on the server `shop`
(`shop__search_products`). The `loom-shop` sidecar (`backend/cmd/loom-shop`, `/loom-shop` in image `loom-sidecars`)
serves it with Galaxus; an `mcp.json` entry named `shop` replaces the sidecar with any server that answers this
contract. loom keeps the policy, never the provider: the tool is offered on shopping questions only (classifier
category `shopping`, kept by an `mcp.json` override that sets no categories of its own), its results are outside
content like a web page, and the products come back as citations and as cards under the answer.

## Tool

`search_products` takes `query` (required), `language` (`en`, `de`, `fr`, `it`, `nl`; default `en`, the
conversation's language, product data comes back in it), `max_results` (1-20, default 8; more is lowered to 20, loom's
limit of sources per call), `min_price` and `max_price` (whole units of the shop's currency, 1-1,000,000).

It answers unnumbered text, one block per product, and the same products, in the same order, as sources (loom
numbers them):

```json
"structuredContent": {"sources": [
  {"url": "https://www.galaxus.ch/de/s1/product/…-24556944",
   "title": "Digitec GaN 3-Port Fast Charger (65 W, 3 Ports)",
   "snippet": "CHF 33.00 · ★ 4.7 (1118)",
   "image": "https://www.galaxus.ch/productimages/…_cropped.jpg",
   "product": {"price": 33, "currency": "CHF", "rating": 4.7, "ratingCount": 1118}}]}
```

`product` makes a source a card: `price` incl. VAT with its ISO 4217 `currency` (both left out when the shop lists no
price), `rating` out of 5 and `ratingCount`. loom drops what it cannot show truthfully (a negative price, a price
without a valid currency, a rating off the scale) and a source with nothing left is a plain one. `image` is fetched by
loom's image proxy, never by the reader's browser. No results is a text saying so with an empty `sources` list.

The results hold no stock, delivery time or full reviews; the shop's summary of review pros and cons is in the text.

## Provider

Galaxus's own MCP server (`https://mcp.galaxus.ch/`): Streamable HTTP, no key, one tool of the same name. Measured
2026-11-12: a session from `initialize` that every later request carries (without one: 400 "Session ID required"; an
unknown id is accepted), answers as SSE, its `_meta` repeating the whole catalog payload (about 6 KB a product, the
sidecar reads 2 MiB at most). The sidecar opens the session on first use and once more when the shop refuses it.

It holds every fact about the shop:

- `agentInfo`, required "for analytics only", is always `loom`: loom names no model.
- `userIntent`, optional and also analytics, is never sent: the user's words stay with loom. The contract has no such
  argument, so a model cannot pass it either.
- Product and image links lose the shop's `utm_*` parameters.
- The shop also offers an MCP Apps view (`ui://mcp-app`); loom draws its own cards and never loads it.
- Every request names the sidecar: `loom-shop/<version> (+https://github.com/trick77/loom)`.

## Setup (sidecar)

`.env`:

```sh
BACKEND_SHOP_TOKEN=            # openssl rand -hex 32; loom and the sidecar share it; empty: product search is off
```

`BACKEND_SHOP_URL` defaults to `http://shop:8110` in `compose.yaml`; outside compose, unset or `off` keeps product
search off. Like the weather, a missing token never fails boot: compose starts the sidecar on every install, which
without a token answers its health probe and refuses every call. The sidecar reads `SHOP_TOKEN`, `SHOP_ADDR` (`:8110`)
and `SHOP_UPSTREAM_URL` (`https://mcp.galaxus.ch/`; `off` serves no tool).

## Replacing it

An `mcp.json` server named `shop` answering `search_products` with the shapes above replaces the sidecar (it overrides
the built-in by name, keeps its `shopping` category and stays read-only).
