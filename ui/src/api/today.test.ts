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
    day: "today",
    location: null,
    askLocation: false,
    sun: null,
    weather: null,
    warning: null,
    runs: [],
  });
  // One plain URL for every screen: no compact flag.
  expect(fetchMock).toHaveBeenCalledWith("/api/today", {
    headers: { "X-Loom-Timezone": browserTimezone() },
  });
});

const dayRange = { high: 12, low: 4, code: 3, precipitationProbability: 40 };
const attribution = { text: "Open-Meteo", url: "https://open-meteo.com/" };

test("getToday keeps a full answer and survives a null one", async () => {
  const full = {
    date: "2026-11-10",
    day: "tomorrow",
    location: { name: "Zürich", kind: "home" },
    askLocation: true,
    sun: {
      rise: "07:27",
      set: "16:56",
      dayMinutes: 569,
      deltaMinutes: -2,
      polar: "",
      progress: null,
    },
    weather: { now: null, day: dayRange, hours: [], attribution },
    warning: { kind: "rain", from: "09:00" },
    runs: [{ id: "n1", threadId: null, title: "t", body: "b", createdAt: "x" }],
  };
  stubFetch(Response.json(full));
  await expect(getToday()).resolves.toEqual(full);
  stubFetch(Response.json({ ...full, day: "yesterday" }));
  await expect(getToday()).resolves.toMatchObject({ day: "today" });
  stubFetch(Response.json(null));
  await expect(getToday()).resolves.toMatchObject({
    date: "",
    day: "today",
    runs: [],
  });
});

test("getToday reads the weather defensively", async () => {
  // The earlier `today` key stands in for `day`; a missing now is none.
  stubFetch(
    Response.json({ weather: { today: dayRange, hours: [], attribution } }),
  );
  await expect(getToday()).resolves.toMatchObject({
    weather: { now: null, day: dayRange },
  });
  // Without the day's range there is no forecast to show.
  stubFetch(Response.json({ weather: { now: { temperature: 1, code: 0 } } }));
  await expect(getToday()).resolves.toMatchObject({ weather: null });
  // Missing hours, credit and sun position get safe defaults.
  stubFetch(
    Response.json({
      weather: { day: dayRange },
      sun: {
        rise: "07:00",
        set: "17:00",
        dayMinutes: 600,
        deltaMinutes: 0,
        polar: "",
      },
    }),
  );
  await expect(getToday()).resolves.toMatchObject({
    weather: { hours: [], attribution: { text: "", url: "" } },
    sun: { progress: null },
  });
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
