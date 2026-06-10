import "@testing-library/jest-dom/vitest";
import { render, screen } from "@testing-library/react";
import { expect, test, vi } from "vitest";

import { ThreadActionsMenu } from "./ThreadActionsMenu";
import type { Thread } from "./api";

function threadFixture(): Thread {
  return {
    id: "t1",
    title: "Chat title",
    starred: false,
    createdAt: "2026-06-01T00:00:00Z",
    updatedAt: "2026-06-04T15:00:00Z",
  };
}

test("uses sidebar text sizing regardless of render location", () => {
  render(
    <ThreadActionsMenu
      menuKey="t1"
      thread={threadFixture()}
      onDelete={vi.fn()}
      onRename={vi.fn()}
      onStarChange={vi.fn()}
    />,
  );

  expect(screen.getByRole("menu", { name: "Chat actions" })).toHaveClass("slopr-sidebar-text");
});

test("adds a divider after select when select is available", () => {
  render(
    <ThreadActionsMenu
      menuKey="t1"
      thread={threadFixture()}
      onSelect={vi.fn()}
      onDelete={vi.fn()}
      onRename={vi.fn()}
      onStarChange={vi.fn()}
    />,
  );

  const menu = screen.getByRole("menu", { name: "Chat actions" });
  const itemRoles = Array.from(menu.children).map((child) => child.getAttribute("role") ?? child.tagName.toLowerCase());

  expect(itemRoles).toEqual(["menuitem", "separator", "menuitem", "menuitem", "menuitem", "separator", "menuitem"]);
});

test("uses the brighter menu divider color for separators", () => {
  render(
    <ThreadActionsMenu
      menuKey="t1"
      thread={threadFixture()}
      onSelect={vi.fn()}
      onDelete={vi.fn()}
      onRename={vi.fn()}
      onStarChange={vi.fn()}
    />,
  );

  for (const separator of screen.getAllByRole("separator")) {
    expect(separator).toHaveClass("bg-[#4a4741]");
  }
});

test("uses sidebar icon sizing for menu icons", () => {
  render(
    <ThreadActionsMenu
      menuKey="t1"
      thread={threadFixture()}
      onSelect={vi.fn()}
      onDelete={vi.fn()}
      onRename={vi.fn()}
      onStarChange={vi.fn()}
    />,
  );

  for (const label of ["Select", "Star", "Rename", "Add to project", "Delete"]) {
    const icon = screen.getByRole("menuitem", { name: label }).querySelector("[aria-hidden='true']");

    expect(icon).toHaveClass("h-[21px]");
    expect(icon).toHaveClass("w-[21px]");
  }
});

test("shows enabled add to project for project-less chats when handler is provided", () => {
  render(
    <ThreadActionsMenu
      menuKey="t1"
      thread={threadFixture()}
      onDelete={vi.fn()}
      onRename={vi.fn()}
      onAddToProject={vi.fn()}
      onStarChange={vi.fn()}
    />,
  );

  expect(screen.getByRole("menuitem", { name: "Add to project" })).toBeEnabled();
});

test("shows remove from project for project chats when handler is provided", () => {
  render(
    <ThreadActionsMenu
      menuKey="t1"
      thread={{ ...threadFixture(), projectId: "p1" }}
      onDelete={vi.fn()}
      onRename={vi.fn()}
      onRemoveFromProject={vi.fn()}
      onStarChange={vi.fn()}
    />,
  );

  expect(screen.getByRole("menuitem", { name: "Remove from project" })).toBeEnabled();
  expect(screen.queryByRole("menuitem", { name: "Add to project" })).not.toBeInTheDocument();
});

test("shows archive when handler is provided", () => {
  render(
    <ThreadActionsMenu
      menuKey="t1"
      thread={threadFixture()}
      onDelete={vi.fn()}
      onRename={vi.fn()}
      onArchive={vi.fn()}
      onStarChange={vi.fn()}
    />,
  );

  expect(screen.getByRole("menuitem", { name: "Archive" })).toBeInTheDocument();
});
