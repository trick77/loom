import { Icon } from "./Icon";

/**
 * UserMenu — popup opened from the sidebar user row. Settings opens the settings
 * modal; Language is a deliberate dead entry for now (wired later); Log out runs
 * the existing logout. Styling mirrors ThreadActionsMenu.
 */
export function UserMenu({
  onSettings,
  onLogout,
  onClose,
  className = "bottom-full left-0 mb-2",
}: {
  onSettings(): void;
  onLogout(): void;
  onClose(): void;
  className?: string;
}) {
  return (
    <div
      aria-label="User menu"
      className={`ui-sidebar-text absolute z-30 w-[220px] overflow-hidden rounded-[10px] border border-[#454540] bg-[#363632] shadow-[0_18px_32px_rgba(0,0,0,0.38)] ${className}`}
      role="menu"
    >
      <button
        className="flex h-[34px] w-full items-center gap-2.5 px-3 text-left text-[#f3f0e8] hover:bg-[#3f3f3a]"
        role="menuitem"
        type="button"
        onClick={() => {
          onClose();
          onSettings();
        }}
      >
        <Icon name="settings" size="19px" className="grid h-[21px] w-[21px] shrink-0 place-items-center" />
        Settings
      </button>
      <button
        className="flex h-[34px] w-full items-center gap-2.5 px-3 text-left text-[#f3f0e8] hover:bg-[#3f3f3a]"
        role="menuitem"
        type="button"
        onClick={() => {
          /* Language switching is not wired yet — deliberate dead entry. */
        }}
      >
        <Icon name="globe" size="19px" className="grid h-[21px] w-[21px] shrink-0 place-items-center" />
        Language
      </button>
      <div className="mx-[14px] my-[5px] h-px bg-[#4a4741]" role="separator" />
      <button
        className="flex h-[34px] w-full items-center gap-2.5 px-3 text-left text-[#f3f0e8] hover:bg-[#3f3f3a]"
        role="menuitem"
        type="button"
        onClick={() => {
          onClose();
          onLogout();
        }}
      >
        <LogoutMenuIcon />
        Log out
      </button>
    </div>
  );
}

function LogoutMenuIcon() {
  return (
    <svg className="h-[21px] w-[21px] shrink-0" viewBox="0 0 24 24" aria-hidden="true" fill="none">
      <path
        d="M14 7V5.5C14 4.7 13.3 4 12.5 4H6C5.2 4 4.5 4.7 4.5 5.5v13c0 .8.7 1.5 1.5 1.5h6.5c.8 0 1.5-.7 1.5-1.5V17"
        stroke="currentColor"
        strokeWidth="1.8"
        strokeLinecap="round"
        strokeLinejoin="round"
      />
      <path d="M10 12h10m0 0-3-3m3 3-3 3" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  );
}
