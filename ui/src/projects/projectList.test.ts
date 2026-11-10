import { describe, expect, test } from "vitest";

import type { Project } from "../api";
import { filterProjects, sortProjects } from "./projectList";

function project(overrides: Partial<Project>): Project {
  return {
    id: "p",
    name: "Project",
    description: "",
    starred: false,
    createdAt: "2026-01-01T00:00:00Z",
    updatedAt: "2026-01-01T00:00:00Z",
    lastActivityAt: "2026-01-01T00:00:00Z",
    ...overrides,
  };
}

describe("filterProjects", () => {
  const items = [
    project({ id: "a", name: "Malaga City Trip" }),
    project({ id: "b", name: "Budget", description: "Spain holiday costs" }),
    project({ id: "c", name: "Claims" }),
  ];

  test("returns every project for a blank query", () => {
    expect(filterProjects(items, "  ").map((p) => p.id)).toEqual([
      "a",
      "b",
      "c",
    ]);
  });

  test("matches name and description, ignoring case", () => {
    expect(filterProjects(items, "MAL").map((p) => p.id)).toEqual(["a"]);
    expect(filterProjects(items, "spain").map((p) => p.id)).toEqual(["b"]);
  });
});

describe("sortProjects", () => {
  const items = [
    project({
      id: "a",
      name: "beta",
      createdAt: "2026-03-01T00:00:00Z",
      updatedAt: "2026-01-01T00:00:00Z",
      lastActivityAt: "2026-02-01T00:00:00Z",
    }),
    project({
      id: "b",
      name: "Alpha",
      createdAt: "2026-01-01T00:00:00Z",
      updatedAt: "2026-03-01T00:00:00Z",
      lastActivityAt: "2026-01-01T00:00:00Z",
    }),
    project({
      id: "c",
      name: "gamma",
      createdAt: "2026-02-01T00:00:00Z",
      updatedAt: "2026-02-01T00:00:00Z",
      lastActivityAt: "2026-03-01T00:00:00Z",
    }),
  ];
  const ids = (sort: Parameters<typeof sortProjects>[1]) =>
    sortProjects(items, sort).map((p) => p.id);

  test("recent activity, newest first", () => {
    expect(ids("recent")).toEqual(["c", "a", "b"]);
  });

  test("last edited, newest first", () => {
    expect(ids("edited")).toEqual(["b", "c", "a"]);
  });

  test("date created, newest first", () => {
    expect(ids("created")).toEqual(["a", "c", "b"]);
  });

  test("name, alphabetical regardless of case", () => {
    expect(ids("name")).toEqual(["b", "a", "c"]);
  });

  test("leaves the input untouched", () => {
    sortProjects(items, "name");
    expect(items.map((p) => p.id)).toEqual(["a", "b", "c"]);
  });
});
