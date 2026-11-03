import { useCallback, useEffect, useState } from "react";

import { clearStravaOutcome, readStravaOutcome } from "../api";
import { useMediaQuery } from "./useMediaQuery";

// useShellChrome owns the shell's open/closed UI state that is not about any
// thread: the sidebar (desktop rail and mobile drawer), the user menu, the
// per-thread action menu, and the settings and search overlays. Escape handling
// stays in the shell, where the order of the Escape stack is set.
export function useShellChrome() {
  const [openThreadMenuID, setOpenThreadMenuID] = useState<string | null>(null);
  const [userMenuOpen, setUserMenuOpen] = useState(false);
  // Back from Strava's consent page (?strava=…): read during the first render,
  // before the route effect rewrites the URL, and Settings opens on the
  // Strava section with the outcome. The address bar loses it right after.
  const [stravaOutcome, setStravaOutcome] = useState(readStravaOutcome);
  const [settingsOpen, setSettingsOpen] = useState(stravaOutcome !== null);
  useEffect(() => {
    if (stravaOutcome !== null) clearStravaOutcome();
  }, [stravaOutcome]);
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
  // The outcome is shown once: a later Settings opens plain.
  const closeSettings = useCallback(() => {
    setSettingsOpen(false);
    setStravaOutcome(null);
  }, []);
  const openSearch = useCallback(() => setSearchOpen(true), []);
  const closeSearch = useCallback(() => setSearchOpen(false), []);
  const toggleThreadMenu = useCallback(
    (menuKey: string) =>
      setOpenThreadMenuID((current) => (current === menuKey ? null : menuKey)),
    [],
  );
  const closeThreadMenu = useCallback(() => setOpenThreadMenuID(null), []);

  return {
    openThreadMenuID,
    toggleThreadMenu,
    closeThreadMenu,
    userMenuOpen,
    toggleUserMenu,
    closeUserMenu,
    settingsOpen,
    stravaOutcome,
    openSettings,
    closeSettings,
    searchOpen,
    openSearch,
    closeSearch,
    isMobile,
    sidebarCollapsed,
    railCollapsed,
    toggleDesktopCollapsed,
    mobileSidebarOpen,
    openMobileSidebar,
    closeMobileSidebar,
  };
}
