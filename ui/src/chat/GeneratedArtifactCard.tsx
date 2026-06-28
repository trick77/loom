import { useEffect, useState } from "react";

import { downloadArtifact, type Artifact } from "../api";
import { buildImageStats, fileTypeLabel, formatFileSize } from "./artifacts";
import { DownloadIcon } from "./icons";
import { Icon } from "./Icon";
import { ImageLightbox } from "./ImageLightbox";

export function GeneratedArtifactCard({ artifact }: { artifact: Artifact }) {
  const [error, setError] = useState("");
  const [previewUrl, setPreviewUrl] = useState("");
  const [lightboxOpen, setLightboxOpen] = useState(false);
  // A deleted artifact has no bytes on disk: render a tombstone (disabled
  // download + notice) and skip every fetch/preview path below.
  const deleted = artifact.deleted === true;
  const isImage = artifact.mimeType.startsWith("image/") && !deleted;
  const imageStats = isImage ? buildImageStats(artifact) : null;
  const typeLabel = fileTypeLabel(artifact.displayFilename);

  useEffect(() => {
    if (!isImage) {
      setPreviewUrl("");
      return;
    }
    let cancelled = false;
    let objectUrl = "";
    setError("");
    setPreviewUrl("");
    void downloadArtifact(artifact.downloadUrl)
      .then((blob) => {
        if (cancelled) return;
        objectUrl = URL.createObjectURL(blob);
        setPreviewUrl(objectUrl);
      })
      .catch(() => {
        if (!cancelled) setError("Preview failed");
      });
    return () => {
      cancelled = true;
      if (objectUrl !== "") URL.revokeObjectURL(objectUrl);
    };
  }, [artifact.downloadUrl, isImage]);

  async function handleDownload() {
    setError("");
    try {
      const blob = await downloadArtifact(artifact.downloadUrl);
      const url = URL.createObjectURL(blob);
      const anchor = document.createElement("a");
      anchor.href = url;
      anchor.download = artifact.displayFilename;
      document.body.append(anchor);
      anchor.click();
      anchor.remove();
      URL.revokeObjectURL(url);
    } catch {
      setError("Download failed");
    }
  }

  function handleOpenPreview() {
    if (previewUrl === "") return;
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
            title={`Preview ${artifact.displayFilename}`}
            aria-label={`Preview ${artifact.displayFilename}`}
            style={{ aspectRatio: `${artifact.width} / ${artifact.height}` }}
          >
            {previewUrl !== "" && (
              <img
                className="absolute inset-0 h-full w-full object-contain"
                src={previewUrl}
                alt={artifact.displayFilename}
                loading="lazy"
              />
            )}
          </button>
        ) : (
          <button
            className="block min-h-[16rem] w-full cursor-zoom-in bg-[#1f1f1d]"
            onClick={handleOpenPreview}
            type="button"
            title={`Preview ${artifact.displayFilename}`}
            aria-label={`Preview ${artifact.displayFilename}`}
          >
            {previewUrl !== "" && (
              <img
                className="block max-h-[28rem] w-full object-contain"
                src={previewUrl}
                alt={artifact.displayFilename}
                loading="lazy"
              />
            )}
          </button>
        ))}
      <div className="flex items-center gap-3 px-4 py-3">
        {!isImage && (
          <div className="grid h-9 w-9 shrink-0 place-items-center rounded-md bg-[#3a3a37] text-[#c7c5bd]">
            {typeLabel ? (
              <span className="text-[10px] font-semibold uppercase leading-none tracking-tight">{typeLabel}</span>
            ) : (
              <Icon name="artifact" size="20px" />
            )}
          </div>
        )}
        <div className="min-w-0 flex-1">
          <div className={`ui-message-text truncate ${deleted ? "text-[#aaa79e] line-through" : ""}`}>
            {artifact.displayFilename}
          </div>
          <div className="ui-meta-text text-[#aaa79e]">
            {artifact.mimeType} · {formatFileSize(artifact.sizeBytes)}
          </div>
          {imageStats !== null && <div className="font-mono text-xs text-[#88857d]">{imageStats}</div>}
          {deleted && <div className="ui-meta-text text-[#d09a73]">This file was deleted</div>}
          {error !== "" && <div className="ui-meta-text text-[#d36f67]">{error}</div>}
        </div>
        {deleted ? (
          <span
            className="grid h-8 w-8 shrink-0 cursor-not-allowed place-items-center rounded-md bg-[#33332f] text-[#6f6d66]"
            title="File was deleted"
            aria-label="File was deleted"
            aria-disabled="true"
          >
            <DownloadIcon />
          </span>
        ) : (
          <button
            className="grid h-8 w-8 shrink-0 place-items-center rounded-md bg-[#3a3a37] text-[#c7c5bd] transition-colors hover:bg-[#454540] hover:text-[#f3f0e8]"
            onClick={handleDownload}
            type="button"
            title={`Download ${artifact.displayFilename}`}
            aria-label={`Download ${artifact.displayFilename}`}
          >
            <DownloadIcon />
          </button>
        )}
      </div>
      {lightboxOpen && previewUrl !== "" && (
        <ImageLightbox
          src={previewUrl}
          alt={artifact.displayFilename}
          onClose={() => setLightboxOpen(false)}
        />
      )}
    </div>
  );
}
