import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import type { TFunction } from "i18next";

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
function nextRefreshLabel(u: Usage, pending: number, t: TFunction): string {
  if (pending === 0) return t("settings.upToDate");
  const note = t("settings.pendingNote", { formatted: fmt(pending) });
  if (u.userMemoryUpdatedAt === null) return t("settings.eligibleNow", { note });
  const windowMs = u.userMemoryRefreshWindowHours * 3_600_000;
  const remainingMs = windowMs - (Date.now() - new Date(u.userMemoryUpdatedAt).getTime());
  if (remainingMs <= 0) return t("settings.eligibleNow", { note });
  return t("settings.eligibleInHours", { hours: Math.ceil(remainingMs / 3_600_000), note });
}

function memoryRows(u: Usage, t: TFunction): Row[] {
  const pct = u.userMemoryMax > 0 ? Math.round((u.userMemoryLength / u.userMemoryMax) * 100) : 0;
  const pending = Math.max(u.userMemoryTotalMessages - u.userMemorySourceMessages, 0);
  const directivesPct =
    u.userDirectivesMax > 0 ? Math.round((u.userDirectivesLength / u.userDirectivesMax) * 100) : 0;
  return [
    { label: t("settings.memoryLength"), value: `${fmt(u.userMemoryLength)} / ${fmt(u.userMemoryMax)} (${pct}%)` },
    {
      label: t("settings.lastUpdated"),
      value: u.userMemoryUpdatedAt === null ? t("settings.never") : formatTimeAgo(u.userMemoryUpdatedAt),
    },
    {
      label: t("settings.messagesCaptured"),
      value: t("settings.messagesCapturedValue", {
        captured: fmt(u.userMemorySourceMessages),
        total: fmt(u.userMemoryTotalMessages),
      }),
    },
    { label: t("settings.nextRefresh"), value: nextRefreshLabel(u, pending, t) },
    {
      label: t("settings.otherInstructions"),
      value: t("settings.otherInstructionsValue", {
        directives: fmt(u.userDirectivesCount),
        len: fmt(u.userDirectivesLength),
        max: fmt(u.userDirectivesMax),
        pct: directivesPct,
      }),
    },
  ];
}

function sectionsFor(u: Usage, t: TFunction): { group: string; rows: Row[] }[] {
  return [
    {
      group: t("settings.groupMemory"),
      rows: memoryRows(u, t),
    },
    {
      group: t("settings.groupTokens"),
      rows: [
        { label: t("settings.total"), value: fmt(u.totalTokens) },
        { label: t("settings.prompt"), value: fmt(u.promptTokens) },
        { label: t("settings.completion"), value: fmt(u.completionTokens) },
        { label: t("settings.cached"), value: fmt(u.cachedTokens) },
        { label: t("settings.reasoning"), value: fmt(u.reasoningTokens) },
      ],
    },
    {
      group: t("settings.groupEmbeddings"),
      rows: [
        { label: t("settings.embeddingTokens"), value: fmt(u.embeddingTokens) },
        { label: t("settings.embeddingRequests"), value: fmt(u.embeddingRequests) },
      ],
    },
    {
      group: t("settings.groupTools"),
      rows: [
        { label: t("settings.webSearches"), value: fmt(u.webSearches) },
        { label: t("settings.webFetches"), value: fmt(u.webFetches) },
        { label: t("settings.obscuraFetches"), value: fmt(u.obscuraFetches) },
        { label: t("settings.imageGenerations"), value: fmt(u.imageGens) },
      ],
    },
    {
      group: t("settings.groupActivity"),
      rows: [
        { label: t("settings.threadsCreated"), value: fmt(u.threadsCreated) },
        { label: t("settings.projectsCreated"), value: fmt(u.projectsCreated) },
      ],
    },
  ];
}

export function UsagePanel() {
  const { t } = useTranslation();
  const [usage, setUsage] = useState<Usage | null>(null);
  const [error, setError] = useState("");

  useEffect(() => {
    let active = true;
    getUsage()
      .then((u) => {
        if (active) setUsage(u);
      })
      .catch(() => {
        if (active) setError(t("settings.loadUsageFailed"));
      });
    return () => {
      active = false;
    };
  }, [t]);

  return (
    <div className="flex flex-col gap-6">
      <h2 className="text-lg text-[#f4f0e8]">{t("settings.usage")}</h2>
      {error !== "" ? (
        <p className="text-[#d98278]">{error}</p>
      ) : usage === null ? (
        <p className="text-[#8f8b82]">{t("settings.loading")}</p>
      ) : (
        sectionsFor(usage, t).map((section) => (
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
