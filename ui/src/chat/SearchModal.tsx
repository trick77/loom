import { useEffect, useId, useLayoutEffect, useRef, useState } from "react";

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
  const contentRef = useRef<HTMLDivElement | null>(null);
  const [listHeight, setListHeight] = useState<number | undefined>(undefined);
  const titleID = useId();
  const { results } = useThreadSearch(query);
  const hasQuery = query.trim() !== "";

  useEffect(() => {
    inputRef.current?.focus();
  }, []);

  // Animate the results area between heights as the result set changes (claude.ai
  // grows/shrinks the modal smoothly rather than snapping). CSS can't transition
  // to `auto`, so we measure the content's natural height, clamp it to a viewport
  // bound, and set it as an explicit px height the `transition-[height]` class
  // animates. A ResizeObserver catches every reflow — result count changing,
  // slower full-text snippets arriving, and viewport resizes.
  useLayoutEffect(() => {
    const content = contentRef.current;
    if (content === null) return;
    const measure = () => {
      const max = Math.min(440, Math.round(window.innerHeight * 0.6));
      setListHeight(Math.min(content.scrollHeight, max));
    };
    measure();
    const observer = new ResizeObserver(measure);
    observer.observe(content);
    window.addEventListener("resize", measure);
    return () => {
      observer.disconnect();
      window.removeEventListener("resize", measure);
    };
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
      className="fixed inset-0 z-[60] grid items-start justify-items-center bg-[rgba(10,10,9,0.62)] px-4 pt-[12vh]"
      onClick={(event) => {
        if (event.target === event.currentTarget) onClose();
      }}
    >
      <div
        aria-labelledby={titleID}
        aria-modal="true"
        role="dialog"
        className="flex w-full max-w-[640px] flex-col overflow-hidden rounded-xl border border-[#4b4a46] bg-[#2a2a28] shadow-[0_28px_70px_rgba(0,0,0,0.55)]"
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
            className="grid h-8 w-8 shrink-0 place-items-center rounded-md text-[#c3c2b7] transition-colors hover:bg-[#3f3f3a] hover:text-white"
          >
            {/* Match claude.ai's close button proportions: 20px glyph in a 32px
                hit target, lighter resting tint. */}
            <Icon name="close" size="20px" />
          </button>
        </div>

        <div
          className="overflow-y-auto border-t border-[#3a3a37] transition-[height] duration-150 ease-[cubic-bezier(0.165,0.84,0.44,1)]"
          style={{ height: listHeight }}
        >
          <div ref={contentRef} className="px-1.5 py-1.5">
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
                      <span className="flex min-w-0 flex-1 flex-col">
                        <span className="truncate text-[15px] text-[#ecece6]">
                          {/* Clean + highlight only while searching; empty-state
                              recents show the raw title, matching the sidebar and
                              Threads list (which clean only during search). */}
                          {hasQuery ? highlightTerms(result.thread.title, query) : result.thread.title}
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
    </div>
  );
}
