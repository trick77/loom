import { useCallback, useState } from "react";

import {
  type ComposerDrafts,
  type DraftScope,
  getDraft,
  setDraftPastedTexts,
  setDraftText as setScopedDraftText,
} from "./composerDrafts";
import { createPastedText } from "./pastedText";

// useComposerDrafts owns the composer's unsent text and staged "Pasted" chips
// for every surface (see composerDrafts.ts for the scoping), plus the focus
// tick a retry bumps to put the caret back into the textarea.
export function useComposerDrafts() {
  const [drafts, setDrafts] = useState<ComposerDrafts>({});
  const [focusTick, setFocusTick] = useState(0);

  const setDraftText = useCallback((scope: DraftScope, text: string) => {
    setDrafts((current) => setScopedDraftText(current, scope, text));
  }, []);
  // Large pastes collapse into removable chips shown above the textarea and are
  // folded back into the outgoing message on send (never uploaded or indexed).
  const addPastedText = useCallback((scope: DraftScope, text: string) => {
    setDrafts((current) =>
      setDraftPastedTexts(current, scope, [
        ...getDraft(current, scope).pastedTexts,
        createPastedText(text),
      ]),
    );
  }, []);
  const removePastedText = useCallback((scope: DraftScope, id: string) => {
    setDrafts((current) =>
      setDraftPastedTexts(
        current,
        scope,
        getDraft(current, scope).pastedTexts.filter(
          (pasted) => pasted.id !== id,
        ),
      ),
    );
  }, []);
  const requestFocus = useCallback(() => {
    setFocusTick((tick) => tick + 1);
  }, []);

  return {
    drafts,
    setDrafts,
    setDraftText,
    addPastedText,
    removePastedText,
    focusTick,
    requestFocus,
  };
}
