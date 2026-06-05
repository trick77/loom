# Document Generation Artifacts Design

## Scope

Add agent-assisted document generation to Slopr, modeled after AnythingLLM's separation between
generated files and knowledge ingestion. A user can ask the assistant to create a file such as a PDF,
PowerPoint presentation, Word document, spreadsheet, Markdown file, CSV, HTML file, or plain text
file. Slopr saves the generated file into the user's Artifacts area and renders a download card in
the chat.

This feature is not a document editor and does not import generated files into RAG automatically.
Generated files become knowledge only when the user explicitly indexes them through the existing
"Add to knowledge" ingestion path.

## Reference Behavior

AnythingLLM implements this as a built-in agent skill bundle rather than as a generic export button.
It exposes separate tool functions for text, PDF, XLSX, DOCX, and PPTX creation, stores generated
binary files server-side under UUID-backed filenames, and emits a chat download card that can be
re-rendered from chat history. Slopr should follow the product shape but adapt the storage and access
model to Slopr's per-user volume and SSE chat stream.

## User Experience

The user asks naturally, for example:

- "Create a PDF summary of this thread."
- "Build a short PPTX deck from these notes."
- "Create an Excel file with this table."
- "Save this as README.md."

When the model chooses a document-generation tool, Slopr streams a tool status event, creates the
file, stores it in the correct Artifacts location, and then shows a compact artifact card in the
assistant message. The card shows the display filename, file type, size, and a download/open action.

Storage location:

- Project thread: `projects/<project-id>/outputs/`
- Project-less thread: `files/outputs/`

The user-facing filename comes from the tool call, sanitized for display and file-system safety.
Slopr also stores an internal artifact id and exact relative path so downloads never trust a raw path
from the browser.

## Supported Formats

Initial supported formats:

- Text-like: `txt`, `md`, `csv`, `json`, `html`, `xml`, `yaml`, `log`
- PDF: generated from Markdown/plain text
- DOCX: generated from Markdown/plain text
- XLSX: generated from CSV-like sheet data
- PPTX: generated from structured slide data

PPTX support should start with deterministic slide layouts and basic themes. It should not attempt
full free-form slide editing, master-template imports, or pixel-perfect design automation in v1.

## Architecture

Add a small backend document-generation package with one focused generator per format family:

- `artifacts`: sandboxed per-user output paths, filename sanitization, artifact metadata, MIME types,
  and download lookup.
- `docgen/text`: writes UTF-8 text-like files from model-provided content.
- `docgen/pdf`: converts Markdown/plain text to PDF.
- `docgen/docx`: converts Markdown/plain text to DOCX.
- `docgen/xlsx`: converts CSV/sheet data to XLSX.
- `docgen/pptx`: converts structured presentation JSON to PPTX.

The first PDF generator is intentionally simple: it targets Latin-script Markdown/plain text,
uses Slopr's embedded Go font, and does not yet provide robust word wrapping, table layout, or
automatic pagination for very long lines/documents. Rich PDF layout should be a follow-up generator
iteration rather than hidden complexity in v1.

Expose these generators to the existing tool loop as built-in tools, not as MCP servers. The tools
are local Slopr capabilities, need access to the authenticated user and current thread/project scope,
and must emit first-class SSE artifact events.

The chat stream adds a new event type, `artifact`, with metadata:

- artifact id
- display filename
- extension
- MIME type
- size in bytes
- project id when applicable
- download URL

The assistant message persists the same metadata so old threads can re-render download cards after a
page reload.

## Data Model

Add an `artifacts` table:

- `id`
- `user_id`
- `thread_id`
- `project_id NULL`
- `display_filename`
- `volume_relpath`
- `mime_type`
- `size_bytes`
- `source` (`assistant_generated`)
- `created_at`

Every artifact query is scoped by `user_id`. Downloads look up by artifact id and user id, then serve
the stored `volume_relpath` through the same volume sandbox rules used for Artifacts. The browser
never supplies a filesystem path for generated-file downloads.

Assistant messages store generated artifact references in message metadata or a small JSON column
added beside existing message fields. The persisted reference is intentionally metadata only; the
file bytes live in the user's volume.

## Tool Contracts

Tool names should be stable and explicit:

- `create_text_file`
- `create_pdf_file`
- `create_docx_file`
- `create_xlsx_file`
- `create_pptx_presentation`

All tools require a `filename`. Format-specific payloads:

- text: `content`, optional `extension`
- PDF/DOCX: `content` as Markdown/plain text, optional `title`
- XLSX: either `csvData` for one sheet or `sheets[]` with `name` and `csvData`
- PPTX: `title`, optional `theme`, and `slides[]` or `sections[]`

Tools validate size and structural limits before writing. Initial limits:

- max generated text input per file: 1 MiB
- max sheets: 10
- max rows per sheet: 5,000
- max PPTX slides: 30
- max artifact file size: 25 MiB

If validation fails, the tool returns a readable error to the model and no file is written.

## Security

All file writes are confined to the current user's volume root. The implementation rejects absolute
paths, `..`, reserved `.slopr` paths, symlink escapes, empty filenames, and filenames that normalize
to unsafe names. Generated filenames are collision-safe; if a display filename already exists, Slopr
adds a suffix instead of overwriting silently.

Only the owning user can download an artifact. Admins do not get implicit cross-user artifact access.
Project deletion removes generated files under `projects/<project-id>/` through the same confirmed
project-folder deletion behavior already defined for project files.

No generated artifact is indexed into RAG automatically. This prevents accidental feedback loops and
keeps the user's distinction between "output file" and "knowledge source" explicit.

## Frontend

Add an artifact card component inside chat history. It should use Slopr's existing Warm Editorial
tokens, keep a compact row shape, and render:

- file-type icon
- display filename
- file size
- download button
- optional "Add to knowledge" action when document ingestion exists for that file type

During streaming, the card appears when the backend emits the artifact event. On reload, the card is
rendered from persisted message artifact metadata.

## Error Handling

Generation failures are streamed as tool status/error text and persisted as normal assistant content.
Partial files are removed if a generator fails after creating a temporary file. Final writes are
atomic: write to `.slopr/tmp`, fsync where practical, then rename into `outputs/`.

Download errors return JSON for API callers and a visible frontend error for browser users:

- `404` when the artifact id does not exist for the current user
- `410` when metadata exists but the file is missing from disk
- `403` when authenticated but not allowed

## Testing

Backend tests cover:

- user-scoped artifact creation and download lookup
- project and project-less output paths
- filename sanitization and collision suffixes
- rejection of traversal, absolute paths, `.slopr`, and symlink escape
- text generator output
- PDF/DOCX/XLSX/PPTX generator smoke tests with non-empty valid files
- SSE artifact event emission from the chat/tool path
- persisted message artifact metadata rehydration

Frontend tests cover:

- artifact card renders during stream
- historical artifact card renders from message metadata
- download calls the artifact endpoint
- failed download shows an error
- "Add to knowledge" is absent or inert until ingestion support is implemented

## Out of Scope

- Full office document editing
- Importing or modifying existing PPTX/DOCX/XLSX files
- Automatically indexing generated files into RAG
- User-uploaded document ingestion changes
- Custom enterprise templates
- Browser-side generation
- Native OS "open file" behavior
