import { expect, test } from "vitest";

import { appendStreamingDelta, updateMessageAttachment } from "./chatUtils";
import type { MessageWithActivityTrace } from "./types";

test("appendStreamingDelta concatenates deltas within a round verbatim", () => {
  expect(appendStreamingDelta("Hel", "lo", false)).toBe("Hello");
  expect(appendStreamingDelta("Hello.", " World", false)).toBe("Hello. World");
});

test("appendStreamingDelta opens a fresh paragraph at a round boundary", () => {
  expect(appendStreamingDelta("All done.", "Based on the results", true)).toBe(
    "All done.\n\nBased on the results",
  );
});

test("appendStreamingDelta does not break before any prose has streamed", () => {
  expect(appendStreamingDelta("", "First word", true)).toBe("First word");
});

test("appendStreamingDelta does not double-separate when prose already ends in whitespace", () => {
  expect(appendStreamingDelta("Para one.\n\n", "Para two", true)).toBe("Para one.\n\nPara two");
  expect(appendStreamingDelta("Trailing space ", "next", true)).toBe("Trailing space next");
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
