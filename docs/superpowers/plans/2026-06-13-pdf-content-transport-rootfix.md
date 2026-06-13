# PDF generation root-cause fix — FINDINGS & RESOLUTION

> Status: RESOLVED at the code layer, verified live against real mimo-v2.5-pro.
> NOTE: this supersedes the original plan in this file, which proposed a
> `source=previous_message` approach. That approach was implemented, tested live,
> and REVERTED — MiMo did not adopt it (it regenerates the document as blocks/
> content regardless). The real root cause and fix are below.

## Root cause (confirmed by reproduction + counter-test)

`create_pdf_file` failures ("the model stopped responding") were NOT truncation,
NOT a reasoning spiral, and NOT a model hang. **MiMo does not stream tool-call
arguments incrementally.** It streams reasoning, then goes SILENT for tens of
seconds while serializing the entire tool argument server-side, then flushes it in
one burst. For a large document that silent gap exceeds the 60s idle watchdog,
which aborts a model that is still working.

Decisive evidence (real MiMo, "Put the spec into a PDF", ~10KB spec):

| Idle window | Result |
|---|---|
| 60s (default) — before fix | `inference failed err="the model stopped responding" tool=create_pdf_file tool_arg_bytes=11 since_last_delta_ms=60000` |
| 300s — counter-test | `inference completed finish_reason=tool_calls tool_arg_bytes=11671 max_idle_ms=82711` → PDF created |
| 60s (default) — AFTER fix | `inference completed finish_reason=tool_calls tool_arg_bytes=13194 max_idle_ms=94151` → PDF created |

Why it took N prior attempts: every earlier fix (#202 cap 2048→8192, #214
8192→32768, the 5-min documentToolTimeout, the streamed-progress UI) treated a
symptom. The token cap never mattered because the stream dies mid-generation, not
at `finish_reason=length`. Small docs worked only because their argument generates
in <60s.

## The fix (commit 2c635e9)

1. **llm (the fix):** once a tool call is underway in a document-capable turn,
   widen the idle watchdog to `documentToolTimeout` (`toolCallIdleTimeout` in
   client.go; `extendIdleForToolCall` in stream.go). The reasoning/content phase
   and all non-document turns keep the fast 60s watchdog. The coarse total
   deadline (`timeoutForTools`, 5min) stays the backstop. **KEEP
   documentToolTimeout** — it is the correct backstop; the earlier plan to remove
   it was wrong.
2. **docgen (opportunistic):** accept `blocks` serialized as a JSON-encoded string
   (MiMo emits `"blocks":"[...]"`), decoding instead of dropping the document.
   NOTE: observed MiMo blocks-strings were often *invalid* JSON, in which case the
   model self-recovers via the markdown `content` retry; this fix only salvages the
   valid-string case. It did not contribute to the verified repro — the watchdog
   widening did all the work.
3. **llm RCA logging (commit aa166c9, kept):** `content_bytes`, `reasoning_bytes`,
   `tool_arg_bytes`, `tool` on every inference line — this is what made the root
   cause diagnosable.

## Known remaining behaviour (not a bug in our code)

The happy path is ~2.5 min: MiMo's first `blocks` attempt is frequently malformed
JSON, so it burns one ~90s round, then retries with markdown `content` and
succeeds. "Works" does not mean "fast." Reducing this is a model-prompting concern,
out of scope here.

## Proxy layer — checked, OK

Prod traffic is nginx (`ui/nginx.conf`) → backend. The `/api/` location has
`proxy_read_timeout 1h`, `proxy_buffering off`, `proxy_cache off`, so the silent
tool-arg burst is tolerated end-to-end. The original failing logs showed the Go
watchdog (`ErrStreamStalled`), not nginx 504 — consistent with this.

**Heartbeat hardening — BUILT (commit 280d6a0).** `sse.Writer.Heartbeat` emits a
periodic SSE comment (every 20s) while the stream is silent, so every downstream
hop (proxy / LB / edge such as Cloudflare ~100s) stays alive regardless of its
own idle timeout — this is the community-standard fix and makes the bug class
structurally impossible, not just safe for today's nginx config. Verified live:
on a turn with `max_idle_ms=113522` (113s upstream silence), 3 keepalive comments
landed in the SSE exactly between `tool_pending` and `event: artifact`, and the
PDF was produced. The backend watchdog widening protects the upstream hop
(MiMo→backend); the heartbeat protects the downstream hop (backend→client). They
are independent and complementary.

## Commit triage (Jun 12–13) — final

- #202 / #214 token-cap raises: KEEP (load-bearing for inline new docs) but NOT the
  fix; they never could be.
- `ae08dd3` AddAutoRow layout + non-BMP sanitize: KEEP (unrelated correctness).
- `documentToolTimeout` (5min total) + `d6ddaeb`: KEEP — correct backstop.
- #207 streamed-PDF progress UI: harmless; left in place.
- #212 idle watchdog + metric: KEEP, now made tool-aware (this fix).
- `4727545` truncation guard (finish_reason==length → clear error): KEEP.
