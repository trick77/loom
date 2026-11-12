import { useEffect, useState } from "react";

import { countIdeas, IDEAS_CHANGED, type IdeaCounts } from "../api";

/**
 * The open and done counts: loaded on mount and again whenever an idea
 * changes anywhere (IDEAS_CHANGED). Null until the first answer; a failed
 * load keeps the last counts, nothing shows an error for them.
 */
export function useIdeaCounts(): IdeaCounts | null {
  const [counts, setCounts] = useState<IdeaCounts | null>(null);
  useEffect(() => {
    let alive = true;
    const load = () => {
      countIdeas().then(
        (next) => {
          if (alive) setCounts(next);
        },
        () => {},
      );
    };
    load();
    window.addEventListener(IDEAS_CHANGED, load);
    return () => {
      alive = false;
      window.removeEventListener(IDEAS_CHANGED, load);
    };
  }, []);
  return counts;
}

/** The sidebar's number: open ideas. */
export function useOpenIdeasCount(): number {
  return useIdeaCounts()?.open ?? 0;
}
