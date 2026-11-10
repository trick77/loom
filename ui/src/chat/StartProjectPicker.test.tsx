import "@testing-library/jest-dom/vitest";
import { fireEvent, render, screen, within } from "@testing-library/react";
import { describe, expect, test, vi } from "vitest";

import type { Project } from "../api";
import { StartProjectPicker } from "./StartProjectPicker";

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

const projects = [
  project({
    id: "trip",
    name: "Malaga City Trip",
    lastActivityAt: "2026-03-01T00:00:00Z",
    createdAt: "2026-01-01T00:00:00Z",
  }),
  project({
    id: "bi",
    name: "PowerBi",
    starred: true,
    description: "dashboards",
    lastActivityAt: "2026-01-01T00:00:00Z",
  }),
  project({
    id: "claims",
    name: "Claims",
    starred: true,
    lastActivityAt: "2026-02-01T00:00:00Z",
  }),
  project({
    id: "alpha",
    name: "Alpha",
    lastActivityAt: "2026-01-15T00:00:00Z",
    createdAt: "2026-04-01T00:00:00Z",
  }),
];

function renderPicker(
  overrides: Partial<Parameters<typeof StartProjectPicker>[0]> = {},
) {
  const props = {
    projects,
    selectedProject: null,
    onSelect: vi.fn(),
    onCreateProject: vi.fn(),
    ...overrides,
  };
  render(<StartProjectPicker {...props} />);
  return props;
}

function open() {
  fireEvent.click(screen.getByRole("button", { name: "Project" }));
  return screen.getByRole("dialog", { name: "Choose a project" });
}

function optionNames() {
  // The icon font's glyphs sit in the private use area; only the name counts.
  return screen
    .getAllByRole("option")
    .map((o) => o.textContent?.replace(/[-]/g, ""));
}

describe("StartProjectPicker", () => {
  test("is closed at first and opens on the trigger", () => {
    renderPicker();
    const trigger = screen.getByRole("button", { name: "Project" });
    expect(trigger).toHaveAttribute("aria-expanded", "false");
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    open();
    expect(trigger).toHaveAttribute("aria-expanded", "true");
    expect(
      screen.getByRole("combobox", { name: "Search projects" }),
    ).toHaveFocus();
  });

  test("closes on Escape and on a click outside", () => {
    renderPicker();
    open();
    fireEvent.keyDown(window, { key: "Escape" });
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    open();
    fireEvent.pointerDown(document.body);
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  });

  test("lists starred projects first, each group by recent activity", () => {
    renderPicker();
    open();
    expect(screen.getByText("Starred")).toBeInTheDocument();
    expect(optionNames()).toEqual([
      "Claims",
      "PowerBi",
      "Malaga City Trip",
      "Alpha",
      "New project",
    ]);
  });

  test("sorts by the chosen order", () => {
    renderPicker();
    open();
    fireEvent.click(screen.getByRole("button", { name: "Sort projects" }));
    fireEvent.click(screen.getByRole("menuitemradio", { name: "Name" }));
    expect(optionNames()).toEqual([
      "Claims",
      "PowerBi",
      "Alpha",
      "Malaga City Trip",
      "New project",
    ]);
    fireEvent.click(screen.getByRole("button", { name: "Sort projects" }));
    expect(screen.getByRole("menuitemradio", { name: "Name" })).toHaveAttribute(
      "aria-checked",
      "true",
    );
    fireEvent.click(
      screen.getByRole("menuitemradio", { name: "Date created" }),
    );
    expect(optionNames().slice(2, 4)).toEqual(["Alpha", "Malaga City Trip"]);
  });

  test("search shows a flat list of hits on name and description", () => {
    renderPicker();
    open();
    const search = screen.getByRole("combobox", { name: "Search projects" });
    fireEvent.change(search, { target: { value: "dash" } });
    expect(screen.queryByText("Starred")).not.toBeInTheDocument();
    expect(optionNames()).toEqual(["PowerBi", "New project"]);
    fireEvent.change(search, { target: { value: "zzz" } });
    expect(screen.getByText("No matching projects")).toBeInTheDocument();
  });

  test("picks a project by click", () => {
    const props = renderPicker();
    open();
    fireEvent.click(screen.getByRole("option", { name: "Malaga City Trip" }));
    expect(props.onSelect).toHaveBeenCalledWith("trip");
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  });

  test("nothing is highlighted at first; Enter takes the first hit", () => {
    const props = renderPicker();
    open();
    const search = screen.getByRole("combobox", { name: "Search projects" });
    fireEvent.change(search, { target: { value: "a" } });
    // Hits ignore the starred split: recent activity only.
    expect(optionNames()).toEqual([
      "Malaga City Trip",
      "Claims",
      "Alpha",
      "PowerBi",
      "New project",
    ]);
    expect(search).not.toHaveAttribute("aria-activedescendant");
    for (const option of screen.getAllByRole("option")) {
      expect(option).toHaveAttribute("aria-selected", "false");
    }
    fireEvent.keyDown(search, { key: "Enter" });
    expect(props.onSelect).toHaveBeenCalledWith("trip");
  });

  test("arrows move the highlight, Enter picks it", () => {
    const props = renderPicker();
    open();
    const search = screen.getByRole("combobox", { name: "Search projects" });
    fireEvent.change(search, { target: { value: "a" } });
    fireEvent.keyDown(search, { key: "ArrowDown" });
    const first = screen.getAllByRole("option")[0];
    expect(search).toHaveAttribute("aria-activedescendant", first.id);
    expect(first).toHaveAttribute("aria-selected", "true");
    fireEvent.keyDown(search, { key: "ArrowDown" });
    fireEvent.keyDown(search, { key: "Enter" });
    expect(props.onSelect).toHaveBeenCalledWith("claims");
  });

  test("ArrowUp with nothing highlighted goes to New project", () => {
    const props = renderPicker();
    open();
    const search = screen.getByRole("combobox", { name: "Search projects" });
    fireEvent.keyDown(search, { key: "ArrowUp" });
    fireEvent.keyDown(search, { key: "Enter" });
    expect(props.onCreateProject).toHaveBeenCalled();
    expect(props.onSelect).not.toHaveBeenCalled();
  });

  test("ArrowDown on New project wraps to the first project", () => {
    const props = renderPicker();
    open();
    const search = screen.getByRole("combobox", { name: "Search projects" });
    fireEvent.keyDown(search, { key: "ArrowUp" });
    fireEvent.keyDown(search, { key: "ArrowDown" });
    fireEvent.keyDown(search, { key: "Enter" });
    expect(props.onSelect).toHaveBeenCalledWith("claims");
  });

  test("New project asks to create one", () => {
    const props = renderPicker();
    open();
    fireEvent.click(screen.getByRole("option", { name: "New project" }));
    expect(props.onCreateProject).toHaveBeenCalled();
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  });

  test("a selected project shows as a chip that removes it", () => {
    const props = renderPicker({ selectedProject: projects[0] });
    expect(
      screen.queryByRole("button", { name: "Project" }),
    ).not.toBeInTheDocument();
    const chip = screen.getByRole("button", {
      name: "Remove Malaga City Trip",
    });
    expect(within(chip).getByText("Malaga City Trip")).toBeInTheDocument();
    fireEvent.click(chip);
    expect(props.onSelect).toHaveBeenCalledWith(null);
  });
});
