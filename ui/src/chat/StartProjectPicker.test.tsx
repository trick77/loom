import "@testing-library/jest-dom/vitest";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  within,
} from "@testing-library/react";
import { useState } from "react";
import { describe, expect, test, vi } from "vitest";

import type { Project } from "../api";
import { edgeFade, StartProjectPicker } from "./StartProjectPicker";

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

  test("trigger and chip read in the sidebar's ink, not the menus'", () => {
    renderPicker();
    const trigger = screen.getByRole("button", { name: "Project" });
    expect(trigger).toHaveClass("text-ink-dim", "font-normal");
    expect(trigger).not.toHaveClass("text-ink-bright");
    cleanup();
    renderPicker({ selectedProject: projects[0] });
    const chip = screen.getByRole("button", {
      name: "Remove Malaga City Trip",
    });
    expect(chip).toHaveClass("text-ink-dim", "font-normal");
    expect(chip).not.toHaveClass("text-ink-bright");
  });

  test("removing the chip hands focus to the trigger that replaces it", () => {
    function Harness() {
      const [picked, setPicked] = useState<Project | null>(projects[0]);
      return (
        <StartProjectPicker
          projects={projects}
          selectedProject={picked}
          onSelect={(id) =>
            setPicked(projects.find((p) => p.id === id) ?? null)
          }
          onCreateProject={vi.fn()}
        />
      );
    }
    render(<Harness />);
    const chip = screen.getByRole("button", {
      name: "Remove Malaga City Trip",
    });
    chip.focus();
    fireEvent.click(chip);
    expect(screen.getByRole("button", { name: "Project" })).toHaveFocus();
  });

  test("New project puts focus on the trigger before the dialog opens", () => {
    let focusedAtCreate: Element | null = null;
    renderPicker({
      onCreateProject: vi.fn(() => {
        focusedAtCreate = document.activeElement;
      }),
    });
    open();
    const search = screen.getByRole("combobox", { name: "Search projects" });
    fireEvent.keyDown(search, { key: "ArrowUp" });
    fireEvent.keyDown(search, { key: "Enter" });
    // The dialog reads this as its opener and hands focus back on close.
    expect(focusedAtCreate).toBe(
      screen.getByRole("button", { name: "Project" }),
    );
  });

  test("a pick hands focus to the chip", () => {
    function Harness() {
      const [picked, setPicked] = useState<Project | null>(null);
      return (
        <StartProjectPicker
          projects={projects}
          selectedProject={picked}
          onSelect={(id) =>
            setPicked(projects.find((p) => p.id === id) ?? null)
          }
          onCreateProject={vi.fn()}
        />
      );
    }
    render(<Harness />);
    open();
    fireEvent.click(screen.getByRole("option", { name: "Malaga City Trip" }));
    expect(
      screen.getByRole("button", { name: "Remove Malaga City Trip" }),
    ).toHaveFocus();
  });

  test("Enter that confirms an IME composition picks nothing", () => {
    const props = renderPicker();
    open();
    const search = screen.getByRole("combobox", { name: "Search projects" });
    fireEvent.keyDown(search, { key: "Enter", isComposing: true });
    // Safari: the committing Enter comes with isComposing false, keyCode 229.
    fireEvent.keyDown(search, { key: "Enter", keyCode: 229 });
    expect(props.onSelect).not.toHaveBeenCalled();
    expect(props.onCreateProject).not.toHaveBeenCalled();
    expect(screen.getByRole("dialog")).toBeInTheDocument();
  });

  test("with no projects and no search there is no 'no matches' line", () => {
    renderPicker({ projects: [] });
    open();
    expect(screen.queryByText("No matching projects")).not.toBeInTheDocument();
    expect(optionNames()).toEqual(["New project"]);
  });
});

describe("list edge fade", () => {
  test("the list hands its fade lengths to the scroll-fade mask", () => {
    renderPicker();
    open();
    const list = screen.getByRole("listbox").firstElementChild as HTMLElement;
    expect(list).toHaveClass("scroll-fade-y");
    const metrics = { scrollTop: 0, clientHeight: 178, scrollHeight: 209 };
    for (const key of Object.keys(metrics) as (keyof typeof metrics)[]) {
      Object.defineProperty(list, key, {
        configurable: true,
        get: () => metrics[key],
      });
    }
    const fade = () => [
      list.style.getPropertyValue("--fade-top"),
      list.style.getPropertyValue("--fade-bottom"),
    ];
    fireEvent.scroll(list);
    expect(fade()).toEqual(["0px", "28px"]);
    metrics.scrollTop = 15;
    fireEvent.scroll(list);
    expect(fade()).toEqual(["15px", "16px"]);
    metrics.scrollTop = 31;
    fireEvent.scroll(list);
    expect(fade()).toEqual(["28px", "0px"]);
  });

  test("options keep clear of the fade when the highlight scrolls them in", () => {
    renderPicker();
    open();
    for (const option of screen.getAllByRole("option")) {
      expect(option).toHaveClass("scroll-my-7");
    }
  });
});

describe("edgeFade", () => {
  const list = (scrollTop: number, scrollHeight = 209) => ({
    scrollTop,
    clientHeight: 178,
    scrollHeight,
  });

  test("fades toward the edges the list can still scroll to, 28px at most", () => {
    expect(edgeFade(list(0))).toEqual({ top: 0, bottom: 28 });
    expect(edgeFade(list(15))).toEqual({ top: 15, bottom: 16 });
    expect(edgeFade(list(31))).toEqual({ top: 28, bottom: 0 });
  });

  test("a list that fits, or is within a pixel of its end, fades nowhere", () => {
    expect(edgeFade(list(0, 178))).toEqual({ top: 0, bottom: 0 });
    expect(edgeFade(list(30.5))).toEqual({ top: 28, bottom: 0 });
  });
});
