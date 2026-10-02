import { useCallback } from "react";

import {
  isImageAttachment,
  toSentAttachment,
  useDocumentAttachments,
  type ComposerAttachment,
} from "./useDocumentAttachments";

// useComposerAttachments wires a composer that uploads as files are picked (a
// thread, a project page) to its send: the staged attachments for the scope,
// whether an image is still uploading (the send waits for its artifact id), and
// a send that hands the staged files over and clears them.
export function useComposerAttachments(
  scope: { threadId?: string; projectId?: string },
  onSend: (attachments: ComposerAttachment[]) => void,
) {
  const documentAttachments = useDocumentAttachments(scope);
  const { attachments, clearAttachments } = documentAttachments;

  const imageUploadPending = attachments.some(
    (attachment) =>
      isImageAttachment(attachment) &&
      attachment.artifactId === undefined &&
      attachment.status !== "error",
  );

  const handleSendRequest = useCallback(() => {
    const sentAttachments = attachments.map(toSentAttachment);
    // Not revoked: the sent bubble keeps showing the preview URLs.
    if (sentAttachments.length > 0)
      clearAttachments({ revokePreviewUrls: false });
    onSend(sentAttachments);
  }, [attachments, clearAttachments, onSend]);

  return { ...documentAttachments, imageUploadPending, handleSendRequest };
}
