# loom

Self-hosted multi-user LLM chat: Go backend (JSON/SSE API) + embedded React SPA.

## Working conventions
- TDD. English only (maintainer talks German). Never commit to master: worktree + PR per change.
- Commit subjects + PR titles: Go style `area: what` (`turn: …`, `ui: …`), gated by ship.
- Fresh clone: `ship hooks` before the first push (check, conform and init install it too); without the pre-push hook a push skips the gate.
- Gate: `make check` (`ship check`), floors 75 (Go, UI, patch): raise tests, never a floor. Sandbox escape test
  is CI only (`sandbox-escape.yaml`); isolation change → `make sandbox-escape-test` locally.
- Release by hand: `gh workflow run Release -f bump=auto` (`Release: minor`/`Release-note: …` lines).
- What kit (`github.com/trick77/ship/kit`) provides → from kit, never hand-rolled. Lint config is ship's.

## Locked technical choices (change only with explicit agreement)
- Pure-Go SQLite: `ncruces/go-sqlite3` **v0.23.3** + `sqlite-vec-go-bindings/ncruces` **v0.1.7-alpha.2**,
  bumped together by hand (v0.24+ breaks the binding ABI). `CGO_ENABLED=0`, never `mattn/go-sqlite3`.
- stdlib `net/http`, no framework. Streaming: SSE (kit `sse`).
- LLMs only via `toolkit/llmwire` (wire + every model fact); loom owns routing, budgets, prompts, accounting.
  **Never name a model, vendor or model behaviour in loom**: intents (`ReasoningMinimal`, `MaxAnswerTokens`),
  profile fields; a missing fact goes into llmwire. Tests use `llmwiretest`.
- Tools are MCP; extras in `BACKEND_MCP_SERVERS_FILE` (overrides built-ins by name, secrets only `${VAR}`).
  Outside service → own MCP sidecar (`loom-<name>`, contract `docs/<name>.md`), swappable by an `mcp.json`
  entry. Policy via `toolSpec` flags, never a tool-name check or vendor arg. Results only via text,
  `structuredContent.sources`, files, images, `items`/`link` (confirm card). `users` = loom user id.
- `run_python` → `loom-sandbox` (`docs/sandbox.md`): a job never gets `/data`, a user id or a socket.

## Config
- `BACKEND_*` env: empty = unset; `off` disables an optional one. Secrets via env only; `admin` seeded on first boot.
- Public URL only from `PUBLIC_FQDN` (Traefik rule too). Never a separate public-URL var: a blank one gave
  cookies without Secure, unnoticed.
- `BACKEND_SESSION_SECRET` (≥ 32) signs the session cookie, no session table: rotating it signs everyone out.
- `BACKEND_EMBED_MODEL` change → vector table rebuilt at boot, re-embed in background.
- `BACKEND_DEEP_CHAT_MODEL`: per-thread slot, client sends a slot key, never a model id. A rebuilt history drops
  `ResponseItems` (signed thinking replayed after an edit is a 400).
- Gates (titles, classification, image intent) ask `ReasoningMinimal`; turns and prose `ReasoningBalanced`
  (minimal may mean thinking off: wrong on prose).

## Database / migrations
- `docs/schema.md` = `TestSchemaDoc` golden, regen after a migration or model.go change. Table-bound fields and
  field-vs-column match go unchecked.
- Next free `migrations/NNNN_*.sql`; never edit an applied one. FKs off → `-- migrate:no-transaction` +
  `PRAGMA foreign_keys = OFF; BEGIN; … COMMIT; PRAGMA foreign_keys = ON;` (0032); own pool, so a failed one
  leaks no FK-off connection.
- **vec0 never frees a deleted vector**: every KNN reads the partition's whole chunks (rongo: minutes per search
  at 46k vectors). `CompactVectorsAtBoot` rebuilds past 2x, BOOT ONLY: it holds the write lock for minutes
  (rongo: 8m33s), far past the busy timeout.

## Frontend
- Direction A: themed classes from `ui/src/index.css` tokens (`bg-panel`, `text-ink`).
- Structure = mermaid fence drawn by `@trick77/diagram`, never the image model (exact labels; no action, so it draws after `read_thread`); fails to draw → corpus `.txt` FIRST.

## Queued messages
- Sent during a turn → `queued_messages`, into `messages` only by `ConsumeQueued` (one tx, rowid = pickup).
  Enqueue, consume, stop+drain (ONE step, `stopAndDrain`) and hand-off run under the thread's queue lock, never
  the global lock: every unlocked path lost messages; stop and drain apart let the canceled turn answer the
  queue first (CI flake). Stop hands queued text back; a stopped stream hands on only after its turn ended.

## Security invariants
- Every DB query scoped by `user_id`. User files confined to the user's root (`..`, absolute, symlink
  rejected). Admin endpoints role-gated. Personal data → own token per user (`PerUser`; `_USERS` only grants).
- Outside content never triggers an action (prompt injection, `Engine.acts`). Uploads don't count: every turn
  retrieves them, doc threads would lock for good.
- An acting tool of a `confirm` server never runs in a turn: a card, the click runs the stored args.
- Idea tools only propose a card. Excerpt = outside text: to a model only as a block marked `outside` (its turn
  locked, later turns see a stand-in); `ideas_lookup` refuses after outside content, counts as it.
