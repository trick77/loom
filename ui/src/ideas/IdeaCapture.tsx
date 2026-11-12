import { useEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";

import { announce, createIdea, IDEAS_CHANGED } from "../api";
import { describeActionError } from "../chat/actionErrors";
import { ErrorText } from "../chat/ErrorText";
import { Icon } from "../chat/Icon";
import { useEscapeKey } from "../chat/useEscapeKey";
import { useMediaQuery } from "../chat/useMediaQuery";
import { useMenuPlacement } from "../chat/useMenuPlacement";
import { FollowUpRow, type IdeaDates } from "./FollowUpRow";
import { canHover, ideaInputClass } from "./IdeaEditDialog";
import { useIdeaCapture, type CaptureRequest } from "./IdeaCaptureContext";
import { defaultRemindOn, REASON_MAX, TITLE_MAX, todayDay } from "./ideaText";
import { useIdeaDraft } from "./useIdeaDraft";

type Draft = {
  reason: string;
  title: string;
  titleEdited: boolean;
  dates: IdeaDates;
  /** The user set or dropped a date: the draft call no longer moves them. */
  datesEdited: boolean;
};

const noDates: IdeaDates = { dueOn: null, remindOn: null };

// drafts keeps what was typed into a capture that was closed unsaved, and
// the proposed title, by message, until it is opened again for that message
// (or saved): reopening asks the model nothing again.
const drafts = new Map<string, Draft>();

// saving holds the messages whose capture is being saved: it stays open
// until the answer, so a failure can still be shown.
const savingCaptures = new Set<string>();

/** captureSaving: a capture is being saved; nothing may close or replace it. */
export function captureSaving(): boolean {
  return savingCaptures.size > 0;
}

/** forgetCaptureDrafts drops every kept capture (tests start clean). */
export function forgetCaptureDrafts() {
  drafts.clear();
  savingCaptures.clear();
}

// useClose closes the capture unless it is saving.
function useClose(request: CaptureRequest): () => void {
  const capture = useIdeaCapture();
  return () => {
    if (!savingCaptures.has(request.messageId)) capture?.close();
  };
}

// SWIPE_CLOSE_PX is how far the sheet's handle is pulled down to close it.
const SWIPE_CLOSE_PX = 60;

/**
 * IdeaCapture saves the open capture of its thread: an anchored popover
 * under the answer on a wide screen, a bottom sheet on a narrow one. The
 * reason is required; the title comes from the draft call when the user has
 * not typed one.
 */
export function IdeaCapture({ request }: { request: CaptureRequest }) {
  const narrow = useMediaQuery("(max-width: 767px)");
  return narrow ? (
    <CaptureSheet request={request} />
  ) : (
    <CapturePopover request={request} />
  );
}

function CapturePopover({ request }: { request: CaptureRequest }) {
  const close = useClose(request);
  const ref = useRef<HTMLDivElement>(null);
  const { menuRef, dropUp } = useMenuPlacement({ fitAbove: true, gap: 6 });
  // A press outside closes it; its own bulb toggles it by its click instead
  // (closing here too would reopen it right away).
  const closeRef = useRef(close);
  useEffect(() => {
    closeRef.current = close;
  });
  useEffect(() => {
    const onPointerDown = (event: PointerEvent) => {
      const target = event.target as Element | null;
      if (target === null || ref.current?.contains(target)) return;
      if (
        target.closest(`[data-idea-bulb]`)?.getAttribute("data-idea-bulb") ===
        request.messageId
      )
        return;
      closeRef.current();
    };
    document.addEventListener("pointerdown", onPointerDown);
    return () => document.removeEventListener("pointerdown", onPointerDown);
  }, [request.messageId]);
  return (
    <div ref={ref}>
      <div
        ref={menuRef}
        role="dialog"
        aria-modal="false"
        className={`prompt-panel-in absolute left-0 z-30 w-[460px] max-w-[calc(100vw-32px)] rounded-[14px] bg-popover p-3.5 font-sans text-[14px] text-ink-bright shadow-[inset_0_0_0_1px_rgba(255,255,255,0.04),0_18px_32px_rgba(0,0,0,0.38)] ${dropUp ? "bottom-full mb-1.5" : "top-full mt-1.5"}`}
      >
        <CaptureForm request={request} autoFocus={canHover()} />
      </div>
    </div>
  );
}

// useKeyboardInset is how much of the layout viewport the on-screen keyboard
// covers (visualViewport), so a sheet fixed to the bottom stays above it.
function useKeyboardInset(): number {
  const [inset, setInset] = useState(0);
  useEffect(() => {
    const vv = window.visualViewport;
    if (vv === null || vv === undefined) return;
    const update = () =>
      setInset(Math.max(0, window.innerHeight - vv.height - vv.offsetTop));
    update();
    vv.addEventListener("resize", update);
    vv.addEventListener("scroll", update);
    return () => {
      vv.removeEventListener("resize", update);
      vv.removeEventListener("scroll", update);
    };
  }, []);
  return inset;
}

function CaptureSheet({ request }: { request: CaptureRequest }) {
  const { t } = useTranslation();
  const inset = useKeyboardInset();
  const [pull, setPull] = useState(0);
  const startY = useRef<number | null>(null);
  const close = useClose(request);
  return createPortal(
    <>
      <div
        className="fixed inset-0 z-40 bg-[rgba(10,10,9,0.55)]"
        aria-hidden="true"
        onClick={close}
      />
      <div
        role="dialog"
        aria-modal="true"
        aria-label={t("ideas.capture.title")}
        className="prompt-panel-in fixed inset-x-0 z-50 overflow-y-auto rounded-t-2xl bg-popover px-3.5 pt-2 font-sans text-[14px] text-ink-bright shadow-[0_-18px_40px_rgba(0,0,0,0.45)]"
        style={{
          bottom: inset,
          maxHeight: `calc(100dvh - ${inset}px - 24px)`,
          paddingBottom: "calc(16px + env(safe-area-inset-bottom))",
          transform: pull > 0 ? `translateY(${pull}px)` : undefined,
        }}
      >
        {/* The handle: pull it down to close. */}
        <div
          className="mx-auto mb-2 flex h-6 w-24 touch-none items-center justify-center"
          onPointerDown={(event) => {
            startY.current = event.clientY;
            event.currentTarget.setPointerCapture?.(event.pointerId);
          }}
          onPointerMove={(event) => {
            if (startY.current !== null)
              setPull(Math.max(0, event.clientY - startY.current));
          }}
          onPointerUp={() => {
            if (pull > SWIPE_CLOSE_PX) close();
            startY.current = null;
            setPull(0);
          }}
          onPointerCancel={() => {
            startY.current = null;
            setPull(0);
          }}
        >
          <span className="h-1 w-10 rounded-full bg-[#5f5d57]" />
        </div>
        {/* No autofocus on touch: the keyboard waits until the field is tapped. */}
        <CaptureForm request={request} autoFocus={false} />
      </div>
    </>,
    document.body,
  );
}

function CaptureForm({
  request,
  autoFocus,
}: {
  request: CaptureRequest;
  autoFocus: boolean;
}) {
  const capture = useIdeaCapture();
  const { t } = useTranslation();
  const kept = drafts.get(request.messageId);
  const [reason, setReason] = useState(kept?.reason ?? "");
  const [title, setTitle] = useState(kept?.title ?? "");
  const [titleEdited, setTitleEdited] = useState(kept?.titleEdited ?? false);
  const [dates, setDates] = useState<IdeaDates>(kept?.dates ?? noDates);
  const [datesEdited, setDatesEdited] = useState(kept?.datesEdited ?? false);
  const [today] = useState(() => todayDay());
  // A save survives this form remounting (popover ↔ sheet at the breakpoint).
  const [saving, setSaving] = useState(() =>
    savingCaptures.has(request.messageId),
  );
  // Read by a late proposal: a field the user emptied stays empty, dates the
  // user set stay.
  const editedRef = useRef({ title: titleEdited, dates: datesEdited });
  useEffect(() => {
    editedRef.current = { title: titleEdited, dates: datesEdited };
  }, [titleEdited, datesEdited]);
  const [error, setError] = useState("");
  const close = useClose(request);
  useEscapeKey(close);

  // What was typed, and a proposed title, survive a close until this
  // message's capture opens again.
  const latest = useRef<Draft>({
    reason,
    title,
    titleEdited,
    dates,
    datesEdited,
  });
  useEffect(() => {
    latest.current = { reason, title, titleEdited, dates, datesEdited };
  }, [reason, title, titleEdited, dates, datesEdited]);
  useEffect(() => {
    const messageId = request.messageId;
    return () => {
      const draft = latest.current;
      if (draft.reason.trim() !== "" || draft.title !== "")
        drafts.set(messageId, draft);
      else drafts.delete(messageId);
    };
  }, [request.messageId]);

  // The title is proposed as the capture opens, unless it has one (typed or
  // proposed before), and asked again as the reason changes, for the due
  // date it names. A proposed title stays once there (no title jumping
  // while the reason is typed); one the user typed is never overwritten. A
  // due date the reason names brings the server's default follow-up; dates
  // the user set or dropped are left alone, and an answer without a date
  // (no model, a busy or failed call) drops none.
  useIdeaDraft(
    request.excerpt,
    reason,
    // Nothing left to ask once the dates are the user's and the title is
    // set: no call whose answer would be thrown away.
    {
      askNow: title === "" && !titleEdited,
      enabled: !datesEdited || (title === "" && !titleEdited),
    },
    (draft) => {
      if (draft.title !== "")
        setTitle((current) =>
          current === "" && !editedRef.current.title ? draft.title : current,
        );
      if (draft.dueOn !== null && !editedRef.current.dates)
        setDates({
          dueOn: draft.dueOn,
          remindOn: defaultRemindOn(draft.dueOn, today),
        });
    },
  );

  const canSave = reason.trim() !== "" && !saving && capture !== null;
  const save = async () => {
    if (!canSave || capture === null) return;
    const { threadId } = capture;
    setSaving(true);
    savingCaptures.add(request.messageId);
    setError("");
    try {
      const idea = await createIdea({
        title,
        reason,
        excerpt: request.excerpt,
        threadId,
        messageId: request.messageId,
        // Both sent: the follow-up shown is the one saved.
        dueOn: dates.dueOn,
        remindOn: dates.remindOn,
      });
      drafts.delete(request.messageId);
      latest.current = {
        reason: "",
        title: "",
        titleEdited: false,
        dates: noDates,
        datesEdited: false,
      };
      announce(IDEAS_CHANGED);
      savingCaptures.delete(request.messageId);
      capture.saved(idea.id, threadId, request.messageId);
    } catch (err) {
      savingCaptures.delete(request.messageId);
      setError(describeActionError(err, t("ideas.actionFailed")) ?? "");
      setSaving(false);
    }
  };

  return (
    <form
      onSubmit={(event) => {
        event.preventDefault();
        void save();
      }}
    >
      <div className="flex items-center gap-2 font-medium">
        <Icon name="bulb" size="18px" className="text-unread-ink" />
        {t("ideas.capture.title")}
        <button
          type="button"
          aria-label={t("common.close")}
          className="ml-auto text-[#8f8b82] hover:text-white"
          onClick={close}
        >
          <Icon name="close" size="16px" />
        </button>
      </div>
      {request.excerpt !== "" && (
        // A two-line preview: paragraph breaks would only spend its lines.
        // Plain text, never markup (the passage is outside text).
        <blockquote className="mt-2.5 line-clamp-2 border-l-2 border-border-strong py-0.5 pl-3.5 font-serif text-[14px] font-[360] leading-5 text-[#a8a59c]">
          {request.excerpt.replace(/\s+/g, " ")}
        </blockquote>
      )}
      <label className="mt-3 block">
        <span className="mb-1.5 flex justify-between text-[12px] text-[#a8a59c]">
          {t("ideas.form.reason")}
          <span className="text-[#807d74]">{t("ideas.form.required")}</span>
        </span>
        <textarea
          autoFocus={autoFocus}
          rows={2}
          maxLength={REASON_MAX}
          value={reason}
          placeholder={t("ideas.form.reasonPlaceholder")}
          onChange={(event) => setReason(event.target.value)}
          className={`${ideaInputClass} block resize-none font-serif font-[360] leading-[22px]`}
        />
      </label>
      <FollowUpRow
        value={dates}
        today={today}
        onChange={(next) => {
          setDates(next);
          setDatesEdited(true);
        }}
      />
      <label className="mt-3 block">
        <span className="mb-1.5 flex justify-between text-[12px] text-[#a8a59c]">
          {t("ideas.form.title")}
          {!titleEdited && title !== "" && (
            <span className="text-[#807d74]">
              {t("ideas.capture.suggested")}
            </span>
          )}
        </span>
        <input
          maxLength={TITLE_MAX}
          value={title}
          onChange={(event) => {
            setTitle(event.target.value);
            setTitleEdited(true);
          }}
          className={ideaInputClass}
        />
      </label>
      {error !== "" && <ErrorText>{error}</ErrorText>}
      <div className="mt-3.5 flex items-center gap-2">
        {capture !== null && capture.threadTitle() !== "" && (
          <span className="min-w-0 truncate text-[12px] text-[#807d74]">
            {t("ideas.capture.source", { title: capture.threadTitle() })}
          </span>
        )}
        <button
          type="submit"
          disabled={!canSave}
          className="ml-auto inline-flex shrink-0 items-center rounded-lg bg-white px-3 py-1.5 text-[14px] font-medium text-[#1d1d1b] hover:bg-[#ece9e2] disabled:bg-[#282827] disabled:text-[#8c8a82]"
        >
          {t("ideas.capture.save")}
        </button>
      </div>
    </form>
  );
}
