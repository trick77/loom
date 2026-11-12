import { useCallback, useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";

import {
  announce,
  deleteIdea,
  getIdea,
  IDEAS_CHANGED,
  listIdeas,
  restoreIdea,
  updateIdea,
  type Idea,
  type IdeaState,
} from "../api";
import { describeActionError } from "../chat/actionErrors";
import { ErrorText } from "../chat/ErrorText";
import { Icon } from "../chat/Icon";
import { useEscapeKey } from "../chat/useEscapeKey";
import { useMenuPlacement } from "../chat/useMenuPlacement";
import { useOutsideRefPointerDown } from "../chat/useOutsidePointerDown";
import { SidebarOpenButton } from "../SidebarOpenButton";
import {
  menuDeleteItemClass,
  menuIconClass,
  menuItemClass,
} from "../ThreadActionsMenu";
import { IdeaEditDialog } from "./IdeaEditDialog";
import { ideaAge, ideaDates, ideaThreadDraft } from "./ideaText";
import { useIdeaCounts } from "./useOpenIdeasCount";

// UNDO_MS is how long the "deleted" toast offers its undo.
export const UNDO_MS = 8000;

type Removed = { idea: Idea; index: number };

const buttonBase =
  "inline-flex items-center gap-1.5 whitespace-nowrap rounded-lg px-3 py-1.5 text-[14px] font-medium leading-5 transition-colors";
const whiteButton = `${buttonBase} bg-white text-[#1d1d1b] hover:bg-[#ece9e2]`;
const solidButton = `${buttonBase} bg-[#343433] text-[#f5f3ee] hover:bg-[#3d3d3b] disabled:cursor-default disabled:bg-[#282827] disabled:text-[#8c8a82]`;
const ghostButton = `${buttonBase} text-[#a8a59c] hover:text-white`;

export function IdeasPage({
  initialIdeaID,
  onOpenSidebar,
  onStartThread,
  onSessionExpired,
}: {
  /** The idea to open on arrival (/ideas/:id). */
  initialIdeaID: string | null;
  onOpenSidebar(): void;
  /** Opens the new-thread screen with this draft; never sends. */
  onStartThread(text: string): void;
  onSessionExpired(): void;
}) {
  const { t } = useTranslation();
  const [tab, setTab] = useState<IdeaState>("open");
  const [items, setItems] = useState<Idea[] | null>(null);
  const [expanded, setExpanded] = useState<string | null>(initialIdeaID);
  const [editing, setEditing] = useState<Idea | "new" | null>(null);
  // Every delete the toast can still undo: a second delete joins the first
  // instead of taking its undo away.
  const [removed, setRemoved] = useState<Removed[]>([]);
  const [error, setError] = useState("");
  const [loadError, setLoadError] = useState("");
  // The arriving idea decides the tab before the first list loads.
  const [ready, setReady] = useState(initialIdeaID === null);

  const fail = useCallback(
    (err: unknown, fallback = t("ideas.actionFailed")) => {
      const message = describeActionError(err, fallback);
      if (message === null) onSessionExpired();
      else setError(message);
    },
    [t, onSessionExpired],
  );

  // The tabs' counts follow IDEAS_CHANGED like the sidebar's: whatever
  // changed an idea announces it once, both recount.
  const counts = useIdeaCounts();
  const changed = useCallback(() => announce(IDEAS_CHANGED), []);

  // The tab on screen, for an answer that lands after the user switched.
  const tabRef = useRef(tab);
  useEffect(() => {
    tabRef.current = tab;
  }, [tab]);
  // The delete in flight per idea: an undo waits for it, or the restore
  // could reach the server first and the delete win.
  const deletes = useRef(new Map<string, Promise<void>>());

  useEffect(() => {
    if (initialIdeaID === null) return;
    let cancelled = false;
    getIdea(initialIdeaID).then(
      (idea) => {
        if (cancelled) return;
        setTab(idea.state);
        setReady(true);
      },
      () => {
        if (!cancelled) setReady(true);
      },
    );
    return () => {
      cancelled = true;
    };
  }, [initialIdeaID]);

  useEffect(() => {
    if (!ready) return;
    let cancelled = false;
    listIdeas(tab).then(
      (list) => {
        if (cancelled) return;
        setItems(list);
        setLoadError("");
      },
      (err: unknown) => {
        if (cancelled) return;
        const message = describeActionError(err, t("ideas.loadFailed"));
        if (message === null) onSessionExpired();
        else setLoadError(message);
      },
    );
    return () => {
      cancelled = true;
    };
  }, [tab, ready, t, onSessionExpired]);

  // The opened idea scrolls into view once its row is there.
  const scrolled = useRef(false);
  useEffect(() => {
    if (scrolled.current || initialIdeaID === null || items === null) return;
    scrolled.current = true;
    document
      .getElementById(`idea-${initialIdeaID}`)
      ?.scrollIntoView?.({ block: "nearest" });
  }, [items, initialIdeaID]);

  // Stable: the toast's timer restarts whenever its onDismiss changes.
  const dismissRemoved = useCallback(() => setRemoved([]), []);

  const drop = (id: string) =>
    setItems((current) => (current ?? []).filter((i) => i.id !== id));

  const setDone = async (idea: Idea, done: boolean) => {
    setError("");
    try {
      await updateIdea(idea.id, { done });
      drop(idea.id);
      changed();
    } catch (err) {
      fail(err);
    }
  };

  // Delete is optimistic and asks nothing: the toast offers the undo.
  const remove = async (idea: Idea) => {
    setError("");
    const index = (items ?? []).findIndex((i) => i.id === idea.id);
    drop(idea.id);
    setRemoved((current) => [...current, { idea, index }]);
    const pending = deleteIdea(idea.id);
    deletes.current.set(idea.id, pending);
    try {
      await pending;
      changed();
    } catch (err) {
      // Only this delete leaves the toast, and the idea returns only to the
      // list it came from.
      setRemoved((current) => current.filter((r) => r.idea.id !== idea.id));
      if (tabRef.current === idea.state) {
        setItems((current) => insertAt(current ?? [], idea, index));
      }
      fail(err);
    } finally {
      deletes.current.delete(idea.id);
    }
  };

  // Undo brings back every delete the toast holds, the last one first so
  // each lands at its old index.
  const undo = async () => {
    const batch = [...removed].reverse();
    setRemoved([]);
    for (const { idea, index } of batch) {
      try {
        await deletes.current.get(idea.id);
        const back = await restoreIdea(idea.id);
        if (back.state === tabRef.current) {
          setItems((current) => insertAt(current ?? [], back, index));
        }
      } catch (err) {
        fail(err);
      }
    }
    changed();
  };

  const saved = (idea: Idea) => {
    setEditing(null);
    setItems((current) => {
      const list = current ?? [];
      if (list.some((i) => i.id === idea.id)) {
        return list.map((i) => (i.id === idea.id ? idea : i));
      }
      return idea.state === tab ? [idea, ...list] : list;
    });
    changed();
  };

  const empty = items !== null && items.length === 0;
  const noneAtAll = counts !== null && counts.open + counts.done === 0;

  return (
    <div className="flex h-full flex-col overflow-y-auto">
      <div className="relative mx-auto w-full max-w-[802px] px-4 pb-16 pt-10 md:px-6">
        <SidebarOpenButton variant="floating" onClick={onOpenSidebar} />
        <header className="flex flex-wrap items-center justify-between gap-2">
          <h1 className="font-serif text-[28px] font-medium leading-8 text-[#f4f0e8]">
            {t("ideas.title")}
          </h1>
          <button
            type="button"
            onClick={() => setEditing("new")}
            className={whiteButton}
          >
            <Icon name="plus" size="14px" />
            {t("ideas.newIdea")}
          </button>
        </header>
        <p className="mt-2 text-[14px] text-[#8f8b82]">{t("ideas.subtitle")}</p>
        {loadError !== "" && <ErrorText>{loadError}</ErrorText>}
        {error !== "" && <ErrorText>{error}</ErrorText>}
        {noneAtAll && empty ? (
          <div className="py-[72px] text-center">
            <Icon name="bulb" size="34px" className="text-[#5f5d57]" />
            <div className="mt-3 text-[#d8d5cc]">{t("ideas.empty")}</div>
            <div className="mx-auto mt-1 max-w-[420px] text-[14px] text-[#807d74]">
              {t("ideas.emptyHint")}
            </div>
          </div>
        ) : (
          <>
            <div className="mt-5 flex items-center gap-1" role="tablist">
              {(["open", "done"] as const).map((option) => (
                <button
                  key={option}
                  type="button"
                  role="tab"
                  aria-selected={tab === option}
                  onClick={() => {
                    if (option === tab) return;
                    setItems(null);
                    setError("");
                    setLoadError("");
                    setTab(option);
                    setExpanded(null);
                  }}
                  className={`ui-control-text rounded-lg px-3 py-1 ${tab === option ? "bg-track text-white" : "text-[#a8a59c] hover:text-white"}`}
                >
                  {t(option === "open" ? "ideas.tabOpen" : "ideas.tabDone", {
                    count: counts?.[option] ?? 0,
                  })}
                </button>
              ))}
            </div>
            {empty && (
              <div className="py-10 text-center text-[14px] text-[#807d74]">
                {t(tab === "open" ? "ideas.empty" : "ideas.emptyDone")}
              </div>
            )}
            <ul className="mt-4">
              {(items ?? []).map((idea) => (
                <IdeaRow
                  key={idea.id}
                  idea={idea}
                  open={expanded === idea.id}
                  onToggle={() =>
                    setExpanded((current) =>
                      current === idea.id ? null : idea.id,
                    )
                  }
                  onStartThread={() => onStartThread(ideaThreadDraft(idea, t))}
                  onDone={() => void setDone(idea, idea.state !== "done")}
                  onEdit={() => setEditing(idea)}
                  onDelete={() => void remove(idea)}
                />
              ))}
            </ul>
          </>
        )}
      </div>
      {editing !== null && (
        <IdeaEditDialog
          idea={editing === "new" ? null : editing}
          onClose={() => setEditing(null)}
          onSaved={saved}
          onSessionExpired={onSessionExpired}
        />
      )}
      {removed.length > 0 && (
        // Keyed by the last delete: each new one restarts the timer.
        <UndoToast
          key={removed[removed.length - 1].idea.id}
          text={
            removed.length === 1
              ? t("ideas.deleted", { title: removed[0].idea.title })
              : t("ideas.deletedMany", { count: removed.length })
          }
          onUndo={() => void undo()}
          onDismiss={dismissRemoved}
        />
      )}
    </div>
  );
}

function insertAt(list: Idea[], idea: Idea, index: number): Idea[] {
  if (list.some((i) => i.id === idea.id)) return list;
  const at = index < 0 || index > list.length ? 0 : index;
  return [...list.slice(0, at), idea, ...list.slice(at)];
}

function IdeaRow({
  idea,
  open,
  onToggle,
  onStartThread,
  onDone,
  onEdit,
  onDelete,
}: {
  idea: Idea;
  open: boolean;
  onToggle(): void;
  onStartThread(): void;
  onDone(): void;
  onEdit(): void;
  onDelete(): void;
}) {
  const { t, i18n } = useTranslation();
  const [menuOpen, setMenuOpen] = useState(false);
  const age = ideaAge(idea, t);
  const dates = ideaDates(idea, t, i18n.language);
  const done = idea.state === "done";
  return (
    <li
      id={`idea-${idea.id}`}
      className={`ui-idea-row relative rounded-[10px] ${
        open
          ? "my-1.5 bg-card shadow-[inset_0_0_0_1px_var(--color-border)]"
          : "border-t border-raised first:border-t-transparent"
      }`}
    >
      <button
        type="button"
        aria-expanded={open}
        onClick={onToggle}
        className={`block w-full rounded-[10px] px-3 py-3.5 text-left transition-colors ${open ? "" : "hover:bg-raised"}`}
      >
        <span className="flex items-baseline gap-3 pr-8 [@media(hover:hover)]:pr-0">
          <span
            className={`min-w-0 truncate text-[15px] ${done ? "text-[#908e85] line-through decoration-[#5f5d57]" : "text-[#ecece6]"}`}
          >
            {idea.title}
          </span>
          <span
            className={`ui-idea-age ml-auto shrink-0 text-[13px] ${age.warn ? "text-unread-ink" : "text-[#8a887f]"} ${menuOpen ? "invisible" : ""}`}
          >
            {age.text}
          </span>
        </span>
        <span
          className={`mt-1 block whitespace-pre-wrap font-serif font-[360] ${open ? "text-[16px] leading-6 text-ink-soft" : "line-clamp-2 text-[15px] leading-[22px] text-ink-dim"}`}
        >
          <span className="mr-1.5 font-sans text-[12px] font-normal tracking-[0.01em] text-[#807d74]">
            {t("ideas.why")}
          </span>
          {idea.reason}
        </span>
        <span className="mt-2 flex flex-wrap items-center gap-1.5 text-[13px] text-[#8a887f]">
          {idea.threadTitle === "" && !idea.threadDeleted ? (
            <span>{t("ideas.noThread")}</span>
          ) : (
            <>
              <span>{t("ideas.from")}</span>
              <span
                className={
                  idea.threadDeleted
                    ? "text-[#8a887f] line-through decoration-[#5f5d57]"
                    : "text-[#b9b6ad]"
                }
              >
                {idea.threadTitle}
              </span>
              {idea.threadDeleted && <span>{t("ideas.threadDeleted")}</span>}
            </>
          )}
          {dates !== null && (
            <>
              <span>·</span>
              <Icon name="clock" size="13px" className="text-[#5f5d57]" />
              <span>{dates}</span>
            </>
          )}
        </span>
      </button>
      <IdeaMenu
        done={done}
        open={menuOpen}
        onOpenChange={setMenuOpen}
        onDone={onDone}
        onEdit={onEdit}
        onDelete={onDelete}
      />
      {open && (
        <div className="prompt-panel-in px-3 pb-3.5">
          {idea.excerpt !== "" && (
            <>
              <div className="mb-1.5 mt-3.5 text-[12px] text-[#807d74]">
                {t("ideas.passage")}
              </div>
              <blockquote className="whitespace-pre-wrap border-l-2 border-border-strong py-0.5 pl-3.5 font-serif text-[15px] font-[360] leading-[23px] text-[#a8a59c]">
                {idea.excerpt}
              </blockquote>
            </>
          )}
          <div className="mt-4 flex flex-wrap items-center gap-2">
            <button
              type="button"
              className={whiteButton}
              onClick={onStartThread}
            >
              <Icon name="messages" size="14px" />
              {t("ideas.startThread")}
            </button>
            <button
              type="button"
              className={solidButton}
              disabled
              title={idea.threadDeleted ? t("ideas.threadGone") : undefined}
            >
              <Icon name="externalLink" size="14px" />
              {t("ideas.jumpToPassage")}
            </button>
            <button type="button" className={ghostButton} onClick={onDone}>
              <Icon name="check" size="14px" />
              {t(done ? "ideas.reopen" : "ideas.done")}
            </button>
            <span className="ml-auto flex gap-0.5">
              <button
                type="button"
                aria-label={t("ideas.edit")}
                title={t("ideas.edit")}
                onClick={onEdit}
                className="grid h-7 w-7 place-items-center rounded-md text-[#a8a59c] hover:bg-[#343433] hover:text-white"
              >
                <Icon name="edit" size="17px" />
              </button>
              <button
                type="button"
                aria-label={t("ideas.delete")}
                title={t("ideas.delete")}
                onClick={onDelete}
                className="grid h-7 w-7 place-items-center rounded-md text-[#a8a59c] hover:bg-[#343433] hover:text-white"
              >
                <Icon name="trash" size="17px" />
              </button>
            </span>
          </div>
        </div>
      )}
    </li>
  );
}

// IdeaMenu is a row's kebab: shown on hover, always on a touch screen.
function IdeaMenu({
  done,
  open,
  onOpenChange,
  onDone,
  onEdit,
  onDelete,
}: {
  done: boolean;
  open: boolean;
  onOpenChange(open: boolean): void;
  onDone(): void;
  onEdit(): void;
  onDelete(): void;
}) {
  const { t } = useTranslation();
  const ref = useRef<HTMLDivElement>(null);
  const { menuRef, verticalClass } = useMenuPlacement();
  useOutsideRefPointerDown(open, ref, () => onOpenChange(false));
  useEscapeKey(() => onOpenChange(false), { active: open });
  const item = (
    label: string,
    icon: Parameters<typeof Icon>[0]["name"],
    onClick: () => void,
    danger = false,
  ) => (
    <button
      type="button"
      role="menuitem"
      className={`${danger ? menuDeleteItemClass : menuItemClass} ${danger ? "" : "text-ink-bright"}`}
      onClick={() => {
        onOpenChange(false);
        onClick();
      }}
    >
      <Icon name={icon} size="16px" className={menuIconClass} />
      {label}
    </button>
  );
  return (
    <div ref={ref} className="absolute right-2 top-[9px]">
      <button
        type="button"
        aria-label={t("ideas.menu")}
        aria-expanded={open}
        onClick={() => onOpenChange(!open)}
        className="ui-idea-kebab grid h-7 w-7 place-items-center rounded-md text-[#d8d4ca] hover:bg-[#343433] hover:text-white"
      >
        <Icon name="moreVertical" size="18px" />
      </button>
      {open && (
        <div
          ref={menuRef}
          role="menu"
          className={`ui-sidebar-text absolute right-0 z-20 w-[176px] rounded-[10px] border border-[#454540] bg-[#363632] p-1 shadow-[0_18px_32px_rgba(0,0,0,0.38)] ${verticalClass}`}
        >
          {item(t(done ? "ideas.reopen" : "ideas.done"), "check", onDone)}
          {item(t("ideas.edit"), "edit", onEdit)}
          <div className="mx-0.5 my-1 h-px bg-[#454540]" />
          {item(t("ideas.delete"), "trash", onDelete, true)}
        </div>
      )}
    </div>
  );
}

// UndoToast offers to take a delete back: it stays UNDO_MS or until
// dismissed (Escape too).
function UndoToast({
  text,
  onUndo,
  onDismiss,
}: {
  text: string;
  onUndo(): void;
  onDismiss(): void;
}) {
  const { t } = useTranslation();
  useEscapeKey(onDismiss);
  useEffect(() => {
    const handle = window.setTimeout(onDismiss, UNDO_MS);
    return () => window.clearTimeout(handle);
  }, [onDismiss]);
  return (
    <div className="pointer-events-none fixed inset-x-0 bottom-6 z-50 flex justify-center px-4">
      <div
        role="status"
        className="prompt-panel-in pointer-events-auto flex min-w-0 max-w-full items-center gap-2.5 rounded-[10px] border border-[#454540] bg-[#363632] px-3 py-2 text-[14px] shadow-[0_18px_32px_rgba(0,0,0,0.38)]"
      >
        <Icon name="trash" size="15px" className="shrink-0 text-[#8f8b82]" />
        <span className="min-w-0 truncate text-[#ecece6]">{text}</span>
        <button
          type="button"
          className="shrink-0 text-unread-ink hover:text-white"
          onClick={onUndo}
        >
          {t("ideas.undo")}
        </button>
        <button
          type="button"
          aria-label={t("common.close")}
          className="shrink-0 text-[#8f8b82] hover:text-white"
          onClick={onDismiss}
        >
          <Icon name="close" size="14px" />
        </button>
      </div>
    </div>
  );
}
