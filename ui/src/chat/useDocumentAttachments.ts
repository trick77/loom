import { useCallback, useEffect, useState } from "react";

import {
  deleteArtifact,
  deleteDocument,
  DOCUMENT_MAX_ATTACHMENTS_PER_MESSAGE,
  indexDocument,
  type MessageAttachment,
  uploadDocument,
  uploadImageAttachment,
} from "../api";
import { isRevocablePreview } from "../components/AttachmentPreview";
import { isWithinUploadSizeLimit } from "./attachmentFiles";

export type ComposerAttachmentStatus =
  "queued" | "uploading" | "processing" | "ready" | "error";

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
  // True only for attachments the composer itself uploaded from a picked file.
  // Gates the delete-on-remove path: removing such an attachment also deletes it
  // server-side, but a re-attached existing artifact (e.g. a generated image via
  // composerAttachmentFromArtifact) must never be deleted — it isn't ours to drop.
  uploadedByComposer?: boolean;
};

let nextAttachmentID = 0;

export function createComposerAttachment(
  file: File,
  status: ComposerAttachmentStatus = "uploading",
): ComposerAttachment {
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
    uploadedByComposer: true,
  };
}

export function toSentAttachment(
  attachment: ComposerAttachment,
): ComposerAttachment {
  const { file: _file, ...sent } = attachment;
  return sent;
}

// deleteUploadedAttachment removes a composer-uploaded attachment server-side when
// it is taken off the composer, so it doesn't linger as an orphan ("as if it had
// never existed"). It is a no-op for re-attached existing artifacts — only what
// the composer itself uploaded (uploadedByComposer) is ours to delete. Best
// effort: a failed delete must not block the UI removal.
export function deleteUploadedAttachment(attachment: ComposerAttachment): void {
  if (attachment.uploadedByComposer !== true) return;
  if (attachment.documentId !== undefined) {
    void deleteDocument(attachment.documentId).catch(() => {});
  } else if (attachment.artifactId !== undefined) {
    void deleteArtifact(attachment.artifactId).catch(() => {});
  }
}

// composerAttachmentFromArtifact turns an existing (already-persisted) artifact —
// e.g. an assistant-generated image — into a ready composer attachment so it can be
// re-sent as a model image input. It carries no File: the artifact already lives on
// the server, so the upload step is skipped and only its id is wired through as an
// imageAttachmentId. The download URL doubles as the preview thumbnail source.
export function composerAttachmentFromArtifact(artifact: {
  id: string;
  displayFilename: string;
  mimeType: string;
  sizeBytes: number;
  downloadUrl: string;
  thumbnailUrl?: string;
}): ComposerAttachment {
  nextAttachmentID += 1;
  return {
    id: `attachment-${Date.now()}-${nextAttachmentID}`,
    filename: artifact.displayFilename,
    mimeType: artifact.mimeType,
    sizeBytes: artifact.sizeBytes,
    status: "ready",
    previewUrl: artifact.thumbnailUrl ?? artifact.downloadUrl,
    artifactId: artifact.id,
  };
}

// composerAttachmentFromMessageAttachment rehydrates a persisted sent attachment
// (from a message loaded on reload) into the ready composer-attachment shape the
// sent-message renderer expects, so a reloaded message's previews look identical
// to a freshly sent one. It carries no File and is already "ready"; the artifact
// download URL doubles as the image thumbnail source (documents have none yet).
// The id is the stable artifact/document id so it is a stable React key.
export function composerAttachmentFromMessageAttachment(
  attachment: MessageAttachment,
): ComposerAttachment {
  const id =
    attachment.artifactId ?? attachment.documentId ?? attachment.filename;
  return {
    id: `sent-${id}`,
    filename: attachment.filename,
    mimeType: attachment.mimeType,
    sizeBytes: attachment.sizeBytes,
    status: "ready",
    previewUrl: attachment.thumbnailUrl ?? attachment.downloadUrl,
    documentId: attachment.documentId,
    artifactId: attachment.artifactId,
  };
}

export function isImageAttachment(
  attachment: Pick<ComposerAttachment, "mimeType" | "filename">,
): boolean {
  return (
    attachment.mimeType.startsWith("image/") ||
    /\.(png|jpe?g|webp|gif|svg)$/i.test(attachment.filename)
  );
}

// PDFs get an inline preview modal instead of a download-on-click; gate on either
// the MIME type or a .pdf filename, mirroring isImageAttachment's dual signal.
export function isPdfAttachment(
  attachment: Pick<ComposerAttachment, "mimeType" | "filename">,
): boolean {
  return (
    attachment.mimeType === "application/pdf" ||
    /\.pdf$/i.test(attachment.filename)
  );
}

type AttachmentStatusHandler = (
  id: string,
  patch: Partial<ComposerAttachment>,
) => void;

// The bucket for a hook with neither a thread nor a project — the deferred
// start-screen flush, which only ever uploads and never stages.
const GLOBAL_ATTACHMENT_SCOPE = "global";
// Shared identity for "nothing staged here", so a scope with no files does not
// hand out a fresh array on every render.
const NO_ATTACHMENTS: ComposerAttachment[] = Object.freeze(
  [],
) as unknown as ComposerAttachment[];

// Shared "+" composer attachment flow: upload a picked file, add it to knowledge,
// and surface ingestion progress via attachNote. Scope decides where the document
// lands for retrieval: a projectId scopes it to a project; a project-less upload
// with a threadId is private to that one thread; without either it is user-global.
// The scope can be overridden per call (used by the new-thread deferred upload,
// which only knows the freshly created thread id at send time).
export function useDocumentAttachments(scope: {
  threadId?: string;
  projectId?: string;
}) {
  const [attachNote, setAttachNote] = useState("");
  // Staged files are held per scope, not in one list, for the same reason drafts
  // are (see composerDrafts.ts): the hosting panel is not remounted when you move
  // between threads or projects, so a single list would follow you and bind to
  // whatever you send next. Keying by thread first means ThreadPanel's scope is
  // stable even though it passes a `projectId` that resolves a beat after mount.
  const scopeKey =
    scope.threadId !== undefined
      ? `thread:${scope.threadId}`
      : scope.projectId !== undefined
        ? `project:${scope.projectId}`
        : GLOBAL_ATTACHMENT_SCOPE;
  const [attachmentsByScope, setAttachmentsByScope] = useState<
    Record<string, ComposerAttachment[]>
  >({});
  const attachments = attachmentsByScope[scopeKey] ?? NO_ATTACHMENTS;

  const setScopeAttachments = useCallback(
    (
      key: string,
      next: (current: ComposerAttachment[]) => ComposerAttachment[],
    ) => {
      setAttachmentsByScope((current) => {
        const updated = next(current[key] ?? NO_ATTACHMENTS);
        // Prune rather than keep an empty list, so moving through threads does not
        // leave an entry behind for every one visited.
        if (updated.length === 0) {
          if (!(key in current)) return current;
          const pruned = { ...current };
          delete pruned[key];
          return pruned;
        }
        return { ...current, [key]: updated };
      });
    },
    [],
  );

  // Deliberately searches every scope rather than the current one: an upload
  // started in thread A must still mark A's chip ready if it lands after the user
  // has moved to thread B. Attachment ids are unique per pick, so this cannot hit
  // the wrong chip.
  const updateAttachment = useCallback(
    (id: string, patch: Partial<ComposerAttachment>) => {
      setAttachmentsByScope((current) => {
        let changed = false;
        const next: Record<string, ComposerAttachment[]> = {};
        for (const [key, list] of Object.entries(current)) {
          if (!list.some((attachment) => attachment.id === id)) {
            next[key] = list;
            continue;
          }
          changed = true;
          next[key] = list.map((attachment) =>
            attachment.id === id ? { ...attachment, ...patch } : attachment,
          );
        }
        return changed ? next : current;
      });
    },
    [],
  );

  const removeAttachment = useCallback(
    (id: string) => {
      const removed = attachments.find((attachment) => attachment.id === id);
      if (isRevocablePreview(removed?.previewUrl))
        URL.revokeObjectURL(removed.previewUrl);
      // Delete it server-side too (only if the composer uploaded it), so removing
      // it leaves no orphan; done outside the state updater to avoid a double
      // request under React StrictMode's double-invoked updaters.
      if (removed !== undefined) deleteUploadedAttachment(removed);
      setScopeAttachments(scopeKey, (current) =>
        current.filter((attachment) => attachment.id !== id),
      );
    },
    [attachments, scopeKey, setScopeAttachments],
  );

  const clearAttachments = useCallback(
    (options: { revokePreviewUrls?: boolean } = {}) => {
      const revokePreviewUrls = options.revokePreviewUrls ?? true;
      setScopeAttachments(scopeKey, (current) => {
        if (revokePreviewUrls) {
          current.forEach((attachment) => {
            if (isRevocablePreview(attachment.previewUrl))
              URL.revokeObjectURL(attachment.previewUrl);
          });
        }
        return [];
      });
    },
    [scopeKey, setScopeAttachments],
  );

  // The note is per-hook rather than per-scope, so drop it when the scope changes:
  // "Uploading report.pdf…" from the thread you just left has nothing to say about
  // the one you are looking at. A chip's own status carries the real state, and
  // survives the move.
  useEffect(() => {
    setAttachNote("");
  }, [scopeKey]);

  const handleAttachFiles = useCallback(
    (files: File[], override?: { threadId?: string; projectId?: string }) => {
      const threadId = override?.threadId ?? scope.threadId;
      const projectId = override?.projectId ?? scope.projectId;
      const sizeFiltered = files.filter(isWithinUploadSizeLimit);
      if (sizeFiltered.length < files.length) {
        setAttachNote("Files must be 25 MB or smaller.");
      }
      const remaining =
        DOCUMENT_MAX_ATTACHMENTS_PER_MESSAGE - attachments.length;
      if (remaining <= 0) {
        setAttachNote(
          `You can attach up to ${DOCUMENT_MAX_ATTACHMENTS_PER_MESSAGE} files per message.`,
        );
        return;
      }
      const accepted = sizeFiltered.slice(0, remaining);
      if (accepted.length < sizeFiltered.length) {
        setAttachNote(
          `You can attach up to ${DOCUMENT_MAX_ATTACHMENTS_PER_MESSAGE} files per message.`,
        );
      }
      if (accepted.length === 0) return;
      const pending = accepted.map((file) =>
        createComposerAttachment(
          file,
          threadId === undefined && projectId === undefined
            ? "queued"
            : "uploading",
        ),
      );
      setScopeAttachments(scopeKey, (current) => [...current, ...pending]);
      if (threadId !== undefined || projectId !== undefined) {
        void uploadAttachments(
          pending,
          { threadId, projectId },
          updateAttachment,
          setAttachNote,
        );
      }
    },
    [
      attachments.length,
      scope.threadId,
      scope.projectId,
      scopeKey,
      setScopeAttachments,
      updateAttachment,
    ],
  );

  const uploadExistingAttachments = useCallback(
    (
      existingAttachments: ComposerAttachment[],
      override: { threadId?: string; projectId?: string },
      onStatus: AttachmentStatusHandler,
    ) => {
      return uploadAttachments(
        existingAttachments,
        override,
        onStatus,
        setAttachNote,
      );
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
      const doc = await uploadDocument(attachment.file, {
        threadId,
        projectId,
      });
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
      const message =
        error instanceof Error
          ? error.message
          : `Failed to upload ${attachment.filename}.`;
      onStatus(attachment.id, { status: "error", error: message });
      setAttachNote(message);
    }
  };

  for (const attachment of attachments) {
    if (attachment.file === undefined || attachment.artifactId !== undefined)
      continue;
    if (threadId === undefined && projectId === undefined) {
      setAttachNote(`${attachment.filename} will upload when you send.`);
      continue;
    }
    if (isImageAttachment(attachment)) {
      setAttachNote(`Uploading ${attachment.filename}…`);
      onStatus(attachment.id, { status: "uploading" });
      try {
        const image = await uploadImageAttachment(attachment.file, {
          threadId,
          projectId,
        });
        onStatus(attachment.id, { status: "ready", artifactId: image.id });
        setAttachNote("");
      } catch (error) {
        const message =
          error instanceof Error
            ? error.message
            : `Failed to upload ${attachment.filename}.`;
        onStatus(attachment.id, { status: "error", error: message });
        setAttachNote(message);
      }
      continue;
    }
    // Await the upload (not the background indexing) so the document's id is set
    // before the caller collects documentAttachmentIds on send — otherwise a
    // document attached on a new thread is uploaded fire-and-forget and its id
    // misses the send, so the model never sees it and it isn't persisted. Mirrors
    // the awaited image path above; indexDocument still runs in the background.
    await uploadDocumentAttachment(attachment);
  }
}
