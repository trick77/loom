import { useEffect, useRef } from "react";

import { draftIdea, type IdeaDraft } from "../api";

// DRAFT_DEBOUNCE_MS is the pause in typing the reason after which the draft
// call is asked again (for the due date the reason names).
export const DRAFT_DEBOUNCE_MS = 700;

type Ask = { excerpt: string; reason: string };

/**
 * useIdeaDraft asks the draft call for a title and a due date. As it mounts
 * (when askNow) it sends the passage alone, for a title; whenever the reason
 * changes it sends, debounced, the reason alone: the date comes only from
 * the user's own words (the server drops any from a call that carried the
 * passage, which is outside text), and the passage is not paid for again.
 * Calls never overlap: the server answers a second call of the same user
 * with nothing while one runs, so a call waits for the one in flight and
 * then sends only the latest reason. Everything is aborted on unmount.
 */
export function useIdeaDraft(
  excerpt: string,
  reason: string,
  { askNow = false, enabled = true }: { askNow?: boolean; enabled?: boolean },
  onDraft: (draft: IdeaDraft) => void,
) {
  const onDraftRef = useRef(onDraft);
  useEffect(() => {
    onDraftRef.current = onDraft;
  });
  const send = useRef<(ask: Ask) => void>(() => {});
  // The reason the form opened with: no call until it changes.
  const opened = useRef(reason);

  useEffect(() => {
    let busy = false;
    let queued: Ask | null = null;
    const controller = new AbortController();
    const run = (ask: Ask) => {
      if (busy) {
        queued = ask;
        return;
      }
      busy = true;
      void draftIdea(ask.excerpt, ask.reason, controller.signal)
        .then(
          (draft) => {
            if (!controller.signal.aborted) onDraftRef.current(draft);
          },
          () => {},
        )
        .finally(() => {
          busy = false;
          if (controller.signal.aborted || queued === null) return;
          const next = queued;
          queued = null;
          run(next);
        });
    };
    send.current = run;
    if (askNow && excerpt.trim() !== "") run({ excerpt, reason: "" });
    return () => {
      controller.abort();
      send.current = () => {};
    };
    // Asked once per opening: askNow only matters then.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [excerpt]);

  useEffect(() => {
    if (!enabled || reason.trim() === "" || reason === opened.current) return;
    const timer = window.setTimeout(
      () => send.current({ excerpt: "", reason }),
      DRAFT_DEBOUNCE_MS,
    );
    return () => window.clearTimeout(timer);
  }, [reason, enabled]);
}
