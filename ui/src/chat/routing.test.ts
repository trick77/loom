import { expect, test } from "vitest";

import { pathForRoute, routeFromPath } from "./routing";

test("routes project pages", () => {
	expect(routeFromPath("/projects")).toEqual({ view: "projects" });
	expect(routeFromPath("/projects/proj_1")).toEqual({ view: "project", projectID: "proj_1" });
	expect(pathForRoute({ view: "projects" })).toBe("/projects");
	expect(pathForRoute({ view: "project", projectID: "proj_1" })).toBe("/projects/proj_1");
});

test("routes thread pages", () => {
	expect(routeFromPath("/threads")).toEqual({ view: "threads" });
	expect(routeFromPath("/thread/t1")).toEqual({ view: "thread", threadID: "t1" });
	expect(pathForRoute({ view: "threads" })).toBe("/threads");
	expect(pathForRoute({ view: "thread", threadID: "t1" })).toBe("/thread/t1");
});

test("old /chat URLs are not supported (no backward compatibility)", () => {
	expect(routeFromPath("/chat/t1")).toEqual({ view: "new" });
	expect(routeFromPath("/chats")).toEqual({ view: "new" });
});
