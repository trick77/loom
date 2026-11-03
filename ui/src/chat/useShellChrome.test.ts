import { act, renderHook } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";

import { useShellChrome } from "./useShellChrome";

describe("useShellChrome Strava outcome", () => {
  afterEach(() => window.history.replaceState(null, "", "/"));

  it("keeps the outcome when the route rewrites the URL before Settings mounts", () => {
    window.history.replaceState(null, "", "/?strava=connected");
    const { result } = renderHook(() => useShellChrome());
    // The route effect turns / into /new and drops the query.
    window.history.replaceState(null, "", "/new");
    expect(result.current.settingsOpen).toBe(true);
    expect(result.current.stravaOutcome).toBe("connected");
    act(() => result.current.closeSettings());
    expect(result.current.stravaOutcome).toBeNull();
    act(() => result.current.openSettings());
    expect(result.current.stravaOutcome).toBeNull();
  });

  it("opens nothing without ?strava", () => {
    const { result } = renderHook(() => useShellChrome());
    expect(result.current.settingsOpen).toBe(false);
    expect(result.current.stravaOutcome).toBeNull();
  });
});
