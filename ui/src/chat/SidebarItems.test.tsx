import "@testing-library/jest-dom/vitest";
import { render, screen, within } from "@testing-library/react";
import { expect, test, vi } from "vitest";

import type { Thread } from "../api";
import { SidebarSection } from "./SidebarItems";

function thread(id: string, overrides: Partial<Thread> = {}): Thread {
  return {
    id,
    title: id,
    starred: false,
    createdAt: "2026-10-20T10:00:00Z",
    updatedAt: "2026-10-20T10:00:00Z",
    ...overrides,
  };
}

test("only a run thread without the user's reply carries the schedule clock", () => {
  render(
    <SidebarSection
      title="Recent"
      threads={[
        thread("fresh run", { scheduleId: "s1" }),
        thread("replied run", { scheduleId: "s1", scheduleReplied: true }),
        thread("plain"),
      ]}
      activeThreadID={null}
      openThreadMenuID={null}
      onSelect={vi.fn()}
      onDelete={vi.fn()}
      onRename={vi.fn()}
      onStarChange={vi.fn()}
      onToggleMenu={vi.fn()}
      onCloseMenu={vi.fn()}
    />,
  );
  const clocks = (name: string) =>
    within(
      screen.getByRole("button", { name: new RegExp(`^${name}`) }),
    ).queryAllByRole("img");
  expect(clocks("fresh run")).toHaveLength(1);
  expect(clocks("replied run")).toHaveLength(0);
  expect(clocks("plain")).toHaveLength(0);
});

test("a thread with unread news shows the bell's dot before its clock", () => {
  render(
    <SidebarSection
      title="Recent"
      threads={[
        thread("news run", { scheduleId: "s1", scheduleUnread: true }),
        thread("read run", { scheduleId: "s1" }),
      ]}
      activeThreadID={null}
      openThreadMenuID={null}
      onSelect={vi.fn()}
      onDelete={vi.fn()}
      onRename={vi.fn()}
      onStarChange={vi.fn()}
      onToggleMenu={vi.fn()}
      onCloseMenu={vi.fn()}
    />,
  );
  const marks = (name: string) =>
    within(screen.getByRole("button", { name: new RegExp(`^${name}`) }))
      .queryAllByRole("img")
      .map((mark) => mark.getAttribute("aria-label"));
  expect(marks("news run")).toEqual(["Unread news", "Schedules"]);
  expect(marks("read run")).toEqual(["Schedules"]);
});
