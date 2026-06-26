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

// reconcileUserMessage folds the server-confirmed user message into the list.
// When the optimistic placeholder identified by `placeholderID` is present it is
// replaced in place — keeping its slot and clientKey so the React key is stable
// (no remount/scroll jump) — and any stray copy of the confirmed id is dropped, so
// a delayed/duplicate user_message event can never leave two bubbles behind. When
// the placeholder is gone but a copy of the confirmed message is already present
// (e.g. a route refresh reloaded it), the list is returned unchanged: that keeps
// the loaded object's richer fields, key and position rather than overwriting them
// with the streamed payload. Otherwise the message is appended once.
export function reconcileUserMessage(
  messages: MessageWithActivityTrace[],
  placeholderID: string | null,
  confirmed: MessageWithActivityTrace,
): MessageWithActivityTrace[] {
  const placeholderIndex =
    placeholderID !== null ? messages.findIndex((message) => message.id === placeholderID) : -1;
  if (placeholderIndex !== -1) {
    const reconciled: MessageWithActivityTrace = {
      ...confirmed,
      clientKey: messages[placeholderIndex].clientKey,
    };
    const result: MessageWithActivityTrace[] = [];
    messages.forEach((message, index) => {
      if (index === placeholderIndex) result.push(reconciled);
      else if (message.id !== confirmed.id) result.push(message);
    });
    return result;
  }
  if (messages.some((message) => message.id === confirmed.id)) return messages;
  return [...messages, { ...confirmed, clientKey: confirmed.id }];
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
