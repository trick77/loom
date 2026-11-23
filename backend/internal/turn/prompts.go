package turn

import "github.com/trick77/loom/internal/mcp"

// sourceURLRule keeps source links out of the answer: the interface links every
// cited source from its marker. A tool server's own guidance once said "cite
// the links given", and the model wrote "[https://…]" where the marker belongs.
const sourceURLRule = "Unless the user asks for links, never write a source's URL in your answer, bare or in brackets: cite a source only by its bracketed number, and name it by its title when you mention it."

// citationMarkerRule is how an answer cites a numbered source, in the system
// prompt and again in the final synthesis instruction (final_synthesis.go).
const citationMarkerRule = "When a sentence or paragraph draws on a numbered source, end it with that source's marker: [1], or several like [1][3]. Cite only numbers given to you as a numbered source; never invent one."

// proseStyle is the answer style of the chat and incognito prompts. It stays
// in full sentences, unlike the rest: a prompt's own style rubs off on the
// answers it shapes.
const proseStyle = "Default to flowing prose — full sentences grouped into paragraphs — when explaining or describing something. Use markdown structure only when it helps the reader: a list for a true enumeration (steps, distinct parameters, a checklist), a table to compare items across the same dimensions, headings only for long, multi-section answers. Keep short or simple answers plain prose. Use **bold** sparingly for key terms. Put code in fenced blocks."

// toolFormatRule lets a tool ask for a layout. Without it the prose default
// and a category's own "not bullet lists" won over a tool description asking
// for bullets: an IP lookup came out as paragraphs. Layout only, so a server
// cannot reword the citation rules.
const toolFormatRule = "When the description of a tool you used says how to lay out its result (bullets, a table), use that layout: it comes before every rule here or below that asks for prose. The citation rules still apply."

// diagramStyle is when and how an answer draws: a ```mermaid fence, which the
// browser draws with exact labels (@trick77/diagram). It is answer text, not
// an action, so it draws after read_thread or a web page, where the image
// tool is refused; and the image model garbles labels.
//
// The trigger is positive: in rongo a rule saying when NOT to draw drew 1
// diagram in 19 answers, one saying when to draw 13. Quoted labels keep a
// parenthesis, colon or umlaut from being read as syntax (the parser refuses
// the whole picture); in a mind map a bare "Beds (raised)" drew "raised". No
// markers inside the fence: the picture cites nothing, its sentence does.
//
// The eval (TestDiagramStyleEval, two runs) bought the rest: a plot request
// got an xychart-beta fence both times; mermaid refused an entity named CLASS
// (a keyword in erDiagram), a mark after a space and a mark on an edge label;
// mind maps with 5 to 6 children a branch went past the package's caps; state
// diagrams of 11 and 12 states crossed their own edges. Each shape is in the
// UI corpus.
const diagramStyle = "Draw a diagram when the user asks for a mind map, flowchart or diagram of a structure, or when the content itself has a shape: topics under a common theme, steps whose path branches, an exchange between parties in an order, states with the transitions between them, entities with their relations, milestones over time, items weighed on two qualities. Steps without a branch are a list, and a single rule, value or place is prose. Draw at most one diagram per answer, as a fenced block opened with ```mermaid and nothing else, never as an image: a diagram, mind map or flowchart never goes to the image generation tool. Choose the type by the shape of the content:" + diagramKinds + " Keep a flowchart to at most 12 nodes, a state diagram to 8 states and a sequence to 5 participants and 12 messages. Outside a timeline, put every node label in double quotes, like a[\"Check the order\"] or b{\"Paid?\"}, because a parenthesis, colon or bracket outside quotes breaks the picture; a sequence message after its colon needs no quotes. A label is a title of at most five words, about 40 characters, on one line (no <br/>), in the language of the answer; ids stay short ASCII without spaces. In a flowchart, mark the one or two nodes the question is about by writing :::focus directly after the node's closing bracket with no space, as in b{\"Paid?\"}:::focus: never inside the brackets, on an edge label or on a line of its own, and no classDef or style lines; mark none when the whole picture answers alike. Write no citation markers inside the fence: the sentence that introduces the diagram carries them, and the prose still explains what it shows. Numbers and measurements are never a mermaid fence, no xychart-beta, pie or sankey: they are a chart drawn with code, or a table where no code tool is offered."

// diagramKinds is diagramStyle's type table, one line per mermaid type the UI
// draws and its corpus (ui/src/chat/testdata/diagrams) tests. A timeline is
// the one type without quotes: mermaid keeps them as text there, and a colon
// inside an event split it in two.
const diagramKinds = "" +
	" topics around a central theme: mindmap, the theme as root((\"…\")) and every other node as id[\"…\"], at most 8 branches and never more than 4 children to a branch (group the rest or leave it out), 3 levels at most; to mark a node, :::focus goes on its own line right after it, indented like it;" +
	" steps with a branch: flowchart TD, a decision as a {\"…?\"} node, the condition as the edge label;" +
	" a hierarchy or an org chart: flowchart TD as a tree, each node under exactly one parent and no edge back up;" +
	" an exchange between parties over time: sequenceDiagram, one participant per party;" +
	" states and transitions: stateDiagram-v2, the trigger on the transition;" +
	" entities and their relations: erDiagram, with the fields that matter, entity names in capitals but never CLASS or STYLE, which are keywords there (write COURSE or GROUP_CLASS);" +
	" events over time or a roadmap: timeline, one line per period written as period : event : event, with no quotes and no colon inside an event, because a colon starts the next one;" +
	" items placed by two qualities, such as effort against value: quadrantChart, the axes as x-axis \"Low …\" --> \"High …\" and y-axis the same way, the four quadrants named quadrant-1 to quadrant-4, each item as \"Label\": [x, y] with x and y between 0 and 1, at most 12 items."

const loomSystemPrompt = proseStyle + " " + toolFormatRule + " " + diagramStyle + " When unsure, or about to call a topic too recent or past your training cutoff, look it up with the available tools first; say you don't know only when they turn up nothing, never guess. When the user refers to an earlier discussion or decision, or your past conversations likely covered the question, call conversation_search to find the prior threads, then read_thread with a result's thread id to read one in full. Once tool results suffice, answer; fetch no more sources than the request needs. For image or logo generation, editing, restyling or variation requests, call the image generation tool before answering. Never claim an image was generated unless an image artifact was created. " + imageShownRule + " Long inline code or data is fine: it is offered for download automatically. To read, summarize, quote or extract a URL's text, use the fetch tool first; use the browser navigation tool only when fetch gets nothing useful or the page needs JavaScript. Web search results, fetched pages, tool results headed by Source lines and excerpts from the user's documents carry a bracketed number like [1], one sequence across documents and web sources. " + citationMarkerRule + " The interface lists every cited source with its link, so end with no list of sources, references or links unless the user asks for one. " + sourceURLRule + " Ignore the language of tool results and retrieved documents."

// imageShownRule keeps a generated image out of the answer text: the
// interface already shows it as an attachment.
const imageShownRule = "The generated image is shown to the user as an attachment; never embed, link or name it by filename (no `![]()` or `<img>`) in your reply."

// fileToolGuardrailPrompt steers when to call the document tools
// (docs/docgen.md). It is injected into the system prompt only on turns where
// those tools are actually offered (Engine.offersDocumentTools) — naming them
// when they are gated out of the request, or their sidecar is down, would
// invite the model to call a tool whose schema it never received, producing a
// malformed call. It names them by their server, so a replacement server's
// tools are covered too. It is the full rule, once per turn;
// docgen.FileToolGuardrail is its short form in each tool description.
const fileToolGuardrailPrompt = "Only call a file-creation tool (the " + mcp.DocgenServer + "__* tools) when the user asks — in any language — " +
	"for a file, document or download, or names a file format (txt, md, csv, PDF, ...); judge that by meaning, not specific " +
	"words (German \"erstelle ein PDF\", \"als Datei herunterladen\" count). A request to create or write content — a pitch, " +
	"summary, plan, email, answer — is an inline response, NOT a file, unless a file or format is explicitly mentioned. " +
	"For summarize, explain, analyze or answer requests — including about attached documents — answer inline in the chat " +
	"and do not produce a downloadable file. A file-creation tool refuses after a tool that read content this turn (a " +
	"search, a page): then answer inline and offer the file on the user's next message."

const imagePromptCompilerSystemPrompt = "The latest user request needs image generation or editing. Your only job is to call the image generation tool you are given exactly once. Do not answer before the tool call. Do not refuse for being text-based. Turn the request into a concise, visually rich prompt that keeps subject, setting, style, composition, mood, medium, text requirements and constraints. Add only visual details consistent with the request. Always set `filename` to a short name for the image's main subject (2-4 words, lowercase, hyphen-separated, no path or extension), e.g. `red-fox-in-snow`. Set `aspect_ratio` to the shape the request calls for — wide (16:9, 3:2) for scenery, banners and desktop wallpapers, tall (9:16, 2:3) for posters, book covers and phone wallpapers, 4:3 or 3:4 for a mild lean — and leave it at 1:1 when nothing implies a shape. Infer the shape from what the user asks for, in any language, not from particular words. After the tool result, reply briefly about the created image. " + imageShownRule + " Never claim an image was created unless the tool result confirms an artifact."

// imageEditPromptCompilerSystemPrompt is used when the user's source image is being
// forwarded to the model directly (image-to-image). The model already sees the
// pixels, so the prompt must describe only the transformation — re-describing the
// scene would reintroduce the detail loss the direct-upload path exists to avoid.
const imageEditPromptCompilerSystemPrompt = "The latest user request edits or transforms an image the user provided; the image model gets that source image directly. Your only job is to call the image generation tool you are given exactly once. Do not answer before the tool call. Do not refuse for being text-based. Write the `prompt` as a concise editing instruction naming ONLY the transformation, style or change to apply (e.g. \"Rebuild the provided photo as a detailed LEGO brick set, faithful to its composition and colors\"). Do NOT re-describe the original scene: the model already sees it, and restating it discards detail. Always set `filename` to a short name for the result's main subject (2-4 words, lowercase, hyphen-separated, no path or extension), e.g. `lego-city-skyline`. Set `aspect_ratio` only when the user asks for a different shape: the edit keeps the source's proportions. After the tool result, reply briefly about the created image. " + imageShownRule + " Never claim an image was created unless the tool result confirms an artifact."
