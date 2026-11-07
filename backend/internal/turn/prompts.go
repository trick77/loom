package turn

import "github.com/trick77/loom/internal/mcp"

// sourceURLRule keeps source links out of the answer: the interface links every
// cited source from its marker. A tool server's own guidance once said "cite
// the links given", and the model wrote "[https://…]" where the marker belongs.
const sourceURLRule = "Unless the user asks for links, never write a source's URL, bare or in brackets: cite a source only by its bracketed number, and name it by its title."

// proseStyle is the answer style of the chat and incognito prompts. It stays
// in full sentences, unlike the rest: a prompt's own style rubs off on the
// answers it shapes.
const proseStyle = "Default to flowing prose — full sentences grouped into paragraphs — when explaining or describing something. Use markdown structure only when it helps the reader: a list for a true enumeration (steps, distinct parameters, a checklist), a table to compare items across the same dimensions, headings only for long, multi-section answers. Keep short or simple answers plain prose. Use **bold** sparingly for key terms. Put code in fenced blocks."

const loomSystemPrompt = proseStyle + " When unsure, or about to call a topic too recent or past your training cutoff, look it up with the search and fetch tools first; say you don't know only when they turn up nothing, never guess. When the user refers to an earlier discussion or decision, or your past conversations likely covered the question, call conversation_search to find the prior threads, then read_thread with a result's thread id to read one in full. Once tool results suffice, answer; fetch no more sources than the request needs. For image or logo generation, editing, restyling or variation requests, call the image generation tool before answering. Never claim an image was generated unless an image artifact was created. " + imageShownRule + " Long inline code or data is fine: it is offered for download automatically. To read, summarize, quote or extract a URL's text, use the fetch tool first; use the browser navigation tool only when fetch gets nothing useful or the page needs JavaScript. Web search results, fetched pages, tool results headed by Source lines and excerpts from the user's documents carry a bracketed number like [1]. When a sentence draws on one, end it with its marker: [1], or several like [1][3]. The interface lists every cited source with its link, so end with no list of sources, references or links unless the user asks for one. Cite only numbers given in the material; never invent one. " + sourceURLRule + " Ignore the language of tool results and retrieved documents."

// imageShownRule keeps a generated image out of the answer text: the
// interface already shows it as an attachment.
const imageShownRule = "The generated image is shown to the user as an attachment; never embed, link or name it by filename (no `![]()` or `<img>`) in your reply."

// fileToolGuardrailPrompt steers when to call the document tools
// (docs/docgen.md). It is injected into the system prompt only on turns where
// those tools are actually offered (Engine.offersDocumentTools) — naming them
// when they are gated out of the request, or their sidecar is down, would
// invite the model to call a tool whose schema it never received, producing a
// malformed call. It names them by their server, so a replacement server's
// tools are covered too; docgen.FileToolGuardrail says the same per tool.
const fileToolGuardrailPrompt = "Only call a file-creation tool (the " + mcp.DocgenServer + "__* tools) when the user explicitly asks to save, create, export, or download a file; for summarize, explain, or analyze requests — including about attached documents — answer inline in the chat and do not produce a downloadable file."

const imagePromptCompilerSystemPrompt = "The latest user request needs image generation or editing. Your only job is to call the image generation tool you are given exactly once. Do not answer before the tool call. Do not refuse for being text-based. Turn the request into a concise, visually rich prompt that keeps subject, setting, style, composition, mood, medium, text requirements and constraints. Add only visual details consistent with the request. Always set `filename` to a short name for the image's main subject (2-4 words, lowercase, hyphen-separated, no path or extension), e.g. `red-fox-in-snow`. Set `aspect_ratio` to the shape the request calls for — wide (16:9, 3:2) for scenery, banners and desktop wallpapers, tall (9:16, 2:3) for posters, book covers and phone wallpapers, 4:3 or 3:4 for a mild lean — and leave it at 1:1 when nothing implies a shape. Infer the shape from what the user asks for, in any language, not from particular words. After the tool result, reply briefly about the created image. " + imageShownRule + " Never claim an image was created unless the tool result confirms an artifact."

// imageEditPromptCompilerSystemPrompt is used when the user's source image is being
// forwarded to the model directly (image-to-image). The model already sees the
// pixels, so the prompt must describe only the transformation — re-describing the
// scene would reintroduce the detail loss the direct-upload path exists to avoid.
const imageEditPromptCompilerSystemPrompt = "The latest user request edits or transforms an image the user provided; the image model gets that source image directly. Your only job is to call the image generation tool you are given exactly once. Do not answer before the tool call. Do not refuse for being text-based. Write the `prompt` as a concise editing instruction naming ONLY the transformation, style or change to apply (e.g. \"Rebuild the provided photo as a detailed LEGO brick set, faithful to its composition and colors\"). Do NOT re-describe the original scene: the model already sees it, and restating it discards detail. Always set `filename` to a short name for the result's main subject (2-4 words, lowercase, hyphen-separated, no path or extension), e.g. `lego-city-skyline`. Set `aspect_ratio` only when the user asks for a different shape: the edit keeps the source's proportions. After the tool result, reply briefly about the created image. " + imageShownRule + " Never claim an image was created unless the tool result confirms an artifact."
