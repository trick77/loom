import { useEffect, useState } from "react";
import { getModelInfo, peekModelInfo, type ModelInfo } from "./api/model";

// useModelInfo returns the configured models' facts from /api/model, or null
// until it has answered (or when it failed). One fetch per page, shared by
// every caller; once it has answered, a new caller starts with it instead of
// rendering once without.
export function useModelInfo(): ModelInfo | null {
  const [info, setInfo] = useState<ModelInfo | null>(peekModelInfo);
  useEffect(() => {
    if (info !== null) return;
    let active = true;
    void getModelInfo().then((next) => {
      if (active && next !== null) setInfo(next);
    });
    return () => {
      active = false;
    };
  }, [info]);
  return info;
}
