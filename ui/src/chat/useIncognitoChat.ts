import { useCallback, useState } from "react";

import { streamIncognitoMessage, type MessagePastedText } from "../api";
import {
  clearDraft,
  INCOGNITO_DRAFT_SCOPE,
  setDraft as setScopedDraft,
  type ComposerDrafts,
} from "./composerDrafts";
import { graftStreamedBlocks } from "./contentBlocks";
import {
  pastedTextFromBlock,
  toPastedTextBlock,
  type PastedText,
} from "./pastedText";
import { INCOGNITO_RUN_KEY } from "./streamRuns";
import { createTurnHandlers, newTempID } from "./turnHandlers";
import type { MessageWithActivityTrace } from "./types";
import type { useStreamRuns } from "./useStreamRuns";

type StreamRunControls = ReturnType<typeof useStreamRuns>;

// useIncognitoChat owns incognito mode: a standalone, ephemeral chat reachable
// only from /new. Its transcript lives entirely here and is never persisted or
// added to the thread lists; exiting or leaving discards it. Its turn is just
// another run, under a reserved key, so the shell's run registry, draft store
// and error reporting are passed in rather than duplicated.
export function useIncognitoChat({
  beginStreamRun,
  patchStreamRun,
  endStreamRun,
  abortStreamRun,
  setDrafts,
  requestComposerFocus,
  setSendError,
  handleActionError,
  translateStreamError,
  t,
}: {
  beginStreamRun: StreamRunControls["begin"];
  patchStreamRun: StreamRunControls["patch"];
  endStreamRun: StreamRunControls["end"];
  abortStreamRun: StreamRunControls["abort"];
  setDrafts(update: (current: ComposerDrafts) => ComposerDrafts): void;
  requestComposerFocus(): void;
  setSendError(message: string): void;
  handleActionError(
    error: unknown,
    fallback: string,
    setError: (message: string) => void,
  ): void;
  translateStreamError(error: unknown): unknown;
  t(key: string): string;
}) {
  const [incognito, setIncognito] = useState(false);
  const [incognitoMessages, setIncognitoMessages] = useState<
    MessageWithActivityTrace[]
  >([]);

  const enterIncognito = useCallback(() => {
    // Incognito starts clean: it takes over the whole surface, so it gets its own
    // draft scope and its own run key rather than borrowing the start screen's.
    // Normal threads keep streaming behind it — they are separate runs, and
    // nothing about them is visible or reachable from here.
    abortStreamRun(INCOGNITO_RUN_KEY);
    endStreamRun(INCOGNITO_RUN_KEY, { keepFailedTurnVisible: false });
    setDrafts((current) => clearDraft(current, INCOGNITO_DRAFT_SCOPE));
    setSendError("");
    setIncognitoMessages([]);
    setIncognito(true);
  }, [abortStreamRun, endStreamRun, setDrafts, setSendError]);

  const exitIncognito = useCallback(() => {
    // Discard the ephemeral transcript — nothing was ever written, so there is
    // nothing to clean up server-side.
    abortStreamRun(INCOGNITO_RUN_KEY);
    endStreamRun(INCOGNITO_RUN_KEY, { keepFailedTurnVisible: false });
    setIncognito(false);
    setIncognitoMessages([]);
    setDrafts((current) => clearDraft(current, INCOGNITO_DRAFT_SCOPE));
    setSendError("");
  }, [abortStreamRun, endStreamRun, setDrafts, setSendError]);

  // sendIncognitoContent mirrors sendContent's live-block accumulation but routes
  // to the stateless endpoint: no thread is created, no navigation happens, and the
  // whole prior transcript is replayed as history (the server keeps none). The
  // assistant message is appended to the in-memory transcript only.
  async function sendIncognitoContent(
    content: string,
    restoreDraftOnError: boolean,
    // On error restore the textarea to restore.draft (without the merged pasted
    // blocks) and re-stage restore.pastedTexts. Defaults to the full `content`.
    restore?: { draft: string; pastedTexts: PastedText[] },
  ) {
    setDrafts((current) => clearDraft(current, INCOGNITO_DRAFT_SCOPE));
    setSendError("");
    const history = incognitoMessages
      .filter(
        (message) => message.role === "user" || message.role === "assistant",
      )
      .map((message) => ({
        role: message.role as "user" | "assistant",
        content: message.content,
      }));
    const tempID = newTempID("incognito-user");
    const optimisticMessage: MessageWithActivityTrace = {
      id: tempID,
      clientKey: tempID,
      threadId: "incognito",
      role: "user",
      content,
      createdAt: new Date().toISOString(),
      // Render collapsed pastes as chips here too (incognito is ephemeral, so this
      // is the in-session bubble only), matching the persisted path in sendContent.
      ...(restore && restore.pastedTexts.length > 0
        ? { pastedTexts: restore.pastedTexts.map(toPastedTextBlock) }
        : {}),
    };
    setIncognitoMessages((current) => [...current, optimisticMessage]);
    const abortController = new AbortController();
    beginStreamRun(INCOGNITO_RUN_KEY, abortController);
    const turn = createTurnHandlers({
      patch: (next) => patchStreamRun(INCOGNITO_RUN_KEY, next),
      onAssistantMessage: (message, liveBlocks) => {
        // Give each turn a unique id so React keys and per-message actions never
        // collide (the server returns a constant synthetic id).
        const uniqueID = newTempID("incognito-assistant");
        const grafted = graftStreamedBlocks(
          { ...message, id: uniqueID },
          liveBlocks,
        );
        setIncognitoMessages((current) => [
          ...current,
          { ...grafted, clientKey: uniqueID },
        ]);
      },
    });
    let keepFailedTurnVisible = false;
    try {
      await streamIncognitoMessage(
        content,
        history,
        turn.handlers,
        abortController.signal,
      );
    } catch (error) {
      if (error instanceof DOMException && error.name === "AbortError") return;
      if (abortController.signal.aborted) return;
      keepFailedTurnVisible = true;
      // Drop the optimistic user bubble that never got a reply so the user can retry.
      setIncognitoMessages((current) =>
        current.filter((message) => message.id !== tempID),
      );
      if (restoreDraftOnError) {
        setDrafts((current) =>
          setScopedDraft(current, INCOGNITO_DRAFT_SCOPE, {
            text: restore?.draft ?? content,
            pastedTexts: restore?.pastedTexts ?? [],
          }),
        );
      }
      handleActionError(
        translateStreamError(error),
        t("thread.sendFailed"),
        (message) => patchStreamRun(INCOGNITO_RUN_KEY, { error: message }),
      );
    } finally {
      endStreamRun(INCOGNITO_RUN_KEY, {
        keepFailedTurnVisible,
        controller: abortController,
      });
    }
  }

  const handleIncognitoRetry = useCallback(
    (content: string, pastedTexts?: MessagePastedText[]) => {
      const blocks = pastedTexts ?? [];
      if (content.trim() === "" && blocks.length === 0) return;
      setDrafts((current) =>
        setScopedDraft(current, INCOGNITO_DRAFT_SCOPE, {
          text: content,
          pastedTexts: blocks.map(pastedTextFromBlock),
        }),
      );
      requestComposerFocus();
    },
    [requestComposerFocus, setDrafts],
  );

  return {
    incognito,
    incognitoMessages,
    enterIncognito,
    exitIncognito,
    sendIncognitoContent,
    handleIncognitoRetry,
  };
}
