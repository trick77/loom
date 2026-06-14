import { useCallback, useState } from "react";

import {
  DOCUMENT_MAX_ATTACHMENTS_PER_MESSAGE,
  indexDocument,
  uploadDocument,
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
  documentId?: string;
  artifactId?: string;
  file?: File;
};

let nextAttachmentID = 0;

export function createComposerAttachment(file: File, status: ComposerAttachmentStatus = "uploading"): ComposerAttachment {
  nextAttachmentID += 1;
  return {
    id: `attachment-${Date.now()}-${nextAttachmentID}`,
    filename: file.name,
    mimeType: file.type,
    sizeBytes: file.size,
    status,
    file,
  };
}

export function toSentAttachment(attachment: ComposerAttachment): ComposerAttachment {
  const { file: _file, ...sent } = attachment;
  return sent;
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
    setAttachments((current) => current.filter((attachment) => attachment.id !== id));
  }, []);

  const clearAttachments = useCallback(() => {
    setAttachments([]);
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

  const uploadDocumentAttachment = async (attachment: ComposerAttachment) => {
    if (attachment.file === undefined) return;
    setAttachNote(`Uploading ${attachment.filename}…`);
    onStatus(attachment.id, { status: "uploading" });
    try {
      const doc = await uploadDocument(attachment.file, { threadId, projectId });
      // The document is usable inline as soon as it is uploaded — its full text is
      // injected into the prompt on send — so don't block sending on embedding.
      // Mark ready immediately, then index in the background so the large-document
      // RAG fallback and project knowledge retrieval stay available.
      onStatus(attachment.id, {
        status: "ready",
        documentId: doc.id,
        artifactId: doc.artifactId,
      });
      setAttachNote("");
      void indexDocument(doc.id).catch(() => {
        // Best-effort: inline full-text still works even if background indexing fails.
      });
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
    void uploadDocumentAttachment(attachment);
  }
}
