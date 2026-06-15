import type { Message } from "../api";
import type { ComposerAttachment } from "./useDocumentAttachments";
import type { MessageWithActivityTrace } from "./types";

export function greetingForNow(fullName: string) {
  const name = fullName.trim().split(/\s+/)[0];
  const hour = new Date().getHours();
  if (hour < 10) return `Morning, ${name}`;
  if (hour >= 22) return `Up late, ${name}?`;
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

// Append a streaming content delta to the accumulated assistant text. The
// assistant loop streams each tool round's prose as its own run of deltas, all
// concatenated into one string; across a round boundary (model finishes a
// round's prose, runs tools, then resumes) the last sentence of one round and
// the first of the next would otherwise fuse ("…done.Based on…"). When a round
// boundary is pending we open a fresh paragraph instead — but only if there is
// preceding text that does not already end in whitespace, so a boundary before
// any prose (or one the model already separated) adds nothing.
export function appendStreamingDelta(current: string, delta: string, turnBreakPending: boolean): string {
  if (turnBreakPending && current !== "" && !/\s$/.test(current)) {
    return `${current}\n\n${delta}`;
  }
  return current + delta;
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
