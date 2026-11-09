import { afterEach, expect, test, vi } from "vitest";

import { AuthExpiredError, UserFacingError } from "./http";
import { browserTimezone } from "./schedules";
import { getToday, PlaceNotFoundError, setLocation } from "./today";

afterEach(() => {
  vi.unstubAllGlobals();
});

function stubFetch(response: Response) {
  const fetchMock = vi.fn().mockResolvedValue(response);
  vi.stubGlobal("fetch", fetchMock);
  return fetchMock;
}

test("getToday sends the browser's timezone; a short answer shows nothing", async () => {
  const fetchMock = stubFetch(Response.json({ date: "2026-11-09" }));
  await expect(getToday()).resolves.toEqual({
    date: "2026-11-09",
    view: "tiles",
    location: null,
    askLocation: false,
    sun: null,
    weather: null,
    warning: null,
    runs: [],
  });
  expect(fetchMock).toHaveBeenCalledWith("/api/today", {
    headers: { "X-Loom-Timezone": browserTimezone() },
  });
});

test("getToday keeps a full answer and survives a null one", async () => {
  const full = {
    date: "2026-11-09",
    view: "line",
    location: { name: "Zürich", kind: "home" },
    askLocation: true,
    sun: null,
    weather: null,
    warning: { kind: "rain", from: "17:00" },
    runs: [{ id: "n1", threadId: null, title: "t", body: "b", createdAt: "x" }],
  };
  stubFetch(Response.json(full));
  await expect(getToday()).resolves.toEqual(full);
  stubFetch(Response.json(null));
  await expect(getToday()).resolves.toMatchObject({ date: "", runs: [] });
});

test("getToday maps a 401 to an expired session", async () => {
  stubFetch(new Response(null, { status: 401 }));
  await expect(getToday()).rejects.toBeInstanceOf(AuthExpiredError);
});

test("setLocation posts the place and returns it", async () => {
  const place = { name: "Zürich", kind: "home" };
  const fetchMock = stubFetch(Response.json({ place }));
  await expect(setLocation("Zürich")).resolves.toEqual({ place });
  expect(fetchMock).toHaveBeenCalledWith("/api/me/location", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ place: "Zürich" }),
  });
});

test("setLocation tells an unknown place from other errors", async () => {
  stubFetch(Response.json({ code: "not_found" }, { status: 422 }));
  await expect(setLocation("Atlantis")).rejects.toBeInstanceOf(
    PlaceNotFoundError,
  );
  stubFetch(Response.json({ error: "busy, try again" }, { status: 409 }));
  await expect(setLocation("Bern")).rejects.toEqual(
    new UserFacingError("busy, try again"),
  );
  stubFetch(new Response(null, { status: 401 }));
  await expect(setLocation("Bern")).rejects.toBeInstanceOf(AuthExpiredError);
  stubFetch(new Response("oops", { status: 500 }));
  await expect(setLocation("Bern")).rejects.toThrow("failed to save the place");
});
