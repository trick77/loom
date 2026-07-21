import { useTranslation } from "react-i18next";

import { Icon } from "./chat/Icon";

// Mobile-only button that opens the sidebar drawer. Reuses the sidebar icon.
//
// "inline" sits within a compact header bar (StartPanel/ThreadPanel). "floating"
// pins the button to the top-left corner of a positioned (relative) ancestor —
// used on the big-serif-title pages so the toggle stays corner-anchored like
// claude.ai instead of floating beside the title. Requires the parent to be
// `relative`.
export function SidebarOpenButton({
  onClick,
  variant = "inline",
}: {
  onClick(): void;
  variant?: "inline" | "floating";
}) {
  const { t } = useTranslation();
  const placement =
    variant === "floating" ? "absolute left-3 top-3 z-10" : "-ml-1";
  return (
    <button
      type="button"
      aria-label={t("sidebar.showSidebar")}
      onClick={onClick}
      className={`ui-sidebar-btn ${placement} grid h-7 w-7 shrink-0 place-items-center rounded text-[#aaa79e] transition-colors hover:text-white md:hidden`}
    >
      <Icon name="sidebar" size="18px" className="ui-sidebar-icon" />
    </button>
  );
}
