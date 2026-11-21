import { useEffect, useState, useSyncExternalStore } from "react";
import { useTranslation } from "react-i18next";

import {
  dismissToolAction,
  getToolAction,
  runToolAction,
  ToolActionGoneError,
  ToolActionServerError,
  type ActionCardData,
  type ActionItem,
} from "../api";
import { describeActionError } from "./actionErrors";
import { Icon } from "./Icon";

// The card as the server answered the user's click, kept above the bubbles
// by action, so the swap from the streaming answer to the stored one shows
// the outcome, not the question again. It holds only while the card still
// shows the state it was clicked in: a reload that brings a newer state wins.
type Clicked = { from: ActionCardData["state"]; to: ActionCardData };
const outcomes = new Map<string, Clicked>();
const listeners = new Set<() => void>();

function setOutcome(card: ActionCardData, to: ActionCardData) {
  outcomes.set(card.actionId, { from: card.state, to });
  listeners.forEach((listener) => listener());
}

function subscribe(listener: () => void) {
  listeners.add(listener);
  return () => listeners.delete(listener);
}

/** forgetActionOutcomes drops every kept click (tests start clean). */
export function forgetActionOutcomes() {
  outcomes.clear();
}

/**
 * A row the card lists: one element of an argument. extra holds every field
 * the title and link do not show, as "name: value": the card shows all the
 * click sends.
 */
export type ActionRow = { title: string; url: string | null; extra: string[] };

const httpURL = (value: unknown): string | null => {
  if (typeof value !== "string") return null;
  try {
    const url = new URL(value.trim());
    return url.protocol === "https:" || url.protocol === "http:"
      ? url.href
      : null;
  } catch {
    return null;
  }
};

const textOf = (value: unknown): string =>
  typeof value === "string" ? value : JSON.stringify(value);

const firstKey = (fields: Record<string, unknown>, keys: string[]) =>
  keys.find((key) => fields[key] !== undefined);

function rowOf(value: unknown): ActionRow {
  if (value !== null && typeof value === "object" && !Array.isArray(value)) {
    const fields = value as Record<string, unknown>;
    const urlKey = firstKey(fields, ["url", "link", "href"]);
    const url = urlKey === undefined ? null : httpURL(fields[urlKey]);
    const titleKey = firstKey(fields, ["title", "name", "label"]);
    const named = titleKey === undefined ? undefined : fields[titleKey];
    const hasTitle = typeof named === "string" && named.trim() !== "";
    const title = hasTitle ? named : "";
    // A url that is no http(s) link stays as text with the rest.
    const shown = new Set(
      [
        hasTitle ? titleKey : undefined,
        url !== null ? urlKey : undefined,
      ].filter((key) => key !== undefined),
    );
    const extra = Object.entries(fields)
      .filter(([key]) => !shown.has(key))
      .map(([key, field]) => `${key}: ${textOf(field)}`);
    return { title, url, extra };
  }
  const url = httpURL(value);
  return { title: url === null ? textOf(value) : "", url, extra: [] };
}

/**
 * actionRows lays the call's arguments out as rows: the elements of its
 * first array argument (the one a result's items line up with), then any
 * other argument as "name: value". Everything is the model's text: plain
 * text, a link only for http(s).
 */
export function actionRows(args: Record<string, unknown>): {
  list: ActionRow[];
  rest: ActionRow[];
} {
  let list: ActionRow[] | null = null;
  const rest: ActionRow[] = [];
  for (const [name, value] of Object.entries(args ?? {})) {
    if (list === null && Array.isArray(value)) {
      list = value.map(rowOf);
      continue;
    }
    const row = rowOf(value);
    rest.push({
      ...row,
      title: row.title !== "" ? `${name}: ${row.title}` : `${name}:`,
    });
  }
  return { list: list ?? [], rest };
}

// shownURL is a link as the card prints it: no scheme, no "www.".
const shownURL = (url: string) =>
  url.replace(/^https?:\/\//, "").replace(/^www\./, "");

const buttonBase =
  "inline-flex items-center gap-1.5 rounded-lg px-2.5 py-1 font-sans text-[13px] font-medium leading-5 transition-colors disabled:opacity-50";
const solid = `${buttonBase} bg-[#343433] text-[#f5f3ee] hover:bg-[#3d3d3b]`;
const white = `${buttonBase} bg-white text-[#1d1d1b] hover:bg-[#ece9e2]`;
const ghost = `${buttonBase} text-[#a8a59c] hover:text-ink`;

// A status the server named that loom does not know is shown as such,
// never as done.
function StatusTag({ item }: { item: ActionItem }) {
  const { t } = useTranslation();
  const done = item.status === "done";
  return (
    <span className="inline-flex shrink-0 items-center gap-1.5 whitespace-nowrap text-[12px] text-muted">
      <Icon
        name={
          done ? "check" : item.status === "" ? "moreHorizontal" : "checkCircle"
        }
        size="13px"
        className={done ? "text-success" : "text-[#5f5d57]"}
      />
      {item.message || t(`toolActions.status.${item.status || "unknown"}`)}
    </span>
  );
}

// pollRunning is how often a card that shows a call running elsewhere (a
// click in another tab) asks how it stands.
const pollRunning = 3000;

/**
 * ActionCard is a confirm server's tool call in the thread. Nothing runs
 * until the user clicks Run: the card shows in full (plain text, links only
 * for http(s)) what the click sends.
 */
export function ActionCard({ card }: { card: ActionCardData }) {
  const { t } = useTranslation();
  const clicked = useSyncExternalStore(subscribe, () =>
    outcomes.get(card.actionId),
  );
  const shown =
    clicked !== undefined && clicked.from === card.state ? clicked.to : card;
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  // A call running without this card's click (another tab) settles there:
  // ask until it does.
  const runningElsewhere = shown.state === "running" && !busy;
  useEffect(() => {
    if (!runningElsewhere) return;
    const timer = window.setInterval(() => {
      getToolAction(card.actionId).then(
        (next) => {
          if (next.state !== "running") setOutcome(card, next);
        },
        () => {},
      );
    }, pollRunning);
    return () => window.clearInterval(timer);
  }, [runningElsewhere, card]);

  const act = async (verb: "run" | "dismiss") => {
    if (busy) return;
    setBusy(true);
    setError("");
    try {
      const next =
        verb === "run"
          ? await runToolAction(card.actionId)
          : await dismissToolAction(card.actionId);
      setOutcome(card, next);
    } catch (err) {
      if (err instanceof ToolActionGoneError) {
        setOutcome(card, { ...shown, state: "gone" });
      } else if (err instanceof ToolActionServerError) {
        setError(t("toolActions.serverFailed"));
      } else {
        setError(
          describeActionError(err, t("toolActions.actionFailed")) ??
            t("toolActions.actionFailed"),
        );
      }
    } finally {
      setBusy(false);
    }
  };

  const state = shown.state;
  if (state === "dismissed" || state === "gone") {
    return (
      <div className="flex max-w-[560px] items-center gap-2 font-sans text-[13px] text-muted">
        <Icon name="play" size="14px" className="shrink-0 text-[#5f5d57]" />
        {t(`toolActions.${state}`)}
      </div>
    );
  }

  const unknown = state === "unknown";
  const running = busy || state === "running";
  // Settled: nothing more to click.
  const settled = state === "done" || state === "failed" || unknown;
  const { list, rest } = actionRows(shown.arguments);
  const items =
    state === "done" || state === "failed" ? (shown.result?.items ?? []) : [];
  const counts = { done: 0, skipped: 0, failed: 0 };
  for (const item of items) {
    if (item.status !== "") counts[item.status]++;
  }
  const counted = (["done", "skipped", "failed"] as const)
    .filter((key) => counts[key] > 0)
    .map((key) => t(`toolActions.count.${key}`, { count: counts[key] }))
    .join(" · ");
  const summary = unknown
    ? t("toolActions.unknown")
    : settled
      ? state === "failed"
        ? t("toolActions.failed")
        : counted || t("toolActions.done")
      : running
        ? t("toolActions.running")
        : t("toolActions.ask");
  const title = shown.title || shown.server;
  const link = settled ? shown.result?.link : undefined;
  // The server's text shows when no status speaks for it: a failure with a
  // reason, or a result without items.
  const resultText =
    settled && !unknown && items.length === 0 ? (shown.result?.text ?? "") : "";

  return (
    <article className="flex max-w-[560px] items-start gap-3.5 rounded-2xl border border-border bg-card py-3.5 pl-[18px] pr-3.5 font-sans">
      <Icon
        name={
          state === "done"
            ? "check"
            : state === "failed" || unknown
              ? "alertCircle"
              : "play"
        }
        size="20px"
        className={`-ml-1 mt-px shrink-0 ${
          state === "done"
            ? "text-success"
            : state === "failed"
              ? "text-danger"
              : unknown
                ? "text-[#8f8b82]"
                : "text-unread-ink"
        }`}
      />
      <div className="min-w-0 flex-1">
        <h3 className="m-0 flex flex-wrap items-baseline gap-x-2.5 text-[15px] font-medium text-ink">
          <span className="whitespace-pre-wrap break-words">{title}</span>
          <small className="text-[12px] font-normal text-muted">
            {shown.title ? `${shown.server} · ${summary}` : summary}
          </small>
        </h3>
        {list.length + rest.length > 0 && (
          <ul className="m-0 mt-2 flex list-none flex-col gap-2 p-0">
            {[...list, ...rest].map((row, index) => {
              const item = index < list.length ? items[index] : undefined;
              const failed = item?.status === "failed";
              return (
                <li key={index} className="flex min-w-0 items-baseline gap-3">
                  <div className="min-w-0 flex-1">
                    {row.title !== "" && (
                      <div
                        className={`whitespace-pre-wrap break-words font-serif text-[15px] font-[360] leading-[22px] ${failed ? "text-muted" : "text-ink-dim"}`}
                      >
                        {row.title}
                      </div>
                    )}
                    {row.url !== null && (
                      <a
                        href={row.url}
                        target="_blank"
                        rel="noopener noreferrer"
                        className="inline-flex items-center gap-1.5 break-all text-[13px] text-[#8a887f] no-underline hover:text-ink"
                      >
                        {shownURL(row.url)}
                        <Icon
                          name="externalLink"
                          size="12px"
                          className="shrink-0 text-[#5f5d57]"
                        />
                      </a>
                    )}
                    {row.extra.map((line, lineIndex) => (
                      <div
                        key={lineIndex}
                        className="whitespace-pre-wrap break-all text-[13px] text-[#8a887f]"
                      >
                        {line}
                      </div>
                    ))}
                    {failed && item.message !== "" && (
                      <div className="mt-0.5 text-[13px] text-danger">
                        {item.message}
                      </div>
                    )}
                  </div>
                  {item !== undefined && !failed && <StatusTag item={item} />}
                </li>
              );
            })}
          </ul>
        )}
        {resultText !== "" && (
          <p
            className={`mb-0 mt-2 whitespace-pre-wrap break-words text-[13px] ${state === "failed" ? "text-danger" : "text-ink-dim"}`}
          >
            {resultText}
          </p>
        )}
        {state === "failed" && resultText === "" && (
          <p className="mb-0 mt-2 text-[13px] text-danger">
            {t("toolActions.failedGeneric")}
          </p>
        )}
        {unknown && (
          <p className="mb-0 mt-2 text-[13px] text-ink-dim">
            {t("toolActions.unknownHint", { server: shown.server })}
          </p>
        )}
        {error !== "" && (
          <div className="mt-2 text-[13px] text-danger">{error}</div>
        )}
        {!settled && (
          <div className="mt-3 flex flex-wrap gap-2">
            <button
              type="button"
              className={white}
              disabled={running}
              onClick={() => void act("run")}
            >
              {running && (
                <Icon name="spinner" size="13px" className="animate-spin" />
              )}
              {running ? t("toolActions.running") : t("toolActions.run")}
            </button>
            <button
              type="button"
              className={ghost}
              disabled={running}
              onClick={() => void act("dismiss")}
            >
              {t("toolActions.dismiss")}
            </button>
          </div>
        )}
        {link !== undefined && httpURL(link.url) !== null && (
          <div className="mt-3 flex flex-wrap gap-2">
            <a
              href={link.url}
              target="_blank"
              rel="noopener noreferrer"
              className={`${solid} no-underline`}
            >
              {t("toolActions.open", { label: link.label || shown.server })}
              <Icon name="externalLink" size="13px" />
            </a>
          </div>
        )}
      </div>
    </article>
  );
}
