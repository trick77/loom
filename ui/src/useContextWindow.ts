import { useEffect, useState } from "react";
import { getModelInfo, type ModelInfo } from "./api/model";

// useModelInfo returns the configured chat model's facts (id, display name,
// context window) from /api/model, or null until it has answered (or when it
// failed). One fetch per page, shared by every caller.
export function useModelInfo(): ModelInfo | null {
  const [info, setInfo] = useState<ModelInfo | null>(null);
  useEffect(() => {
    let active = true;
    void getModelInfo().then((next) => {
      if (active && next !== null) setInfo(next);
    });
    return () => {
      active = false;
    };
  }, []);
  return info;
}

// useContextWindow returns a model's context window in tokens, or undefined
// until it is known: the configured slot model whose id matches, else the chat
// model's (a message from a model no longer configured, or none named).
export function useContextWindow(model?: string): number | undefined {
  const info = useModelInfo();
  return contextWindowFor(info, model);
}

export function contextWindowFor(
  info: ModelInfo | null,
  model?: string,
): number | undefined {
  const slot = info?.slots?.find((s) => s.model === model);
  return slot?.contextWindow ?? info?.contextWindow;
}
