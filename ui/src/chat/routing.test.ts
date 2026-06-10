import { expect, test } from "vitest";

import { pathForRoute, routeFromPath } from "./routing";

test("routes project pages", () => {
	expect(routeFromPath("/projects")).toEqual({ view: "projects" });
	expect(routeFromPath("/projects/proj_1")).toEqual({ view: "project", projectID: "proj_1" });
	expect(pathForRoute({ view: "projects" })).toBe("/projects");
	expect(pathForRoute({ view: "project", projectID: "proj_1" })).toBe("/projects/proj_1");
});
