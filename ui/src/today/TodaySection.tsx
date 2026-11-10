import { useId } from "react";
import { useTranslation } from "react-i18next";

import type { Today, TodayWeather } from "../api/today";
import { formatDay } from "./format";
import { LocationChip } from "./LocationChip";
import { LocationPrompt } from "./LocationPrompt";
import { ScheduleNews } from "./ScheduleNews";
import { SunLine } from "./SunLine";
import { TonightPill } from "./TonightPill";
import { WeatherTiles } from "./WeatherTile";

// placeChip is the chip naming the shown place; null without one.
function placeChip(today: Today, onReload: () => void, compact = false) {
  if (today.location === null) return null;
  return (
    <LocationChip
      canLocate={today.canLocate}
      compact={compact}
      credit={today.locationCredit}
      location={today.location}
      onChanged={onReload}
    />
  );
}

// WeatherBlock is the day's tiles under a "Today"/"Tomorrow" heading: side
// by side where they fit, stacked on a phone.
function WeatherBlock({
  today,
  weather,
  onReload,
}: {
  today: Today;
  weather: TodayWeather;
  onReload(): void;
}) {
  const { t, i18n } = useTranslation();
  const headingId = useId();
  return (
    <section aria-labelledby={headingId} className="flex flex-col gap-3.5">
      {today.day === "tomorrow" && today.tonight !== null && (
        <TonightPill warning={today.tonight} />
      )}
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
        {placeChip(today, onReload)}
      </div>
      <div className="grid grid-cols-[repeat(auto-fit,minmax(260px,1fr))] gap-3">
        <WeatherTiles today={today} weather={weather} />
      </div>
    </section>
  );
}

// TodaySection is the start page's "Today" under the composer: the weather
// and sun for the user's place (tomorrow's from 20:00), the unread schedule
// runs, and the question for a place when none is known.
export function TodaySection({
  today,
  onOpenThread,
  onMarkRead,
  onMarkAllRead,
  onReload,
}: {
  today: Today;
  onOpenThread(threadID: string): void;
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
        <WeatherBlock onReload={onReload} today={today} weather={weather} />
      ) : (
        today.sun !== null && (
          <SunLine
            chip={placeChip(today, onReload, true)}
            day={today.day}
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
      {today.askLocation && (
        <LocationPrompt canLocate={today.canLocate} onSaved={onReload} />
      )}
    </div>
  );
}
