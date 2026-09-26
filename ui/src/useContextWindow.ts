import { useEffect, useState } from "react";
import { getModelInfo } from "./api/model";

// useContextWindow returns the chat model's context window in tokens, or
// undefined until /api/model has answered (or when it failed). One fetch per
// page, shared by every caller.
export function useContextWindow(): number | undefined {
  const [contextWindow, setContextWindow] = useState<number | undefined>();
  useEffect(() => {
    let active = true;
    void getModelInfo().then((info) => {
      if (active && info !== null) setContextWindow(info.contextWindow);
    });
    return () => {
      active = false;
    };
  }, []);
  return contextWindow;
}
