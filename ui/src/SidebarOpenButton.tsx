// Mobile-only button that opens the sidebar drawer. Reuses the sidebar icon.
export function SidebarOpenButton({ onClick }: { onClick(): void }) {
  return (
    <button
      type="button"
      aria-label="Show sidebar"
      onClick={onClick}
      className="-ml-1 grid h-7 w-7 shrink-0 place-items-center rounded text-[#aaa79e] transition-colors hover:text-white md:hidden"
    >
      <svg className="h-[18px] w-[18px]" viewBox="0 0 24 24" fill="none" aria-hidden="true">
        <rect x="4" y="5" width="16" height="14" rx="2" stroke="currentColor" strokeWidth="1.5" />
        <path d="M9.5 5v14" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" />
      </svg>
    </button>
  );
}
