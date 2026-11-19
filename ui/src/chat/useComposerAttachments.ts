import { useCallback } from "react";

import {
  isImageAttachment,
  toSentAttachment,
  useDocumentAttachments,
  type ComposerAttachment,
} from "./useDocumentAttachments";

// ComposerSend sends the staged attachments with the draft. It resolves
// whether they went with it: false (an empty draft, a send queued behind a
// running turn, a send that failed before the server stored the message)
// puts them back as chips.
export type ComposerSend = (
  attachments: ComposerAttachment[],
) => void | Promise<boolean>;

// useComposerAttachments wires a composer that uploads as files are picked (a
// thread, a project page) to its send: the staged attachments for the scope,
// whether an image is still uploading (the send waits for its artifact id), and
// a send that hands the staged files over and clears them.
export function useComposerAttachments(
  scope: { threadId?: string; projectId?: string },
  onSend: ComposerSend,
) {
  const documentAttachments = useDocumentAttachments(scope);
  const { attachments, takeAttachments } = documentAttachments;

  const imageUploadPending = attachments.some(
    (attachment) =>
      isImageAttachment(attachment) &&
      attachment.artifactId === undefined &&
      attachment.status !== "error",
  );

  const handleSendRequest = useCallback(() => {
    const sentAttachments = attachments.map(toSentAttachment);
    if (sentAttachments.length === 0) {
      void onSend(sentAttachments);
      return;
    }
    const restore = takeAttachments();
    void Promise.resolve(onSend(sentAttachments)).then((taken) => {
      if (taken === false) restore();
    });
  }, [attachments, takeAttachments, onSend]);

  return { ...documentAttachments, imageUploadPending, handleSendRequest };
}
