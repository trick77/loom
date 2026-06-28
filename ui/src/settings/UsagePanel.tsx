import { useEffect, useState } from "react";

import { getUsage, type Usage } from "../api";
import { formatTimeAgo } from "../timeago";

type Row = { label: string; value: string };

// Format an integer using a thin space (narrow no-break space, U+202F) as the
// thousands separator, e.g. 1234567 -> "1 234 567".
function fmt(n: number): string {
  return String(n).replace(/\B(?=(\d{3})+(?!\d))/g, "\u202f");
}

// nextRefreshLabel describes when the user memory is next eligible to refresh,
// derived client-side from the rolling refresh window. The background worker only
// regenerates when there is new activity (messages pending) AND the memory is past
// its window, so: no pending -> up to date; never generated -> eligible now;
// otherwise count down the remaining window.
function nextRefreshLabel(u: Usage, pending: number): string {
  if (pending === 0) return "Up to date";
  const pendingNote = `${fmt(pending)} pending`;
  if (u.userMemoryUpdatedAt === null) return `Eligible now (${pendingNote})`;
  const windowMs = u.userMemoryRefreshWindowHours * 3_600_000;
  const remainingMs = windowMs - (Date.now() - new Date(u.userMemoryUpdatedAt).getTime());
  if (remainingMs <= 0) return `Eligible now (${pendingNote})`;
  return `~${Math.ceil(remainingMs / 3_600_000)}h (${pendingNote})`;
}

function memoryRows(u: Usage): Row[] {
  const pct = u.userMemoryMax > 0 ? Math.round((u.userMemoryLength / u.userMemoryMax) * 100) : 0;
  const pending = Math.max(u.userMemoryTotalMessages - u.userMemorySourceMessages, 0);
  return [
    { label: "User memory length", value: `${fmt(u.userMemoryLength)} / ${fmt(u.userMemoryMax)} (${pct}%)` },
    {
      label: "Last updated",
      value: u.userMemoryUpdatedAt === null ? "Never" : formatTimeAgo(u.userMemoryUpdatedAt),
    },
    { label: "Messages captured", value: `${fmt(u.userMemorySourceMessages)} of ${fmt(u.userMemoryTotalMessages)}` },
    { label: "Next refresh", value: nextRefreshLabel(u, pending) },
  ];
}

function sectionsFor(u: Usage): { group: string; rows: Row[] }[] {
  return [
    {
      group: "Memory",
      rows: memoryRows(u),
    },
    {
      group: "Tokens",
      rows: [
        { label: "Total", value: fmt(u.totalTokens) },
        { label: "Prompt", value: fmt(u.promptTokens) },
        { label: "Completion", value: fmt(u.completionTokens) },
        { label: "Cached", value: fmt(u.cachedTokens) },
        { label: "Reasoning", value: fmt(u.reasoningTokens) },
      ],
    },
    {
      group: "Embeddings",
      rows: [
        { label: "Embedding tokens", value: fmt(u.embeddingTokens) },
        { label: "Embedding requests", value: fmt(u.embeddingRequests) },
      ],
    },
    {
      group: "Tools",
      rows: [
        { label: "Web searches", value: fmt(u.webSearches) },
        { label: "Web fetches", value: fmt(u.webFetches) },
        { label: "Obscura fetches", value: fmt(u.obscuraFetches) },
        { label: "Image generations", value: fmt(u.imageGens) },
      ],
    },
    {
      group: "Activity",
      rows: [
        { label: "Threads created", value: fmt(u.threadsCreated) },
        { label: "Projects created", value: fmt(u.projectsCreated) },
      ],
    },
    {
      group: "Memory",
      rows: [{ label: "User memory length", value: `${fmt(u.userMemoryLength)} / ${fmt(u.userMemoryMax)}` }],
    },
  ];
}

export function UsagePanel() {
  const [usage, setUsage] = useState<Usage | null>(null);
  const [error, setError] = useState("");

  useEffect(() => {
    let active = true;
    getUsage()
      .then((u) => {
        if (active) setUsage(u);
      })
      .catch(() => {
        if (active) setError("Failed to load usage.");
      });
    return () => {
      active = false;
    };
  }, []);

  return (
    <div className="flex flex-col gap-6">
      <h2 className="text-lg text-[#f4f0e8]">Usage</h2>
      {error !== "" ? (
        <p className="text-[#d98278]">{error}</p>
      ) : usage === null ? (
        <p className="text-[#8f8b82]">Loading…</p>
      ) : (
        sectionsFor(usage).map((section) => (
          <div key={section.group} className="flex flex-col gap-1.5">
            <div className="text-sm font-medium text-[#8f8b82]">{section.group}</div>
            {section.rows.map((row) => (
              <div
                key={row.label}
                className="flex justify-between border-b border-[#343432] py-1.5 text-sm"
              >
                <span className="text-[#cfccc3]">{row.label}</span>
                <span className="tabular-nums text-[#f4f0e8]">{row.value}</span>
              </div>
            ))}
          </div>
        ))
      )}
    </div>
  );
}
