import { useId } from "react";
import { useTranslation } from "react-i18next";

import type { Today, TodayWeather } from "../api/today";
import { Attribution } from "./Attribution";
import { formatDay } from "./format";
import { LocationChip } from "./LocationChip";
import { LocationPrompt } from "./LocationPrompt";
import { ScheduleNews } from "./ScheduleNews";
import { SunLine } from "./SunLine";
import { SunTile } from "./SunTile";
import { WeatherTile } from "./WeatherTile";

// WeatherBlock is the day's tiles under a "Today"/"Tomorrow" heading: side
// by side where they fit, stacked on a phone.
function WeatherBlock({
  today,
  weather,
  onOpenMemory,
}: {
  today: Today;
  weather: TodayWeather;
  onOpenMemory(): void;
}) {
  const { t, i18n } = useTranslation();
  const headingId = useId();
  return (
    <section aria-labelledby={headingId} className="flex flex-col gap-3.5">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <h2
          className="text-[13px] font-medium tracking-[0.02em] text-muted"
          id={headingId}
        >
          {t(
            today.day === "tomorrow"
              ? "today.headingTomorrow"
              : "today.heading",
            { date: formatDay(today.date, i18n.language) },
          )}
        </h2>
        {today.location !== null && (
          <LocationChip location={today.location} onOpenMemory={onOpenMemory} />
        )}
      </div>
      <div className="grid grid-cols-[repeat(auto-fit,minmax(260px,1fr))] gap-3">
        <WeatherTile today={today} weather={weather} />
        {today.sun !== null && <SunTile day={today.day} sun={today.sun} />}
      </div>
      <Attribution attribution={weather.attribution} className="text-right" />
    </section>
  );
}

// TodaySection is the start page's "Today" under the composer: the weather
// and sun for the user's place (tomorrow's from 18:00), the unread schedule
// runs, and the question where the user lives when no memory says so.
export function TodaySection({
  today,
  onOpenThread,
  onOpenMemory,
  onMarkRead,
  onMarkAllRead,
  onReload,
}: {
  today: Today;
  onOpenThread(threadID: string): void;
  onOpenMemory(): void;
  onMarkRead(id: string): void;
  onMarkAllRead(ids: string[]): void;
  onReload(): void;
}) {
  const weather = today.weather;
  if (
    weather === null &&
    today.sun === null &&
    today.runs.length === 0 &&
    !today.askLocation
  ) {
    return null;
  }
  return (
    <div className="mt-8 flex flex-col gap-7">
      {weather !== null ? (
        <WeatherBlock
          onOpenMemory={onOpenMemory}
          today={today}
          weather={weather}
        />
      ) : (
        today.sun !== null && (
          <SunLine
            location={today.location}
            onOpenMemory={onOpenMemory}
            sun={today.sun}
          />
        )
      )}
      <ScheduleNews
        onMarkAllRead={() => onMarkAllRead(today.runs.map((run) => run.id))}
        onMarkRead={onMarkRead}
        onOpenThread={onOpenThread}
        runs={today.runs}
      />
      {today.askLocation && <LocationPrompt onSaved={onReload} />}
    </div>
  );
}
