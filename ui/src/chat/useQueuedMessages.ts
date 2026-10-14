import { useCallback, useRef, useState } from "react";
import type { MessagePastedText, QueuedMessage } from "../api";

// QueuedBubble is a message the user sent while the thread's turn was running,
// shown below the live answer until the turn picks it up.
export type QueuedBubble = {
  clientMessageId: string;
  content: string;
  pastedTexts: MessagePastedText[];
};

export type QueuedByThread = Record<string, QueuedBubble[]>;

export function toQueuedBubble(message: QueuedMessage): QueuedBubble {
  return {
    clientMessageId: message.clientMessageId,
    content: message.content,
    pastedTexts: message.pastedTexts ?? [],
  };
}

// useQueuedMessages keeps the queued bubbles of every thread, keyed by thread
// id: a turn on another thread picks up its queue while you look elsewhere.
// The ref mirrors the state for stream callbacks that outlive a render.
export function useQueuedMessages() {
  const [queued, setQueued] = useState<QueuedByThread>({});
  const queuedRef = useRef<QueuedByThread>({});
  // The ref is the source of truth, written at once, so a stream callback in
  // the same tick reads what was just added.
  const update = useCallback(
    (threadID: string, next: (current: QueuedBubble[]) => QueuedBubble[]) => {
      const all = queuedRef.current;
      const list = next(all[threadID] ?? []);
      const result = { ...all };
      if (list.length === 0) delete result[threadID];
      else result[threadID] = list;
      queuedRef.current = result;
      setQueued(result);
    },
    [],
  );
  const add = useCallback(
    (threadID: string, bubble: QueuedBubble) =>
      update(threadID, (current) => [...current, bubble]),
    [update],
  );
  // remove drops a bubble; true when it was there.
  const remove = useCallback(
    (threadID: string, clientMessageId: string | undefined): boolean => {
      if (clientMessageId === undefined || clientMessageId === "") return false;
      const present = (queuedRef.current[threadID] ?? []).some(
        (bubble) => bubble.clientMessageId === clientMessageId,
      );
      if (present)
        update(threadID, (current) =>
          current.filter(
            (bubble) => bubble.clientMessageId !== clientMessageId,
          ),
        );
      return present;
    },
    [update],
  );
  // replace sets a thread's queue to what the server reports.
  const replace = useCallback(
    (threadID: string, messages: QueuedMessage[]) =>
      update(threadID, () => messages.map(toQueuedBubble)),
    [update],
  );
  const clear = useCallback(
    (threadID: string) => update(threadID, () => []),
    [update],
  );
  const has = useCallback(
    (threadID: string) => (queuedRef.current[threadID] ?? []).length > 0,
    [],
  );
  return { queued, add, remove, replace, clear, has };
}
