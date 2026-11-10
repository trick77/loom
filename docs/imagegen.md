# imagegen: the image tool contract

loom generates images through one MCP tool, `generate` on the server `imagegen` (`imagegen__generate`). The
`loom-imagegen` sidecar (`backend/cmd/loom-imagegen`, `/loom-imagegen` in image `loom-sidecars`) serves it with fal; an `mcp.json`
entry named `imagegen` replaces the sidecar with any server that answers this contract. loom keeps the policy
around the tool, never the provider:

- an image turn (the intent gate) forces the tool, one image per turn, scheduled runs never get it;
- it fills three arguments itself, hidden from the model;
- it stores the image as an artifact and keeps the thread's model.

The sidecar holds the fal key and every model fact (which model renders text, the 1024 px cap for it, an edit keeping
its source's shape). It is stateless (no volume) and best-effort: loom boots without it and offers images while it
answers. The cost of the boundary: an edit's source and every result cross HTTP as base64, so one edit of a 20 MiB
photo holds ~100 MiB across loom and the sidecar for its duration.

## Setup (sidecar)

`.env`:

```sh
COMPOSE_PROFILES=imagegen            # plus any other profiles, comma-separated
BACKEND_IMAGE_GEN_URL=http://imagegen:8040
BACKEND_IMAGE_GEN_TOKEN=              # openssl rand -hex 32; loom and the sidecar share it
BACKEND_IMAGE_GEN_API_KEY=           # fal key; compose hands it and the other BACKEND_IMAGE_GEN_* to the sidecar
```

`BACKEND_IMAGE_GEN_MODEL`, `_TYPOGRAPHY_MODEL` (`off`: every image on the model), `_BASE_URL` and `_POLL_TIMEOUT`
(at most 2m30s) keep their meaning; they now configure the sidecar, loom does not read them.

## Replacing it

```json
"imagegen": { "type": "http", "url": "http://my-images:8080/mcp", "artifacts": true,
  "headers": { "Authorization": "Bearer ${MY_IMAGES_TOKEN}" } }
```

`artifacts: true` is required: without it loom drops the image, offers no image turn, and holds the reply to 4 MiB.
With it a call may run 3 min and reply up to ~35 MiB; up to 4 images per reply, 25 MiB together.

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
