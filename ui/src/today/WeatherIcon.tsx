import type { TodayWarning } from "../api/today";
import type { Condition } from "./weatherCodes";

// Icons draw in currentColor: the sun's gold, the sky's soft grey, the
// warning's accent.
const SUN_CLASS = "text-sun";
const SKY_CLASS = "text-ink-dim";
const WARN_CLASS = "text-accent-strong";

const SUN = (
  <>
    <circle cx="12" cy="12" r="4" />
    <path d="M12 2v2M12 20v2M2 12h2M20 12h2M4.9 4.9l1.4 1.4M17.7 17.7l1.4 1.4M4.9 19.1l1.4-1.4M17.7 6.3l1.4-1.4" />
  </>
);
const MOON = <path d="M20 14.5A8 8 0 1 1 9.5 4a6.5 6.5 0 0 0 10.5 10.5z" />;
const CLOUD = (
  <path d="M7 19h10a4 4 0 0 0 .4-8A6 6 0 0 0 6 11.5 3.8 3.8 0 0 0 7 19z" />
);
const HIGH_CLOUD = (
  <path d="M7 15h10a4 4 0 0 0 .4-8A6 6 0 0 0 6 7.5 3.8 3.8 0 0 0 7 15z" />
);
const PARTLY_DAY = (
  <>
    <path d="M9 3.5v1.2M4.2 5.6l.9.9M2.5 10h1.2M13.8 5.6l-.9.9" />
    <path d="M6 11a3.2 3.2 0 0 1 6.1-1.4" />
    <path d="M8 20h9a3.5 3.5 0 0 0 .3-7A5 5 0 0 0 8 13.6 3.2 3.2 0 0 0 8 20z" />
  </>
);
const PARTLY_NIGHT = (
  <>
    <path d="M11 4.5A4.5 4.5 0 0 0 5 9.6a4.5 4.5 0 0 0 1.4.9" />
    <path d="M8 20h9a3.5 3.5 0 0 0 .3-7A5 5 0 0 0 8 13.6 3.2 3.2 0 0 0 8 20z" />
  </>
);
const FOG = <path d="M4 9h16M6 13h12M4 17h16M8 21h8" />;
const RAIN = (
  <>
    {HIGH_CLOUD}
    <path d="M8 18l-1 3M12 18l-1 3M16 18l-1 3" />
  </>
);
const SNOW = (
  <>
    {HIGH_CLOUD}
    <path d="M8 19v.01M12 18v.01M16 19v.01M10 21.5v.01M14 21.5v.01" />
  </>
);
// FROST is a thermometer with a snowflake beside it.
const FROST = (
  <>
    <path d="M10 13.5V5a2 2 0 0 0-4 0v8.5a4 4 0 1 0 4 0z" />
    <path d="M8 15v-4" />
    <path d="M18 4v8M14.5 6l7 4M14.5 10l7-4" />
  </>
);
const STORM = (
  <>
    {HIGH_CLOUD}
    <path d="M12.5 15.5 10.5 19h3l-2 3.5" />
  </>
);

function glyph(condition: Condition, night: boolean) {
  switch (condition) {
    case "clear":
      return night ? MOON : SUN;
    case "partly":
      return night ? PARTLY_NIGHT : PARTLY_DAY;
    case "overcast":
      return CLOUD;
    case "fog":
      return FOG;
    case "rain":
      return RAIN;
    case "snow":
      return SNOW;
    case "storm":
      return STORM;
  }
}

// WeatherIcon draws a sky as an inline stroke icon. With a label it is an
// image of its own; without, decoration next to text that says the same.
export function WeatherIcon({
  condition,
  night = false,
  size,
  warn = false,
  label,
}: {
  condition: Condition;
  night?: boolean;
  size: number;
  warn?: boolean;
  label?: string;
}) {
  const color = warn
    ? WARN_CLASS
    : condition === "clear" && !night
      ? SUN_CLASS
      : SKY_CLASS;
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth={size >= 32 ? 1.5 : 1.8}
      strokeLinecap="round"
      strokeLinejoin="round"
      data-condition={condition}
      data-night={night ? "true" : undefined}
      {...(label === undefined
        ? { "aria-hidden": true }
        : { role: "img", "aria-label": label })}
      className={`shrink-0 ${color}`}
    >
      {glyph(condition, night)}
    </svg>
  );
}

function warningGlyph(kind: TodayWarning["kind"]) {
  switch (kind) {
    case "rain":
      return RAIN;
    case "snow":
      return SNOW;
    case "storm":
      return STORM;
    case "frost":
      return FROST;
  }
}

// WarningIcon marks a weather warning, in the accent color: next to text that
// names it, so it is decoration.
export function WarningIcon({
  kind,
  size,
}: {
  kind: TodayWarning["kind"];
  size: number;
}) {
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth={1.8}
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden
      data-warning={kind}
      className={`shrink-0 ${WARN_CLASS}`}
    >
      {warningGlyph(kind)}
    </svg>
  );
}

// SunsetIcon and SunriseIcon mark the sun times in the compact line.
export function SunTimeIcon({ rising }: { rising: boolean }) {
  return (
    <svg
      width={18}
      height={18}
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth={1.8}
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden
      className={`shrink-0 ${SUN_CLASS}`}
    >
      <path d="M3 18h18M6.5 18a5.5 5.5 0 0 1 11 0" />
      <path
        d={
          rising
            ? "M12 11V7M9.5 9.5 12 7l2.5 2.5"
            : "M12 6v4M9.5 8.5 12 11l2.5-2.5"
        }
      />
    </svg>
  );
}

// PinIcon marks the place.
export function PinIcon({ size = 14 }: { size?: number }) {
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth={2}
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden
      className="shrink-0"
    >
      <path d="M12 21s-7-6.2-7-11.5a7 7 0 0 1 14 0C19 14.8 12 21 12 21z" />
      <circle cx="12" cy="9.5" r="2.5" />
    </svg>
  );
}
