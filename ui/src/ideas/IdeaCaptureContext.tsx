import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
  useSyncExternalStore,
  type ReactNode,
} from "react";
import { useTranslation } from "react-i18next";

import { Icon } from "../chat/Icon";

/**
 * What a capture starts from: the message and the passage it keeps.
 * passage marks a marked passage; without it the capture is the whole
 * answer's (its lightbulb's).
 */
export type CaptureRequest = {
  messageId: string;
  excerpt: string;
  passage?: boolean;
};

/** One capture's identity: its kept draft, a turned-down offer. */
export const captureKey = (request: CaptureRequest) =>
  `${request.messageId}\n${request.passage === true ? "p" : "a"}\n${request.excerpt}`;

export type IdeaCaptureValue = {
  threadId: string;
  /** A getter: a renamed thread re-renders no answer. */
  threadTitle(): string;
  /** The user's message an answer replied to, for display ("" if none). */
  questionFor(messageId: string): string;
  open(request: CaptureRequest): void;
  close(): void;
  /** messageId's capture of threadId was saved: the toast offers to view it. */
  saved(ideaID: string, threadId: string, messageId: string): void;
  /** The open capture, as a store: only the bubbles it concerns re-render. */
  subscribe(listener: () => void): () => void;
  current(): CaptureRequest | null;
};

const IdeaCaptureContext = createContext<IdeaCaptureValue | null>(null);

/**
 * The capture's actions for one thread. Only a stored thread provides them:
 * an incognito chat and a public share get null, so no lightbulb.
 */
export function useIdeaCapture(): IdeaCaptureValue | null {
  return useContext(IdeaCaptureContext);
}

/** The capture open for messageId, or null: re-renders only on its change. */
export function useOpenCapture(
  capture: IdeaCaptureValue | null,
  messageId: string | undefined,
): CaptureRequest | null {
  const subscribe = useCallback(
    (listener: () => void) => capture?.subscribe(listener) ?? (() => {}),
    [capture],
  );
  return useSyncExternalStore(subscribe, () => {
    const active = capture?.current() ?? null;
    return active !== null && active.messageId === messageId ? active : null;
  });
}

// SAVED_TOAST_MS is how long "Idea saved" stays.
export const SAVED_TOAST_MS = 6000;

export function IdeaCaptureProvider({
  threadId,
  threadTitle,
  questionFor,
  onOpenIdea,
  children,
}: {
  /** Null offers no capture (no stored thread yet). */
  threadId: string | null;
  threadTitle: string;
  questionFor?(messageId: string): string;
  onOpenIdea?(ideaID: string): void;
  children: ReactNode;
}) {
  // One capture at a time per thread, kept outside React state so opening
  // one does not re-render every answer of the thread.
  const store = useRef<{
    active: CaptureRequest | null;
    listeners: Set<() => void>;
  }>({
    active: null,
    listeners: new Set(),
  });
  const set = useCallback((active: CaptureRequest | null) => {
    store.current.active = active;
    store.current.listeners.forEach((listener) => listener());
  }, []);
  const subscribe = useCallback((listener: () => void) => {
    store.current.listeners.add(listener);
    return () => store.current.listeners.delete(listener);
  }, []);
  const current = useCallback(() => store.current.active, []);
  const open = useCallback((request: CaptureRequest) => set(request), [set]);
  const close = useCallback(() => set(null), [set]);
  const [savedID, setSavedID] = useState<string | null>(null);
  // The thread a save belongs to: a save that lands after the user moved on
  // shows no toast in the next thread.
  const threadRef = useRef(threadId);
  useEffect(() => {
    threadRef.current = threadId;
    set(null);
    // eslint-disable-next-line react-hooks/set-state-in-effect -- another thread: its toast goes
    setSavedID(null);
  }, [threadId, set]);
  const saved = useCallback(
    (ideaID: string, savedThread: string, messageId: string) => {
      if (threadRef.current !== savedThread) return;
      // Only the capture that saved closes: another may be open by now.
      if (store.current.active?.messageId === messageId) set(null);
      setSavedID(ideaID);
    },
    [set],
  );
  const titleRef = useRef(threadTitle);
  useEffect(() => {
    titleRef.current = threadTitle;
  }, [threadTitle]);
  const getTitle = useCallback(() => titleRef.current, []);
  // Read when a capture opens, like the title: new messages re-render no
  // answer.
  const questionRef = useRef(questionFor);
  useEffect(() => {
    questionRef.current = questionFor;
  });
  const getQuestion = useCallback(
    (messageId: string) => questionRef.current?.(messageId) ?? "",
    [],
  );
  const value = useMemo(
    () =>
      threadId === null
        ? null
        : {
            threadId,
            threadTitle: getTitle,
            questionFor: getQuestion,
            open,
            close,
            saved,
            subscribe,
            current,
          },
    [threadId, getTitle, getQuestion, open, close, saved, subscribe, current],
  );
  return (
    <IdeaCaptureContext.Provider value={value}>
      {children}
      {savedID !== null && (
        <SavedToast
          key={savedID}
          onView={() => {
            setSavedID(null);
            onOpenIdea?.(savedID);
          }}
          onDismiss={() => setSavedID(null)}
        />
      )}
    </IdeaCaptureContext.Provider>
  );
}

// SavedToast is a passing status: it takes no Escape (that stops a running
// answer), it leaves by itself.
function SavedToast({
  onView,
  onDismiss,
}: {
  onView(): void;
  onDismiss(): void;
}) {
  const { t } = useTranslation();
  const dismiss = useRef(onDismiss);
  useEffect(() => {
    dismiss.current = onDismiss;
  });
  useEffect(() => {
    const handle = window.setTimeout(() => dismiss.current(), SAVED_TOAST_MS);
    return () => window.clearTimeout(handle);
  }, []);
  return (
    <div className="pointer-events-none fixed inset-x-0 bottom-28 z-50 flex justify-center px-4">
      <div
        role="status"
        className="prompt-panel-in pointer-events-auto flex items-center gap-2.5 rounded-[10px] border border-[#454540] bg-[#363632] px-3 py-2 font-sans text-[14px] shadow-[0_18px_32px_rgba(0,0,0,0.38)]"
      >
        <Icon name="check" size="15px" className="shrink-0 text-success" />
        <span className="text-[#ecece6]">{t("ideas.capture.saved")}</span>
        <button
          type="button"
          className="text-unread-ink hover:text-white"
          onClick={onView}
        >
          {t("ideas.capture.view")}
        </button>
        <button
          type="button"
          aria-label={t("common.close")}
          className="text-[#8f8b82] hover:text-white"
          onClick={onDismiss}
        >
          <Icon name="close" size="14px" />
        </button>
      </div>
    </div>
  );
}
