# Design: Persistentes Pro-Benutzer-Usage-Accounting + User-Menü + Settings/Usage-Modal

Datum: 2026-06-12
Status: Entwurf zur Review

## Ziel

Pro Benutzerkonto dauerhaft mitzählen, was „verbraucht" wurde, und diese Zahlen
in der UI sichtbar machen. Die Zähler müssen erhalten bleiben, wenn ein Benutzer
einen Chat oder ein Projekt löscht — sie dürfen also **nicht** aus den `messages`-
oder `threads`-Zeilen abgeleitet werden, sondern werden nach jedem Call in eine
eigene, monotone Zählertabelle aggregiert.

Erfasst werden pro Benutzer:

- **Tokens** (aufgeschlüsselt): prompt, completion, cached, reasoning, total
- **Web-Searches** (Tavily `tavily_search`)
- **Web-Fetches normal** (`fetch__fetch`)
- **Web-Fetches Obscura** (Headless-Browser, inkl. Fetch→Obscura-Fallback)
- **Image-Generierungen** (`generate_image` / Image-Tools)
- **Chats erstellt** (lifetime, monoton)
- **Projekte erstellt** (lifetime, monoton)

Zusätzlich im Usage-Panel angezeigt (aber **kein** Zähler, sondern ein
Live-Wert): die **aktuelle Länge der User-Memories** des Benutzers in Zeichen
(`len([]rune(content))` aus `user_memory.content`, Limit `MaxUserMemoryLength`).

Sichtbar gemacht über: ein neues **User-Menü** (Popup) → **Settings**-Modal →
Nav-Eintrag **Usage**.

## Nicht-Ziele (YAGNI)

- Keine Zeit-Buckets (täglich/monatlich) — nur All-Time-Totals.
- Kein Kosten-/Abrechnungs-Modell, keine Limits/Quotas.
- Kein funktionierendes Language-Switching — der Menüeintrag „Language" ist
  vorerst ein **toter Eintrag** (Platzhalter, später verdrahtet).
- Keine Claude-spezifischen Menüpunkte (Upgrade plan, Gift, Get help, Connectors …).
- Keine Suchleiste im Settings-Modal (bewusst weggelassen).

## Architektur-Prinzip

Keine God-Classes. `ChatShell.tsx` ist bereits >2500 Zeilen — Menü, Modal und
Usage-Panel kommen als **eigene Komponentendateien** hinzu, nicht hinein.
Backend-seitig ein **eigenes** `usage`-Paket/Store, das an genau definierten
Stellen aufgerufen wird; bestehende Stores/Handler werden nicht aufgebläht.

---

## Teil A — Backend: Usage-Accounting

### A1. Datenmodell (neue Migration `0005_user_usage.sql`)

```sql
CREATE TABLE user_usage_totals (
    user_id           TEXT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    prompt_tokens     INTEGER NOT NULL DEFAULT 0,
    completion_tokens INTEGER NOT NULL DEFAULT 0,
    cached_tokens     INTEGER NOT NULL DEFAULT 0,
    reasoning_tokens  INTEGER NOT NULL DEFAULT 0,
    total_tokens      INTEGER NOT NULL DEFAULT 0,
    web_searches      INTEGER NOT NULL DEFAULT 0,
    web_fetches       INTEGER NOT NULL DEFAULT 0,
    obscura_fetches   INTEGER NOT NULL DEFAULT 0,
    image_gens        INTEGER NOT NULL DEFAULT 0,
    chats_created     INTEGER NOT NULL DEFAULT 0,
    projects_created  INTEGER NOT NULL DEFAULT 0,
    updated_at        TEXT NOT NULL DEFAULT (datetime('now'))
);
```

- PK = `user_id`, ein Zeilen-pro-Benutzer-Aggregat. FK nur auf `users` →
  überlebt das Löschen von Chats/Projekten; wird erst beim Löschen des Benutzers
  selbst (Cascade) entfernt. Korrekt, da Zähler dem Konto gehören.
- Alle Updates sind additive `UPDATE … SET col = col + ?` via Upsert
  (`INSERT … ON CONFLICT(user_id) DO UPDATE`), damit die erste Aktivität die
  Zeile anlegt. Monoton steigend — Löschungen verringern nie.

### A2. `usage`-Store (neues Paket `internal/usage`)

Ein klar abgegrenzter Store mit schmalem Interface, das die Inkrement-Stellen
aufrufen. Kleine, fokussierte Methoden statt einer Sammelmethode:

```go
type Store interface {
    AddTokens(ctx, userID string, u TokenDelta) error   // prompt/completion/cached/reasoning/total
    IncWebSearch(ctx, userID string) error
    IncWebFetch(ctx, userID string) error               // normaler fetch
    IncObscuraFetch(ctx, userID string) error
    IncImageGen(ctx, userID string) error
    IncChatCreated(ctx, userID string) error
    IncProjectCreated(ctx, userID string) error
    Get(ctx, userID string) (Totals, error)
}
```

Fehler beim Zählen sind **nicht** fatal für die eigentliche Anfrage: sie werden
geloggt (`slog.Warn`) und verschluckt, damit ein Zähler-Schreibfehler nie eine
Chat-Antwort oder Tool-Ausführung scheitern lässt.

### A3. Die fünf Inkrement-Stellen

| Stelle | Datei | Counter |
|---|---|---|
| 1. Token-Persist nach Assistant-Turn | `internal/httpapi/message_stream_handler.go` (~Z.155) | `AddTokens` |
| 2. MCP-Tool-Dispatch | `internal/httpapi/tool_dispatch.go` `executeToolCall` | `IncWebSearch` / `IncWebFetch` |
| 3. Fetch→Obscura-Fallback | `internal/httpapi/tool_dispatch.go` `fetchObscuraFallback` | `IncObscuraFetch` |
| 4. Image-Tool | `internal/httpapi/tool_dispatch.go` `executeImageTool` | `IncImageGen` |
| 5a. Chat-Erstellung | `internal/chat/thread_store.go` `CreateThread` (bzw. Aufrufer im Handler) | `IncChatCreated` |
| 5b. Projekt-Erstellung | `internal/chat/project_store.go` `CreateProject` (bzw. Aufrufer) | `IncProjectCreated` |

**Counting-Regeln (explizit, da fehleranfällig):**

- Tool-Namen → Counter in `executeToolCall` per Mapping:
  `tavily_search → IncWebSearch`, `fetch__fetch → IncWebFetch`.
  Nur **erfolgreiche** Calls zählen (nach erfolgreicher `CallTool`-Rückgabe).
- **Fetch→Obscura-Fallback:** Wenn ein `fetch__fetch` fehlschlägt und der
  Obscura-Fallback einspringt, zählt das **nur als Obscura-Fetch** — der
  fehlgeschlagene normale Fetch zählt **nicht**. Konkret: `executeToolCall`
  inkrementiert `web_fetches` erst nach Erfolg; bei Misserfolg übernimmt
  `fetchObscuraFallback` und inkrementiert bei Erfolg `obscura_fetches`.
- **Token-Timing (Verifikationspunkt):** `usageTotal.Total()` muss gelesen
  werden, **nachdem** alle Hintergrund-Goroutinen (Titel-Generierung,
  Reasoning-Abstract), die in denselben `UsageAccumulator` schreiben, fertig
  sind. Bei der Umsetzung ist zu verifizieren, dass die Persist-Stelle (Z.155)
  nach deren Abschluss läuft; sonst gehen Helper-Tokens verloren. Falls nicht
  garantiert, Persist hinter ein Warten/Join der Goroutinen ziehen.
- Image-Gen zählt pro erfolgreich erzeugtem Bild-Artefakt einmal.

### A4. API-Endpoint

`GET /api/me/usage` (auth-required, wie `/api/me/memory`) → JSON mit allen
Zählern für den eingeloggten Benutzer. Handler in `internal/httpapi`, dünn:
ruft `usage.Store.Get` und serialisiert. Zusätzlich liest der Handler die
**aktuelle User-Memory-Länge** (Runen-Länge von `user_memory.content` via
bestehendem User-Memory-Store) und gibt sie als `userMemoryLength` (plus
`userMemoryMax` = `MaxUserMemoryLength`) mit aus — Live-Wert, nicht aus der
Zählertabelle. So bleibt das Panel ein einziger Fetch.

---

## Teil B — Frontend: User-Menü (Popup)

Ersetzt den heutigen Inline-`Logout`-Button in der User-Zeile unten in der
Sidebar (`ChatShell.tsx` ~Z.989–1006).

- **Trigger:** Klick auf die User-Zeile (Name, Rolle/Level, oder leerer Bereich
  rechts) öffnet ein Popup-Menü oberhalb der Zeile.
- **Eigene Komponente:** `ui/src/chat/UserMenu.tsx` (analog `ThreadActionsMenu`),
  nutzt das bestehende `MenuSeparator`-/`role="menu"`-Muster und die `Icon`-Glyphen.
- **Einträge (von oben):**
  1. **Settings** — Glyph `settings` → öffnet das Settings-Modal.
  2. **Language** — Glyph `globe` → **toter Eintrag** (No-op-Platzhalter,
     visuell vorhanden, später verdrahtet).
  3. `MenuSeparator` (Divider wie in anderen Menüs).
  4. **Log out** — Exit-Glyph → bisheriges `onLogout`.
- **Logout-Glyph:** im aktuellen `Icon`-Mapping (78 Glyphen) ist kein Exit/
  Logout-Glyph erfasst. Während der Umsetzung passenden Codepoint via Specimen
  (`/icons.html`) ermitteln und ins `CODEPOINTS`-Mapping aufnehmen.
- Schließen bei Outside-Click / Escape (gleiches Verhalten wie Thread-Menü).

## Teil C — Frontend: Settings-Modal mit Usage-Panel

Layout folgt dem bereitgestellten Screenshot: zentriertes Overlay-Modal, links
schmale Nav-Spalte (Header „Settings"), rechts die Inhalts-Pane, Schliessen-X
oben rechts. **Keine** Suchleiste.

- **Eigene Komponenten** (keine Einbettung in `ChatShell`):
  - `ui/src/settings/SettingsModal.tsx` — Modal-Rahmen, Overlay, Nav-Liste,
    aktive Auswahl, Close.
  - `ui/src/settings/UsagePanel.tsx` — rendert die Usage-Stats, lädt Daten über
    eine neue `getUsage()`-Funktion in `ui/src/api.ts`.
- **Nav:** genau ein Eintrag **Usage** (aktiv). Struktur erlaubt später weitere
  Einträge, aber jetzt nur dieser eine.
- **Usage-Panel-Inhalt:** alle Zähler, Tokens **aufgeschlüsselt**:
  - Tokens: prompt / completion / cached / reasoning / total
  - Web-Searches, Web-Fetches (normal), Web-Fetches (Obscura)
  - Image-Generierungen
  - Chats erstellt, Projekte erstellt
  - **User-Memory-Länge** (Live-Wert, z. B. „1234 / 8000 Zeichen") — als eigene
    Zeile/Gruppe „Memory", klar von den Lifetime-Zählern abgesetzt.
  - Darstellung schlicht als Label/Wert-Liste (Gruppen: „Tokens", „Tools",
    „Aktivität", „Memory"); Stil/Farben aus dem bestehenden Theme.

## Datenfluss

```
LLM-Turn / Tool-Call / Chat- bzw. Projekt-Erstellung
        │  (nach Erfolg)
        ▼
usage.Store  ──UPDATE col = col + delta──►  user_usage_totals (eine Zeile/User)
        ▲
        │  GET /api/me/usage
ui/api.ts getUsage() ──► UsagePanel  ◄── SettingsModal ◄── UserMenu (Settings)
```

## Fehlerbehandlung

- Zähler-Schreibfehler: loggen + verschlucken, nie die Hauptaktion scheitern lassen.
- `GET /api/me/usage` für Benutzer ohne Zeile: leeres/Null-Aggregat zurückgeben
  (alle Werte 0), nicht 404.
- Frontend: Lade-/Fehlerzustand im Usage-Panel (Spinner / kurze Fehlermeldung).

## Tests

- **Backend:** `usage`-Store-Unit-Tests (Upsert legt Zeile an; additive Inkremente;
  Get auf leeren User → Nullen). Handler-Test für `/api/me/usage`. Mindestens ein
  Test, der die Fallback-Regel absichert: fehlgeschlagener Fetch + Obscura-Erfolg →
  `obscura_fetches+1`, `web_fetches+0`.
- **Frontend:** `UserMenu`-Test (Einträge, Divider, Settings öffnet Modal, Logout
  ruft Callback, Language ist No-op). `UsagePanel`-Test (rendert Werte aus API-Mock).

## Offene Verifikationspunkte für die Umsetzung

1. Token-Timing relativ zu Hintergrund-Goroutinen (siehe A3).
2. Exakter Aufrufort der Chat-/Projekt-Inkremente (Store-Methode vs. Handler),
   sodass jede Erstellung genau einmal zählt.
3. Logout-Glyph-Codepoint via Specimen ermitteln.
