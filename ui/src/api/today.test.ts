import { afterEach, expect, test, vi } from "vitest";

import { AuthExpiredError, UserFacingError } from "./http";
import { browserTimezone } from "./schedules";
import {
  coarsePoint,
  dropLocated,
  getToday,
  locateHere,
  PlaceNotFoundError,
  setLocation,
} from "./today";

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
    locationCredit: null,
    askLocation: false,
    canLocate: false,
    sun: null,
    weather: null,
    air: null,
    warning: null,
    tonight: null,
    runs: [],
    ideas: [],
    refreshAt: null,
  });
  // One plain URL for every screen: no compact flag.
  expect(fetchMock).toHaveBeenCalledWith("/api/today", {
    headers: { "X-Loom-Timezone": browserTimezone() },
  });
});

const dayRange = { high: 12, low: 4, code: 3, precipitationProbability: 40 };

test("getToday keeps a full answer and survives a null one", async () => {
  const full = {
    date: "2026-11-10",
    day: "tomorrow",
    location: { name: "Baden", kind: "located" },
    locationCredit: { text: "© OpenStreetMap", url: "https://osm.example/" },
    askLocation: false,
    canLocate: true,
    sun: {
      rise: "07:27",
      set: "16:56",
      dayMinutes: 569,
      polar: "",
      progress: null,
    },
    weather: {
      now: null,
      day: dayRange,
      hours: [],
      details: {
        feelsLike: null,
        humidity: null,
        wind: 30,
        gusts: 55,
        windDirection: 260,
        uvMax: 1,
        precipitationSum: 0,
        sunshineHours: 2,
      },
      rain: [{ time: "06:00", precipitation: 0, probability: 5 }],
      outlook: [
        {
          date: "2026-11-11",
          high: 8,
          low: 1,
          code: 3,
          precipitationProbability: null,
        },
      ],
    },
    air: {
      aqi: 64,
      level: "poor",
      max: true,
      pollen: [{ kind: "grass", level: "low" }],
      hours: [
        { time: "06:00", aqi: 40 },
        { time: "07:00", aqi: null },
      ],
    },
    warning: { kind: "rain", from: "10:00", day: "tomorrow", chance: 80 },
    tonight: { kind: "rain", from: "20:00", day: "today", chance: 90 },
    runs: [{ id: "n1", threadId: null, title: "t", body: "b", createdAt: "x" }],
    ideas: [],
    refreshAt: "2026-11-10T00:00:00+01:00",
  };
  stubFetch(Response.json(full));
  await expect(getToday()).resolves.toEqual(full);
  // A warning without its day is for the shown day, tonight's for today;
  // without a chance it has none.
  stubFetch(
    Response.json({
      ...full,
      warning: { kind: "rain", from: "09:00" },
      tonight: { kind: "frost", from: "23:00" },
    }),
  );
  await expect(getToday()).resolves.toMatchObject({
    warning: { kind: "rain", from: "09:00", day: "tomorrow", chance: null },
    tonight: { kind: "frost", from: "23:00", day: "today", chance: null },
  });
  // An older server sends no tonight; a kind without words is no warning.
  stubFetch(Response.json({ ...full, tonight: undefined }));
  await expect(getToday()).resolves.toMatchObject({ tonight: null });
  // An older server's tonight past midnight falls while everyone sleeps.
  stubFetch(
    Response.json({
      ...full,
      tonight: { kind: "rain", from: "02:00", day: "tomorrow" },
    }),
  );
  await expect(getToday()).resolves.toMatchObject({ tonight: null });
  stubFetch(
    Response.json({
      ...full,
      warning: { kind: "hail", from: "09:00", day: "tomorrow" },
      tonight: { kind: "rain" },
    }),
  );
  await expect(getToday()).resolves.toMatchObject({
    warning: null,
    tonight: null,
  });
  stubFetch(
    Response.json({ ...full, day: "yesterday", refreshAt: 1, warning: null }),
  );
  await expect(getToday()).resolves.toMatchObject({
    day: "today",
    refreshAt: null,
  });
  stubFetch(Response.json(null));
  await expect(getToday()).resolves.toMatchObject({
    date: "",
    day: "today",
    runs: [],
  });
});

test("getToday reads the weather defensively", async () => {
  // The earlier `today` key stands in for `day`; a missing now is none.
  stubFetch(Response.json({ weather: { today: dayRange, hours: [] } }));
  await expect(getToday()).resolves.toMatchObject({
    weather: { now: null, day: dayRange },
  });
  // An older server sends no further cards; an empty list is none either.
  stubFetch(
    Response.json({
      weather: { day: dayRange, hours: [], rain: [], outlook: null },
    }),
  );
  await expect(getToday()).resolves.toMatchObject({
    weather: { details: null, rain: null, outlook: null },
    air: null,
  });
  // Air with nothing to show is none; with pollen alone it stays.
  stubFetch(Response.json({ air: { aqi: null, pollen: [] } }));
  await expect(getToday()).resolves.toMatchObject({ air: null });
  stubFetch(
    Response.json({ air: { pollen: [{ kind: "birch", level: "high" }] } }),
  );
  await expect(getToday()).resolves.toMatchObject({
    air: { aqi: null, level: "", max: false, hours: [] },
  });
  // Hours without a time are dropped, an index that is no number is a gap.
  stubFetch(
    Response.json({
      air: {
        aqi: 30,
        hours: [{ time: "09:00", aqi: 31.6 }, { aqi: 5 }, { time: "10:00" }],
      },
    }),
  );
  await expect(getToday()).resolves.toMatchObject({
    air: {
      hours: [
        { time: "09:00", aqi: 31.6 },
        { time: "10:00", aqi: null },
      ],
    },
  });
  // Kinds and levels without words here are dropped; no pollen list is
  // unknown, not none.
  stubFetch(
    Response.json({
      air: {
        aqi: 30,
        level: "toxic",
        pollen: [
          { kind: "olive", level: "high" },
          { kind: "birch", level: "extreme" },
          { kind: "grass", level: "low" },
        ],
      },
    }),
  );
  await expect(getToday()).resolves.toMatchObject({
    air: { aqi: 30, level: "", pollen: [{ kind: "grass", level: "low" }] },
  });
  // Only kinds it cannot name: unknown, never "hardly any".
  stubFetch(
    Response.json({
      air: { aqi: 30, pollen: [{ kind: "olive", level: "high" }] },
    }),
  );
  await expect(getToday()).resolves.toMatchObject({ air: { pollen: null } });
  stubFetch(Response.json({ air: { aqi: 30, level: "fair" } }));
  await expect(getToday()).resolves.toMatchObject({
    air: { aqi: 30, level: "fair", pollen: null },
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
        polar: "",
      },
    }),
  );
  await expect(getToday()).resolves.toMatchObject({
    weather: { hours: [] },
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

test("coarsePoint rounds a position to about a kilometre", () => {
  expect(coarsePoint(47.477123, -8.308519)).toEqual({
    latitude: 47.48,
    longitude: -8.31,
  });
});

test("locateHere sends the point and returns the town kept", async () => {
  const answer = {
    place: { name: "Baden", kind: "located" },
    attribution: { text: "© OpenStreetMap", url: "https://osm.example/" },
  };
  const fetchMock = stubFetch(Response.json(answer));
  await expect(
    locateHere({ latitude: 47.48, longitude: 8.31 }),
  ).resolves.toEqual(answer);
  expect(fetchMock).toHaveBeenCalledWith("/api/me/location:locate", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ latitude: 47.48, longitude: 8.31 }),
  });
});

test("locateHere tells no town from other errors", async () => {
  const point = { latitude: 1, longitude: 2 };
  stubFetch(Response.json({ code: "not_found" }, { status: 422 }));
  await expect(locateHere(point)).rejects.toBeInstanceOf(PlaceNotFoundError);
  stubFetch(Response.json({ error: "busy" }, { status: 409 }));
  await expect(locateHere(point)).rejects.toEqual(new UserFacingError("busy"));
  stubFetch(Response.json({ error: "not configured" }, { status: 503 }));
  await expect(locateHere(point)).rejects.toEqual(
    new UserFacingError("not configured"),
  );
  stubFetch(new Response(null, { status: 401 }));
  await expect(locateHere(point)).rejects.toBeInstanceOf(AuthExpiredError);
  stubFetch(new Response("oops", { status: 500 }));
  await expect(locateHere(point)).rejects.toThrow("failed to locate");
});

test("dropLocated deletes the located town", async () => {
  const fetchMock = stubFetch(new Response(null, { status: 204 }));
  await expect(dropLocated()).resolves.toBeUndefined();
  expect(fetchMock).toHaveBeenCalledWith("/api/me/location:locate", {
    method: "DELETE",
  });
  stubFetch(new Response(null, { status: 401 }));
  await expect(dropLocated()).rejects.toBeInstanceOf(AuthExpiredError);
  stubFetch(new Response("oops", { status: 500 }));
  await expect(dropLocated()).rejects.toThrow("failed to drop the location");
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
