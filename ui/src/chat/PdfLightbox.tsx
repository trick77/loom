import { useEffect, useState } from "react";

import { downloadArtifact } from "../api";
import { Icon } from "./Icon";

// Full-screen modal that previews a PDF artifact inline. The backend serves
// artifact bytes with `Content-Disposition: attachment`, so pointing an iframe
// straight at downloadUrl would force a download. Instead we fetch the bytes via
// downloadArtifact and render them through a `blob:` object URL, which browsers
// display inline natively (no pdf.js needed). Closes on backdrop click or Escape;
// clicking the document itself does not close it.
export function PdfLightbox({
  downloadUrl,
  filename,
  onClose,
}: {
  downloadUrl: string;
  filename: string;
  onClose: () => void;
}) {
  const [objectUrl, setObjectUrl] = useState("");
  const [error, setError] = useState(false);

  useEffect(() => {
    function onKeyDown(event: KeyboardEvent) {
      if (event.key === "Escape") onClose();
    }
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, [onClose]);

  useEffect(() => {
    let cancelled = false;
    let url = "";
    setError(false);
    setObjectUrl("");
    void downloadArtifact(downloadUrl)
      .then((blob) => {
        if (cancelled) return;
        url = URL.createObjectURL(blob);
        setObjectUrl(url);
      })
      .catch(() => {
        if (!cancelled) setError(true);
      });
    return () => {
      cancelled = true;
      if (url !== "") URL.revokeObjectURL(url);
    };
  }, [downloadUrl]);

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/80 p-6"
      onClick={onClose}
      role="dialog"
      aria-modal="true"
      aria-label={`Preview ${filename}`}
    >
      <button
        className="absolute right-4 top-4 z-10 grid h-9 w-9 place-items-center rounded-md bg-black/40 text-[#f3f0e8] transition-colors hover:bg-black/60"
        onClick={onClose}
        type="button"
        title="Close preview"
        aria-label="Close preview"
      >
        <Icon name="close" size="20px" />
      </button>
      <div
        className="flex h-[85vh] w-full max-w-5xl items-center justify-center overflow-hidden rounded-lg bg-[#1f1f1d]"
        onClick={(event) => event.stopPropagation()}
      >
        {error ? (
          <div className="ui-meta-text px-4 text-[#d36f67]">Preview failed</div>
        ) : objectUrl === "" ? (
          <div className="ui-meta-text px-4 text-[#aaa79e]">Loading preview…</div>
        ) : (
          <iframe className="h-full w-full border-0" src={objectUrl} title={filename} />
        )}
      </div>
    </div>
  );
}
