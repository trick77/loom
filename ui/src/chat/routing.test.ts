import { expect, test } from "vitest";

import { pathForRoute, routeFromPath } from "./routing";

test("routes project pages", () => {
  expect(routeFromPath("/projects")).toEqual({ view: "projects" });
  expect(routeFromPath("/projects/proj_1")).toEqual({
    view: "project",
    projectID: "proj_1",
  });
  expect(pathForRoute({ view: "projects" })).toBe("/projects");
  expect(pathForRoute({ view: "project", projectID: "proj_1" })).toBe(
    "/projects/proj_1",
  );
});

test("routes thread pages", () => {
  expect(routeFromPath("/threads")).toEqual({ view: "threads" });
  expect(routeFromPath("/thread/t1")).toEqual({
    view: "thread",
    threadID: "t1",
  });
  expect(pathForRoute({ view: "threads" })).toBe("/threads");
  expect(pathForRoute({ view: "thread", threadID: "t1" })).toBe("/thread/t1");
});

test("old /chat URLs are not supported (no backward compatibility)", () => {
  expect(routeFromPath("/chat/t1")).toEqual({ view: "new" });
  expect(routeFromPath("/chats")).toEqual({ view: "new" });
});

test("routes schedule pages", () => {
  expect(routeFromPath("/schedules")).toEqual({ view: "schedules" });
  expect(routeFromPath("/schedules/s%201")).toEqual({
    view: "schedule",
    scheduleID: "s 1",
  });
  expect(routeFromPath("/schedules/")).toEqual({ view: "new" });
  expect(pathForRoute({ view: "schedules" })).toBe("/schedules");
  expect(pathForRoute({ view: "schedule", scheduleID: "s 1" })).toBe(
    "/schedules/s%201",
  );
});

test("routes the ideas page and one idea on it", () => {
  expect(routeFromPath("/ideas")).toEqual({ view: "ideas" });
  expect(routeFromPath("/ideas/i%201")).toEqual({
    view: "idea",
    ideaID: "i 1",
  });
  expect(routeFromPath("/ideas/")).toEqual({ view: "new" });
  expect(pathForRoute({ view: "ideas" })).toBe("/ideas");
  expect(pathForRoute({ view: "idea", ideaID: "i 1" })).toBe("/ideas/i%201");
});
