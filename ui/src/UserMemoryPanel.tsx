import { useEffect, useState } from "react";

import { getUserMemory } from "./api";
import { Icon } from "./chat/Icon";
import { MemoryMarkdown } from "./MemoryMarkdown";

/**
 * UserMemoryPanel shows the auto-generated personal memory — durable facts about
 * the user that are injected into every thread so the assistant stays
 * personalized. It renders the memory as markdown, so the structured `## Core`
 * (protected identity), `## Current focus` (active work), and `## Style` (learned
 * response preferences) sections show as distinct labeled groups. It is read-only.
 */
export function UserMemoryPanel() {
  const [content, setContent] = useState("");
  const [loading, setLoading] = useState(true);

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

  const hasContent = content.trim() !== "";

  return (
    <div>
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
    </div>
  );
}
