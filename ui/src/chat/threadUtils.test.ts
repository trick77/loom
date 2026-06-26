import { expect, test } from "vitest";

import { reconcileUserMessage, updateMessageAttachment } from "./threadUtils";
import type { MessageWithActivityTrace } from "./types";

function userMessage(
  overrides: Partial<MessageWithActivityTrace> & { id: string },
): MessageWithActivityTrace {
  return {
    threadId: "t1",
    role: "user",
    content: "Hello",
    createdAt: "2026-06-14T00:00:00Z",
    ...overrides,
  };
}

test("replaces the optimistic placeholder in place, keeping its clientKey", () => {
  const messages = [
    userMessage({ id: "m0", content: "earlier" }),
    userMessage({ id: "temp-1", clientKey: "temp-1", content: "hi" }),
  ];

  const result = reconcileUserMessage(messages, "temp-1", userMessage({ id: "m1", content: "hi" }));

  expect(result.map((m) => m.id)).toEqual(["m0", "m1"]);
  // clientKey carried over from the placeholder => stable React key, no remount.
  expect(result[1].clientKey).toBe("temp-1");
});

test("replaces in place even when called after the reset that caused the bug", () => {
  // Regression: the original code read optimisticUserMessageID *inside* the deferred
  // updater, which React could run after it had been reset to null — losing the
  // placeholder and appending a second bubble. The fix captures the temp id into a
  // const first, so this call always receives the real placeholder id and dedups.
  const messages = [userMessage({ id: "temp-1", clientKey: "temp-1", content: "hi" })];

  const result = reconcileUserMessage(messages, "temp-1", userMessage({ id: "m1", content: "hi" }));

  expect(result.map((m) => m.id)).toEqual(["m1"]);
  expect(result[0].clientKey).toBe("temp-1");
});

test("preserves an already-loaded copy on a duplicate/late event (no key flip or field overwrite)", () => {
  // A route refresh loaded the persisted message (richer fields, keyed off its id),
  // and the placeholder is already gone. A delayed/duplicate user_message must not
  // remount the bubble (clientKey flip) or revert its fields to the streamed payload.
  const loaded = userMessage({
    id: "m1",
    content: "hi",
    createdAt: "2026-06-14T00:00:01Z",
    attachments: [
      { id: "att-1", filename: "a.pdf", mimeType: "application/pdf", sizeBytes: 1, status: "ready" },
    ],
  });
  const messages = [userMessage({ id: "m0", content: "earlier" }), loaded];

  const result = reconcileUserMessage(messages, null, userMessage({ id: "m1", content: "hi" }));

  expect(result).toEqual(messages);
  expect(result[1]).toBe(loaded); // same object reference => no remount, fields intact
});

test("appends once when no placeholder was inserted (id null, no temp bubble)", () => {
  // placeholderID is null only when the optimistic insert was skipped, so the list
  // holds no temp bubble to reconcile — the confirmed message is simply added once.
  const messages = [userMessage({ id: "m0", content: "earlier" })];

  const result = reconcileUserMessage(messages, null, userMessage({ id: "m1", content: "hi" }));

  expect(result.map((m) => m.id)).toEqual(["m0", "m1"]);
});

test("is idempotent when a copy of the confirmed message is already present", () => {
  // A route refresh may have loaded the persisted message while the placeholder lingers.
  const messages = [
    userMessage({ id: "m1", content: "hi" }),
    userMessage({ id: "temp-1", clientKey: "temp-1", content: "hi" }),
  ];

  const result = reconcileUserMessage(messages, "temp-1", userMessage({ id: "m1", content: "hi" }));

  expect(result.filter((m) => m.id === "m1")).toHaveLength(1);
  expect(result.map((m) => m.id)).toEqual(["m1"]);
});

test("appends when neither the placeholder nor a confirmed copy exist", () => {
  const messages = [userMessage({ id: "m0", content: "earlier" })];

  const result = reconcileUserMessage(messages, "temp-gone", userMessage({ id: "m1", content: "hi" }));

  expect(result.map((m) => m.id)).toEqual(["m0", "m1"]);
  expect(result[1].clientKey).toBe("m1");
});

test("updates an attachment inside an already rendered user message", () => {
  const messages: MessageWithActivityTrace[] = [
    {
      id: "m1",
      threadId: "t1",
      role: "user",
      content: "Summarize this",
      createdAt: "2026-06-14T00:00:00Z",
      attachments: [
        {
          id: "att-1",
          filename: "briefing.pdf",
          mimeType: "application/pdf",
          sizeBytes: 1024,
          status: "uploading",
        },
      ],
    },
  ];

  const updated = updateMessageAttachment(messages, "att-1", {
    status: "ready",
    documentId: "doc-1",
  });

  expect(updated[0].attachments?.[0]).toMatchObject({
    id: "att-1",
    status: "ready",
    documentId: "doc-1",
  });
  expect(messages[0].attachments?.[0].status).toBe("uploading");
});
