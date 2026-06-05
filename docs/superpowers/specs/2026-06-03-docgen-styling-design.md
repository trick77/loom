# Document Generation Styling Design

## Context

Slopr can already generate PPTX, PDF, DOCX, XLSX, and text artifacts (see
`2026-06-03-document-generation-artifacts-design.md`). The output is functionally correct but
visually bland: every generator emits hand-written OOXML or low-level PDF primitives with no styling
applied. Concretely:

- **PPTX** defines a tasteful Slopr color theme in `theme1.xml`, but no slide ever references it.
  Titles are plain 40pt black text, bullets are manually positioned 24pt black text with no bullet
  glyphs, no background fills, and no accent color. There is a single blank layout.
- **PDF** uses `gopdf` with one Go font, black text, no color, and no page-overflow handling — long
  content runs off the bottom of the page.

The goal is to make generated **PPTX and PDF** documents look professional and varied, without
introducing license- or telemetry-encumbered dependencies, and while keeping the deterministic,
self-contained architecture the project deliberately chose.

DOCX and XLSX are explicitly **out of scope** for this iteration.

## Decisions (locked during brainstorming)

- **Approach:** Enrich the existing hand-written OOXML (PPTX) and replace the low-level PDF engine,
  introducing a shared theme/layout layer. No general-purpose Office library (e.g. unioffice) — its
  licensing (commercial/AGPL) is unsuitable for a self-hosted product.
- **Ambition:** "Polish + Layouts" — apply the theme everywhere AND add layout variety plus tables.
  Images and charts are out of scope.
- **Formats:** PPTX and PDF only.
- **PDF engine:** `github.com/johnfercher/maroto/v2` (MIT, actively maintained, ~2.7k stars). It
  provides a row/column grid, native tables, automatic page breaks, and cell styling. Pulls in
  `phpdave11/gofpdf` (MIT) and `pdfcpu/pdfcpu` (Apache-2.0). No telemetry.

## Architecture

### Shared theme layer — `backend/internal/docgen/theme.go` (new)

A single source of truth for palette and fonts, consumed by both generators.

- **Palette** (lifted from the existing PPTX theme so the two formats match):
  - Text/dark: `1D1D1B`
  - Background/light: `F3F0E8` (cream)
  - Accents: `9A6B4F` (terracotta), `6F8B6B` (sage), `C7A35F` (gold), plus the existing secondary
    accents.
- **Contrast helpers:** pick light text on accent backgrounds and accent text on the cream
  background, so every layout stays legible.
- **PDF fonts:** the Go font family (`goregular`, `gobold`, `goitalic`) from
  `golang.org/x/image/font/gofont` — already an available dependency, BSD-licensed, no new assets.
- **PPTX fonts:** keep the theme fonts (`Aptos` / `Aptos Display`), which render natively in
  PowerPoint and LibreOffice.

Colors are stored once (hex for OOXML, RGB for maroto) and exposed through small accessors so neither
generator hard-codes values.

### PPTX — layout variety

`pptx.go` keeps the package/relationship plumbing. Per-layout slide rendering moves into a new
`pptx_layouts.go`, where each layout is a small, independently testable function that emits **styled**
DrawingML shapes (background rectangles with `a:solidFill`, titles with accent `a:rPr`, real bullets
via `a:buChar`/`a:buFont`).

A slide gains a `layout` field; the generator switches on it:

| `layout` | Renders |
|---|---|
| `title` | Title slide: large centered title + subtitle, accent background band |
| `section` | Section divider: full-bleed accent background, large light title |
| `bullets` *(default)* | Accent title + bulleted body with real glyphs and accent markers |
| `two-column` | Title + two bullet columns |
| `big-number` | Large statistic + caption |
| `quote` | Large quote + attribution, accent bar |
| `table` | Title + DrawingML table (`a:tbl`): accent header row, zebra body rows |

`bullets` is the default so existing `{title, bullets}` slides keep working unchanged.

### PDF — rebuilt on maroto — `backend/internal/docgen/pdf.go`

- A colored **header band** (accent) carries the document title and optional subtitle.
- Content is a typed `blocks` array instead of a raw Markdown string, matching the "layouts + tables"
  goal. Each block becomes one maroto row:

  | block `type` | Renders |
  |---|---|
  | `heading` | Accent-colored, size-graded heading (level 1–3) |
  | `paragraph` | Body text |
  | `bullets` | Bulleted list |
  | `table` | Accent header row + zebra rows (maroto `props.Cell`) |
  | `columns` | Two side-by-side cells (`Col(6)+Col(6)`) |
  | `callout` | Colored background box for emphasis |

- maroto handles **automatic page breaks**, eliminating the current overflow bug.
- **Backward-compatible fallback:** if the tool call provides a `content` string and no `blocks`, it
  is rendered as a sequence of paragraph/heading blocks (lightweight Markdown-ish parse: `# ` →
  heading, blank line → paragraph break), so older call shapes still produce output.

## Tool schemas & LLM steering

- **PPTX schema:** add a `layout` enum plus the typed per-layout fields (`subtitle`, `columns`,
  `number`, `caption`, `quote`, `attribution`, `table`). Defaults keep `{title, bullets}` valid.
- **PDF schema:** add `blocks` (primary) and `title`/`subtitle`; keep `content` accepted as a
  fallback.
- **Tool `Description` text** is the main lever for getting the model to *use* the new capabilities:
  instruct it to open decks with a `title` slide, use `section` dividers, pick `table` for tabular
  data, vary layouts, and use PDF `blocks` (headings, tables, callouts) rather than one flat text
  blob.

## Data flow

Unchanged from the existing artifact pipeline. The richer payload arrives in
`GenerateRequest.Payload`; each generator parses its typed fields, renders into the provided
`io.Writer`, and returns `GeneratedMeta`. Size limit, sandbox path resolution, persistence, and the
download card are all reused as-is.

## Error handling

- Unknown/missing `layout` → fall back to `bullets` (never error on layout alone).
- Malformed typed fields (e.g. a `table` without rows) → skip that element and continue, so one bad
  slide/block never fails the whole document.
- maroto build/render errors propagate as a tool error string, consistent with the existing
  `executeBuiltInTool` behavior.

## Testing & verification

- **Unit tests:** one per PPTX layout and per PDF block type, asserting the styled markup / maroto
  rows are emitted (e.g. accent fill present on a `section`, `a:tbl` present on a `table`, header
  band present in the PDF).
- **Validity (key risk):** the hand-written PPTX must still open **without** a "PowerPoint needs to
  repair" dialog. Verify by rendering each layout with
  `soffice --headless --convert-to pdf` and confirming a clean conversion; add a round-trip smoke
  test per layout.
- **Visual check:** generate a sample deck and a sample report covering every layout/block, render to
  PDF, and eyeball them.
- **Existing guards:** size-limit and smoke tests stay green; `go test ./internal/docgen`.

## Risks

- **OOXML validity:** tables and background shapes make the hand-written XML more complex; an invalid
  part triggers PowerPoint's repair prompt. Mitigated by per-layout LibreOffice round-trip tests.
- **New dependency surface:** maroto adds gofpdf + pdfcpu. All permissive-licensed and maintained;
  acceptable given the large code reduction for tables/columns/page-flow.
- **LLM adoption:** richer schemas only help if the model uses them; the tool descriptions must steer
  it, and defaults must keep the simple path working.

## Out of scope

- DOCX and XLSX styling.
- Images and charts.
- Replacing the OOXML approach with an Office library.
