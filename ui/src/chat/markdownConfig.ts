import type { PluggableList } from "unified";
import rehypeKatex from "rehype-katex";
import remarkGfm from "remark-gfm";
import remarkMath from "remark-math";

// KaTeX's stylesheet ships the fonts referenced by the rendered markup. Importing it
// once here — the shared entry every markdown surface pulls in — lets Vite bundle the
// CSS and font files, so formulas lay out correctly wherever markdown is rendered.
import "katex/dist/katex.min.css";

export { normalizeMathDelimiters } from "./mathDelimiters";

// remark-math turns `$...$` / `$$...$$` into math nodes; rehype-katex renders them.
// Shared across the chat, activity-trace, and memory renderers so math looks identical
// everywhere. remarkGfm stays first to preserve existing table/strikethrough parsing.
export const markdownRemarkPlugins: PluggableList = [remarkGfm, remarkMath];

// throwOnError:false makes invalid or half-streamed LaTeX degrade to plain text
// instead of a red error box — important because rehype-katex re-runs on every
// streaming delta, when a formula is often still incomplete.
export const rehypeKatexPlugin: PluggableList[number] = [rehypeKatex, { throwOnError: false }];
