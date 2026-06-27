import Markdown from "react-markdown";
import remarkGfm from "remark-gfm";

/**
 * MemoryMarkdown renders a stored memory string as markdown. Both the user and
 * project memory panels share it so they render identically — notably the
 * structured user memory's `## Core` / `## Current focus` / `## Style` headings
 * show as distinct labeled sections rather than literal text. Links open in a new tab.
 * Styling comes from the `.ui-memory-markdown` rules in index.css.
 */
export function MemoryMarkdown({ content, className }: { content: string; className?: string }) {
  return (
    <div className={className ? `ui-memory-markdown ${className}` : "ui-memory-markdown"}>
      <Markdown
        remarkPlugins={[remarkGfm]}
        components={{
          a({ children, ...props }) {
            return (
              <a {...props} target="_blank" rel="noreferrer">
                {children}
              </a>
            );
          },
        }}
      >
        {content}
      </Markdown>
    </div>
  );
}
