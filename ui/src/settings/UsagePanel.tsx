import { useEffect, useState } from "react";

import { getUsage, type Usage } from "../api";

type Row = { label: string; value: string };

function sectionsFor(u: Usage): { group: string; rows: Row[] }[] {
  return [
    {
      group: "Tokens",
      rows: [
        { label: "Total", value: String(u.totalTokens) },
        { label: "Prompt", value: String(u.promptTokens) },
        { label: "Completion", value: String(u.completionTokens) },
        { label: "Cached", value: String(u.cachedTokens) },
        { label: "Reasoning", value: String(u.reasoningTokens) },
      ],
    },
    {
      group: "Embeddings",
      rows: [
        { label: "Embedding tokens", value: String(u.embeddingTokens) },
        { label: "Embedding requests", value: String(u.embeddingRequests) },
      ],
    },
    {
      group: "Tools",
      rows: [
        { label: "Web searches", value: String(u.webSearches) },
        { label: "Web fetches", value: String(u.webFetches) },
        { label: "Obscura fetches", value: String(u.obscuraFetches) },
        { label: "Image generations", value: String(u.imageGens) },
      ],
    },
    {
      group: "Activity",
      rows: [
        { label: "Chats created", value: String(u.chatsCreated) },
        { label: "Projects created", value: String(u.projectsCreated) },
      ],
    },
    {
      group: "Memory",
      rows: [{ label: "User memory length", value: `${u.userMemoryLength} / ${u.userMemoryMax}` }],
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
