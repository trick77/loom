import { useCallback, useEffect, useRef, useState } from "react";

import {
  DOCUMENT_ACCEPT,
  deleteDocument,
  indexDocument,
  listDocuments,
  uploadDocument,
  type Document,
} from "../api";
import { formatFileSize } from "../chat/artifacts";
import { Icon } from "../chat/Icon";
import type { IconName } from "../chat/Icon";
import { AttachmentPreview } from "../components/AttachmentPreview";

// A document is still settling (badge should show "Indexing…" and the panel keeps
// polling) while it is anything other than a terminal state.
const TRANSIENT_STATUSES: ReadonlySet<Document["status"]> = new Set([
  "pending",
  "extracting",
  "embedding",
]);

type Badge = { label: string; icon: IconName; className: string };

function statusBadge(doc: Document): Badge {
  switch (doc.status) {
    case "embedded":
      return { label: "Ready", icon: "checkCircle", className: "text-[#8fbf7f]" };
    case "error":
      return { label: "Error", icon: "alertCircle", className: "text-accent" };
    case "stale":
      return { label: "Stale", icon: "warning", className: "text-[#c9a227]" };
    default:
      return { label: "Indexing…", icon: "spinner", className: "text-[#8f8b82]" };
  }
}

/**
 * ProjectKnowledgePanel lists the documents added to a project's knowledge and
 * lets the user upload, re-index, and remove them. It mirrors AnythingLLM's
 * workspace documents: uploaded files are extracted and embedded so every chat in
 * the project can retrieve them. It sits beside the auto-generated memory panel
 * but is user-owned content, not a generated digest.
 */
export function ProjectKnowledgePanel({ projectId }: { projectId: string }) {
  const [docs, setDocs] = useState<Document[]>([]);
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [hoveredID, setHoveredID] = useState<string | null>(null);
  const [dragging, setDragging] = useState(false);
  const fileInputRef = useRef<HTMLInputElement>(null);

  const refresh = useCallback(async () => {
    try {
      const items = await listDocuments(projectId);
      setDocs(items);
    } catch {
      // Best-effort: a transient list failure leaves the previous view in place.
    } finally {
      setLoading(false);
    }
  }, [projectId]);

  useEffect(() => {
    setLoading(true);
    void refresh();
  }, [refresh]);

  // Poll while any document is still extracting/embedding so the badge advances
  // to "Ready" without a manual reload; stop once everything has settled.
  useEffect(() => {
    if (!docs.some((d) => TRANSIENT_STATUSES.has(d.status))) return;
    const timer = setInterval(() => void refresh(), 2000);
    return () => clearInterval(timer);
  }, [docs, refresh]);

  const handleFiles = async (files: FileList | null) => {
    if (files === null || files.length === 0) return;
    setBusy(true);
    setError("");
    try {
      for (const file of Array.from(files)) {
        const doc = await uploadDocument(file, { projectId });
        void indexDocument(doc.id).catch(() => undefined);
      }
      await refresh();
    } catch (err) {
      setError(err instanceof Error ? err.message : "Upload failed.");
    } finally {
      setBusy(false);
      if (fileInputRef.current !== null) fileInputRef.current.value = "";
    }
  };

  const handleDelete = async (doc: Document) => {
    setError("");
    try {
      await deleteDocument(doc.id);
      setDocs((current) => current.filter((d) => d.id !== doc.id));
    } catch (err) {
      setError(err instanceof Error ? err.message : `Could not remove ${doc.filename}.`);
    }
  };

  const handleReindex = async (doc: Document) => {
    setError("");
    try {
      await indexDocument(doc.id);
      await refresh();
    } catch (err) {
      setError(err instanceof Error ? err.message : `Could not re-index ${doc.filename}.`);
    }
  };

  // The dropzone doubles as the upload control: drag-and-drop on desktop, tap to
  // open the file picker on touch devices (where drag-and-drop is unavailable).
  const openPicker = () => {
    if (!busy) fileInputRef.current?.click();
  };
  const handleDrop = (event: React.DragEvent) => {
    event.preventDefault();
    setDragging(false);
    void handleFiles(event.dataTransfer.files);
  };
  const handleDragOver = (event: React.DragEvent) => {
    event.preventDefault();
    if (!dragging) setDragging(true);
  };

  return (
    <section
      aria-label="Knowledge"
      className={`overflow-hidden rounded-2xl border bg-[#1f1f1d] transition-colors ${
        dragging ? "border-accent" : "border-[#343432]"
      }`}
      onDragOver={handleDragOver}
      onDragLeave={() => setDragging(false)}
      onDrop={handleDrop}
    >
      <div className="flex items-center gap-1.5 px-5 pt-5">
        <h2 className="flex items-center gap-1.5 text-[15px] font-medium text-[#ecece6]">
          <Icon name="artifact" size="21px" className="text-[#d5d2c9]" />
          <span>Knowledge</span>
        </h2>
        {busy && <Icon name="spinner" size="14px" className="text-[#8f8b82]" />}
        <input
          ref={fileInputRef}
          type="file"
          multiple
          accept={DOCUMENT_ACCEPT}
          className="hidden"
          onChange={(event) => void handleFiles(event.target.files)}
        />
      </div>

      <p className="mt-1.5 px-5 text-[13px] leading-5 text-[#8a887f]">
        Upload documents so every chat in this project can search and cite them.
      </p>

      {error !== "" && (
        <p className="mt-2 px-5 text-[13px] text-accent" role="alert">
          {error}
        </p>
      )}

      {loading ? (
        <p className="mt-3 px-5 pb-5 text-sm text-[#8f8b82]">Loading…</p>
      ) : docs.length === 0 ? (
        <div className="px-5 pb-5 pt-3">
          <button
            type="button"
            onClick={openPicker}
            aria-label="Add documents to knowledge"
            className={`flex w-full flex-col items-center gap-1 rounded-xl border border-dashed px-4 py-7 text-center transition-colors ${
              dragging ? "border-accent bg-[#2a2a28]" : "border-[#3f3f3c] hover:bg-[#2a2a28]"
            }`}
          >
            <Icon name="upload" size="21px" className="text-[#d5d2c9]" />
            <span className="text-sm leading-5 text-[#c7c5bd]">Drag documents here or tap to upload</span>
          </button>
        </div>
      ) : (
        <ul className="mt-2 max-h-[420px] overflow-y-auto px-2 pb-2">
          {docs.map((doc) => {
            const badge = statusBadge(doc);
            const hovered = hoveredID === doc.id;
            return (
              <li
                key={doc.id}
                className="group flex items-center gap-3 rounded-xl px-3 py-2 hover:bg-[#2a2a28]"
                onPointerEnter={() => setHoveredID(doc.id)}
                onPointerLeave={() => setHoveredID((current) => (current === doc.id ? null : current))}
              >
                <AttachmentPreview
                  mimeType={doc.mimeType}
                  filename={doc.filename}
                  className="grid h-9 w-9 shrink-0 place-items-center overflow-hidden rounded-md bg-[#3a3a37] text-[#c7c5bd]"
                />
                <div className="min-w-0 flex-1">
                  <div className="truncate text-[15px] leading-5 text-[#ecece6]" title={doc.filename}>
                    {doc.filename}
                  </div>
                  <div className="flex items-center gap-1.5 text-[13px] leading-5 text-[#8a887f]">
                    <span>{formatFileSize(doc.sizeBytes)}</span>
                    <span>·</span>
                    <span className={`flex items-center gap-1 ${badge.className}`}>
                      <Icon name={badge.icon} size="13px" />
                      {badge.label}
                    </span>
                  </div>
                </div>
                <div
                  className={`flex shrink-0 items-center gap-0.5 ${hovered ? "opacity-100" : "opacity-0"} transition-opacity`}
                >
                  {(doc.status === "error" || doc.status === "stale") && (
                    <button
                      type="button"
                      aria-label={`Re-index ${doc.filename}`}
                      className="grid h-7 w-7 place-items-center rounded-md text-[#d5d2c9] hover:bg-[#343432]"
                      onClick={() => void handleReindex(doc)}
                    >
                      <Icon name="retry" size="14px" />
                    </button>
                  )}
                  <button
                    type="button"
                    aria-label={`Remove ${doc.filename}`}
                    className="grid h-7 w-7 place-items-center rounded-md text-[#d5d2c9] hover:bg-[#343432]"
                    onClick={() => void handleDelete(doc)}
                  >
                    <Icon name="trash" size="14px" />
                  </button>
                </div>
              </li>
            );
          })}
        </ul>
      )}

      {!loading && docs.length > 0 && (
        <div className="px-2 pb-3">
          <button
            type="button"
            onClick={openPicker}
            aria-label="Add documents to knowledge"
            className={`flex w-full items-center justify-center gap-1.5 rounded-xl border border-dashed px-3 py-2.5 text-[13px] transition-colors ${
              dragging
                ? "border-accent bg-[#2a2a28] text-[#c7c5bd]"
                : "border-[#3f3f3c] text-[#807d74] hover:bg-[#2a2a28] hover:text-[#c7c5bd]"
            }`}
          >
            <Icon name="upload" size="18px" />
            <span>Drag here or tap to add</span>
          </button>
        </div>
      )}
    </section>
  );
}
