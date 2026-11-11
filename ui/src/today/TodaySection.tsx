import { useId, type ReactNode } from "react";
import { useTranslation } from "react-i18next";

import type { Today, TodayWeather } from "../api/today";
import { formatDay } from "./format";
import { LocationChip } from "./LocationChip";
import { LocationPrompt } from "./LocationPrompt";
import { ScheduleNews } from "./ScheduleNews";
import { SunLine } from "./SunLine";
import { TonightPill } from "./TonightPill";
import { WeatherTiles } from "./WeatherTile";

// WeatherBlock is the day's tiles under a "Today"/"Tomorrow" heading: side
// by side where they fit, stacked on a phone.
function WeatherBlock({
  today,
  weather,
  chip,
}: {
  today: Today;
  weather: TodayWeather;
  /** The place chip; none without a place. */
  chip: ReactNode;
}) {
  const { t, i18n } = useTranslation();
  const headingId = useId();
  return (
    <section aria-labelledby={headingId} className="flex flex-col gap-3.5">
      {today.day === "tomorrow" && today.tonight !== null && (
        <TonightPill warning={today.tonight} />
      )}
      {/* Enters first, the tiles after it (WeatherTiles). A new shown day
          (the 20:00 turn) enters its heading again, as its tiles do; the
          chip stays, its popover open or a drop running. */}
      <div className="flex flex-wrap items-center justify-between gap-3">
        <h2
          // pl-2.5: flush with the project picker's text above it (its px-2.5).
          className="weather-enter pl-2.5 text-[13px] font-medium tracking-[0.02em] text-muted"
          id={headingId}
          key={`${today.date}/${today.day}`}
        >
          {t(
            today.day === "tomorrow"
              ? "today.headingTomorrow"
              : "today.heading",
            { date: formatDay(today.date, i18n.language) },
          )}
        </h2>
        {chip && <div className="weather-enter">{chip}</div>}
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
  onSessionExpired,
}: {
  today: Today;
  onOpenThread(threadID: string): void;
  onMarkRead(id: string): void;
  onMarkAllRead(ids: string[]): void;
  onReload(): void;
  onSessionExpired(): void;
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
  // The chip naming the shown place: in the weather's heading, or compact on
  // the sun line.
  const chip = (compact: boolean) =>
    today.location !== null && (
      <LocationChip
        canLocate={today.canLocate}
        compact={compact}
        credit={today.locationCredit}
        location={today.location}
        onChanged={onReload}
        onSessionExpired={onSessionExpired}
      />
    );
  return (
    <div className="mt-8 flex flex-col gap-7">
      {weather !== null ? (
        <WeatherBlock chip={chip(false)} today={today} weather={weather} />
      ) : (
        today.sun !== null && (
          <SunLine chip={chip(true)} day={today.day} sun={today.sun} />
        )
      )}
      <ScheduleNews
        onMarkAllRead={() => onMarkAllRead(today.runs.map((run) => run.id))}
        onMarkRead={onMarkRead}
        onOpenThread={onOpenThread}
        runs={today.runs}
      />
      {today.askLocation && (
        <LocationPrompt
          canLocate={today.canLocate}
          onSaved={onReload}
          onSessionExpired={onSessionExpired}
        />
      )}
    </div>
  );
}
