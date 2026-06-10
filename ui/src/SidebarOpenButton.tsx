import { Icon } from "./chat/Icon";

// Mobile-only button that opens the sidebar drawer. Reuses the sidebar icon.
export function SidebarOpenButton({ onClick }: { onClick(): void }) {
  return (
    <button
      type="button"
      aria-label="Show sidebar"
      onClick={onClick}
      className="slopr-sidebar-btn -ml-1 grid h-7 w-7 shrink-0 place-items-center rounded text-[#aaa79e] transition-colors hover:text-white md:hidden"
    >
      <Icon name="sidebar" size="18px" className="slopr-sidebar-icon" />
    </button>
  );
}
