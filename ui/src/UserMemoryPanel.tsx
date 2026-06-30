import { useEffect, useState } from "react";

import { getUserDirectives, getUserMemory, type UserDirective } from "./api";
import { Icon } from "./chat/Icon";
import { MemoryMarkdown } from "./MemoryMarkdown";

/**
 * UserMemoryPanel shows the two layers of personal memory injected into every
 * thread, both read-only here:
 *
 * 1. Derived memory — auto-generated, refreshed daily in the background. Rendered
 *    as markdown, so its `## Work context`, `## Personal context`, `## Top of
 *    mind`, and `## Brief history` (Recent months / Earlier context / Long-term
 *    background) sections show as distinct labeled groups.
 * 2. Other instructions — the user's explicit standing instructions. These are
 *    steered by telling Loom in chat (it manages them via tools); the UI only
 *    displays them.
 */
export function UserMemoryPanel() {
  const [content, setContent] = useState("");
  const [loading, setLoading] = useState(true);
  const [directives, setDirectives] = useState<UserDirective[]>([]);

  useEffect(() => {
    let active = true;
    setLoading(true);
    getUserMemory()
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
  }, []);

  useEffect(() => {
    let active = true;
    getUserDirectives()
      .then((items) => {
        if (active) setDirectives(items);
      })
      .catch(() => {
        if (active) setDirectives([]);
      });
    return () => {
      active = false;
    };
  }, []);

  const hasContent = content.trim() !== "";

  return (
    <div className="flex flex-col gap-4">
      <section
        aria-label="Memories"
        className="rounded-2xl border border-[#343432] bg-[#1f1f1d] p-5"
      >
        <h2 className="flex items-center gap-1.5 text-sm font-medium text-[#ecece6]">
          <Icon name="memory" size="21px" className="text-[#d5d2c9]" />
          <span>Memories</span>
        </h2>

        <p className="mt-1.5 text-[13px] leading-5 text-[#8a887f]">
          What Loom has picked up about you across your threads, so each new one starts with context.
        </p>

        {loading ? (
          <p className="mt-3 text-sm text-[#807d74]">Loading…</p>
        ) : hasContent ? (
          <div className="mt-3 text-base text-[#f3f0e8]" data-user-memory-content>
            <MemoryMarkdown content={content} />
          </div>
        ) : (
          <p className="mt-3 text-sm leading-5 text-[#807d74]">
            Memories will show here after a few threads.
          </p>
        )}
      </section>

      <section
        aria-label="Other instructions"
        className="rounded-2xl border border-[#343432] bg-[#1f1f1d] p-5"
      >
        <h2 className="flex items-center gap-1.5 text-sm font-medium text-[#ecece6]">
          <Icon name="memory" size="21px" className="text-[#d5d2c9]" />
          <span>Other instructions</span>
        </h2>

        <p className="mt-1.5 text-[13px] leading-5 text-[#8a887f]">
          Standing instructions you've asked Loom to follow. Add, change, or remove these by telling Loom in chat.
        </p>

        {directives.length > 0 ? (
          <ul className="mt-3 flex flex-col gap-1.5" data-user-directives>
            {directives.map((directive) => (
              <li
                key={directive.id}
                className="flex gap-2 text-base leading-6 text-[#f3f0e8]"
              >
                <span aria-hidden className="select-none text-[#807d74]">•</span>
                <span>{directive.content}</span>
              </li>
            ))}
          </ul>
        ) : (
          <p className="mt-3 text-sm leading-5 text-[#807d74]">
            No saved instructions yet.
          </p>
        )}
      </section>
    </div>
  );
}
