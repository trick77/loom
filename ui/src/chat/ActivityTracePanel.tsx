import { useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import Markdown from "react-markdown";
import rehypeHighlight from "rehype-highlight";

import {
  markdownRemarkPlugins,
  normalizeMathDelimiters,
  rehypeKatexPlugin,
} from "./markdownConfig";

import {
  externalHTTPURL,
  faviconURL,
  summarizeTrace,
  type ActivityTraceEvent,
  type ActivityTraceToolEvent,
} from "../activityTrace";
import i18n from "../i18n";
import { Icon } from "./Icon";
import { rehypeStreamFade } from "./streamFade";

export function ActivityTracePanel({
  events,
  active,
  streaming = false,
  sweep = false,
  expanded: controlledExpanded,
  initiallyExpanded = false,
  onExpandedChange,
}: {
  events: ActivityTraceEvent[];
  active: boolean;
  streaming?: boolean;
  // Whether the reasoning-title label should shimmer. Passed true only for the
  // live active panel while the turn is still working (thinking / running tools,
  // no answer prose yet). The bouncing tail dots carry the generic "still
  // working" cue; the sweep is reserved for a real reasoning title.
  sweep?: boolean;
  expanded?: boolean;
  initiallyExpanded?: boolean;
  onExpandedChange?(expanded: boolean): void;
}) {
  const { t } = useTranslation();
  const [uncontrolledExpanded, setUncontrolledExpanded] =
    useState(initiallyExpanded);
  const expanded = controlledExpanded ?? uncontrolledExpanded;
  const [bodyMounted, setBodyMounted] = useState(expanded);
  useEffect(() => {
    if (expanded) {
      setBodyMounted(true);
      return;
    }
    const timer = window.setTimeout(() => setBodyMounted(false), 320);
    return () => window.clearTimeout(timer);
  }, [expanded]);
  if (events.length === 0 && !active) return null;
  const generatedTitle = latestReasoningTitle(events);
  // No "Thinking" fallback: the live active panel is only rendered once a real
  // reasoning title exists (before that, the tail dots are the sole cue), so the
  // live label is always a title. `summarizeTrace` covers past/completed panels
  // (and the rare tool-first case), shown statically.
  const label = generatedTitle ?? summarizeTrace(events);
  // Only a reasoning title ever shimmers, and only while the turn is working
  // (sweep). It stops the moment answer prose streams and at done — the tail dots
  // likewise vanish then. Tool titles and the status pill never sweep.
  const sweeping = sweep && generatedTitle !== undefined;
  // The trace is always a timeline: reasoning rows get a clock node, the line
  // connects them, and a terminal "Done" node caps the turn once it has settled
  // (no longer thinking and no longer streaming the answer).
  const complete = events.length > 0 && !active && !streaming;
  // The chevron only appears once there is something to reveal — i.e. once
  // reasoning (or a tool) has started streaming. During the bare "Thinking"
  // phase there is just the sweeping label, with no toggle affordance.
  const hasBody = events.length > 0;
  return (
    <div
      aria-label={active ? t("activityTrace.label") : undefined}
      aria-live={active ? "polite" : undefined}
      className="ui-activity-trace"
      role={active ? "status" : undefined}
    >
      <button
        aria-expanded={expanded}
        aria-label={
          expanded
            ? t("activityTrace.hideActivity")
            : t("activityTrace.showActivity")
        }
        className="ui-activity-trace-toggle"
        disabled={!hasBody}
        type="button"
        onClick={() => {
          const next = !expanded;
          if (controlledExpanded === undefined) setUncontrolledExpanded(next);
          onExpandedChange?.(next);
        }}
      >
        <span className="ui-activity-trace-label">
          {sweeping ? (
            <span className="ui-thinking-label-active" data-text={label}>
              {label}
            </span>
          ) : (
            <span>{label}</span>
          )}
          {hasBody && (
            <span
              aria-hidden="true"
              className={
                expanded
                  ? "ui-thinking-chevron-expanded"
                  : "ui-thinking-chevron"
              }
            />
          )}
        </span>
      </button>
      {bodyMounted && (
        <div
          className={
            expanded
              ? "ui-activity-trace-collapsible ui-activity-trace-collapsible-expanded"
              : "ui-activity-trace-collapsible"
          }
          aria-hidden={expanded ? undefined : true}
        >
          <div className="ui-activity-trace-collapsible-inner">
            <div className="ui-activity-trace-body">
              {events.map((event) => (
                <ActivityTraceRow
                  key={event.id}
                  event={event}
                  headline={label}
                  streaming={streaming}
                />
              ))}
              {complete && <ActivityTraceDoneRow />}
            </div>
          </div>
        </div>
      )}
    </div>
  );
}

// Cap a long reasoning block at this many pixels; beyond it the text fades out
// and a "Show more" toggle reveals the rest. Must match the CSS max-height on
// .ui-activity-reasoning-clamp (12rem @ 16px root).
const REASONING_CAP_PX = 192;

function ReasoningContent({
  content,
  streaming = false,
}: {
  content: string;
  streaming?: boolean;
}) {
  const { t } = useTranslation();
  const [showFull, setShowFull] = useState(false);
  const [overflowing, setOverflowing] = useState(false);
  const ref = useRef<HTMLDivElement>(null);
  // scrollHeight reports the full content height even while max-height/overflow
  // clamp it, so this measures correctly in both states and re-runs on every
  // streaming delta as the content grows.
  useLayoutEffect(() => {
    const el = ref.current;
    if (el === null) return;
    setOverflowing(el.scrollHeight > REASONING_CAP_PX);
  }, [content]);
  const clamped = overflowing && !showFull;
  // Rewrite legacy \(...\) / \[...\] into $-delimiters; memoized so the masking regexes
  // don't re-run over the full reasoning text on every streaming delta.
  const normalized = useMemo(() => normalizeMathDelimiters(content), [content]);
  return (
    <>
      <div
        ref={ref}
        className={
          clamped
            ? "ui-activity-reasoning ui-activity-reasoning-clamp"
            : "ui-activity-reasoning"
        }
      >
        <Markdown
          remarkPlugins={markdownRemarkPlugins}
          rehypePlugins={
            streaming
              ? [rehypeKatexPlugin, rehypeHighlight, rehypeStreamFade]
              : [rehypeKatexPlugin, rehypeHighlight]
          }
        >
          {normalized}
        </Markdown>
      </div>
      {overflowing && (
        <button
          className="ui-activity-reasoning-more"
          type="button"
          onClick={() => setShowFull((value) => !value)}
        >
          {showFull ? t("activityTrace.showLess") : t("activityTrace.showMore")}
        </button>
      )}
    </>
  );
}

function ActivityTraceRow({
  event,
  headline,
  streaming,
}: {
  event: ActivityTraceEvent;
  headline: string;
  streaming: boolean;
}) {
  const { t } = useTranslation();
  if (event.type === "reasoning") {
    // Every reasoning round is a timeline node marked with the clock glyph,
    // regardless of running/done — the terminal "Done" node carries the
    // checkmark that ends the turn.
    // Skip the per-round title when it just repeats the collapsed headline
    // above (the common single-round case) — otherwise it reads as a duplicate.
    const title = event.title?.trim();
    const showTitle =
      title !== undefined && title !== "" && title !== headline.trim();
    return (
      <div className="ui-activity-trace-row ui-activity-trace-row-reasoning">
        <span
          className="ui-activity-trace-icon ui-activity-trace-icon-clock"
          aria-hidden="true"
        >
          <ClockTraceIcon />
        </span>
        <div className="min-w-0 flex-1">
          {showTitle && (
            <div className="ui-activity-reasoning-title">{event.title}</div>
          )}
          <ReasoningContent
            content={event.content.trim()}
            streaming={streaming}
          />
        </div>
      </div>
    );
  }
  const status = activityToolStatusMeta(event);
  const fetchUrl =
    event.summary.kind === "fetch" ? event.summary.url : undefined;
  const fetchFavicon =
    fetchUrl === undefined ? undefined : faviconURL(fetchUrl);
  const fetchHref =
    fetchUrl === undefined ? undefined : externalHTTPURL(fetchUrl);
  // Tool-call titles never sweep: the collapsed trace label above is always
  // sweeping while the turn is active (a running tool implies active), so a
  // second shimmer here would just be redundant.
  const toolIcon =
    event.summary.kind === "search" ? (
      <GlobeTraceIcon />
    ) : event.summary.kind === "conversationSearch" ? (
      <ConversationSearchTraceIcon />
    ) : event.summary.kind === "lookup" ? (
      <LookupTraceIcon />
    ) : event.summary.kind === "generated" ? (
      <GeneratedTraceIcon />
    ) : fetchFavicon !== undefined ? (
      <img
        className="ui-activity-fetch-icon-favicon"
        src={fetchFavicon}
        alt=""
      />
    ) : (
      <FetchTraceIcon />
    );
  return (
    <div className="ui-activity-trace-row ui-activity-trace-row-tool">
      <span className="ui-activity-trace-icon" aria-hidden="true">
        {toolIcon}
      </span>
      <div className="min-w-0 flex-1">
        <div className="ui-activity-tool-header flex items-center justify-between gap-3">
          <span className="flex min-w-0 items-center gap-2">
            <span className="ui-activity-tool-title">
              {event.summary.title}
            </span>
          </span>
          {status !== undefined && (
            <span
              className={`ui-activity-status-pill shrink-0 ${status.className}`}
            >
              {status.label}
            </span>
          )}
        </div>
        {fetchUrl !== undefined &&
          (fetchHref !== undefined ? (
            <a
              className="ui-activity-tool-url"
              href={fetchHref}
              target="_blank"
              rel="noreferrer"
            >
              {fetchUrl}
              <Icon
                name="externalLink"
                size="0.8em"
                className="ml-1 inline-block align-[-0.1em]"
              />
            </a>
          ) : (
            <span className="ui-activity-tool-url">{fetchUrl}</span>
          ))}
        {event.preview?.kind === "searchResults" &&
          event.preview.results.length > 0 && (
            <>
              <div className="ui-activity-result-count">
                {t("activityTrace.results", {
                  count: event.preview.resultCount,
                })}
              </div>
              <div className="ui-activity-result-list">
                {event.preview.results.map((result, index) => (
                  <SearchResultRow
                    key={`${result.url ?? result.title}-${index}`}
                    result={result}
                  />
                ))}
              </div>
            </>
          )}
      </div>
    </div>
  );
}

function ActivityTraceDoneRow() {
  const { t } = useTranslation();
  return (
    <div className="ui-activity-trace-row ui-activity-trace-row-done">
      <span
        className="ui-activity-trace-icon ui-activity-trace-icon-done"
        aria-hidden="true"
      >
        <Icon name="checkCircle" size="1.125rem" />
      </span>
      <div className="min-w-0 flex-1">
        <span className="ui-activity-done-label">
          {t("activityTrace.done")}
        </span>
      </div>
    </div>
  );
}

function SearchResultRow({
  result,
}: {
  result: { title: string; url?: string; domain?: string; snippet?: string };
}) {
  const favicon = result.url === undefined ? undefined : faviconURL(result.url);
  const href =
    result.url === undefined ? undefined : externalHTTPURL(result.url);
  const title = <div className="ui-activity-result-title">{result.title}</div>;
  return (
    <div className="ui-activity-result-row">
      {favicon !== undefined ? (
        <img alt="" className="ui-activity-favicon" src={favicon} />
      ) : (
        <span className="ui-activity-favicon" aria-hidden="true">
          {faviconInitial(result.domain ?? result.title)}
        </span>
      )}
      <div className="min-w-0">
        {href === undefined ? (
          title
        ) : (
          <a
            className="ui-activity-result-link"
            href={href}
            target="_blank"
            rel="noreferrer"
          >
            {title}
          </a>
        )}
      </div>
      {result.domain !== undefined && (
        <div className="ui-activity-result-domain">{result.domain}</div>
      )}
    </div>
  );
}

function latestReasoningTitle(
  events: ActivityTraceEvent[],
): string | undefined {
  for (let index = events.length - 1; index >= 0; index -= 1) {
    const event = events[index];
    if (event.type !== "reasoning") continue;
    const title = event.title?.trim();
    if (title !== undefined && title !== "") return title;
  }
  return undefined;
}

// Only a failed tool call carries a pill. A finished one needs none — the row
// itself records that the call happened — and neither does a running one: the
// collapsed trace label above sweeps for as long as the turn is active, which
// already says something is in flight, so a per-row "Running" was the same
// signal repeated once per row.
function activityToolStatusMeta(event: ActivityTraceToolEvent):
  | {
      label: string;
      className: string;
    }
  | undefined {
  if (event.status === "failed")
    return {
      label: i18n.t("activityTrace.failed"),
      className: "ui-activity-status-failed",
    };
  return undefined;
}

function GlobeTraceIcon() {
  return (
    <Icon name="globe" size="1.125rem" className="ui-activity-globe-icon" />
  );
}

function ConversationSearchTraceIcon() {
  // Loupe (own-history search), distinct from the globe used for web search.
  // No globe-icon class — that one applies a tilt that would skew the loupe.
  return <Icon name="search" size="1.125rem" />;
}

function LookupTraceIcon() {
  // Loupe for IP-reputation lookups (ipverse) — same glyph as conversation search.
  return <Icon name="search" size="1.125rem" />;
}

function ClockTraceIcon() {
  // Reasoning timeline node — the Anthropicons clock-with-arc glyph (the same
  // reference design the previous hand-tuned SVG approximated).
  return (
    <Icon name="clock" size="1.125rem" className="ui-activity-clock-icon" />
  );
}

function GeneratedTraceIcon() {
  return (
    <Icon
      name="generatedArtifact"
      size="1rem"
      className="ui-activity-trace-icon-generated"
    />
  );
}

function FetchTraceIcon() {
  return (
    <Icon
      name="externalLink"
      size="1.125rem"
      className="ui-activity-fetch-icon"
    />
  );
}

function faviconInitial(value: string): string {
  return value.trim().charAt(0).toUpperCase() || "*";
}
