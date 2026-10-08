import { act, fireEvent, renderHook } from "@testing-library/react";
import { expect, test } from "vitest";

import { useShellChrome } from "./useShellChrome";

test("menus and the desktop rail toggle", () => {
  const { result } = renderHook(() => useShellChrome());

  act(() => result.current.toggleUserMenu());
  expect(result.current.userMenuOpen).toBe(true);
  act(() => result.current.closeUserMenu());
  expect(result.current.userMenuOpen).toBe(false);

  act(() => result.current.toggleThreadMenu("thread:a"));
  expect(result.current.openThreadMenuID).toBe("thread:a");
  act(() => result.current.toggleThreadMenu("thread:b"));
  expect(result.current.openThreadMenuID).toBe("thread:b");
  act(() => result.current.toggleThreadMenu("thread:b"));
  expect(result.current.openThreadMenuID).toBeNull();

  act(() => result.current.toggleDesktopCollapsed());
  expect(result.current.sidebarCollapsed).toBe(true);
});

test("the openers stay stable across renders", () => {
  const { result, rerender } = renderHook(() => useShellChrome());
  const first = result.current;

  act(() => result.current.openSettings());
  act(() => result.current.openSearch());
  rerender();

  expect(result.current.settingsOpen).toBe(true);
  expect(result.current.searchOpen).toBe(true);
  expect(result.current.openMobileSidebar).toBe(first.openMobileSidebar);
  expect(result.current.toggleThreadMenu).toBe(first.toggleThreadMenu);
  expect(result.current.closeThreadMenu).toBe(first.closeThreadMenu);
});

test("Escape closes the mobile drawer", () => {
  const { result } = renderHook(() => useShellChrome());

  act(() => result.current.openMobileSidebar());
  expect(result.current.mobileSidebarOpen).toBe(true);
  act(() => {
    fireEvent.keyDown(window, { key: "Escape" });
  });

  expect(result.current.mobileSidebarOpen).toBe(false);
});
