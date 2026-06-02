import { createContext, useContext, useState, type ReactNode } from "react";
import type { Message } from "./api";
import { buildMetricsString } from "./metrics";

export const SHOW_METRICS_KEY = "spark_show_chat_metrics";

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

/**
 * Shares the "always show metrics" preference across every bubble. A single
 * provider wraps the whole transcript, so updating its state already re-renders
 * every consuming bubble — no cross-instance event plumbing is needed.
 */
export function MetricsProvider({ children }: { children: ReactNode }) {
  const [showAlways, setShowAlways] = useState(readShowAlways);

  function toggle() {
    const next = !showAlways;
    writeShowAlways(next);
    setShowAlways(next);
  }

  return <MetricsContext.Provider value={{ showAlways, toggle }}>{children}</MetricsContext.Provider>;
}

/** Renders the hover-only (or always-on) stats line under an assistant bubble. */
export function MessageMetrics({ message }: { message: Message }) {
  const { showAlways, toggle } = useContext(MetricsContext);
  const line = buildMetricsString(message);
  if (line === null) return null;

  return (
    <button
      type="button"
      onClick={toggle}
      title={showAlways ? "Click to show metrics only on hover" : "Click to always show metrics"}
      className={`mt-1 block border-none bg-transparent text-left font-mono text-xs text-[#88857d] transition-opacity duration-300 ${
        showAlways ? "opacity-100" : "opacity-0 group-hover:opacity-100"
      }`}
    >
      {line}
    </button>
  );
}
