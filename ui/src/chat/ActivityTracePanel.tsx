import { useEffect, useLayoutEffect, useRef, useState } from "react";
import Markdown from "react-markdown";
import rehypeHighlight from "rehype-highlight";
import remarkGfm from "remark-gfm";

import {
  faviconURL,
  summarizeTrace,
  type ActivityTraceEvent,
  type ActivityTraceToolEvent,
} from "../activityTrace";

export function ActivityTracePanel({
  events,
  active,
  streaming = false,
  expanded: controlledExpanded,
  initiallyExpanded = false,
  onExpandedChange,
}: {
  events: ActivityTraceEvent[];
  active: boolean;
  streaming?: boolean;
  expanded?: boolean;
  initiallyExpanded?: boolean;
  onExpandedChange?(expanded: boolean): void;
}) {
  const [uncontrolledExpanded, setUncontrolledExpanded] = useState(initiallyExpanded);
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
  const label = active ? "Thinking" : summarizeTrace(events);
  // Sweep the label for the whole turn: "Thinking" while reasoning, then the
  // abstract once it settles — both shimmer until the answer finishes streaming.
  const sweeping = active || streaming;
  // Cap the timeline with a "Done" node once the whole turn has settled (no
  // longer thinking and no longer streaming the answer) — but only when there
  // was an actual tool timeline to end. A reasoning-only trace has no timeline,
  // so it gets no terminal node.
  const usedTools = events.some((event) => event.type === "tool");
  const complete = usedTools && !active && !streaming;
  return (
    <div
      aria-label={active ? "Slopr activity trace" : undefined}
      aria-live={active ? "polite" : undefined}
      className="slopr-activity-trace"
      role={active ? "status" : undefined}
    >
      <button
        aria-expanded={expanded}
        aria-label={expanded ? "Hide activity" : "Show activity"}
        className="slopr-activity-trace-toggle"
        type="button"
        onClick={() => {
          const next = !expanded;
          if (controlledExpanded === undefined) setUncontrolledExpanded(next);
          onExpandedChange?.(next);
        }}
      >
        <span className="slopr-activity-trace-label">
          {sweeping ? (
            <span className="slopr-thinking-label-active" data-text={label}>
              {label}
            </span>
          ) : (
            <span>{label}</span>
          )}
          <span aria-hidden="true" className={expanded ? "slopr-thinking-chevron-expanded" : "slopr-thinking-chevron"} />
        </span>
      </button>
      {bodyMounted && (
        <div
          className={
            expanded
              ? "slopr-activity-trace-collapsible slopr-activity-trace-collapsible-expanded"
              : "slopr-activity-trace-collapsible"
          }
          aria-hidden={expanded ? undefined : true}
        >
          <div className="slopr-activity-trace-collapsible-inner">
            <div className={`slopr-activity-trace-body${usedTools ? "" : " slopr-activity-trace-body-flat"}`}>
              {events.map((event) => (
                <ActivityTraceRow key={event.id} event={event} headline={label} timeline={usedTools} />
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
// .slopr-activity-reasoning-clamp (12rem @ 16px root).
const REASONING_CAP_PX = 192;

function ReasoningContent({ content }: { content: string }) {
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
  return (
    <>
      <div ref={ref} className={clamped ? "slopr-activity-reasoning slopr-activity-reasoning-clamp" : "slopr-activity-reasoning"}>
        <Markdown remarkPlugins={[remarkGfm]} rehypePlugins={[rehypeHighlight]}>
          {content}
        </Markdown>
      </div>
      {overflowing && (
        <button className="slopr-activity-reasoning-more" type="button" onClick={() => setShowFull((value) => !value)}>
          {showFull ? "Show less" : "Show more"}
        </button>
      )}
    </>
  );
}

function ActivityTraceRow({
  event,
  headline,
  timeline,
}: {
  event: ActivityTraceEvent;
  headline: string;
  timeline: boolean;
}) {
  if (event.type === "reasoning") {
    // The node glyph is a timeline affordance — only show it when tools make the
    // trace an actual timeline. Without tools there is no icon column at all, so
    // the reasoning sits flush-left, aligned with the headline and the answer.
    const iconClass =
      event.status === "done"
        ? "slopr-activity-trace-icon slopr-activity-trace-icon-reasoning slopr-activity-trace-icon-reasoning-complete"
        : "slopr-activity-trace-icon slopr-activity-trace-icon-reasoning";
    // Skip the per-round title when it just repeats the collapsed headline
    // above (the common single-round case) — otherwise it reads as a duplicate.
    const title = event.title?.trim();
    const showTitle = title !== undefined && title !== "" && title !== headline.trim();
    return (
      <div className="slopr-activity-trace-row slopr-activity-trace-row-reasoning">
        {timeline && <span className={iconClass} aria-hidden="true" />}
        <div className="min-w-0 flex-1">
          {showTitle && <div className="slopr-activity-reasoning-title">{event.title}</div>}
          <ReasoningContent content={event.content.trim()} />
        </div>
      </div>
    );
  }
  const status = activityToolStatusMeta(event);
  const fetchUrl = event.summary.kind === "fetch" ? event.summary.url : undefined;
  const fetchFavicon = fetchUrl === undefined ? undefined : faviconURL(fetchUrl);
  const fetchHref = fetchUrl === undefined ? undefined : externalHTTPURL(fetchUrl);
  return (
    <div className="slopr-activity-trace-row slopr-activity-trace-row-tool">
      <span
        className={
          event.summary.kind === "search"
            ? "slopr-activity-trace-icon"
            : "slopr-activity-trace-icon slopr-activity-trace-icon-arrow"
        }
        aria-hidden="true"
      >
        {event.summary.kind === "search" ? <GlobeTraceIcon /> : <FetchTraceIcon />}
      </span>
      <div className="min-w-0 flex-1">
        <div className="slopr-activity-tool-header flex items-center justify-between gap-3">
          <span className="flex min-w-0 items-center gap-2">
            <span className="slopr-activity-tool-title">{event.summary.title}</span>
            {fetchFavicon !== undefined && (
              <img className="slopr-activity-favicon slopr-activity-tool-favicon" src={fetchFavicon} alt="" />
            )}
          </span>
          <span className={`slopr-activity-status-pill shrink-0 ${status.className}`}>{status.label}</span>
        </div>
        {fetchUrl !== undefined &&
          (fetchHref !== undefined ? (
            <a className="slopr-activity-tool-url" href={fetchHref} target="_blank" rel="noreferrer">
              {fetchUrl}
            </a>
          ) : (
            <span className="slopr-activity-tool-url">{fetchUrl}</span>
          ))}
        {event.preview?.kind === "searchResults" && event.preview.results.length > 0 && (
          <>
            <div className="slopr-activity-result-count">
              {event.preview.resultCount} {event.preview.resultCount === 1 ? "result" : "results"}
            </div>
            <div className="slopr-activity-result-list">
              {event.preview.results.map((result, index) => (
                <SearchResultRow key={`${result.url ?? result.title}-${index}`} result={result} />
              ))}
            </div>
          </>
        )}
      </div>
    </div>
  );
}

function ActivityTraceDoneRow() {
  return (
    <div className="slopr-activity-trace-row slopr-activity-trace-row-done">
      <span
        className="slopr-activity-trace-icon slopr-activity-trace-icon-reasoning slopr-activity-trace-icon-reasoning-complete"
        aria-hidden="true"
      />
      <div className="min-w-0 flex-1">
        <span className="slopr-activity-done-label">Done</span>
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
  const href = result.url === undefined ? undefined : externalHTTPURL(result.url);
  const title = <div className="slopr-activity-result-title">{result.title}</div>;
  return (
    <div className="slopr-activity-result-row">
      {favicon !== undefined ? (
        <img alt="" className="slopr-activity-favicon" src={favicon} />
      ) : (
        <span className="slopr-activity-favicon" aria-hidden="true">
          {faviconInitial(result.domain ?? result.title)}
        </span>
      )}
      <div className="min-w-0">
        {href === undefined ? (
          title
        ) : (
          <a className="slopr-activity-result-link" href={href} target="_blank" rel="noreferrer">
            {title}
          </a>
        )}
      </div>
      {result.domain !== undefined && <div className="slopr-activity-result-domain">{result.domain}</div>}
    </div>
  );
}

function externalHTTPURL(value: string): string | undefined {
  try {
    const url = new URL(value);
    return url.protocol === "http:" || url.protocol === "https:" ? url.toString() : undefined;
  } catch {
    return undefined;
  }
}

function activityToolStatusMeta(event: ActivityTraceToolEvent): { label: string; className: string } {
  if (event.status === "failed") return { label: "Failed", className: "slopr-activity-status-failed" };
  if (event.status === "running") return { label: "Running", className: "slopr-activity-status-neutral" };
  return { label: "Done", className: "slopr-activity-status-neutral" };
}

function GlobeTraceIcon() {
  return (
    <svg className="slopr-activity-globe-icon" viewBox="0 0 24 24" aria-hidden="true">
      <circle cx="12" cy="12" r="9" />
      <path d="M3 12h18" />
      <path d="M12 3c2.25 2.45 3.35 5.45 3.35 9s-1.1 6.55-3.35 9" />
      <path d="M12 3c-2.25 2.45-3.35 5.45-3.35 9s1.1 6.55 3.35 9" />
    </svg>
  );
}

function FetchTraceIcon() {
  return (
    <svg viewBox="0 0 24 24" aria-hidden="true">
      <path d="M7 17 17 7" />
      <path d="M9 7h8v8" />
    </svg>
  );
}

function faviconInitial(value: string): string {
  return value.trim().charAt(0).toUpperCase() || "*";
}
