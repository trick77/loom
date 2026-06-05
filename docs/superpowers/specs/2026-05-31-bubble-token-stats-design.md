# Design: Token-Stats pro Assistant-Bubble (Hover)

Datum: 2026-05-31
Branch: worktree-chat-composer-narrow
Status: genehmigt (bereit für Implementierungsplan)

## Ziel

Unter jeder Assistant-Bubble eine schlanke, einzeilige Statistik einblenden —
Modell, Dauer, Durchsatz (tok/s), Token-Counts und Zeitstempel. Standardmässig
nur beim Hovern sichtbar; per Klick auf „immer sichtbar" umschaltbar (persistiert
in `localStorage`). Vorbild: AnythingLLM `RenderMetrics`.

## Ausgangslage (verifiziert im Code)

- Backend (Go) holt bereits `stream_options.include_usage` → `llm.TokenUsage`
  mit `prompt/completion/total/cached/reasoning` Tokens.
- Token-Counts werden **pro Assistant-Nachricht in der DB persistiert**
  (`AddMessageWithUsage`) und sind im `chat.Message`-JSON enthalten
  (`promptTokens`, `completionTokens`, `totalTokens`, `cachedTokens`,
  `reasoningTokens`).
- Die Dauer wird in `StreamChatWithTools` gemessen (`time.Since(start)`), aber
  **nur geloggt** — nicht persistiert, nicht an `StreamResult`, nicht ans
  Frontend gesendet.
- Das fertige Assistant-`Message`-Objekt erreicht das Frontend bereits über das
  `assistant_message`-SSE-Event (`onAssistantMessage(payload as Message)`); beim
  Thread-Laden kommen persistierte Messages via `listMessages`.
- `runAssistantLoop` gibt die **finale** Generierungsrunde zurück (die ohne
  Tool-Calls). Persistierte `Usage` = letzte Generierung.
- Das Frontend (`ChatShell.tsx`, `api.ts`) nutzt **keines** der Token-Felder.

**Konsequenz:** Die einzige fehlende Datengrösse ist die Dauer (plus, als
Design-Entscheid, der Modellname). Alles andere fliesst bereits durch die
bestehenden Pfade — das macht dies zu einer kleinen, end-to-end durchzufädelnden
Änderung statt eines grossen Umbaus.

## Entscheidungen

- **Inhalt:** voll — inkl. cached/reasoning Tokens.
- **Dauer:** dauerhaft in DB persistieren (Migration).
- **Modellname:** mitspeichern (per Nachricht), da „volle Transparenz" gewählt
  und das Config-Modell sich über die Zeit ändern kann — aus aktueller Config
  abzuleiten wäre für historische Messages falsch. Geht in **dieselbe** Migration
  wie `duration_ms`.
- **Sichtbarkeit:** Hover + Toggle wie AnythingLLM (localStorage + Event +
  React-Context, alle Bubbles synchron).

## Backend (Go)

### Migration `0005_message_metrics.sql`
```sql
ALTER TABLE messages ADD COLUMN duration_ms INTEGER;
ALTER TABLE messages ADD COLUMN model TEXT;
```
(Migrations laufen lexikografisch, je in eigener Transaktion — siehe
`store/migrate.go`. Pattern wie `0004_message_token_usage.sql`.)

### `llm.StreamResult` (types.go)
- Neues Feld `Duration time.Duration`.
- Neues Feld `Model string` (aus `c.model`).
- `stream.go` füllt beide in `finishStream`/Rückgabe von `StreamChatWithTools`
  und `StreamChatResult`; die bereits vorhandene `time.Since(start)`-Messung wird
  ins Result übernommen statt nur geloggt.

### Handler (`message_stream_handlers.go`)
- Beim Persistieren des finalen `assistantResult` Dauer + Modell mitgeben.
- `chat.MessageTokenUsage` (bzw. eine erweiterte Persist-Eingabe) um
  `DurationMs *int` und `Model *string` ergänzen; `messageUsageFromLLM` füllt sie
  aus `assistantResult.Duration` / `assistantResult.Model`.

### `chat.Message`-Struct (model.go)
- `DurationMs *int  \`json:"durationMs,omitempty"\``
- `Model      *string \`json:"model,omitempty"\``

### `MessageStore` (message_store.go)
- `duration_ms`, `model` in **INSERT** ergänzen (Spalten + Platzhalter + Args).
- `duration_ms`, `model` in **beiden SELECTs** (~Zeile 100 und ~127) und in den
  `Scan(...)`-Aufrufen ergänzen. Beide Lesepfade müssen angepasst werden, sonst
  fallen die Felder beim Reload still raus.

### Tradeoff (dokumentiert, kein Blocker)
Bei Tool-Runden zeigt die Dauer nur die **finale** Generierung, nicht die
Gesamt-Wartezeit inkl. Tool-Ausführung. Das hält `tok/s = completion_tokens /
duration` kohärent mit den persistierten `completion_tokens`.

## Frontend (React/TS)

### `api.ts` `Message`-Type
Neue optionale Felder: `promptTokens?`, `completionTokens?`, `totalTokens?`,
`cachedTokens?`, `reasoningTokens?`, `durationMs?`, `model?`.

### Komponente `MessageMetrics`
- Gerendert in `AssistantText` unterhalb von `MessageActions` (für alle drei
  AssistantText-Render-Zweige: prosa, download-only, mixed).
- Eine `font-mono`-Zeile, Teile mit ` · ` verbunden:
  `model · 5.2s (42.13 tok/s) · 1 234 → 567 (1 801 tok) · cached 128 · reasoning 64 · 14:32`
  — `cached`/`reasoning` nur wenn > 0; `model`/Zeitstempel nur wenn vorhanden.
- **Berechnung:** `outputTps = completionTokens / (durationMs / 1000)`.
- **Formatierung:** Dauer analog ALLM `formatDuration` (ms / s / m s / h m s);
  tok/s < 1000 mit 2 Nachkommastellen, sonst Tausendertrennung.
- **Render-Guard (faithful zu ALLM):** `if (!durationMs || !outputTps) return
  null`. Alte/Null-Messages rendern nichts statt `NaN tok/s`. Token-only-Messages
  (Dauer fehlt) degradieren ebenfalls sauber → nichts.

### Hover + Toggle
- Zeile default `opacity-0` + `group-hover:opacity-100 transition` (die
  Bubble-Wrapper tragen bereits `className="group"`).
- Klick togglet „immer sichtbar" (`opacity-100`), persistiert in
  `localStorage["slop_show_chat_metrics"]`.
- Synchronisation aller Bubbles via `CustomEvent("slop_show_metrics_change")`
  und einem React-Context-Provider, der um den Transcript-Bereich gelegt wird
  (analog ALLM `MetricsProvider`).

## Tests

- **Go:** `message_store`-Roundtrip mit `duration_ms`/`model`; Handler persistiert
  Dauer + Modell aus `StreamResult`; `StreamResult.Duration` wird gesetzt.
- **Frontend:** `MessageMetrics` rendert nichts ohne Dauer/tok-s; formatiert
  Dauer + tok/s korrekt; cached/reasoning nur > 0; Toggle schreibt localStorage
  und feuert das Event.

## Bewusst weggelassen (YAGNI)

- Keine Gesamt-Wartezeit inkl. Tool-Ausführung, kein TTFT.
- Keine Aggregat-/Thread-weiten Statistiken.
- Keine Konfigurierbarkeit der angezeigten Felder.
