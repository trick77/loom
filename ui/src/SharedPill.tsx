// Badge marking a thread that has an active public share link. Mirrors the
// prompt-classifier pill (see MessageMetrics.tsx) so chat lists stay visually
// consistent. Rendered in front of the thread title in every chat list.
export function SharedPill() {
  return (
    <span className="inline-flex shrink-0 items-center rounded-full bg-[#46453f] px-2 py-0.5 font-sans text-[0.75rem] leading-[1.45rem] text-[#d6d3ca]">
      Shared
    </span>
  );
}
