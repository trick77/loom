import { afterEach, expect, test, vi } from "vitest";

import { AuthExpiredError, UserFacingError } from "./http";
import {
  browserTimezone,
  createSchedule,
  deleteSchedule,
  forgetScheduleMemory,
  getSchedule,
  getScheduleMemory,
  listNotifications,
  listScheduleRuns,
  listSchedules,
  markAllNotificationsRead,
  markNotificationRead,
  previewSchedule,
  runScheduleNow,
  ScheduleIntervalError,
  unreadNotificationCount,
  updateSchedule,
} from "./schedules";

afterEach(() => {
  vi.unstubAllGlobals();
});

function stubFetch(response: Response) {
  const fetchMock = vi.fn().mockResolvedValue(response);
  vi.stubGlobal("fetch", fetchMock);
  return fetchMock;
}

test("previewSchedule sends the interval with the browser's timezone", async () => {
  const fetchMock = stubFetch(Response.json({ cron: "37 14 * * 4" }));
  await expect(previewSchedule("once a week")).resolves.toEqual({
    cron: "37 14 * * 4",
  });
  expect(fetchMock).toHaveBeenCalledWith("/api/schedules:parse", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({
      interval: "once a week",
      timezone: browserTimezone(),
    }),
  });
});

test("interval errors carry their code; validation errors their text", async () => {
  stubFetch(
    Response.json({ error: "too often", code: "too_often" }, { status: 422 }),
  );
  await expect(previewSchedule("x")).rejects.toBeInstanceOf(
    ScheduleIntervalError,
  );
  stubFetch(
    Response.json({ error: "the prompt is required" }, { status: 400 }),
  );
  await expect(createSchedule({ interval: "daily" })).rejects.toEqual(
    new UserFacingError("the prompt is required"),
  );
  stubFetch(new Response(null, { status: 401 }));
  await expect(updateSchedule("s1", {})).rejects.toBeInstanceOf(
    AuthExpiredError,
  );
  stubFetch(new Response("oops", { status: 500 }));
  await expect(updateSchedule("s1", {})).rejects.toThrow(
    "failed to update schedule",
  );
});

test("createSchedule and updateSchedule", async () => {
  let fetchMock = stubFetch(Response.json({ id: "s1" }));
  await createSchedule({ prompt: "p", interval: "daily" });
  expect(fetchMock.mock.calls[0][0]).toBe("/api/schedules");
  expect(JSON.parse(fetchMock.mock.calls[0][1].body)).toMatchObject({
    prompt: "p",
    timezone: browserTimezone(),
  });
  fetchMock = stubFetch(Response.json({ id: "s1" }));
  await updateSchedule("s 1", { enabled: false });
  expect(fetchMock.mock.calls[0][0]).toBe("/api/schedules/s%201");
  expect(fetchMock.mock.calls[0][1].method).toBe("PATCH");
});

test("run now maps a conflict to the server's text", async () => {
  stubFetch(new Response(null, { status: 202 }));
  await expect(runScheduleNow("s1")).resolves.toBeUndefined();
  stubFetch(Response.json({ error: "already running" }, { status: 409 }));
  await expect(runScheduleNow("s1")).rejects.toEqual(
    new UserFacingError("already running"),
  );
  stubFetch(new Response(null, { status: 401 }));
  await expect(runScheduleNow("s1")).rejects.toBeInstanceOf(AuthExpiredError);
  stubFetch(new Response(null, { status: 500 }));
  await expect(runScheduleNow("s1")).rejects.toThrow("failed to start the run");
});

test("reads and deletes", async () => {
  let fetchMock = stubFetch(Response.json([]));
  await listSchedules();
  expect(fetchMock).toHaveBeenCalledWith("/api/schedules");
  fetchMock = stubFetch(Response.json({}));
  await getSchedule("s1");
  expect(fetchMock).toHaveBeenCalledWith("/api/schedules/s1");
  fetchMock = stubFetch(Response.json([]));
  await listScheduleRuns("s1");
  expect(fetchMock).toHaveBeenCalledWith("/api/schedules/s1/runs");
  fetchMock = stubFetch(Response.json({}));
  await getScheduleMemory("s1");
  expect(fetchMock).toHaveBeenCalledWith("/api/schedules/s1/memory");
  fetchMock = stubFetch(new Response(null, { status: 204 }));
  await forgetScheduleMemory("s1", "sources", "a.ch/x");
  expect(fetchMock.mock.calls[0][0]).toBe(
    "/api/schedules/s1/memory/sources?key=a.ch%2Fx",
  );
  fetchMock = stubFetch(new Response(null, { status: 204 }));
  await forgetScheduleMemory("s1", "findings");
  expect(fetchMock.mock.calls[0][0]).toBe("/api/schedules/s1/memory/findings");
  fetchMock = stubFetch(new Response(null, { status: 204 }));
  await deleteSchedule("s1");
  expect(fetchMock.mock.calls[0][1].method).toBe("DELETE");
});

test("notifications", async () => {
  let fetchMock = stubFetch(Response.json([]));
  await listNotifications();
  expect(fetchMock).toHaveBeenCalledWith("/api/notifications");
  stubFetch(Response.json({ count: 3 }));
  await expect(unreadNotificationCount()).resolves.toBe(3);
  fetchMock = stubFetch(new Response(null, { status: 204 }));
  await markNotificationRead("n1");
  expect(fetchMock.mock.calls[0][0]).toBe("/api/notifications/n1/read");
  fetchMock = stubFetch(new Response(null, { status: 204 }));
  await markAllNotificationsRead();
  expect(fetchMock.mock.calls[0][0]).toBe("/api/notifications/read-all");
});

test("browserTimezone falls back to UTC", () => {
  const spy = vi.spyOn(Intl, "DateTimeFormat").mockImplementation(() => {
    throw new Error("no intl");
  });
  expect(browserTimezone()).toBe("UTC");
  spy.mockRestore();
});
