import type { PluggableList } from "unified";
import rehypeKatex from "rehype-katex";
import remarkGfm from "remark-gfm";
import remarkMath from "remark-math";

// KaTeX's stylesheet ships the fonts referenced by the rendered markup. Importing it
// once here — the shared entry every markdown surface pulls in — lets Vite bundle the
// CSS and font files, so formulas lay out correctly wherever markdown is rendered.
import "katex/dist/katex.min.css";

export { normalizeMathDelimiters } from "./mathDelimiters";

// remark-math turns `$$...$$` into math nodes; rehype-katex renders them.
// Shared across the chat, activity-trace, and memory renderers so math looks identical
// everywhere. remarkGfm stays first to preserve existing table/strikethrough parsing.
//
// singleDollarTextMath:false is load-bearing: with the default (true), any two single `$`
// in a message pair into an inline math span, so ordinary prose about money — "the most
// bearish ($63) and most bullish ($800) targets" — was swallowed whole and re-rendered as
// italic KaTeX with the spaces collapsed. Requiring `$$` costs nothing, because a `$`-run
// of two still parses as *inline* math mid-line (micromark's math-text construct); only a
// run at the start of a line becomes a display block (math-flow). So both forms survive
// while a lone `$` is inert. normalizeMathDelimiters emits `$$...$$` to match.
export const markdownRemarkPlugins: PluggableList = [
  remarkGfm,
  [remarkMath, { singleDollarTextMath: false }],
];

// throwOnError:false makes invalid or half-streamed LaTeX degrade to plain text
// instead of a red error box — important because rehype-katex re-runs on every
// streaming delta, when a formula is often still incomplete.
export const rehypeKatexPlugin: PluggableList[number] = [
  rehypeKatex,
  { throwOnError: false },
];
