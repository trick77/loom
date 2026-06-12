import "@testing-library/jest-dom/vitest";
import { render, screen } from "@testing-library/react";
import { expect, test, vi } from "vitest";

import { ProjectMemoryPanel } from "./ProjectMemoryPanel";
import * as api from "../api";
import { ICONS } from "../chat/Icon";

vi.mock("../api", async () => {
  const actual = await vi.importActual<typeof import("../api")>("../api");
  return {
    ...actual,
    getProjectMemory: vi.fn(),
  };
});

const getProjectMemoryMock = vi.mocked(api.getProjectMemory);

test("shows the empty state when there is no memory yet", async () => {
  getProjectMemoryMock.mockResolvedValue({ projectId: "p1", content: "", updatedAt: null });

  render(<ProjectMemoryPanel projectId="p1" />);

  expect(screen.getByRole("region", { name: "Memories" })).toBeInTheDocument();
  const heading = screen.getByRole("heading", { name: "Memories" });
  expect(heading).toBeInTheDocument();
  expect(heading).toHaveTextContent(ICONS.memory);
  expect(await screen.findByText(/Memories will show here after a few chats/)).toHaveClass("h-[490px]");
  expect(screen.queryByText("Memory")).not.toBeInTheDocument();
  expect(screen.queryByText(/Project memory/i)).not.toBeInTheDocument();
});

test("renders memory content when present", async () => {
  getProjectMemoryMock.mockResolvedValue({
    projectId: "p1",
    content: "Travel month: May",
    updatedAt: "2026-06-11T00:00:00Z",
  });

  render(<ProjectMemoryPanel projectId="p1" />);

  expect(await screen.findByText("Travel month: May")).toBeInTheDocument();
});

test("bounds memory content in a flush sidebar-styled scroll region", async () => {
  getProjectMemoryMock.mockResolvedValue({
    projectId: "p1",
    content: Array.from({ length: 80 }, (_, index) => `- Important project fact ${index + 1}`).join(
      "\n",
    ),
    updatedAt: "2026-06-11T00:00:00Z",
  });

  render(<ProjectMemoryPanel projectId="p1" />);

  const memory = await screen.findByTestId("project-memory-content");
  expect(memory).toHaveClass("h-[490px]");
  const scroll = screen.getByTestId("project-memory-scroll");
  expect(scroll).toHaveClass("ui-sidebar-scroll", "h-full", "overflow-y-auto");
  expect(screen.getByTestId("project-memory-bottom-fade")).toHaveClass(
    "pointer-events-none",
    "absolute",
    "bottom-0",
    "h-8",
    "bg-gradient-to-t",
    "to-transparent",
  );
  expect(memory).not.toHaveClass("pr-2");
  expect(scroll.firstElementChild).toHaveClass("px-5");
});

test("renders markdown content instead of raw syntax", async () => {
  getProjectMemoryMock.mockResolvedValue({
    projectId: "p1",
    content: "## Project: Trip\n- **Goal**: Compare options",
    updatedAt: "2026-06-11T00:00:00Z",
  });

  render(<ProjectMemoryPanel projectId="p1" />);

  // Heading rendered as an actual <h2>, not literal "##" text.
  const heading = await screen.findByRole("heading", { name: "Project: Trip" });
  expect(heading.tagName).toBe("H2");
  // Bold text rendered as <strong>, not literal "**".
  expect(screen.getByText("Goal").tagName).toBe("STRONG");
});
