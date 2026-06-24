import { useEffect } from "react";

import { Icon } from "./Icon";

// Full-screen modal preview shared by the generated-image card and the inline
// SVG response bubble. Closes on backdrop click or Escape; clicking the image
// itself does not close it. `fill` scales the image up to fill the modal — used
// for SVGs, which usually carry only a viewBox (no intrinsic size) and would
// otherwise collapse to the ~300×150 default; raster images keep their natural
// size (max-* only, no upscaling).
export function ImageLightbox({
  src,
  alt,
  onClose,
  fill = false,
}: {
  src: string;
  alt: string;
  onClose: () => void;
  fill?: boolean;
}) {
  useEffect(() => {
    function onKeyDown(event: KeyboardEvent) {
      if (event.key === "Escape") onClose();
    }
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, [onClose]);

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/80 p-6"
      onClick={onClose}
      role="dialog"
      aria-modal="true"
      aria-label={`Preview ${alt}`}
    >
      <button
        className="absolute right-4 top-4 grid h-9 w-9 place-items-center rounded-md bg-black/40 text-[#f3f0e8] transition-colors hover:bg-black/60"
        onClick={onClose}
        type="button"
        title="Close preview"
        aria-label="Close preview"
      >
        <Icon name="close" size="20px" />
      </button>
      <img
        className={`object-contain ${fill ? "h-full w-full" : "max-h-full max-w-full"}`}
        src={src}
        alt={alt}
        onClick={(event) => event.stopPropagation()}
      />
    </div>
  );
}
