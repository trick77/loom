import { useState, type ComponentProps } from "react";
import { useTranslation } from "react-i18next";

import { Icon } from "./Icon";

// MarkdownImage renders an image from markdown that a model or a memory
// wrote. A remote image loads only once clicked: showing it would fetch its
// URL the moment the text renders, so an image an injected page slipped into
// an answer could carry what the model knew (memory, documents) out in its
// query string without anyone clicking. An inline (data:) or same-origin
// image shows at once; a bare filename (the model naming a generated image,
// already shown as an artifact card) can never resolve and shows nothing.
// Markdown gives an image only src, alt and title.
export function MarkdownImage({ src, alt, title }: ComponentProps<"img">) {
  const { t } = useTranslation();
  const [load, setLoad] = useState(false);
  if (typeof src !== "string") return null;
  const kind = imageKind(src);
  if (kind === "none") return null;
  if (kind === "local" || load)
    return <img src={src} alt={alt} title={title} />;
  const host = new URL(src).host;
  const label = alt?.trim()
    ? t("messages.loadImageNamed", { host, name: alt.trim() })
    : t("messages.loadImage", { host });
  return (
    <button
      type="button"
      className="ui-markdown-image-load"
      aria-label={label}
      title={src}
      onClick={() => setLoad(true)}
    >
      <Icon name="globe" />
      <span>{label}</span>
    </button>
  );
}

// imageKind sorts an image src: "local" loads at once, "remote" on a click,
// "none" never.
function imageKind(src: string): "local" | "remote" | "none" {
  if (/^data:image\//i.test(src)) return "local";
  if (!/^https?:/i.test(src)) return "none";
  try {
    return new URL(src).origin === window.location.origin ? "local" : "remote";
  } catch {
    return "none";
  }
}
