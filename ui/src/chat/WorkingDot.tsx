/**
 * Tail "working" indicator — three clay dots that bounce in sequence, shown at
 * the bottom of a live assistant turn while it is thinking / running tools but
 * not yet streaming answer prose. It restores a visible "still working" cue in a
 * spot auto-scroll keeps on screen: the ActivityTracePanel's title is anchored at
 * the panel top and scrolls off as the trace grows.
 *
 * The container is a polite live region carrying a screen-reader-only "Working"
 * announcement: before the first reasoning title the ActivityTracePanel is not
 * mounted, so during that gap these dots are the ONLY activity cue and must
 * announce it themselves. The dots themselves are decorative (empty spans).
 */
export function WorkingDot() {
  return (
    <div className="ui-working-dots" role="status">
      <span />
      <span />
      <span />
      <span className="sr-only">Working</span>
    </div>
  );
}
