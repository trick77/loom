import type { Message } from "../api";
import type { ComposerAttachment } from "./useDocumentAttachments";
import type { MessageWithActivityTrace } from "./types";

export function greetingForNow(fullName: string) {
  const name = fullName.trim().split(/\s+/)[0];
  const hour = new Date().getHours();
  if (hour < 10) return `Morning, ${name}`;
  if (hour >= 23) return `Up late, ${name}?`;
  if (hour >= 18) return `Evening, ${name}`;
  if (hour >= 13) return `Afternoon, ${name}`;
  return `${name} returns!`;
}

export function isNearBottom(element: HTMLElement): boolean {
  return element.scrollHeight - element.scrollTop - element.clientHeight <= 48;
}

export function previousUserContent(messages: Message[], beforeIndex: number): string | null {
  for (let index = beforeIndex - 1; index >= 0; index -= 1) {
    const message = messages[index];
    if (message.role === "user") return message.content;
  }
  return null;
}

// reconcileUserMessage folds the server-confirmed user message into the list,
// replacing the optimistic placeholder identified by `placeholderID` in place so
// its clientKey/position survive (a stable React key => no remount or scroll jump).
// It is idempotent: any existing copy of the placeholder *and* of the confirmed id
// are removed before a single insert, so a delayed/duplicate user_message event or
// a copy already loaded by a route refresh can never leave two bubbles behind.
export function reconcileUserMessage(
  messages: MessageWithActivityTrace[],
  placeholderID: string | null,
  confirmed: MessageWithActivityTrace,
): MessageWithActivityTrace[] {
  const placeholder =
    placeholderID !== null
      ? messages.find((message) => message.id === placeholderID)
      : undefined;
  const reconciled: MessageWithActivityTrace = {
    ...confirmed,
    clientKey: placeholder?.clientKey ?? confirmed.id,
  };
  const insertAt = messages.findIndex(
    (message) => message.id === placeholderID || message.id === confirmed.id,
  );
  const without = messages.filter(
    (message) => message.id !== placeholderID && message.id !== confirmed.id,
  );
  if (insertAt === -1) return [...without, reconciled];
  return [...without.slice(0, insertAt), reconciled, ...without.slice(insertAt)];
}

export function updateMessageAttachment(
  messages: MessageWithActivityTrace[],
  attachmentId: string,
  patch: Partial<ComposerAttachment>,
): MessageWithActivityTrace[] {
  return messages.map((message) => {
    if (message.attachments === undefined) return message;
    const attachments = message.attachments.map((attachment) =>
      attachment.id === attachmentId ? { ...attachment, ...patch } : attachment,
    );
    return { ...message, attachments };
  });
}
