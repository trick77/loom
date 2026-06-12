import { useEffect, useState } from "react";

import { getUserMemory } from "./api";
import { Icon } from "./chat/Icon";

/**
 * UserMemoryPanel shows the auto-generated personal memory — the compact set of
 * durable facts about the user (employer, location, lasting preferences) that is
 * injected into every chat so the assistant stays personalized. Unlike the
 * project memory it renders as a flat list of discrete facts. It is read-only.
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

  const facts = toFacts(content);

  return (
    <section
      aria-label="Memories"
      className="rounded-2xl border border-[#343432] bg-[#1f1f1d] p-5"
    >
      <h2 className="flex items-center gap-1.5 text-sm font-medium text-[#ecece6]">
        <Icon name="memory" size="16px" className="text-[#d5d2c9]" />
        <span>Memories</span>
      </h2>
      <p className="ui-meta-text mt-1 text-[#807d74]">
        Durable facts about you, used across every chat.
      </p>
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
          Memories will show here after a few chats.
        </p>
      )}
    </section>
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
