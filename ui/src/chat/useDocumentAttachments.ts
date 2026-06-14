import { useCallback, useState } from "react";

import {
  DOCUMENT_MAX_ATTACHMENTS_PER_MESSAGE,
  indexDocument,
  listDocuments,
  uploadDocument,
  uploadImageAttachment,
} from "../api";
import { isWithinUploadSizeLimit } from "./attachmentFiles";

export type ComposerAttachmentStatus = "queued" | "uploading" | "processing" | "ready" | "error";

export type ComposerAttachment = {
  id: string;
  filename: string;
  mimeType: string;
  sizeBytes: number;
  status: ComposerAttachmentStatus;
  error?: string;
  previewUrl?: string;
  documentId?: string;
  artifactId?: string;
  file?: File;
};

let nextAttachmentID = 0;

export function createComposerAttachment(file: File, status: ComposerAttachmentStatus = "uploading"): ComposerAttachment {
  nextAttachmentID += 1;
  const previewUrl =
    file.type.startsWith("image/") && typeof URL.createObjectURL === "function"
      ? URL.createObjectURL(file)
      : undefined;
  return {
    id: `attachment-${Date.now()}-${nextAttachmentID}`,
    filename: file.name,
    mimeType: file.type,
    sizeBytes: file.size,
    status,
    previewUrl,
    file,
  };
}

export function toSentAttachment(attachment: ComposerAttachment): ComposerAttachment {
  const { file: _file, ...sent } = attachment;
  return sent;
}

export function isImageAttachment(attachment: Pick<ComposerAttachment, "mimeType" | "filename">): boolean {
  return attachment.mimeType.startsWith("image/") || /\.(png|jpe?g|webp|gif)$/i.test(attachment.filename);
}

type AttachmentStatusHandler = (id: string, patch: Partial<ComposerAttachment>) => void;

// Shared "+" composer attachment flow: upload a picked file, add it to knowledge,
// and surface ingestion progress via attachNote. Scope decides where the document
// lands for retrieval: a projectId scopes it to a project; a project-less upload
// with a threadId is private to that one chat; without either it is user-global.
// The scope can be overridden per call (used by the new-chat deferred upload,
// which only knows the freshly created thread id at send time).
export function useDocumentAttachments(scope: { threadId?: string; projectId?: string }) {
  const [attachNote, setAttachNote] = useState("");
  const [attachments, setAttachments] = useState<ComposerAttachment[]>([]);

  const updateAttachment = useCallback((id: string, patch: Partial<ComposerAttachment>) => {
    setAttachments((current) =>
      current.map((attachment) => (attachment.id === id ? { ...attachment, ...patch } : attachment)),
    );
  }, []);

  const removeAttachment = useCallback((id: string) => {
    setAttachments((current) => {
      const removed = current.find((attachment) => attachment.id === id);
      if (removed?.previewUrl !== undefined) URL.revokeObjectURL(removed.previewUrl);
      return current.filter((attachment) => attachment.id !== id);
    });
  }, []);

  const clearAttachments = useCallback((options: { revokePreviewUrls?: boolean } = {}) => {
    const revokePreviewUrls = options.revokePreviewUrls ?? true;
    setAttachments((current) => {
      if (revokePreviewUrls) {
        current.forEach((attachment) => {
          if (attachment.previewUrl !== undefined) URL.revokeObjectURL(attachment.previewUrl);
        });
      }
      return [];
    });
  }, []);

  const handleAttachFiles = useCallback(
    (files: File[], override?: { threadId?: string; projectId?: string }) => {
      const threadId = override?.threadId ?? scope.threadId;
      const projectId = override?.projectId ?? scope.projectId;
      const sizeFiltered = files.filter(isWithinUploadSizeLimit);
      if (sizeFiltered.length < files.length) {
        setAttachNote("Files must be 25 MB or smaller.");
      }
      const remaining = DOCUMENT_MAX_ATTACHMENTS_PER_MESSAGE - attachments.length;
      if (remaining <= 0) {
        setAttachNote(`You can attach up to ${DOCUMENT_MAX_ATTACHMENTS_PER_MESSAGE} files per message.`);
        return;
      }
      const accepted = sizeFiltered.slice(0, remaining);
      if (accepted.length < sizeFiltered.length) {
        setAttachNote(`You can attach up to ${DOCUMENT_MAX_ATTACHMENTS_PER_MESSAGE} files per message.`);
      }
      if (accepted.length === 0) return;
      const pending = accepted.map((file) =>
        createComposerAttachment(file, threadId === undefined && projectId === undefined ? "queued" : "uploading"),
      );
      setAttachments((current) => [...current, ...pending]);
      if (threadId !== undefined || projectId !== undefined) {
        void uploadAttachments(pending, { threadId, projectId }, updateAttachment, setAttachNote);
      }
    },
    [attachments.length, scope.threadId, scope.projectId, updateAttachment],
  );

  const uploadExistingAttachments = useCallback(
    (
      existingAttachments: ComposerAttachment[],
      override: { threadId?: string; projectId?: string },
      onStatus: AttachmentStatusHandler,
    ) => {
      return uploadAttachments(existingAttachments, override, onStatus, setAttachNote);
    },
    [],
  );

  const handleAttachError = useCallback((message: string) => {
    setAttachNote(message);
  }, []);

  return {
    attachNote,
    attachments,
    clearAttachments,
    handleAttachError,
    handleAttachFiles,
    removeAttachment,
    uploadExistingAttachments,
  };
}

async function uploadAttachments(
  attachments: ComposerAttachment[],
  scope: { threadId?: string; projectId?: string },
  onStatus: AttachmentStatusHandler,
  setAttachNote: (message: string) => void,
) {
  const { threadId, projectId } = scope;

  const waitForIngestion = async (attachmentId: string, documentId: string, filename: string) => {
    for (let attempt = 0; attempt < 40; attempt += 1) {
      await new Promise((resolve) => setTimeout(resolve, 1500));
      let docs;
      try {
        docs = await listDocuments(projectId);
      } catch {
        continue;
      }
      const current = docs.find((d) => d.id === documentId);
      if (current === undefined) continue;
      if (current.status === "embedded") {
        onStatus(attachmentId, { status: "ready" });
        setAttachNote("");
        return;
      }
      if (current.status === "error" || current.status === "stale") {
        onStatus(attachmentId, {
          status: "error",
          error: current.error || `Could not index ${filename}.`,
        });
        setAttachNote(`Could not index ${filename}${current.error ? `: ${current.error}` : "."}`);
        return;
      }
    }
    setAttachNote(`${filename} is still processing…`);
  };

  const uploadDocumentAttachment = async (attachment: ComposerAttachment) => {
    if (attachment.file === undefined) return;
    setAttachNote(`Uploading ${attachment.filename}…`);
    onStatus(attachment.id, { status: "uploading" });
    try {
      const doc = await uploadDocument(attachment.file, { threadId, projectId });
      onStatus(attachment.id, {
        status: "processing",
        documentId: doc.id,
        artifactId: doc.artifactId,
      });
      setAttachNote(`Processing ${attachment.filename}…`);
      await indexDocument(doc.id);
      await waitForIngestion(attachment.id, doc.id, attachment.filename);
    } catch (error) {
      const message = error instanceof Error ? error.message : `Failed to upload ${attachment.filename}.`;
      onStatus(attachment.id, { status: "error", error: message });
      setAttachNote(message);
    }
  };

  for (const attachment of attachments) {
    if (attachment.file === undefined || attachment.artifactId !== undefined) continue;
    if (threadId === undefined && projectId === undefined) {
      setAttachNote(`${attachment.filename} will upload when you send.`);
      continue;
    }
    if (isImageAttachment(attachment)) {
      setAttachNote(`Uploading ${attachment.filename}…`);
      onStatus(attachment.id, { status: "uploading" });
      try {
        const image = await uploadImageAttachment(attachment.file, { threadId, projectId });
        onStatus(attachment.id, { status: "ready", artifactId: image.id });
        setAttachNote(`Attached ${attachment.filename}.`);
      } catch (error) {
        const message = error instanceof Error ? error.message : `Failed to upload ${attachment.filename}.`;
        onStatus(attachment.id, { status: "error", error: message });
        setAttachNote(message);
      }
      continue;
    }
    void uploadDocumentAttachment(attachment);
  }
}
