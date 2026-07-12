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

// The image MIME types the app accepts, mapped to the extension the attachment
// pipeline expects. Kept in sync with the image entries of ATTACHMENT_ACCEPT.
const CLIPBOARD_IMAGE_EXTENSIONS: Record<string, string> = {
  "image/png": ".png",
  "image/jpeg": ".jpg",
  "image/webp": ".webp",
  "image/gif": ".gif",
};

// toSupportedImageFile normalizes a clipboard image into a File the attachment
// pipeline accepts. Pasted screenshots arrive as File objects whose name is often
// generic ("image.png") or, in some browsers, empty — and filterAcceptedFiles keeps
// a file only if its name ends in a supported extension. So we synthesize a filename
// from the MIME type unless the original already carries a supported extension.
// Returns null for a MIME type we don't support, so callers can skip it.
export function toSupportedImageFile(file: File): File | null {
  const ext = CLIPBOARD_IMAGE_EXTENSIONS[file.type];
  if (ext === undefined) return null;
  const name = file.name.toLowerCase();
  if (ACCEPTED_EXTENSIONS.some((accepted) => name.endsWith(accepted))) return file;
  return new File([file], `pasted-image${ext}`, { type: file.type });
}

// clipboardImageFiles extracts image files from a paste's clipboard data. It reads
// clipboardData.files first (the modern surface), falling back to iterating .items
// for browsers that only expose images there.
export function clipboardImageFiles(data: DataTransfer): File[] {
  const fromFiles = Array.from(data.files ?? []).filter((file) => file.type.startsWith("image/"));
  if (fromFiles.length > 0) return fromFiles;
  const fromItems: File[] = [];
  for (const item of Array.from(data.items ?? [])) {
    if (item.kind !== "file" || !item.type.startsWith("image/")) continue;
    const file = item.getAsFile();
    if (file !== null) fromItems.push(file);
  }
  return fromItems;
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
