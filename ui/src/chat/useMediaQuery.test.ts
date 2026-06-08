import { act, renderHook } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { useMediaQuery } from "./useMediaQuery";

type Listener = (event: { matches: boolean }) => void;

function installMatchMedia(initial: boolean) {
  let listeners: Listener[] = [];
  const mql = {
    matches: initial,
    media: "",
    addEventListener: (_type: string, cb: Listener) => listeners.push(cb),
    removeEventListener: (_type: string, cb: Listener) => {
      listeners = listeners.filter((listener) => listener !== cb);
    },
    emit(next: boolean) {
      mql.matches = next;
      listeners.forEach((listener) => listener({ matches: next }));
    },
    get listenerCount() {
      return listeners.length;
    },
  };
  window.matchMedia = vi.fn().mockReturnValue(mql) as unknown as typeof window.matchMedia;
  return mql;
}

afterEach(() => {
  // @ts-expect-error - reset between tests
  delete window.matchMedia;
  vi.restoreAllMocks();
});

describe("useMediaQuery", () => {
  it("returns the initial match state", () => {
    installMatchMedia(true);
    const { result } = renderHook(() => useMediaQuery("(max-width: 767px)"));
    expect(result.current).toBe(true);
  });

  it("updates when the media query changes", () => {
    const mql = installMatchMedia(false);
    const { result } = renderHook(() => useMediaQuery("(max-width: 767px)"));
    expect(result.current).toBe(false);
    act(() => mql.emit(true));
    expect(result.current).toBe(true);
  });

  it("removes its listener on unmount", () => {
    const mql = installMatchMedia(false);
    const { unmount } = renderHook(() => useMediaQuery("(max-width: 767px)"));
    expect(mql.listenerCount).toBe(1);
    unmount();
    expect(mql.listenerCount).toBe(0);
  });

  it("falls back to false when matchMedia is unavailable", () => {
    const { result } = renderHook(() => useMediaQuery("(max-width: 767px)"));
    expect(result.current).toBe(false);
  });
});
