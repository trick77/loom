import { expect, test } from "vitest";

import type { Project, Thread } from "../api";
import { tabTitle } from "./tabTitle";

const thread: Thread = {
  id: "t1",
  title: "My great chat",
  starred: false,
  createdAt: "2026-01-01T00:00:00Z",
  updatedAt: "2026-01-01T00:00:00Z",
};

const project: Project = {
  id: "p1",
  name: "My Research Project",
  description: "",
  starred: false,
  createdAt: "2026-01-01T00:00:00Z",
  updatedAt: "2026-01-01T00:00:00Z",
};

test("static views map to their label with the Loom suffix", () => {
  expect(tabTitle({ view: "new" }, null, null)).toBe("New chat — Loom");
  expect(tabTitle({ view: "chats" }, null, null)).toBe("Recents — Loom");
  expect(tabTitle({ view: "artifacts" }, null, null)).toBe("Artifacts — Loom");
  expect(tabTitle({ view: "memory" }, null, null)).toBe("Memories — Loom");
  expect(tabTitle({ view: "projects" }, null, null)).toBe("Projects — Loom");
});

test("chat view uses the active thread title", () => {
  expect(tabTitle({ view: "chat", threadID: "t1" }, thread, null)).toBe("My great chat — Loom");
});

test("chat view falls back to plain Loom while the thread is loading", () => {
  expect(tabTitle({ view: "chat", threadID: "t1" }, null, null)).toBe("Loom");
});

test("project view uses the active project name", () => {
  expect(tabTitle({ view: "project", projectID: "p1" }, null, project)).toBe(
    "My Research Project — Loom",
  );
});

test("project view falls back to the Projects label while loading", () => {
  expect(tabTitle({ view: "project", projectID: "p1" }, null, null)).toBe("Projects — Loom");
});
