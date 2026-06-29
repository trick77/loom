import { useEffect, useLayoutEffect, useRef, useState } from "react";
import Markdown from "react-markdown";
import rehypeHighlight from "rehype-highlight";
import remarkGfm from "remark-gfm";

import {
  externalHTTPURL,
  faviconURL,
  summarizeTrace,
  type ActivityTraceEvent,
  type ActivityTraceToolEvent,
} from "../activityTrace";
import { Icon } from "./Icon";
import { rehypeStreamFade } from "./streamFade";

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
  const generatedTitle = latestReasoningTitle(events);
  const label = generatedTitle ?? (active ? "Thinking" : summarizeTrace(events));
  // Sweep the label for the whole turn: "Thinking" until a generated abstract
  // arrives, then keep that title shimmering until the answer finishes streaming.
  const sweeping = active || streaming;
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
      aria-label={active ? "Loom activity trace" : undefined}
      aria-live={active ? "polite" : undefined}
      className="ui-activity-trace"
      role={active ? "status" : undefined}
    >
      <button
        aria-expanded={expanded}
        aria-label={expanded ? "Hide activity" : "Show activity"}
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
            <span aria-hidden="true" className={expanded ? "ui-thinking-chevron-expanded" : "ui-thinking-chevron"} />
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
                <ActivityTraceRow key={event.id} event={event} headline={label} streaming={streaming} />
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

function ReasoningContent({ content, streaming = false }: { content: string; streaming?: boolean }) {
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
      <div ref={ref} className={clamped ? "ui-activity-reasoning ui-activity-reasoning-clamp" : "ui-activity-reasoning"}>
        <Markdown
          remarkPlugins={[remarkGfm]}
          rehypePlugins={streaming ? [rehypeHighlight, rehypeStreamFade] : [rehypeHighlight]}
        >
          {content}
        </Markdown>
      </div>
      {overflowing && (
        <button className="ui-activity-reasoning-more" type="button" onClick={() => setShowFull((value) => !value)}>
          {showFull ? "Show less" : "Show more"}
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
  if (event.type === "reasoning") {
    // Every reasoning round is a timeline node marked with the clock glyph,
    // regardless of running/done — the terminal "Done" node carries the
    // checkmark that ends the turn.
    // Skip the per-round title when it just repeats the collapsed headline
    // above (the common single-round case) — otherwise it reads as a duplicate.
    const title = event.title?.trim();
    const showTitle = title !== undefined && title !== "" && title !== headline.trim();
    return (
      <div className="ui-activity-trace-row ui-activity-trace-row-reasoning">
        <span className="ui-activity-trace-icon ui-activity-trace-icon-clock" aria-hidden="true">
          <ClockTraceIcon />
        </span>
        <div className="min-w-0 flex-1">
          {showTitle && <div className="ui-activity-reasoning-title">{event.title}</div>}
          <ReasoningContent content={event.content.trim()} streaming={streaming} />
        </div>
      </div>
    );
  }
  const status = activityToolStatusMeta(event);
  const fetchUrl = event.summary.kind === "fetch" ? event.summary.url : undefined;
  const fetchFavicon = fetchUrl === undefined ? undefined : faviconURL(fetchUrl);
  const fetchHref = fetchUrl === undefined ? undefined : externalHTTPURL(fetchUrl);
  // Tool-call titles never sweep: the collapsed trace label above is always
  // sweeping while the turn is active (a running tool implies active), so a
  // second shimmer here would just be redundant.
  const toolIcon =
    event.summary.kind === "search" ? (
      <GlobeTraceIcon />
    ) : event.summary.kind === "conversationSearch" ? (
      <ConversationSearchTraceIcon />
    ) : event.summary.kind === "generated" ? (
      <GeneratedTraceIcon />
    ) : fetchFavicon !== undefined ? (
      <img className="ui-activity-fetch-icon-favicon" src={fetchFavicon} alt="" />
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
          <span className={`ui-activity-status-pill shrink-0 ${status.className}`}>{status.label}</span>
        </div>
        {fetchUrl !== undefined &&
          (fetchHref !== undefined ? (
            <a className="ui-activity-tool-url" href={fetchHref} target="_blank" rel="noreferrer">
              {fetchUrl}
              <Icon name="externalLink" size="0.8em" className="ml-1 inline-block align-[-0.1em]" />
            </a>
          ) : (
            <span className="ui-activity-tool-url">{fetchUrl}</span>
          ))}
        {event.preview?.kind === "searchResults" && event.preview.results.length > 0 && (
          <>
            <div className="ui-activity-result-count">
              {event.preview.resultCount} {event.preview.resultCount === 1 ? "result" : "results"}
            </div>
            <div className="ui-activity-result-list">
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
    <div className="ui-activity-trace-row ui-activity-trace-row-done">
      <span className="ui-activity-trace-icon ui-activity-trace-icon-done" aria-hidden="true">
        <Icon name="checkCircle" size="1.125rem" />
      </span>
      <div className="min-w-0 flex-1">
        <span className="ui-activity-done-label">Done</span>
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
          <a className="ui-activity-result-link" href={href} target="_blank" rel="noreferrer">
            {title}
          </a>
        )}
      </div>
      {result.domain !== undefined && <div className="ui-activity-result-domain">{result.domain}</div>}
    </div>
  );
}

function latestReasoningTitle(events: ActivityTraceEvent[]): string | undefined {
  for (let index = events.length - 1; index >= 0; index -= 1) {
    const event = events[index];
    if (event.type !== "reasoning") continue;
    const title = event.title?.trim();
    if (title !== undefined && title !== "") return title;
  }
  return undefined;
}

function activityToolStatusMeta(event: ActivityTraceToolEvent): { label: string; className: string } {
  if (event.status === "failed") return { label: "Failed", className: "ui-activity-status-failed" };
  if (event.status === "running") return { label: "Running", className: "ui-activity-status-neutral" };
  return { label: "Done", className: "ui-activity-status-neutral" };
}

function GlobeTraceIcon() {
  return <Icon name="globe" size="1.125rem" className="ui-activity-globe-icon" />;
}

function ConversationSearchTraceIcon() {
  // Loupe (own-history search), distinct from the globe used for web search.
  // No globe-icon class — that one applies a tilt that would skew the loupe.
  return <Icon name="search" size="1.125rem" />;
}

function ClockTraceIcon() {
  // Reasoning timeline node — the Anthropicons clock-with-arc glyph (the same
  // reference design the previous hand-tuned SVG approximated).
  return <Icon name="clock" size="1.125rem" className="ui-activity-clock-icon" />;
}

function GeneratedTraceIcon() {
  return <Icon name="generatedArtifact" size="1rem" className="ui-activity-trace-icon-generated" />;
}

function FetchTraceIcon() {
  return <Icon name="externalLink" size="1.125rem" className="ui-activity-fetch-icon" />;
}

function faviconInitial(value: string): string {
  return value.trim().charAt(0).toUpperCase() || "*";
}
