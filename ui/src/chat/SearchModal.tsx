import { useEffect, useId, useRef, useState } from "react";

import { Icon } from "./Icon";
import { formatTimeAgo } from "../timeago";
import { highlightTerms, renderSnippet } from "../search/highlight";
import { useThreadSearch } from "../search/useThreadSearch";

// SearchModal is the claude.ai-style command-palette search opened from the
// sidebar loupe (or ⌘K). Empty box → recent threads; typing → fast title
// matches plus slower full-text snippets (see useThreadSearch). Keyboard:
// ↑/↓ move, Enter opens, Esc closes.
export function SearchModal({
  onClose,
  onSelectThread,
}: {
  onClose(): void;
  onSelectThread(threadID: string): void;
}) {
  const [query, setQuery] = useState("");
  const [selected, setSelected] = useState(0);
  const inputRef = useRef<HTMLInputElement | null>(null);
  const titleID = useId();
  const { results } = useThreadSearch(query);
  const hasQuery = query.trim() !== "";

  useEffect(() => {
    inputRef.current?.focus();
  }, []);

  // Keep the selection in range as results change (e.g. full-text rows arrive).
  useEffect(() => {
    setSelected((current) => (current >= results.length ? 0 : current));
  }, [results.length]);

  function open(index: number) {
    const result = results[index];
    if (result === undefined) return;
    onSelectThread(result.thread.id);
    onClose();
  }

  function handleKeyDown(event: React.KeyboardEvent) {
    if (event.key === "Escape") {
      event.preventDefault();
      onClose();
      return;
    }
    if (event.key === "ArrowDown") {
      event.preventDefault();
      setSelected((current) => (results.length === 0 ? 0 : (current + 1) % results.length));
      return;
    }
    if (event.key === "ArrowUp") {
      event.preventDefault();
      setSelected((current) =>
        results.length === 0 ? 0 : (current - 1 + results.length) % results.length,
      );
      return;
    }
    if (event.key === "Enter") {
      event.preventDefault();
      open(selected);
    }
  }

  return (
    <div
      className="fixed inset-0 z-[60] grid justify-items-center bg-[rgba(10,10,9,0.62)] px-4 pt-[12vh]"
      onClick={(event) => {
        if (event.target === event.currentTarget) onClose();
      }}
    >
      <div
        aria-labelledby={titleID}
        aria-modal="true"
        role="dialog"
        className="flex max-h-[70vh] w-full max-w-[640px] flex-col overflow-hidden rounded-xl border border-[#4b4a46] bg-[#2a2a28] shadow-[0_28px_70px_rgba(0,0,0,0.55)]"
        onKeyDown={handleKeyDown}
      >
        <h2 id={titleID} className="sr-only">
          Search chats
        </h2>
        <div className="flex h-[52px] shrink-0 items-center gap-3 px-4">
          <Icon name="search" size="18px" className="shrink-0 text-[#807d74]" />
          <input
            ref={inputRef}
            type="text"
            value={query}
            onChange={(event) => setQuery(event.target.value)}
            placeholder="Search chats…"
            aria-label="Search chats"
            className="ui-composer-text min-w-0 flex-1 bg-transparent text-ink outline-none placeholder:text-[#807d74]"
          />
          <button
            type="button"
            aria-label="Close"
            onClick={onClose}
            className="grid h-7 w-7 shrink-0 place-items-center rounded-md text-[#aaa79e] transition-colors hover:bg-[#3f3f3a] hover:text-white"
          >
            <Icon name="close" size="18px" />
          </button>
        </div>

        <div className="min-h-0 flex-1 overflow-y-auto border-t border-[#3a3a37] px-1.5 py-1.5">
          {hasQuery && (
            <div className="ui-meta-text px-3 pb-1 pt-1.5 text-[#97958c]">Search results</div>
          )}
          {results.length === 0 ? (
            <div className="px-3 py-6 text-center text-[14px] text-[#807d74]">
              {hasQuery ? "No chats match your search." : "No chats yet."}
            </div>
          ) : (
            <ul>
              {results.map((result, index) => {
                const isSelected = index === selected;
                const timeLabel = formatTimeAgo(
                  result.thread.lastMessageAt ?? result.thread.updatedAt,
                );
                return (
                  <li key={result.thread.id}>
                    <button
                      type="button"
                      onClick={() => open(index)}
                      onMouseMove={() => setSelected(index)}
                      className={`flex w-full items-center gap-3 rounded-lg px-3 py-2 text-left transition-colors ${
                        isSelected ? "bg-[#3f3f3a]" : ""
                      }`}
                    >
                      <Icon name="message" size="18px" className="shrink-0 text-[#8a887f]" />
                      <span className="flex min-w-0 flex-1 flex-col">
                        <span className="truncate text-[15px] text-[#ecece6]">
                          {highlightTerms(result.thread.title, query)}
                        </span>
                        {result.snippet !== undefined && result.snippet !== "" && (
                          <span className="truncate text-[13px] text-[#908e85]">
                            {renderSnippet(result.snippet)}
                          </span>
                        )}
                      </span>
                      {isSelected ? (
                        <span aria-hidden="true" className="shrink-0 text-[14px] text-[#8a887f]">
                          ⏎
                        </span>
                      ) : (
                        <span className="shrink-0 text-[13px] text-[#8a887f]">{timeLabel}</span>
                      )}
                    </button>
                  </li>
                );
              })}
            </ul>
          )}
        </div>
      </div>
    </div>
  );
}
