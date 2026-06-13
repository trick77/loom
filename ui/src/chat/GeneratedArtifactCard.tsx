import { useEffect, useState } from "react";

import { downloadArtifact, type Artifact } from "../api";
import { buildImageStats, formatFileSize } from "./artifacts";
import { CloseIcon, DownloadIcon } from "./icons";
import { Icon } from "./Icon";

export function GeneratedArtifactCard({ artifact }: { artifact: Artifact }) {
  const [error, setError] = useState("");
  const [previewUrl, setPreviewUrl] = useState("");
  const [lightboxOpen, setLightboxOpen] = useState(false);
  const isImage = artifact.mimeType.startsWith("image/");
  const imageStats = isImage ? buildImageStats(artifact) : null;

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

  useEffect(() => {
    if (!lightboxOpen) return;
    function onKeyDown(event: KeyboardEvent) {
      if (event.key === "Escape") setLightboxOpen(false);
    }
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, [lightboxOpen]);

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
            <Icon name="artifact" size="20px" />
          </div>
        )}
        <div className="min-w-0 flex-1">
          <div className="ui-message-text truncate">{artifact.displayFilename}</div>
          <div className="ui-meta-text text-[#aaa79e]">
            {artifact.mimeType} · {formatFileSize(artifact.sizeBytes)}
          </div>
          {imageStats !== null && <div className="font-mono text-xs text-[#88857d]">{imageStats}</div>}
          {error !== "" && <div className="ui-meta-text text-[#d36f67]">{error}</div>}
        </div>
        <button
          className="grid h-8 w-8 shrink-0 place-items-center rounded-md bg-[#3a3a37] text-[#c7c5bd] transition-colors hover:bg-[#454540] hover:text-[#f3f0e8]"
          onClick={handleDownload}
          type="button"
          title={`Download ${artifact.displayFilename}`}
          aria-label={`Download ${artifact.displayFilename}`}
        >
          <DownloadIcon />
        </button>
      </div>
      {lightboxOpen && previewUrl !== "" && (
        <div
          className="fixed inset-0 z-50 flex items-center justify-center bg-black/80 p-6"
          onClick={() => setLightboxOpen(false)}
          role="dialog"
          aria-modal="true"
          aria-label={`Preview ${artifact.displayFilename}`}
        >
          <button
            className="absolute right-4 top-4 grid h-9 w-9 place-items-center rounded-md bg-black/40 text-[#f3f0e8] transition-colors hover:bg-black/60"
            onClick={() => setLightboxOpen(false)}
            type="button"
            title="Close preview"
            aria-label="Close preview"
          >
            <CloseIcon />
          </button>
          <img
            className="max-h-full max-w-full object-contain"
            src={previewUrl}
            alt={artifact.displayFilename}
            onClick={(event) => event.stopPropagation()}
          />
        </div>
      )}
    </div>
  );
}
