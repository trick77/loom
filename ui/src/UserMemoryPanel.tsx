import { useEffect, useRef, useState } from "react";

import { editUserMemory, getUserMemory } from "./api";
import { Icon } from "./chat/Icon";
import { MemoryMarkdown } from "./MemoryMarkdown";
import { MemoryComposer, useDismissOnOutside } from "./MemoryEditComposer";

/**
 * UserMemoryPanel shows the auto-generated personal memory — durable facts about
 * the user that are injected into every thread so the assistant stays
 * personalized. It renders the memory as markdown, so the structured `## Core`
 * (protected identity) and `## Current focus` (active work) sections show as
 * distinct labeled groups. It is read-only.
 */
export function UserMemoryPanel() {
  const [content, setContent] = useState("");
  const [loading, setLoading] = useState(true);
  const [composerOpen, setComposerOpen] = useState(false);
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<string | undefined>(undefined);

  async function handleEdit(instruction: string) {
    setPending(true);
    setError(undefined);
    try {
      const updated = await editUserMemory(instruction);
      setContent(updated.content);
      setComposerOpen(false);
    } catch {
      setError("Couldn't apply that — please try again.");
    } finally {
      setPending(false);
    }
  }

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

  const containerRef = useRef<HTMLDivElement>(null);
  useDismissOnOutside(composerOpen, containerRef, () => setComposerOpen(false));

  return (
    <div className="relative" ref={containerRef}>
      <section
        aria-label="Memories"
        className="rounded-2xl border border-[#343432] bg-[#1f1f1d] p-5 pb-20"
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
      <MemoryComposer
        open={composerOpen}
        onOpen={() => {
          setError(undefined);
          setComposerOpen(true);
        }}
        onClose={() => setComposerOpen(false)}
        pending={pending}
        error={error}
        onSubmit={handleEdit}
      />
    </div>
  );
}
