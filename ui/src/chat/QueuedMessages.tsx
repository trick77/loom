import { useTranslation } from "react-i18next";

import { stripPastedBlocks } from "./pastedText";
import type { QueuedBubble } from "./useQueuedMessages";

// QueuedMessages lists the messages sent while the answer streams, under the
// live answer: muted, marked queued, each with a way to take it back. A turn
// picking one up moves it into the transcript as a sent message.
export function QueuedMessages({
  messages,
  onCancel,
}: {
  messages: QueuedBubble[];
  onCancel(message: QueuedBubble): void;
}) {
  const { t } = useTranslation();
  if (messages.length === 0) return null;
  return (
    <ul aria-label={t("thread.queued")} className="space-y-3">
      {messages.map((message) => {
        const { text } = stripPastedBlocks(
          message.content,
          message.pastedTexts,
        );
        return (
          <li
            key={message.clientMessageId}
            className="ml-auto w-fit max-w-full opacity-60 md:max-w-[38.25rem]"
          >
            <div className="ui-message-text whitespace-pre-wrap break-words rounded-xl border border-dashed border-[#5a5852] bg-[#111110] px-4 py-3 text-[#f3f0e8]">
              {text !== "" ? text : t("composer.pastedText")}
            </div>
            <div className="ui-meta-text mt-1 flex items-center justify-end gap-2 text-[#aaa79e]">
              <span title={t("thread.queuedHint")}>{t("thread.queued")}</span>
              <button
                type="button"
                className="rounded px-1 leading-none hover:text-ink"
                aria-label={t("thread.removeQueued")}
                onClick={() => onCancel(message)}
              >
                ×
              </button>
            </div>
          </li>
        );
      })}
    </ul>
  );
}
