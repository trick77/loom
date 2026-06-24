import { useEffect, useRef, useState } from "react";

import { editUserMemory, getUserMemory } from "./api";
import { Icon } from "./chat/Icon";
import { MemoryComposer, useDismissOnOutside } from "./MemoryEditComposer";

/**
 * UserMemoryPanel shows the auto-generated personal memory — the compact set of
 * durable facts about the user (employer, location, lasting preferences) that is
 * injected into every thread so the assistant stays personalized. Unlike the
 * project memory it renders as a flat list of discrete facts. It is read-only.
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

  const facts = toFacts(content);

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
        {loading ? (
          <p className="mt-3 text-sm text-[#807d74]">Loading…</p>
        ) : facts.length > 0 ? (
          <ul className="mt-3 space-y-1.5 text-sm leading-5 text-[#c7c5bd]" data-user-memory-content>
            {facts.map((fact, index) => (
              <li key={index} className="flex gap-2">
                <span aria-hidden className="select-none text-[#807d74]">
                  •
                </span>
                <span>{fact}</span>
              </li>
            ))}
          </ul>
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

// toFacts splits the stored memory into discrete fact lines, stripping the
// leading bullet markers the model is asked to emit.
function toFacts(content: string): string[] {
  return content
    .split("\n")
    .map((line) => line.replace(/^\s*[-*•]\s*/, "").trim())
    .filter((line) => line !== "");
}
