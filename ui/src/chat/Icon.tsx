import type { CSSProperties } from "react";

/**
 * Anthropicons — Icon-Font-Komponente.
 *
 * Rendert ein Glyph aus der Variable-Font "Anthropic Icons" (siehe @font-face in
 * index.css). Die Codepoints liegen im Private-Use-Bereich U+E000–U+E11E und haben
 * keine sprechenden Namen im Font — die Zuordnung unten wurde visuell verifiziert.
 *
 * Alle 285 Glyphen lassen sich in /icons.html (durchsuchbar) inspizieren.
 *
 * Verwendung:
 *   <Icon name="send" />
 *   <Icon name="trash" className="text-danger" size="1rem" label="Löschen" />
 */
const CODEPOINTS = {
  send: 0xe09e,
  copy: 0xe056,
  edit: 0xe064,
  feather: 0xe0ed,
  trash: 0xe101,
  search: 0xe0d3,
  settings: 0xe0d6,
  sliders: 0xe070,
  attach: 0xe019,
  mic: 0xe0ab,
  micOff: 0xe0ad,
  stop: 0xe0ec,
  play: 0xe0c4,
  pause: 0xe0bb,
  retry: 0xe11d,
  undo: 0xe11e,
  check: 0xe03b,
  checkCircle: 0xe03c,
  warning: 0xe109,
  alertCircle: 0xe10a,
  spinner: 0xe0c1,
  thumbsUp: 0xe0fb,
  thumbsDown: 0xe0f9,
  sun: 0xe0ee,
  moon: 0xe0b1,
  bell: 0xe0b5,
  bellOff: 0xe0b6,
  star: 0xe0e7,
  starFilled: 0xe0e8,
  user: 0xe104,
  users: 0xe106,
  ghost: 0xe075,
  folder: 0xe072,
  folderPlus: 0xe074,
  sidebar: 0xe0dd,
  code: 0xe048,
  braces: 0xe04a,
  terminal: 0xe04f,
  more: 0xe05f,
  chevronDown: 0xe027,
  chevronLeft: 0xe029,
  chevronRight: 0xe02a,
  close: 0xe028,
  message: 0xe037,
  messages: 0xe039,
  addCircle: 0xe032,
  at: 0xe0a9,
  eye: 0xe069,
  eyeOff: 0xe06a,
  archive: 0xe0c9,
  globe: 0xe082,
  clock: 0xe068,
  moreVertical: 0xe062,
  moreHorizontal: 0xe061,
  artifact: 0xe017,
  plus: 0xe001,
  externalLink: 0xe00e,
} as const;

export type IconName = keyof typeof CODEPOINTS;

/** Name → Glyph-String (für direkte Verwendung in content/CSS, falls nötig). */
export const ICONS = Object.fromEntries(
  Object.entries(CODEPOINTS).map(([name, cp]) => [name, String.fromCodePoint(cp)]),
) as Record<IconName, string>;

export function Icon({
  name,
  className = "",
  size = "1.33rem",
  label,
}: {
  name: IconName;
  className?: string;
  /** font-size des Glyphs (steuert die Icon-Größe). */
  size?: string;
  /** Wenn gesetzt, wird das Icon als bedeutungstragend ausgezeichnet (role=img); sonst dekorativ. */
  label?: string;
}) {
  const style: CSSProperties = {
    fontFamily: '"Anthropic Icons"',
    fontSize: size,
    lineHeight: 1,
    fontStyle: "normal",
    fontWeight: 400,
    display: "inline-block",
    flexShrink: 0,
  };
  return (
    <span
      className={className}
      style={style}
      role={label ? "img" : undefined}
      aria-label={label}
      aria-hidden={label ? undefined : true}
    >
      {String.fromCodePoint(CODEPOINTS[name])}
    </span>
  );
}
