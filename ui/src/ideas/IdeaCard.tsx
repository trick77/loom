import { useState, useSyncExternalStore } from "react";
import { useTranslation } from "react-i18next";

import {
  announce,
  confirmIdea,
  deleteIdea,
  getIdea,
  IdeaGoneError,
  IDEAS_CHANGED,
  restoreIdea,
  updateIdea,
  type Idea,
  type IdeaCardData,
} from "../api";
import { describeActionError } from "../chat/actionErrors";
import { Icon } from "../chat/Icon";
import { IdeaEditDialog } from "./IdeaEditDialog";
import { ideaDates } from "./ideaText";

// A card's state after the user's click, as the server answered it ("dismissed"
// for a discarded proposal). Kept above the bubbles, by idea and action, so
// the swap from the streaming answer to the stored one shows the outcome,
// not the question again. It holds only while the card still shows the
// state it was clicked in: a reload that brings a newer state (the idea was
// changed elsewhere) wins.
// "kept" is Keep / Keep open: nothing changed on the server, so a reload
// asks again.
type Outcome = IdeaCardData["state"] | "dismissed" | "kept";
type Clicked = { from: IdeaCardData["state"]; to: Outcome };
const outcomes = new Map<string, Clicked>();
const listeners = new Set<() => void>();
const keyOf = (card: IdeaCardData) => `${card.ideaId}:${card.action}`;

function setOutcome(card: IdeaCardData, to: Outcome) {
  outcomes.set(keyOf(card), { from: card.state, to });
  listeners.forEach((listener) => listener());
}

function subscribe(listener: () => void) {
  listeners.add(listener);
  return () => listeners.delete(listener);
}

/** forgetCardOutcomes drops every kept click (tests start clean). */
export function forgetCardOutcomes() {
  outcomes.clear();
}

// openIdeaPage goes to /ideas/:id through the shell's own router (it
// listens to popstate).
function openIdeaPage(ideaID: string) {
  window.history.pushState({}, "", `/ideas/${encodeURIComponent(ideaID)}`);
  window.dispatchEvent(new PopStateEvent("popstate"));
}

const buttonBase =
  "inline-flex items-center rounded-lg px-2.5 py-1 font-sans text-[13px] font-medium leading-5 transition-colors disabled:opacity-50";
const solid = `${buttonBase} bg-[#343433] text-[#f5f3ee] hover:bg-[#3d3d3b]`;
const white = `${buttonBase} bg-white text-[#1d1d1b] hover:bg-[#ece9e2]`;
const ghost = `${buttonBase} text-[#a8a59c] hover:text-ink`;

/**
 * IdeaCard is an idea tool's proposal in the thread. Nothing it shows is
 * saved, deleted or done until the user clicks here: the card names in full
 * (plain text, never markup) what the click commits.
 */
export function IdeaCard({ card }: { card: IdeaCardData }) {
  const { t, i18n } = useTranslation();
  const clicked = useSyncExternalStore(subscribe, () =>
    outcomes.get(keyOf(card)),
  );
  const state: Outcome =
    clicked !== undefined && clicked.from === card.state
      ? clicked.to
      : card.state;
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [editing, setEditing] = useState<Idea | null>(null);
  // An edit made here shows until the card brings newer fields.
  // Tied to the card version it was made on: a reload brings newer fields.
  const [edited, setEdited] = useState<{
    idea: Idea;
    on: IdeaCardData;
  } | null>(null);
  const shown = edited !== null && edited.on === card ? edited.idea : card;
  const kept = state === "kept";

  const fail = (err: unknown) => {
    // An expired session has no shell to send the user to from here:
    // the action is simply reported as failed.
    setError(
      describeActionError(err, t("ideas.actionFailed")) ??
        t("ideas.actionFailed"),
    );
  };
  // One request at a time; a 404 (handled elsewhere, purged) leaves the card
  // gone rather than failed.
  const act = async (run: () => Promise<Outcome>) => {
    if (busy) return;
    setBusy(true);
    setError("");
    try {
      setOutcome(card, await run());
      announce(IDEAS_CHANGED);
    } catch (err) {
      if (err instanceof IdeaGoneError) setOutcome(card, "gone");
      else fail(err);
    } finally {
      setBusy(false);
    }
  };
  const confirm = () => act(async () => (await confirmIdea(card.ideaId)).state);
  const remove = () =>
    act(async () => {
      await deleteIdea(card.ideaId);
      // Dismissing a proposal discards it; deleting an idea keeps it 30 days.
      return state === "proposed" ? "dismissed" : "deleted";
    });
  const restore = () => act(async () => (await restoreIdea(card.ideaId)).state);
  const setDone = (done: boolean) =>
    act(async () => (await updateIdea(card.ideaId, { done })).state);
  const edit = async () => {
    if (busy) return;
    setBusy(true);
    setError("");
    try {
      setEditing(await getIdea(card.ideaId));
    } catch (err) {
      if (err instanceof IdeaGoneError) setOutcome(card, "gone");
      else fail(err);
    } finally {
      setBusy(false);
    }
  };

  const pending =
    (card.action === "save" && state === "proposed") ||
    (card.action === "remove" && (state === "open" || state === "done")) ||
    (card.action === "done" && state === "open");

  if (state === "gone" || state === "dismissed" || kept) {
    return (
      <div className="flex max-w-[560px] items-center gap-2 font-sans text-[13px] text-muted">
        <Icon name="bulb" size="14px" className="shrink-0 text-[#5f5d57]" />
        {t(
          kept
            ? card.action === "done"
              ? "ideas.card.keptOpen"
              : "ideas.card.kept"
            : state === "dismissed"
              ? "ideas.card.dismissed"
              : "ideas.card.gone",
        )}
      </div>
    );
  }

  const deleted = state === "deleted";
  const label = pending
    ? t(`ideas.card.ask.${card.action}`)
    : deleted
      ? t("ideas.card.deleted")
      : card.action === "done" && state === "done"
        ? t("ideas.card.markedDone")
        : t("ideas.card.saved");
  const dates = ideaDates(shown, t, i18n.language);

  return (
    <article className="flex max-w-[560px] items-start gap-3.5 rounded-2xl border border-border bg-card py-3.5 pl-[18px] pr-3.5 font-sans">
      <Icon
        name={deleted ? "trash" : "bulb"}
        size="20px"
        className={`-ml-1 mt-px shrink-0 ${deleted ? "text-[#8f8b82]" : "text-unread-ink"}`}
      />
      <div className="min-w-0 flex-1">
        <h3 className="m-0 flex flex-wrap items-baseline gap-x-2.5 text-[15px] font-medium text-ink">
          <span
            className={`whitespace-pre-wrap break-words ${deleted ? "text-[#908e85] line-through" : ""}`}
          >
            {shown.title}
          </span>
          <small className="text-[12px] font-normal text-muted">{label}</small>
        </h3>
        {!deleted && (
          <p className="mt-1 whitespace-pre-wrap break-words font-serif text-[15px] font-[360] leading-[22px] text-ink-dim">
            <span className="mr-1.5 font-sans text-[12px] font-normal text-[#807d74]">
              {t("ideas.why")}
            </span>
            {shown.reason}
          </p>
        )}
        {!deleted && dates !== null && (
          <div className="mt-1.5 flex items-center gap-1.5 text-[13px] text-[#8a887f]">
            <Icon name="clock" size="13px" className="text-[#5f5d57]" />
            {dates}
          </div>
        )}
        {error !== "" && (
          <div className="mt-2 text-[13px] text-danger">{error}</div>
        )}
        <div className="mt-3 flex flex-wrap gap-2">
          {pending && card.action === "save" && (
            <>
              <button
                type="button"
                className={white}
                disabled={busy}
                onClick={() => void confirm()}
              >
                {t("ideas.card.save")}
              </button>
              <button
                type="button"
                className={ghost}
                disabled={busy}
                onClick={() => void remove()}
              >
                {t("ideas.card.dismiss")}
              </button>
            </>
          )}
          {pending && card.action === "remove" && (
            <>
              <button
                type="button"
                className={white}
                disabled={busy}
                onClick={() => void remove()}
              >
                {t("ideas.card.delete")}
              </button>
              <button
                type="button"
                className={ghost}
                disabled={busy}
                onClick={() => setOutcome(card, "kept")}
              >
                {t("ideas.card.keep")}
              </button>
            </>
          )}
          {pending && card.action === "done" && (
            <>
              <button
                type="button"
                className={white}
                disabled={busy}
                onClick={() => void setDone(true)}
              >
                {t("ideas.card.markDone")}
              </button>
              <button
                type="button"
                className={ghost}
                disabled={busy}
                onClick={() => setOutcome(card, "kept")}
              >
                {t("ideas.card.keepOpen")}
              </button>
            </>
          )}
          {!pending && deleted && (
            <button
              type="button"
              className={solid}
              disabled={busy}
              onClick={() => void restore()}
            >
              {t("ideas.undo")}
            </button>
          )}
          {!pending && !deleted && (
            <>
              <button
                type="button"
                className={solid}
                onClick={() => openIdeaPage(card.ideaId)}
              >
                {t("ideas.capture.view")}
              </button>
              <button
                type="button"
                className={ghost}
                disabled={busy}
                onClick={() => void edit()}
              >
                {t("ideas.edit")}
              </button>
              <button
                type="button"
                className={ghost}
                disabled={busy}
                onClick={() =>
                  void (card.action === "done" ? setDone(false) : remove())
                }
              >
                {t("ideas.undo")}
              </button>
            </>
          )}
        </div>
      </div>
      {editing !== null && (
        <IdeaEditDialog
          idea={editing}
          onClose={() => setEditing(null)}
          onSaved={(saved) => {
            setEditing(null);
            setEdited({ idea: saved, on: card });
            announce(IDEAS_CHANGED);
          }}
          onSessionExpired={() => {
            setEditing(null);
            setError(t("ideas.actionFailed"));
          }}
        />
      )}
    </article>
  );
}
