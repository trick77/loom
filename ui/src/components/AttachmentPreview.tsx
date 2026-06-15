import { useEffect, useState } from "react";

import { AttachmentExtensionPill } from "../chat/AttachmentExtensionPill";
import { attachmentExtensionLabel } from "../chat/attachmentFiles";
import { FileIcon } from "../chat/icons";

function isImageLike(mimeType: string, filename: string): boolean {
  return mimeType.startsWith("image/") || /\.(png|jpe?g|webp|gif)$/i.test(filename);
}

// isRevocablePreview reports whether a preview URL is a local object URL that must
// be revoked to avoid a leak. Server download URLs (/api/artifacts/…) are stable
// and must NOT be revoked; only blob: URLs created via URL.createObjectURL are.
// Centralising the rule here keeps every owner of an attachment's lifecycle
// (composer hook, sent-message cleanup) from accidentally revoking a server URL.
export function isRevocablePreview(previewUrl: string | undefined): previewUrl is string {
  return previewUrl !== undefined && previewUrl.startsWith("blob:");
}

/**
 * AttachmentPreview is the single thumbnail / typed-icon box used everywhere an
 * attachment, knowledge document, or uploaded artifact is shown in a list, pill,
 * or card. For images with a preview URL it renders a cover-fit thumbnail (falling
 * back to the typed icon if the image fails to load); for everything else it
 * renders a consistent file-type marker — an extension pill when the extension is
 * recognised, otherwise a generic file glyph. The caller owns the box size and
 * chrome via `className`.
 *
 * It deliberately does NOT create or revoke object URLs: whoever owns the
 * attachment's lifecycle (the composer hook, or the sent-message cleanup) does
 * that, using `isRevocablePreview` to tell a revocable blob from a server URL.
 */
export function AttachmentPreview({
  mimeType,
  filename,
  previewUrl,
  alt,
  className,
  fallbackBoxClassName,
  overlayLabel,
  testId,
}: {
  mimeType: string;
  filename: string;
  previewUrl?: string;
  // When set, the thumbnail is given this accessible name; when omitted the image
  // is treated as decorative (the filename is shown as adjacent text), so it is
  // hidden from assistive tech to avoid a redundant announcement.
  alt?: string;
  className?: string;
  // Optional chrome for the non-image marker: larger cells (composer pill, sent
  // file card) wrap the extension pill in a small bordered chip; compact rows
  // leave it unset so the glyph sits bare in the cell.
  fallbackBoxClassName?: string;
  // When true, an image thumbnail carries its extension as a small pill badge
  // overlaid inside the image (used wherever an uploaded image is shown without
  // an adjacent filename — composer image chip and sent-message thumbnail — so the
  // two read identically end to end).
  overlayLabel?: boolean;
  testId?: string;
}) {
  const [broken, setBroken] = useState(false);
  // Reset the broken flag when the source changes so a once-failed image can
  // recover if the same component instance is handed a fresh URL (e.g. an upload
  // swaps a revoked blob: URL for the stable server download URL under the same
  // attachment id, where a stable list key keeps the instance mounted).
  useEffect(() => {
    setBroken(false);
  }, [previewUrl]);
  const extensionLabel = attachmentExtensionLabel(filename);
  const showImage = previewUrl !== undefined && isImageLike(mimeType, filename) && !broken;
  return (
    <div className={`relative ${className ?? ""}`} data-testid={testId}>
      {showImage ? (
        <>
          <img
            className="h-full w-full object-cover"
            src={previewUrl}
            alt={alt ?? ""}
            aria-hidden={alt === undefined ? "true" : undefined}
            loading="lazy"
            onError={() => setBroken(true)}
          />
          {overlayLabel && extensionLabel !== null && (
            <span className="absolute bottom-1 left-1 inline-flex items-center rounded bg-black/55 px-1.5 py-1 leading-none backdrop-blur-sm">
              <AttachmentExtensionPill>{extensionLabel}</AttachmentExtensionPill>
            </span>
          )}
        </>
      ) : (
        <span className="grid h-full w-full place-items-center text-[#c9c5bb]">
          <span className={fallbackBoxClassName}>
            {extensionLabel !== null ? <AttachmentExtensionPill>{extensionLabel}</AttachmentExtensionPill> : <FileIcon />}
          </span>
        </span>
      )}
    </div>
  );
}
