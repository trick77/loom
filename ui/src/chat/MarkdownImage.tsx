import { useState, type ComponentProps, type MouseEvent } from "react";
import { useTranslation } from "react-i18next";

import { Icon } from "./Icon";
import { hostOf } from "./SourceFavicon";

// MarkdownImage renders an image from markdown that a model or a memory
// wrote. Any image that would make a request elsewhere loads only once
// clicked: showing it would fetch its URL the moment the text renders, so an
// image an injected page slipped into an answer could carry what the model
// knew (memory, documents) out in its URL without anyone clicking. That
// includes loom's own image proxies (/api/source-image, /api/favicon), which
// fetch the URL they are given. Inline (data:) images and loom's artifact
// images show at once; a bare filename (the model naming a generated image,
// already shown as an artifact card) can never resolve and shows nothing.
// Markdown gives an image only src, alt and title.
export function MarkdownImage({ src, alt, title }: ComponentProps<"img">) {
  const { t } = useTranslation();
  const [, setLoaded] = useState(false);
  if (typeof src !== "string") return null;
  const kind = imageKind(src);
  if (kind === "none") return null;
  if (kind === "local" || approved.has(src)) {
    return <img src={src} alt={alt} title={title} />;
  }
  const host = hostOf(src);
  const name = alt?.trim() ?? "";
  const load = (event: MouseEvent) => {
    // A linked image ([![badge](img)](href)) puts this chip inside the link:
    // loading the image must not follow it.
    event.preventDefault();
    event.stopPropagation();
    approved.add(src);
    setLoaded(true);
  };
  return (
    <button
      type="button"
      className="ui-markdown-image-load"
      aria-label={
        name
          ? t("messages.loadImageNamed", { host, name })
          : t("messages.loadImage", { host })
      }
      title={src}
      onClick={load}
    >
      <Icon name="globe" />
      {/* The host never shares a truncated span with the model-written alt
          text, so an alt cannot pass for another host. */}
      <span className="ui-markdown-image-host">
        {t("messages.loadImage", { host })}
      </span>
      {name && <span className="ui-markdown-image-alt">{name}</span>}
    </button>
  );
}

// approved is the image URLs loaded on a click this session, so an image
// stays loaded when the markdown re-renders its element (a streamed answer
// settling, a thread opened again).
const approved = new Set<string>();

// artifactImage is the same-origin path of an artifact's image.
const artifactImage = /^\/api\/artifacts\/[^/]+\/(download|thumbnail)$/;

// imageKind sorts an image src: "local" loads at once, "remote" on a click,
// "none" never.
function imageKind(src: string): "local" | "remote" | "none" {
  if (/^data:image\//i.test(src)) return "local";
  if (!/^https?:/i.test(src)) return "none";
  try {
    const url = new URL(src);
    const own =
      url.origin === window.location.origin &&
      url.search === "" &&
      artifactImage.test(url.pathname);
    return own ? "local" : "remote";
  } catch {
    return "none";
  }
}
