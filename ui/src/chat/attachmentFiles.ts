import { ATTACHMENT_ACCEPT, DOCUMENT_MAX_UPLOAD_BYTES } from "../api";

const ACCEPTED_EXTENSIONS = ATTACHMENT_ACCEPT.split(",").map((ext) => ext.trim().toLowerCase());
const ACCEPTED_EXTENSION_LABELS = new Map(
  ACCEPTED_EXTENSIONS.map((ext) => {
    const clean = ext.replace(/^\./, "");
    return [clean, clean === "jpeg" ? "JPG" : clean.toUpperCase()];
  }),
);
const SUPPORTED_FILE_TYPES = "PDF, DOCX, PPTX, XLSX, TXT, MD, CSV, JSON, HTML, PNG, JPG, WEBP, or GIF";

export const UNSUPPORTED_FILE_MESSAGE = `Unsupported file type. Use ${SUPPORTED_FILE_TYPES}.`;

export function filterAcceptedFiles(files: File[]): File[] {
  return files.filter((file) => {
    const name = file.name.toLowerCase();
    return ACCEPTED_EXTENSIONS.some((ext) => name.endsWith(ext));
  });
}

export function isWithinUploadSizeLimit(file: File): boolean {
  return typeof file.size !== "number" || file.size <= DOCUMENT_MAX_UPLOAD_BYTES;
}

export function attachmentExtensionLabel(filename: string): string | null {
  const match = /\.([^.]+)$/.exec(filename.trim().toLowerCase());
  if (match === null) return null;
  return ACCEPTED_EXTENSION_LABELS.get(match[1]) ?? null;
}

// formatAttachmentSize renders a byte count as a compact, human-readable size.
// Shared by every attachment surface (composer pill, sent file card) so the size
// label reads identically everywhere.
export function formatAttachmentSize(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`;
  const kb = bytes / 1024;
  if (kb < 1024) return `${kb.toFixed(kb >= 10 ? 0 : 1)} KB`;
  const mb = kb / 1024;
  return `${mb.toFixed(mb >= 10 ? 0 : 1)} MB`;
}

export function attachAcceptedFiles({
  files,
  onAttachFiles,
  onAttachError,
}: {
  files: File[];
  onAttachFiles?(files: File[]): void;
  onAttachError?(message: string): void;
}) {
  const accepted = filterAcceptedFiles(files);
  if (accepted.length > 0) onAttachFiles?.(accepted);
  if (accepted.length < files.length) onAttachError?.(UNSUPPORTED_FILE_MESSAGE);
}

export function isFileDrag(event: DragEvent | { dataTransfer: DataTransfer | null }): boolean {
  return Array.from(event.dataTransfer?.types ?? []).includes("Files");
}
