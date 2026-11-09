import { useId, useState } from "react";
import { useTranslation } from "react-i18next";

import type { Today, TodayWeather } from "../api/today";
import { useMediaQuery } from "../chat/useMediaQuery";
import { Attribution } from "./Attribution";
import { formatDay } from "./format";
import { LocationChip } from "./LocationChip";
import { LocationPrompt } from "./LocationPrompt";
import { ScheduleNews } from "./ScheduleNews";
import { SunTile } from "./SunTile";
import { SunLine, TodayLine } from "./TodayLine";
import { WeatherTile } from "./WeatherTile";

// Below Tailwind's sm the tiles start folded into the line.
const NARROW = "(max-width: 639px)";

function Tiles({ today, weather }: { today: Today; weather: TodayWeather }) {
  return (
    <div className="grid grid-cols-[repeat(auto-fit,minmax(260px,1fr))] gap-3">
      <WeatherTile weather={weather} sun={today.sun} warning={today.warning} />
      {today.sun !== null && <SunTile sun={today.sun} />}
    </div>
  );
}

// WeatherBlock shows the tiles on the first visit of the day and the line
// after it (on a phone always the line); the line unfolds the tiles.
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
  const narrow = useMediaQuery(NARROW);
  const [expanded, setExpanded] = useState(false);
  const headingId = useId();
  if (today.view === "line" || narrow) {
    return (
      <section aria-label={t("today.title")} className="flex flex-col gap-3">
        <div className="flex justify-center">
          <TodayLine
            expanded={expanded}
            location={today.location}
            onOpenMemory={onOpenMemory}
            onToggle={() => setExpanded((value) => !value)}
            sun={today.sun}
            warning={today.warning}
            weather={weather}
          />
        </div>
        {expanded && <Tiles today={today} weather={weather} />}
        <Attribution
          attribution={weather.attribution}
          className="text-center"
        />
      </section>
    );
  }
  return (
    <section aria-labelledby={headingId} className="flex flex-col gap-3.5">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <h2
          className="text-[13px] font-medium tracking-[0.02em] text-[#97958c]"
          id={headingId}
        >
          {t("today.heading", { date: formatDay(today.date, i18n.language) })}
        </h2>
        {today.location !== null && (
          <LocationChip location={today.location} onOpenMemory={onOpenMemory} />
        )}
      </div>
      <Tiles today={today} weather={weather} />
      <Attribution attribution={weather.attribution} className="text-right" />
    </section>
  );
}

// TodaySection is the start page's "Today" under the composer: the weather
// and sun for the user's place, the unread schedule runs, and the question
// where the user lives when no memory says so.
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
