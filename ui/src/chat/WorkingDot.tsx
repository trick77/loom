/**
 * Tail "working" indicator — three clay dots orbiting on a tilted plane that
 * breathes (the radius swells and shrinks) and slowly precesses, shown at the
 * bottom of a live assistant turn while it is thinking / running tools but not
 * yet streaming answer prose. It restores a visible "still working" cue in a
 * spot auto-scroll keeps on screen: the ActivityTracePanel's title is anchored at
 * the panel top and scrolls off as the trace grows.
 *
 * The container is a polite live region carrying a screen-reader-only "Working"
 * announcement: before the first reasoning title the ActivityTracePanel is not
 * mounted, so during that gap these dots are the ONLY activity cue and must
 * announce it themselves. The dots themselves are decorative (aria-hidden).
 *
 * Each dot needs two elements: the orbit slot swings x, the inner dot carries
 * depth (tilt, scale, opacity). See .ui-working-dots in index.css.
 */
import { useTranslation } from "react-i18next";

export function WorkingDot() {
  const { t } = useTranslation();
  return (
    <div className="ui-working-dots" role="status">
      {[0, 1, 2].map((i) => (
        <span key={i} className="ui-working-orbit" aria-hidden="true">
          <span className="ui-working-dot" />
        </span>
      ))}
      <span className="sr-only">{t("thread.working")}</span>
    </div>
  );
}
