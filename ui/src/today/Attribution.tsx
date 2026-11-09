import type { TodayWeather } from "../api/today";
import { safeURL } from "./format";

// Attribution credits the weather source: its licence asks for it wherever
// the data shows.
export function Attribution({
  attribution,
  className = "",
}: {
  attribution: TodayWeather["attribution"];
  className?: string;
}) {
  const url = safeURL(attribution.url);
  return (
    <div className={`ui-meta-text text-[#8f8b82] ${className}`}>
      {url === null ? (
        attribution.text
      ) : (
        <a
          className="underline-offset-2 hover:text-[#c7c5bd] hover:underline"
          href={url}
          rel="noopener"
          target="_blank"
        >
          {attribution.text}
        </a>
      )}
    </div>
  );
}
