import { useCallback, useState } from "react";

import { useEscapeKey } from "./useEscapeKey";
import { useMediaQuery } from "./useMediaQuery";

// useShellChrome owns the shell's open/closed UI state that is not about any
// thread: the sidebar (desktop rail and mobile drawer), the user menu, the
// per-thread action menu, and the settings and search overlays.
//
// The Escape handler for the mobile drawer registers here, so call this where
// the shell registered it before: Escape goes to the last surface registered,
// and the thread menu's handler (in the shell) must stay above it.
export function useShellChrome() {
  const [openThreadMenuID, setOpenThreadMenuID] = useState<string | null>(null);
  const [userMenuOpen, setUserMenuOpen] = useState(false);
  const [settingsOpen, setSettingsOpen] = useState(false);
  const [searchOpen, setSearchOpen] = useState(false);
  const [sidebarCollapsed, setSidebarCollapsed] = useState(false);
  const [mobileSidebarOpen, setMobileSidebarOpen] = useState(false);
  const isMobile = useMediaQuery("(max-width: 767px)");
  // On mobile the sidebar is an overlay drawer that always shows the full
  // content; the rail-collapse only applies on desktop.
  const railCollapsed = !isMobile && sidebarCollapsed;
  // Stable handlers for the sidebar and the thread menus. The sidebar is
  // memoized and the shell re-renders on every keystroke and streamed token, so
  // an inline arrow here would re-render every thread row each time.
  const openMobileSidebar = useCallback(() => setMobileSidebarOpen(true), []);
  const closeMobileSidebar = useCallback(() => setMobileSidebarOpen(false), []);
  const toggleDesktopCollapsed = useCallback(
    () => setSidebarCollapsed((value) => !value),
    [],
  );
  const toggleUserMenu = useCallback(
    () => setUserMenuOpen((open) => !open),
    [],
  );
  const closeUserMenu = useCallback(() => setUserMenuOpen(false), []);
  const openSettings = useCallback(() => setSettingsOpen(true), []);
  const openSearch = useCallback(() => setSearchOpen(true), []);
  const toggleThreadMenu = useCallback(
    (menuKey: string) =>
      setOpenThreadMenuID((current) => (current === menuKey ? null : menuKey)),
    [],
  );
  const closeThreadMenu = useCallback(() => setOpenThreadMenuID(null), []);
  useEscapeKey(closeMobileSidebar, {
    active: mobileSidebarOpen,
  });

  return {
    openThreadMenuID,
    setOpenThreadMenuID,
    toggleThreadMenu,
    closeThreadMenu,
    userMenuOpen,
    toggleUserMenu,
    closeUserMenu,
    settingsOpen,
    setSettingsOpen,
    openSettings,
    searchOpen,
    setSearchOpen,
    openSearch,
    isMobile,
    sidebarCollapsed,
    railCollapsed,
    toggleDesktopCollapsed,
    mobileSidebarOpen,
    setMobileSidebarOpen,
    openMobileSidebar,
    closeMobileSidebar,
  };
}
