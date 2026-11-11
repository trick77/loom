import {
  useCallback,
  useEffect,
  useId,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
} from "react";
import { flushSync } from "react-dom";
import { useTranslation } from "react-i18next";

import type { Project } from "../api";
import {
  filterProjects,
  sortProjects,
  type ProjectSort,
} from "../projects/projectList";
import { Icon } from "./Icon";
import { useEscapeKey } from "./useEscapeKey";
import { useMenuPlacement } from "./useMenuPlacement";
import { useOutsideRefPointerDown } from "./useOutsidePointerDown";

const SORTS: ProjectSort[] = ["recent", "edited", "created", "name"];

// One row of the popover: a project, or the "New project" footer.
type Entry = { kind: "project"; project: Project } | { kind: "create" };

// Sizes follow claude.ai's start-screen picker, measured 2026-11-10: a 28px
// text trigger that turns into a removable chip once a project is picked, and a
// 510px popover with search, a starred group, a 178px scrolling list and a
// fixed "New project" row.
// Text in loom's menu colour (ink-bright), not claude.ai's.
const rowClass =
  "flex h-7 w-full items-center gap-2 rounded-lg px-2.5 py-1 text-left text-sm font-[435] leading-5 text-ink-bright";
const separatorClass = "mx-2.5 my-1 h-px bg-ink/10";

// The list's edge fade, as on claude.ai (measured 2026-11-11): each edge fades
// over at most 28px, and only as far as the list can still scroll that way, so
// the fade grows in with the first pixels of a scroll instead of popping in.
// The eased mask is .scroll-fade-y in index.css; the list only sets its lengths.
const FADE_MAX = 28;

/** How far a scrolling list fades at each edge: as far as it can still scroll
 * there, at most FADE_MAX. */
export function edgeFade(list: {
  scrollTop: number;
  clientHeight: number;
  scrollHeight: number;
}) {
  const below = list.scrollHeight - list.clientHeight - list.scrollTop;
  return {
    top: Math.min(FADE_MAX, Math.max(0, list.scrollTop)),
    bottom: below < 1 ? 0 : Math.min(FADE_MAX, below),
  };
}

export function StartProjectPicker({
  projects,
  selectedProject,
  onSelect,
  onCreateProject,
}: {
  /** Active projects; the picker never offers an archived one. */
  projects: Project[];
  selectedProject: Project | null;
  onSelect(projectID: string | null): void;
  onCreateProject(): void;
}) {
  const { t } = useTranslation();
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState("");
  const [sort, setSort] = useState<ProjectSort>("recent");
  const [sortOpen, setSortOpen] = useState(false);
  const [active, setActive] = useState(-1);
  const rootRef = useRef<HTMLDivElement>(null);
  const sortRef = useRef<HTMLDivElement>(null);
  const buttonRef = useRef<HTMLButtonElement>(null);
  const listRef = useRef<HTMLDivElement>(null);
  const baseID = useId();
  const optionID = (index: number) => `${baseID}-option-${index}`;

  const searching = query.trim() !== "";
  const { starred, rest } = useMemo(() => {
    const sorted = sortProjects(filterProjects(projects, query), sort);
    // A search shows its hits in one flat list, like claude.ai.
    if (searching) return { starred: [], rest: sorted };
    return {
      starred: sorted.filter((project) => project.starred),
      rest: sorted.filter((project) => !project.starred),
    };
  }, [projects, query, sort, searching]);
  const entries: Entry[] = [
    ...starred.map((project) => ({ kind: "project" as const, project })),
    ...rest.map((project) => ({ kind: "project" as const, project })),
    { kind: "create" },
  ];
  // -1: nothing highlighted yet, as on claude.ai until an arrow key or the
  // pointer moves; Enter then takes the first entry.
  const activeIndex = Math.min(active, entries.length - 1);

  const close = useCallback(() => {
    setOpen(false);
    setSortOpen(false);
  }, []);
  // closeAndFocus closes the popover (and applies `then`, a pick) in one
  // synchronous commit, then focuses the button: the search field that held
  // focus is gone. Trigger and chip are the same <button> node in the same
  // slot, so focus survives a pick or a removal.
  const closeAndFocus = useCallback(
    (then?: () => void) => {
      flushSync(() => {
        close();
        then?.();
      });
      buttonRef.current?.focus();
    },
    [close],
  );
  const closeSort = useCallback(() => setSortOpen(false), []);
  useOutsideRefPointerDown(open, rootRef, close);
  useOutsideRefPointerDown(sortOpen, sortRef, closeSort);
  useEscapeKey(() => closeAndFocus(), { active: open });
  // Registered after the popover's, so Escape closes the sort menu first.
  useEscapeKey(closeSort, { active: sortOpen });

  // Written straight to the list's style: a scroll moves the fade on every
  // event near an edge, and a render of the whole picker each time buys nothing.
  const measureList = useCallback(() => {
    const list = listRef.current;
    if (list === null) return;
    const { top, bottom } = edgeFade(list);
    list.style.setProperty("--fade-top", `${top}px`);
    list.style.setProperty("--fade-bottom", `${bottom}px`);
  }, []);
  useLayoutEffect(measureList, [measureList, open, query, sort, projects]);

  useEffect(() => {
    if (!open || activeIndex < 0) return;
    document
      .getElementById(optionID(activeIndex))
      ?.scrollIntoView?.({ block: "nearest" });
    // optionID only joins strings; activeIndex is what moves.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open, activeIndex]);

  function toggle() {
    if (open) {
      close();
      return;
    }
    setQuery("");
    setActive(-1);
    setOpen(true);
  }

  function choose(entry: Entry) {
    if (entry.kind === "project") {
      closeAndFocus(() => onSelect(entry.project.id));
      return;
    }
    // Focus sits on the trigger before the dialog opens: the dialog takes it
    // for its name field and hands it back to the trigger (or the chip of the
    // new project) when it closes.
    closeAndFocus();
    onCreateProject();
  }

  function handleKeyDown(event: React.KeyboardEvent<HTMLInputElement>) {
    // The Enter that confirms an IME composition belongs to the input method.
    // Safari sends that Enter with isComposing false but keyCode 229.
    if (event.nativeEvent.isComposing || event.keyCode === 229) return;
    if (event.key === "ArrowDown" || event.key === "ArrowUp") {
      event.preventDefault();
      const count = entries.length;
      if (activeIndex < 0) {
        setActive(event.key === "ArrowDown" ? 0 : count - 1);
      } else {
        const step = event.key === "ArrowDown" ? 1 : -1;
        setActive((activeIndex + step + count) % count);
      }
    } else if (event.key === "Enter") {
      event.preventDefault();
      choose(entries[Math.max(activeIndex, 0)]);
    }
  }

  function option(entry: Entry, index: number) {
    const isActive = index === activeIndex;
    const label =
      entry.kind === "create"
        ? t("projects.page.newProject")
        : entry.project.name;
    return (
      <div
        key={entry.kind === "create" ? "create" : entry.project.id}
        aria-selected={isActive}
        // scroll-my-7: the highlight scrolls its row in clear of the 28px fade.
        className={`${rowClass} cursor-pointer scroll-my-7 ${isActive ? "bg-ink/15" : ""}`}
        id={optionID(index)}
        onClick={() => choose(entry)}
        onMouseMove={() => {
          if (!isActive) setActive(index);
        }}
        role="option"
      >
        <span
          aria-hidden
          className="grid h-5 w-5 shrink-0 place-items-center text-ink-bright"
        >
          {/* loom's project glyph, as in the sidebar. */}
          <Icon
            name={entry.kind === "create" ? "plus" : "archive"}
            size={entry.kind === "create" ? "16px" : "18px"}
          />
        </span>
        <span className="min-w-0 flex-1 truncate">{label}</span>
      </div>
    );
  }

  return (
    <div ref={rootRef} className="relative">
      {selectedProject === null ? (
        <button
          ref={buttonRef}
          aria-controls={open ? `${baseID}-dialog` : undefined}
          aria-expanded={open}
          aria-haspopup="dialog"
          className={`flex h-7 items-center gap-1 rounded-lg px-2.5 text-sm font-[435] leading-5 text-ink-bright transition-colors duration-200 hover:bg-ink/5 ${open ? "bg-ink/5" : ""}`}
          onClick={toggle}
          type="button"
        >
          {t("projects.startPicker.trigger")}
          <Icon name="chevronDown" size="12px" className="text-muted" />
        </button>
      ) : (
        <button
          ref={buttonRef}
          aria-label={t("projects.startPicker.remove", {
            name: selectedProject.name,
          })}
          className="flex h-7 max-w-full items-center gap-1 rounded-lg bg-ink/5 px-2.5 text-sm font-[435] leading-5 text-ink-bright transition-colors duration-200 hover:bg-ink/10"
          // The chip turns back into the trigger in the same node; focus stays.
          onClick={() => onSelect(null)}
          type="button"
        >
          <span className="min-w-0 truncate">{selectedProject.name}</span>
          <Icon name="close" size="12px" />
        </button>
      )}
      {open && (
        <PlacedPopover
          aria-label={t("projects.startPicker.menuLabel")}
          className="absolute left-0 z-30 flex w-[510px] max-w-[calc(100vw-2rem)] flex-col rounded-[14px] bg-popover text-sm text-ink-bright shadow-[inset_0_0_0_1px_rgba(255,255,255,0.04),0_18px_32px_rgba(0,0,0,0.38)]"
          id={`${baseID}-dialog`}
        >
          <div className="px-1.5 pt-1.5">
            <div className="flex h-8 items-center gap-2 pl-3 pr-1">
              <Icon name="search" size="16px" className="shrink-0 text-muted" />
              <input
                aria-activedescendant={
                  activeIndex < 0 ? undefined : optionID(activeIndex)
                }
                aria-autocomplete="list"
                aria-controls={`${baseID}-list`}
                aria-expanded
                aria-label={t("projects.startPicker.search")}
                autoFocus
                className="h-7 min-w-0 flex-1 bg-transparent text-sm leading-5 text-ink-bright outline-none placeholder:text-muted"
                onChange={(event) => {
                  setQuery(event.target.value);
                  setActive(-1);
                }}
                onKeyDown={handleKeyDown}
                placeholder={t("projects.startPicker.search")}
                role="combobox"
                type="text"
                value={query}
              />
              <div ref={sortRef} className="relative shrink-0">
                <button
                  aria-expanded={sortOpen}
                  aria-haspopup="menu"
                  aria-label={t("projects.startPicker.sortLabel")}
                  className={`grid h-7 w-7 place-items-center rounded-lg text-muted transition-colors hover:bg-ink/5 hover:text-ink-bright ${sortOpen ? "bg-ink/5 text-ink-bright" : ""}`}
                  onClick={() => setSortOpen((value) => !value)}
                  title={t("projects.startPicker.sortLabel")}
                  type="button"
                >
                  <Icon name="sliders" size="16px" />
                </button>
                {sortOpen && (
                  <div
                    aria-label={t("projects.startPicker.sortLabel")}
                    className="absolute right-0 top-full z-10 mt-1 w-[180px] rounded-xl bg-popover p-1.5 shadow-[inset_0_0_0_1px_rgba(255,255,255,0.06),0_18px_32px_rgba(0,0,0,0.38)]"
                    role="menu"
                  >
                    <div
                      className="px-2.5 py-1 text-[13px] font-[435] leading-[17px] text-muted"
                      role="presentation"
                    >
                      {t("projects.page.sortBy")}
                    </div>
                    {SORTS.map((option) => (
                      <button
                        key={option}
                        aria-checked={sort === option}
                        className={`${rowClass} justify-between hover:bg-ink/15`}
                        onClick={() => {
                          setSort(option);
                          setActive(-1);
                          setSortOpen(false);
                        }}
                        role="menuitemradio"
                        type="button"
                      >
                        {t(`projects.page.sort.${option}`)}
                        {sort === option && (
                          <Icon
                            name="check"
                            size="14px"
                            className="text-select"
                          />
                        )}
                      </button>
                    ))}
                  </div>
                )}
              </div>
            </div>
            <div className={separatorClass} role="presentation" />
          </div>
          <div
            aria-label={t("projects.startPicker.menuLabel")}
            id={`${baseID}-list`}
            role="listbox"
          >
            <div
              ref={listRef}
              className="scroll-fade-y max-h-[178px] overflow-y-auto px-1.5 pb-1.5"
              onScroll={measureList}
            >
              {starred.length > 0 && (
                <div
                  className="h-[25px] px-2.5 py-1 text-[13px] font-[435] leading-[17px] text-muted"
                  role="presentation"
                >
                  {t("projects.startPicker.starred")}
                </div>
              )}
              {starred.map((project, index) =>
                option({ kind: "project", project }, index),
              )}
              {starred.length > 0 && rest.length > 0 && (
                <div className={separatorClass} role="presentation" />
              )}
              {rest.map((project, index) =>
                option({ kind: "project", project }, starred.length + index),
              )}
              {searching && entries.length === 1 && (
                <div className="px-2.5 py-1 text-muted" role="presentation">
                  {t("projects.startPicker.empty")}
                </div>
              )}
            </div>
            <div className="px-1.5 pb-1.5">
              <div className={separatorClass} role="presentation" />
              {option({ kind: "create" }, entries.length - 1)}
            </div>
          </div>
        </PlacedPopover>
      )}
    </div>
  );
}

// PlacedPopover opens below its anchor, or above it when the scroll column has
// no room below (a phone, a short window). Its own component so the placement
// is measured when the popover mounts, not when the picker does.
function PlacedPopover({
  className,
  children,
  ...rest
}: {
  className: string;
  children: React.ReactNode;
  id: string;
  "aria-label": string;
}) {
  // gap 6: the popover's mt-1.5 / mb-1.5.
  const { menuRef, dropUp } = useMenuPlacement({ fitAbove: true, gap: 6 });
  return (
    <div
      ref={menuRef}
      className={`${className} ${dropUp ? "bottom-full mb-1.5" : "top-full mt-1.5"}`}
      role="dialog"
      {...rest}
    >
      {children}
    </div>
  );
}
