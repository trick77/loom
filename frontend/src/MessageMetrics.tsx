import { createContext, useContext, useEffect, useState, type ReactNode } from "react";
import type { Message } from "./api";
import { buildMetricsString } from "./metrics";

export const SHOW_METRICS_KEY = "spark_show_chat_metrics";
const SHOW_METRICS_EVENT = "spark_show_metrics_change";

const MetricsContext = createContext<{
  showAlways: boolean;
  toggle(): void;
}>({ showAlways: false, toggle: () => {} });

function readShowAlways(): boolean {
  try {
    return window.localStorage.getItem(SHOW_METRICS_KEY) === "true";
  } catch {
    return false;
  }
}

function writeShowAlways(value: boolean) {
  try {
    window.localStorage.setItem(SHOW_METRICS_KEY, String(value));
  } catch {
    // localStorage unavailable (private mode / restricted env): toggle stays in memory only.
  }
}

/** Shares the "always show metrics" preference across every bubble. */
export function MetricsProvider({ children }: { children: ReactNode }) {
  const [showAlways, setShowAlways] = useState(readShowAlways);

  useEffect(() => {
    function handle(event: Event) {
      const detail = (event as CustomEvent<{ showAlways: boolean }>).detail;
      if (detail && typeof detail.showAlways === "boolean") setShowAlways(detail.showAlways);
    }
    window.addEventListener(SHOW_METRICS_EVENT, handle);
    return () => window.removeEventListener(SHOW_METRICS_EVENT, handle);
  }, []);

  function toggle() {
    const next = !readShowAlways();
    writeShowAlways(next);
    window.dispatchEvent(new CustomEvent(SHOW_METRICS_EVENT, { detail: { showAlways: next } }));
    setShowAlways(next);
  }

  return <MetricsContext.Provider value={{ showAlways, toggle }}>{children}</MetricsContext.Provider>;
}

function formatTimestamp(createdAt: string): string {
  const date = new Date(createdAt);
  if (Number.isNaN(date.getTime())) return "";
  return date.toLocaleTimeString(undefined, { hour: "2-digit", minute: "2-digit" });
}

/** Renders the hover-only (or always-on) stats line under an assistant bubble. */
export function MessageMetrics({ message }: { message: Message }) {
  const { showAlways, toggle } = useContext(MetricsContext);
  const line = buildMetricsString(message);
  if (line === null) return null;

  const timestamp = formatTimestamp(message.createdAt);
  const full = [line, timestamp].filter(Boolean).join(" · ");

  return (
    <button
      type="button"
      onClick={toggle}
      title={showAlways ? "Click to show metrics only on hover" : "Click to always show metrics"}
      className={`mt-1 block border-none bg-transparent text-left font-mono text-xs text-[#88857d] transition-opacity duration-300 ${
        showAlways ? "opacity-100" : "opacity-0 group-hover:opacity-100"
      }`}
    >
      {full}
    </button>
  );
}
