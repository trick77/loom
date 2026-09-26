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

// useContextWindow returns the chat model's context window in tokens, or
// undefined until it is known.
export function useContextWindow(): number | undefined {
  return useModelInfo()?.contextWindow;
}
