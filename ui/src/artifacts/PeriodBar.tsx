import { useTranslation } from "react-i18next";

import type { ArtifactSummaryMonth } from "../api";
import {
  type ArtifactPeriod,
  summaryMonths,
  summaryYears,
} from "./artifactGroups";

const chipClass = (active: boolean) =>
  `ui-control-text rounded-lg px-3 py-1 transition-colors ${
    active ? "bg-track text-ink" : "text-muted hover:text-ink"
  }`;

// formatMonth names a calendar month ("October 2026", "Oktober 2026") in the
// UI language. Built from local fields, so no zone shifts it to a neighbour.
export function formatMonth(
  year: number,
  month: number,
  language: string,
): string {
  return new Intl.DateTimeFormat(language, {
    month: "long",
    year: "numeric",
  }).format(new Date(year, month - 1, 1));
}

// PeriodBar picks what the Artifacts page lists: the last 30 days, a year, or
// a month. Years and months come from the summary, so only periods that hold
// artifacts are offered, plus the one listed.
export function PeriodBar({
  period,
  months,
  searching,
  onChange,
}: {
  period: ArtifactPeriod;
  months: ArtifactSummaryMonth[];
  // A search runs across all time: no period is the one being listed, so none
  // is marked.
  searching: boolean;
  onChange(period: ArtifactPeriod): void;
}) {
  const { t, i18n } = useTranslation();
  const parsed = summaryMonths(months);
  const selectedMonth =
    !searching && period.kind === "month"
      ? `${period.year}-${String(period.month).padStart(2, "0")}`
      : "";
  // The listed month may be missing from the summary (a type switch, the
  // delete of its last artifact); a controlled select would then show its
  // first option instead (or vanish with no months left), so the listed month
  // stays as an option of its own.
  if (
    period.kind === "month" &&
    selectedMonth !== "" &&
    !parsed.some((month) => month.key === selectedMonth)
  ) {
    parsed.push({
      key: selectedMonth,
      year: period.year,
      month: period.month,
      count: 0,
    });
    parsed.sort((a, b) => b.key.localeCompare(a.key));
  }
  // Likewise the listed year: without its chip none would say what is listed.
  const years = summaryYears(months);
  if (period.kind === "year" && !years.includes(period.year)) {
    years.push(period.year);
    years.sort((a, b) => b - a);
  }
  return (
    <div
      role="group"
      aria-label={t("artifacts.periodLabel")}
      className="mt-3 flex flex-wrap items-center gap-1"
    >
      <button
        type="button"
        aria-pressed={!searching && period.kind === "recent"}
        className={chipClass(!searching && period.kind === "recent")}
        onClick={() => onChange({ kind: "recent" })}
      >
        {t("artifacts.periodRecent")}
      </button>
      {years.map((year) => {
        const active =
          !searching && period.kind === "year" && period.year === year;
        return (
          <button
            key={year}
            type="button"
            aria-pressed={active}
            className={chipClass(active)}
            onClick={() => onChange({ kind: "year", year })}
          >
            {year}
          </button>
        );
      })}
      {parsed.length > 0 && (
        <select
          aria-label={t("artifacts.pickMonth")}
          value={selectedMonth}
          className={`ui-control-text max-w-full rounded-lg border border-border bg-panel px-2 py-1 outline-none [color-scheme:dark] focus:border-border-strong ${
            selectedMonth === "" ? "text-muted" : "text-ink"
          }`}
          onChange={(event) => {
            const picked = parsed.find(
              (month) => month.key === event.target.value,
            );
            if (picked !== undefined)
              onChange({
                kind: "month",
                year: picked.year,
                month: picked.month,
              });
          }}
        >
          <option value="" disabled>
            {t("artifacts.pickMonth")}
          </option>
          {parsed.map((month) => (
            <option key={month.key} value={month.key}>
              {t("artifacts.monthOption", {
                month: formatMonth(month.year, month.month, i18n.language),
                n: month.count,
              })}
            </option>
          ))}
        </select>
      )}
    </div>
  );
}
