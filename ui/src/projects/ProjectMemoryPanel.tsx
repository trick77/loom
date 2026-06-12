import { useEffect, useState } from "react";
import Markdown from "react-markdown";
import remarkGfm from "remark-gfm";

import { getProjectMemory } from "../api";
import { Icon } from "../chat/Icon";

/**
 * ProjectMemoryPanel shows the project's auto-generated shared memory — the
 * compact digest injected into every chat in the project so sibling chats stay
 * aware of each other. It is read-only; the memory refreshes automatically in
 * the background after chats.
 */
export function ProjectMemoryPanel({ projectId }: { projectId: string }) {
  const [content, setContent] = useState("");
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    let active = true;
    setLoading(true);
    getProjectMemory(projectId)
      .then((memory) => {
        if (active) setContent(memory.content);
      })
      .catch(() => {
        if (active) setContent("");
      })
      .finally(() => {
        if (active) setLoading(false);
      });
    return () => {
      active = false;
    };
  }, [projectId]);

  const hasContent = content.trim() !== "";

  return (
    <section
      aria-label="Memories"
      className="rounded-2xl border border-[#343432] bg-[#1f1f1d] p-5"
    >
      <h2 className="flex items-center gap-1.5 text-[15px] font-medium text-[#ecece6]">
        <Icon name="memory" size="16px" className="text-[#d5d2c9]" />
        <span>Memories</span>
      </h2>
      {loading ? (
        <p className="mt-2 text-sm text-[#8f8b82]">Loading…</p>
      ) : hasContent ? (
        <div
          className="ui-memory-markdown mt-2 text-sm leading-5 text-[#c7c5bd]"
          data-project-memory-content
        >
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
      ) : (
        <p className="mt-2 text-sm leading-5 text-[#8f8b82]">
          Memories will show here after a few chats.
        </p>
      )}
    </section>
  );
}
