import { expect, test } from "vitest";

import { updateMessageAttachment } from "./threadUtils";
import type { MessageWithActivityTrace } from "./types";

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
