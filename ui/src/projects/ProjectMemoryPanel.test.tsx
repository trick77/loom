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
  expect(await screen.findByText(/Memories will show here after a few threads/)).toHaveClass("h-[490px]");
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

  // Assert the behaviour-carrying classes only (fixed frame + scrollable
  // sidebar-styled region + non-interactive fade), not every utility class -
  // pinning the full Tailwind list breaks on harmless styling refactors.
  const memory = await screen.findByTestId("project-memory-content");
  expect(memory).toHaveClass("h-[490px]");
  const scroll = screen.getByTestId("project-memory-scroll");
  expect(scroll).toHaveClass("ui-sidebar-scroll", "overflow-y-auto");
  expect(screen.getByTestId("project-memory-bottom-fade")).toHaveClass("pointer-events-none");
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
