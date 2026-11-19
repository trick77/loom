import { useCallback, useEffect, useRef, useState } from "react";

import {
  deleteArtifact,
  deleteDocument,
  indexDocument,
  type MessageAttachment,
  uploadDocument,
  uploadImageAttachment,
} from "../api";
import { isRevocablePreview } from "../components/AttachmentPreview";
import { acceptAttachmentFiles } from "./attachmentFiles";
import { formatFileSize } from "./artifacts";
import { UserFacingError } from "../api/http";
import i18n from "../i18n";

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

// withLandedUploads hands staged attachments back after a send stopped part-way
// through its upload flush. The flush writes upload ids onto the sent copies
// only; an attachment whose upload landed takes them over, so a resend skips
// it. The rest come back as staged, to be uploaded again.
export function withLandedUploads(
  staged: ComposerAttachment[],
  sent: ComposerAttachment[],
): ComposerAttachment[] {
  return staged.map((attachment) => {
    const copy = sent.find((item) => item.id === attachment.id);
    if (
      copy === undefined ||
      copy.status === "error" ||
      copy.artifactId === undefined
    ) {
      return attachment;
    }
    return {
      ...attachment,
      status: copy.status,
      artifactId: copy.artifactId,
      documentId: copy.documentId,
    };
  });
}

// deleteUploadedAttachment removes a composer-uploaded attachment server-side when
// it is taken off the composer, so it doesn't linger as an orphan ("as if it had
// never existed"). It is a no-op for re-attached existing artifacts — only what
// the composer itself uploaded (uploadedByComposer) is ours to delete. Best
// effort: a failed delete must not block the UI removal.
// inflightUploads holds the controller of every upload still running, keyed by
// attachment id. Removing a chip mid-upload aborts its request; an upload that
// completed anyway (the abort raced the response) is deleted server-side so no
// orphan document or artifact is left behind.
const inflightUploads = new Map<string, AbortController>();

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

// attachmentStatusLabel is a chip's status line, on the composer and on a sent
// message alike.
export function attachmentStatusLabel(attachment: ComposerAttachment): string {
  if (attachment.status === "queued") return i18n.t("composer.attached");
  if (attachment.status === "uploading") return i18n.t("composer.uploading");
  if (attachment.status === "processing") return i18n.t("composer.processing");
  if (attachment.status === "ready")
    return formatFileSize(attachment.sizeBytes);
  return attachment.error ?? i18n.t("composer.uploadFailed");
}

// useAttachmentIntake admits picked files next to the `staged` ones (see
// acceptAttachmentFiles). The count is mirrored in a ref, re-synced after every
// render and bumped as files are taken, so two drops in one render both see the
// other's files.
export function useAttachmentIntake(staged: number) {
  const countRef = useRef(staged);
  useEffect(() => {
    countRef.current = staged;
  }, [staged]);
  return useCallback((files: File[]) => {
    const intake = acceptAttachmentFiles(files, countRef.current);
    countRef.current += intake.accepted.length;
    return intake;
  }, []);
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
  const intakeFiles = useAttachmentIntake(attachments.length);

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
      inflightUploads.get(id)?.abort();
      inflightUploads.delete(id);
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

  // takeAttachments clears the scope's chips for a send, previews kept (the
  // sent bubble shows them), and returns a restore for chips the send did not
  // take: they go back to the scope they were taken from, ahead of any picked
  // since.
  const takeAttachments = useCallback(() => {
    const key = scopeKey;
    let taken: ComposerAttachment[] = [];
    setScopeAttachments(key, (current) => {
      taken = current;
      return [];
    });
    return () =>
      setScopeAttachments(key, (current) => [
        ...taken.filter((item) => !current.some((c) => c.id === item.id)),
        ...current,
      ]);
  }, [scopeKey, setScopeAttachments]);

  // The note is per-hook rather than per-scope, so drop it when the scope changes:
  // "Uploading report.pdf…" from the thread you just left has nothing to say about
  // the one you are looking at. A chip's own status carries the real state, and
  // survives the move.
  useEffect(() => {
    // eslint-disable-next-line react-hooks/set-state-in-effect -- a reset keyed on the scope changing
    setAttachNote("");
  }, [scopeKey]);

  const handleAttachFiles = useCallback(
    (files: File[], override?: { threadId?: string; projectId?: string }) => {
      const threadId = override?.threadId ?? scope.threadId;
      const projectId = override?.projectId ?? scope.projectId;
      const { accepted, note } = intakeFiles(files);
      if (note !== null) setAttachNote(note);
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
      scope.threadId,
      scope.projectId,
      scopeKey,
      intakeFiles,
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
    handleAttachError,
    handleAttachFiles,
    removeAttachment,
    takeAttachments,
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

  for (const attachment of attachments) {
    const file = attachment.file;
    if (file === undefined || attachment.artifactId !== undefined) continue;
    if (threadId === undefined && projectId === undefined) {
      setAttachNote(
        i18n.t("composer.uploadDeferred", { filename: attachment.filename }),
      );
      continue;
    }
    setAttachNote(
      i18n.t("composer.uploadingFile", { filename: attachment.filename }),
    );
    onStatus(attachment.id, { status: "uploading" });
    const controller = new AbortController();
    inflightUploads.set(attachment.id, controller);
    const options = { threadId, projectId, signal: controller.signal };
    // Awaited one by one, so every id is set before the caller collects the
    // attachment ids for the send: an image's artifact id is a model input, and
    // a document whose id misses the send is neither seen nor persisted.
    try {
      const ids = isImageAttachment(attachment)
        ? { artifactId: (await uploadImageAttachment(file, options)).id }
        : await uploadDocument(file, options).then((doc) => ({
            documentId: doc.id,
            artifactId: doc.artifactId,
          }));
      if (controller.signal.aborted) {
        // Removed while the request was in flight and the server finished
        // anyway: nothing on screen owns the upload any more.
        deleteUploadedAttachment({ ...attachment, ...ids });
        continue;
      }
      onStatus(attachment.id, { status: "ready", ...ids });
      setAttachNote("");
      // A document is usable inline as soon as it is uploaded (its full text goes
      // into the prompt on send), so the send does not wait for embedding; it is
      // indexed in the background for the large-document RAG fallback and project
      // retrieval. Best effort: inline full text works without it.
      if ("documentId" in ids) {
        void indexDocument(ids.documentId).catch(() => {});
      }
    } catch (error) {
      if (controller.signal.aborted) continue;
      const message =
        error instanceof UserFacingError
          ? error.message
          : i18n.t("errors.uploadFailed", { filename: attachment.filename });
      onStatus(attachment.id, { status: "error", error: message });
      setAttachNote(message);
    } finally {
      if (inflightUploads.get(attachment.id) === controller)
        inflightUploads.delete(attachment.id);
    }
  }
}
