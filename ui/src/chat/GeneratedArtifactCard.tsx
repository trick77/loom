import { useState } from "react";
import { useTranslation } from "react-i18next";

import { downloadArtifact, type Artifact } from "../api";
import { buildImageStats, fileTypeLabel, formatFileSize } from "./artifacts";
import { DownloadIcon } from "./icons";
import { Icon } from "./Icon";
import { ImageLightbox } from "./ImageLightbox";
import { PdfLightbox } from "./PdfLightbox";
import { isPdfAttachment } from "./useDocumentAttachments";
import { downloadBlob } from "./download";

export function GeneratedArtifactCard({ artifact }: { artifact: Artifact }) {
  const { t } = useTranslation();
  const [error, setError] = useState("");
  const [lightboxOpen, setLightboxOpen] = useState(false);
  const [pdfPreviewOpen, setPdfPreviewOpen] = useState(false);
  // A deleted artifact has no bytes on disk: render a tombstone (disabled
  // download + notice) and skip every fetch/preview path below.
  const deleted = artifact.deleted === true;
  const isImage = artifact.mimeType.startsWith("image/") && !deleted;
  // Non-deleted PDFs render a clickable card body that opens an inline preview.
  const isPdf =
    !deleted &&
    !isImage &&
    isPdfAttachment({
      mimeType: artifact.mimeType,
      filename: artifact.displayFilename,
    });
  const imageStats = isImage ? buildImageStats(artifact) : null;
  const typeLabel = fileTypeLabel(artifact.displayFilename);

  // The card shows the server's small JPEG thumbnail when there is one and the
  // full image otherwise; the lightbox always shows the full image. Both are
  // plain image loads on the session cookie (the artifact library does the
  // same), so a chat full of generated images no longer downloads every
  // original as a blob just to render its preview.
  const previewUrl = isImage
    ? (artifact.thumbnailUrl ?? artifact.downloadUrl)
    : "";

  async function handleDownload() {
    setError("");
    try {
      downloadBlob(
        await downloadArtifact(artifact.downloadUrl),
        artifact.displayFilename,
      );
    } catch {
      setError(t("artifactCard.downloadFailed"));
    }
  }

  function handleOpenPreview() {
    setError("");
    setLightboxOpen(true);
  }

  return (
    <div className="max-w-[28rem] overflow-hidden rounded-lg border border-[#3e3d39] bg-[#282826] text-[#f3f0e8]">
      {isImage &&
        // Reserve the image's vertical space up-front so the card never collapses while the
        // blob loads asynchronously (or when it remounts on stream -> committed). A collapse
        // would shrink scrollHeight and make the browser clamp scrollTop upward = unwanted
        // upward jump. With known dimensions we reserve the exact box via aspect-ratio;
        // otherwise we fall back to a min-height floor that bounds the collapse.
        (artifact.width && artifact.height ? (
          <button
            className="relative block max-h-[28rem] w-full cursor-zoom-in overflow-hidden bg-[#1f1f1d]"
            onClick={handleOpenPreview}
            type="button"
            title={t("artifactCard.preview", {
              filename: artifact.displayFilename,
            })}
            aria-label={t("artifactCard.preview", {
              filename: artifact.displayFilename,
            })}
            style={{ aspectRatio: `${artifact.width} / ${artifact.height}` }}
          >
            {previewUrl !== "" && (
              <img
                className="absolute inset-0 h-full w-full object-contain"
                src={previewUrl}
                alt={artifact.displayFilename}
                loading="lazy"
                onError={() => setError(t("artifactCard.previewFailed"))}
              />
            )}
          </button>
        ) : (
          <button
            className="block min-h-[16rem] w-full cursor-zoom-in bg-[#1f1f1d]"
            onClick={handleOpenPreview}
            type="button"
            title={t("artifactCard.preview", {
              filename: artifact.displayFilename,
            })}
            aria-label={t("artifactCard.preview", {
              filename: artifact.displayFilename,
            })}
          >
            {previewUrl !== "" && (
              <img
                className="block max-h-[28rem] w-full object-contain"
                src={previewUrl}
                alt={artifact.displayFilename}
                loading="lazy"
                onError={() => setError(t("artifactCard.previewFailed"))}
              />
            )}
          </button>
        ))}
      <div className="flex items-center gap-3 px-4 py-3">
        {/* For a PDF the icon+filename form a single button that opens the inline
            preview; the download button stays a separate sibling so keyboard
            activation of one never triggers the other. Other files render the
            same content as a plain (non-interactive) block. */}
        {isPdf ? (
          <button
            type="button"
            className="flex min-w-0 flex-1 cursor-pointer items-center gap-3 text-left"
            onClick={() => setPdfPreviewOpen(true)}
            title={t("artifactCard.preview", {
              filename: artifact.displayFilename,
            })}
            aria-label={t("artifactCard.preview", {
              filename: artifact.displayFilename,
            })}
          >
            <ArtifactCardIcon typeLabel={typeLabel} />
            <ArtifactCardInfo
              artifact={artifact}
              deleted={deleted}
              imageStats={imageStats}
              error={error}
            />
          </button>
        ) : (
          <>
            {!isImage && <ArtifactCardIcon typeLabel={typeLabel} />}
            <ArtifactCardInfo
              artifact={artifact}
              deleted={deleted}
              imageStats={imageStats}
              error={error}
            />
          </>
        )}
        {deleted ? (
          <span
            className="grid h-8 w-8 shrink-0 cursor-not-allowed place-items-center rounded-md bg-[#33332f] text-[#6f6d66]"
            title={t("artifactCard.fileDeleted")}
            aria-label={t("artifactCard.fileDeleted")}
            aria-disabled="true"
          >
            <DownloadIcon />
          </span>
        ) : (
          <button
            className="grid h-8 w-8 shrink-0 place-items-center rounded-md bg-[#3a3a37] text-[#c7c5bd] transition-colors hover:bg-[#454540] hover:text-[#f3f0e8]"
            onClick={handleDownload}
            type="button"
            title={t("artifactCard.download", {
              filename: artifact.displayFilename,
            })}
            aria-label={t("artifactCard.download", {
              filename: artifact.displayFilename,
            })}
          >
            <DownloadIcon />
          </button>
        )}
      </div>
      {lightboxOpen && (
        <ImageLightbox
          src={artifact.downloadUrl}
          alt={artifact.displayFilename}
          onClose={() => setLightboxOpen(false)}
        />
      )}
      {pdfPreviewOpen && (
        <PdfLightbox
          downloadUrl={artifact.downloadUrl}
          filename={artifact.displayFilename}
          onClose={() => setPdfPreviewOpen(false)}
        />
      )}
    </div>
  );
}

// The square type badge shown left of the filename for non-image artifacts:
// a short uppercase extension label (e.g. "PDF") or a generic glyph fallback.
function ArtifactCardIcon({ typeLabel }: { typeLabel: string | null }) {
  return (
    <div className="grid h-9 w-9 shrink-0 place-items-center rounded-md bg-[#3a3a37] text-[#c7c5bd]">
      {typeLabel ? (
        <span
          aria-hidden="true"
          className="text-[10px] font-semibold uppercase leading-none tracking-tight"
        >
          {typeLabel}
        </span>
      ) : (
        <Icon name="artifact" size="20px" />
      )}
    </div>
  );
}

// The filename + metadata column shared by the clickable (PDF) and plain card
// layouts, so the two paths can't drift.
function ArtifactCardInfo({
  artifact,
  deleted,
  imageStats,
  error,
}: {
  artifact: Artifact;
  deleted: boolean;
  imageStats: string | null;
  error: string;
}) {
  const { t } = useTranslation();
  return (
    <div className="min-w-0 flex-1">
      <div
        className={`ui-message-text truncate ${deleted ? "text-[#aaa79e] line-through" : ""}`}
      >
        {artifact.displayFilename}
      </div>
      <div className="ui-meta-text text-[#aaa79e]">
        {artifact.mimeType} · {formatFileSize(artifact.sizeBytes)}
      </div>
      {imageStats !== null && (
        <div className="font-mono text-xs text-[#88857d]">{imageStats}</div>
      )}
      {deleted && (
        <div className="ui-meta-text text-[#d09a73]">
          {t("artifactCard.fileWasDeleted")}
        </div>
      )}
      {error !== "" && (
        <div className="ui-meta-text text-[#d36f67]">{error}</div>
      )}
    </div>
  );
}
