import {
  useEffect,
  useRef,
  useState,
  type KeyboardEvent,
  type ReactNode,
} from "react";
import { useTranslation } from "react-i18next";

import { fileName } from "@trick77/diagram";
import {
  configure,
  diagramKind,
  diagramTitle,
  draw,
  drawnNow,
  type Drawn,
} from "@trick77/diagram/mermaid";
import {
  DiagramCard,
  classes as defaultClasses,
  type Classes,
  type ShellProps,
  type Strings,
} from "@trick77/diagram/react";

import { Icon } from "./Icon";
import { DownloadIcon, ExpandIcon } from "./icons";
import { useEscapeKey } from "./useEscapeKey";

/**
 * One diagram in an answer: a ```mermaid fence, drawn by @trick77/diagram.
 * Its own drawing for the kinds it covers, the mermaid renderer's for the
 * rest; the renderer is loaded on the first fence, never with the page.
 *
 * The drawing is model output set as innerHTML, and a fence can come from
 * outside text (a page, a read thread). It is safe by the package's guards:
 * - its own drawing escapes every label;
 * - the renderer fallback is fail-closed: a source holding a link, a URL or
 *   url(), an encoded character code that rebuilds one, any tag with an
 *   attribute, or a bare acting tag in lowercase is refused before it draws
 *   (unsafeSource), so no image node or classDef can make the browser fetch
 *   anything;
 * - a capital bare tag or a `<<name>>` stereotype passes that gate and draws
 *   as text: HTML labels are off and locked, escapeAngles turns brackets into
 *   text before parsing, and the SVG re-check refuses script, iframe,
 *   object, embed, link, base, meta, form and any on* attribute;
 * - directives and front-matter config are dropped, not obeyed: the
 *   renderer's config is locked (strict, no HTML labels), so no init
 *   themeCSS reaches it;
 * - the renderer's SVG is checked again before it is shown (unsafeSvg).
 * A refused source takes the failure path below: the source shown as code,
 * never the reason, which is the package's English.
 *
 * A fence is answer text, not an action: it draws after a read_thread or a
 * web page where the image tool would be refused.
 */

// The renderer's theme is the package's, off loom's --color-* tokens; a note,
// with no ochre token in loom, takes the drawing's warn role.
configure({ ids: "loom-diagram" });

/** loom's own class names for the card and the full view: the package's
 * defaults name radius, shadow and border tokens loom does not have. */
const classes: Classes = {
  ...defaultClasses,
  card: "my-3",
  button:
    "grid h-7 w-7 place-items-center rounded-md text-faint hover:bg-active hover:text-ink-dim",
  frame: "rounded-lg border border-border bg-panel p-3",
  over: "absolute top-1.5 right-1.5 z-10 flex items-center gap-0.5 bg-gradient-to-l from-panel from-70% to-transparent pl-6",
  sheet:
    "flex h-full w-full flex-col overflow-hidden rounded-none border-0 bg-panel font-sans shadow-[0_24px_60px_-18px_rgba(0,0,0,0.6)] sm:h-auto sm:max-h-full sm:w-fit sm:max-w-full sm:min-w-[min(100%,560px)] sm:rounded-ui sm:border sm:border-border-strong",
  header:
    "flex items-center gap-2 border-b border-border px-3 py-2.5 sm:gap-3 sm:px-4.5 sm:py-3",
  title: "min-w-0 flex-1 truncate text-[13.5px] text-ink [contain:inline-size]",
  save: "hidden h-8 items-center gap-1.5 rounded-md border border-border px-2.5 text-[13px] text-ink-dim hover:border-border-strong hover:bg-active sm:flex",
};

const icons = { expand: <ExpandIcon />, download: <DownloadIcon /> };

/** useDrawn is the drawing for a source, null while it is on its way. One
 * already drawn (a card mounted again, the streamed answer replaced by the
 * stored one) is there at once, never behind the placeholder for a frame. */
function useDrawn(src: string): Drawn | null {
  const [state, setState] = useState<{ src: string; drawn: Drawn } | null>(
    null,
  );
  useEffect(() => {
    let live = true;
    void draw(src).then((drawn) => {
      if (live) setState({ src, drawn });
    });
    return () => {
      live = false;
    };
  }, [src]);
  if (state !== null && state.src === src) return state.drawn;
  return drawnNow(src);
}

/** redrawFor draws a source again for the room it is shown in: a timeline
 * lies flat where the text has room for it and stands up where it has not. */
function redrawFor(src: string) {
  return async (width: number): Promise<string | null> => {
    const d = await draw(src, { width });
    return "svg" in d ? d.svg : null;
  };
}

export function Diagram({ src }: { src: string }): ReactNode {
  const { t } = useTranslation();
  const drawn = useDrawn(src);
  const kind = diagramKind(src);
  const title = diagramTitle(src, titles(t), t("diagram.kind.other"));

  if (drawn === null) return <DiagramPending />;
  if ("error" in drawn) {
    return (
      <details className="ui-diagram-failed my-3 rounded-lg border border-border bg-panel p-3 font-sans text-sm text-muted">
        <summary className="cursor-pointer">{t("diagram.failed")}</summary>
        <pre className="mt-2 overflow-x-auto font-mono text-[13px] leading-relaxed">
          <code>{src}</code>
        </pre>
      </details>
    );
  }
  const strings: Strings = {
    full: t("diagram.full"),
    download: t("diagram.download"),
    fullOf: (name) => t("diagram.fullOf", { title: name }),
    downloadOf: (name) => t("diagram.downloadOf", { title: name }),
  };
  return (
    <DiagramCard
      svg={drawn.svg}
      own={drawn.own === true}
      title={title}
      fileName={fileName(kind, "loom")}
      shell={DiagramShell}
      icons={icons}
      strings={strings}
      classes={classes}
      redraw={redrawFor(src)}
    />
  );
}

/** The box a diagram's place holds while its fence streams or the picture is
 * being drawn. */
export function DiagramPending(): ReactNode {
  const { t } = useTranslation();
  return (
    <div className="ui-diagram-pending my-3 rounded-lg border border-border bg-panel p-3 font-sans text-sm text-muted">
      {t("diagram.pending")}
    </div>
  );
}

/** The picture's name by its mermaid type, in the reader's language. */
function titles(t: (key: string) => string): Record<string, string> {
  const flow = t("diagram.kind.flowchart");
  const state = t("diagram.kind.state");
  return {
    flowchart: flow,
    graph: flow,
    sequenceDiagram: t("diagram.kind.sequence"),
    stateDiagram: state,
    "stateDiagram-v2": state,
    erDiagram: t("diagram.kind.er"),
    classDiagram: t("diagram.kind.class"),
    "classDiagram-v2": t("diagram.kind.class"),
    mindmap: t("diagram.kind.mindmap"),
    timeline: t("diagram.kind.timeline"),
    quadrantChart: t("diagram.kind.quadrant"),
  };
}

/** DiagramShell is loom's overlay around the full view, as ImageLightbox has
 * it: a press on the backdrop or Escape closes, the sheet itself does not.
 * Focus moves in on open and Tab stays inside until it closes. */
function DiagramShell({
  label,
  dialogClassName,
  headerClassName,
  header,
  onClose,
  children,
}: ShellProps): ReactNode {
  const { t } = useTranslation();
  const sheet = useRef<HTMLDivElement>(null);
  const close = useRef<HTMLButtonElement>(null);
  useEscapeKey(onClose);

  useEffect(() => {
    const back = document.activeElement as HTMLElement | null;
    close.current?.focus();
    return () => back?.focus();
  }, []);

  function keepFocus(event: KeyboardEvent) {
    if (event.key !== "Tab" || sheet.current === null) return;
    const items = sheet.current.querySelectorAll<HTMLElement>(
      "button, a[href], [tabindex]:not([tabindex='-1'])",
    );
    if (items.length === 0) return;
    const first = items[0];
    const last = items[items.length - 1];
    if (event.shiftKey && document.activeElement === first) {
      event.preventDefault();
      last.focus();
    } else if (!event.shiftKey && document.activeElement === last) {
      event.preventDefault();
      first.focus();
    }
  }

  return (
    <div
      data-testid="diagram-backdrop"
      className="fixed inset-0 z-50 flex items-center justify-center bg-[rgba(0,0,0,0.5)] backdrop-blur-[2px] sm:p-6"
      onClick={onClose}
    >
      <div
        ref={sheet}
        role="dialog"
        aria-modal="true"
        aria-label={label}
        className={dialogClassName}
        onClick={(event) => event.stopPropagation()}
        onKeyDown={keepFocus}
      >
        <div className={headerClassName}>
          {header}
          <button
            ref={close}
            type="button"
            onClick={onClose}
            title={t("diagram.close")}
            aria-label={t("diagram.close")}
            className="grid h-8 w-8 shrink-0 place-items-center rounded-md text-muted hover:bg-active hover:text-ink"
          >
            <Icon name="close" size="18px" />
          </button>
        </div>
        {children}
      </div>
    </div>
  );
}
