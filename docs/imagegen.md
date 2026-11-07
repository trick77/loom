# imagegen: the image tool contract

loom generates images through one MCP tool, `generate` on the server `imagegen` (`imagegen__generate`). The
built-in serves it (fal, `BACKEND_IMAGE_GEN_*`); an `mcp.json` entry named `imagegen` replaces it with any server
that answers this contract. loom keeps the policy around the tool, never the provider:

- an image turn (the intent gate) forces the tool, one image per turn, scheduled runs never get it;
- it fills three arguments itself, hidden from the model;
- it stores the image as an artifact and keeps the thread's model.

## Server entry

```json
"imagegen": { "type": "http", "url": "http://imagegen:8080/mcp", "artifacts": true }
```

`artifacts: true` is required: without it loom drops the image, offers no image turn, and holds the reply to 4 MiB.
With it a call may run 3 min and reply up to ~35 MiB.

## Input

The model fills (the server declares them in its input schema):

| argument | type | |
|---|---|---|
| `prompt` | string, required | at most 4000 characters |
| `filename` | string | no path, no extension |
| `aspect_ratio` | string | e.g. `16:9`, `3:4`; omitted = square |
| `width`, `height` | integer | exact size, only when asked |
| `seed` | integer | |
| `output_format` | `png` \| `jpeg` | |
| `safety_tolerance` | integer 0-5 | |

loom fills (declare them too; loom strips them from what the model sees and drops any value the model sets):

| argument | type | |
|---|---|---|
| `input_images` | `[{"data": base64, "mime_type"}]` | the image to edit, first is the source, up to 20 MiB; keep its proportions unless a shape was asked |
| `text_heavy` | boolean | the picture must carry legible text (logo, sign, poster): pick a model that renders text |
| `model` | string | the model the thread's first image reported: use it, so the look stays |

## Output

One MCP `image` content block (base64 `data`, `mimeType` png/jpeg/webp/gif; the bytes must be that type), plus:

```json
"structuredContent": { "images": [ { "filename": "red-fox", "model": "…", "provider": "…",
  "width": 1024, "height": 576, "duration_ms": 8100 } ] }
```

`images[i]` describes the i-th image block. `model` is what loom sends back as `model` on later calls in the thread.
Text content is optional; loom tells the model which artifact was made. A failure is `isError` with the reason as
text.
