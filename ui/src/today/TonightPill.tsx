import { useTranslation } from "react-i18next";

import type { TodayWarning } from "../api/today";
import { warningKey } from "./weatherCodes";
import { WarningIcon } from "./WeatherIcon";

// TonightPill is what comes tonight while the page already shows tomorrow: a
// pill above tomorrow's heading, apart from it, so tomorrow's tile warns of
// tomorrow only. A full tile for one line outweighed the weather itself.
export function TonightPill({ warning }: { warning: TodayWarning }) {
  const { t } = useTranslation();
  return (
    <div className="flex">
      <span
        className="flex items-center gap-1.5 rounded-full bg-accent-strong/10 px-3 py-1 text-[13px] font-medium text-accent-strong"
        data-testid="tonight-warning"
      >
        <WarningIcon kind={warning.kind} size={14} />
        {t(warningKey("tonight", warning), { from: warning.from })}
        {/* Not laid out in the flex row, but read: no "Rain90%". */}
        {warning.chance !== null && " "}
        {warning.chance !== null && (
          <span className="font-normal opacity-80">
            {t("today.percent", { percent: Math.round(warning.chance) })}
          </span>
        )}
      </span>
    </div>
  );
}
