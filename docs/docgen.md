# docgen: the document tools contract

loom makes downloadable files through the MCP tools of the server `docgen` (`docgen__create_pdf_file` and so on). The
`loom-docgen` sidecar (`backend/cmd/loom-docgen`, image `loom-docgen`) serves them, rendering PDFs with the Gotenberg
service; an `mcp.json` entry named `docgen` replaces the sidecar with any server that answers this contract. loom
keeps the policy around the tools, never the generator:

- every tool of the server is a document tool, whatever its name;
- it offers them only when the turn plausibly wants a file (a document-producing category, or the message asks for a
  file or names a format) and files can be stored, and names them in the system prompt only then;
- it gives a turn that offers them the document budgets (completion tokens, total and tool-call idle timeouts): the
  argument is the whole document;
- a scheduled run never gets them;
- each file a call returns becomes an artifact of the turn; the model sees `created artifact <name> (<n> bytes)`.
  Nothing counts as usage.

The sidecar is stateless (no volume) and always started by `compose.yaml`, after Gotenberg is healthy; it refuses to
start while Gotenberg does not answer. loom discovers it best-effort: a dead sidecar costs the document tools, never
boot.

## Setup (sidecar)

`.env`:

```sh
BACKEND_DOCGEN_TOKEN=          # openssl rand -hex 32; loom and the sidecar share it, both refuse to start without it
```

`BACKEND_DOCGEN_URL` defaults to `http://docgen:8020` in `compose.yaml`; outside compose, unset or `off` keeps the
document tools off. The sidecar reads `DOCGEN_TOKEN`, `DOCGEN_ADDR` (`:8020`) and `DOCGEN_GOTENBERG_URL`
(`http://gotenberg:3000`). loom itself no longer talks to Gotenberg (`BACKEND_GOTENBERG_URL` is gone).

## Replacing it

```json
"docgen": { "type": "http", "url": "http://my-docs:8080/mcp", "artifacts": true,
  "headers": { "Authorization": "Bearer ${MY_DOCS_TOKEN}" } }
```

Without `"artifacts": true` every file is dropped (the startup summary says so).

## Tools

The sidecar's tools, their schemas as in `backend/internal/docgen`:

| tool | makes | arguments |
|---|---|---|
| `create_text_file` | txt, md, csv, json, html, xml, yaml, yml, log | `filename`, `content` (at most 1 MiB), `extension` |
| `create_pdf_file` | pdf | `filename`, `title`, `subtitle`, `blocks` or Markdown `content` |
| `create_xlsx_file` | xlsx | `filename`, `rows` or `csvData` |
| `create_docx_file` | docx | `filename`, `content` |
| `create_pptx_presentation` | pptx | `filename`, `title`, `slides` |

A whole document is one argument: the sidecar takes arguments up to 7 MiB.

## Output

One binary resource per file, no text (loom names the artifact):

```json
{ "type": "resource", "resource": { "uri": "file:///report.pdf", "mimeType": "application/pdf", "blob": "<base64>" } }
```

loom keeps a blob only from a server with `"artifacts": true`, and only when

- the name's extension is one of the table's, and the `mimeType` is loom's type for it (`artifact.MIMEType`,
  parameters aside: `text/markdown; charset=utf-8` for md, `text/plain` for txt, yaml, yml and log);
- the bytes are that type: `%PDF-` for pdf, a ZIP for docx, xlsx and pptx, UTF-8 without NUL for text;
- the reply's files and images together stay within one artifact's 25 MiB and four files.

Anything else is refused and counted to the model. A failure (no content, Gotenberg down) is `isError` with the reason
as text. Text resources (`text`, not `blob`) stay what they always were: files for `run_python`.
