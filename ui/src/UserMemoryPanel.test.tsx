import "@testing-library/jest-dom/vitest";
import { render, screen } from "@testing-library/react";
import { beforeEach, expect, test, vi } from "vitest";

import { UserMemoryPanel } from "./UserMemoryPanel";
import * as api from "./api";
import { ICONS } from "./chat/Icon";

vi.mock("./api", async () => {
  const actual = await vi.importActual<typeof import("./api")>("./api");
  return {
    ...actual,
    getUserMemory: vi.fn(),
    getUserDirectives: vi.fn(),
  };
});

const getUserMemoryMock = vi.mocked(api.getUserMemory);
const getUserDirectivesMock = vi.mocked(api.getUserDirectives);

beforeEach(() => {
  getUserDirectivesMock.mockResolvedValue([]);
});

test("shows the empty state when there is no memory yet", async () => {
  getUserMemoryMock.mockResolvedValue({ content: "", updatedAt: null });

  render(<UserMemoryPanel />);

  expect(screen.getByRole("region", { name: "Memories" })).toBeInTheDocument();
  const heading = screen.getByRole("heading", { name: "Memories" });
  expect(heading).toBeInTheDocument();
  expect(heading).toHaveTextContent(ICONS.memory);
  expect(await screen.findByText(/Memories will show here after a few threads/)).toBeInTheDocument();
  expect(screen.queryByText("Memory")).not.toBeInTheDocument();
});

test("renders a flat bullet memory as a markdown list", async () => {
  getUserMemoryMock.mockResolvedValue({
    content: "- Works at Acme\n- Lives in Zurich",
    updatedAt: "2026-06-11T00:00:00Z",
  });

  render(<UserMemoryPanel />);

  const items = await screen.findAllByRole("listitem");
  expect(items).toHaveLength(2);
  expect(items[0]).toHaveTextContent("Works at Acme");
  expect(items[1]).toHaveTextContent("Lives in Zurich");
  // Markdown consumes the "- " markers; they are not rendered as literal text.
  expect(items[0]).not.toHaveTextContent("- Works");
});

test("renders the Work context and Top of mind sections as distinct headings", async () => {
  getUserMemoryMock.mockResolvedValue({
    content: "## Work context\n- Lives in Zurich\n\n## Top of mind\n- Building Loom",
    updatedAt: "2026-06-11T00:00:00Z",
  });

  render(<UserMemoryPanel />);

  // The structured markdown headings render as real headings, not literal "##".
  expect(await screen.findByRole("heading", { name: "Work context" })).toBeInTheDocument();
  expect(screen.getByRole("heading", { name: "Top of mind" })).toBeInTheDocument();
  expect(screen.getByText("Lives in Zurich")).toBeInTheDocument();
  expect(screen.getByText("Building Loom")).toBeInTheDocument();
  expect(screen.queryByText(/## Work context/)).not.toBeInTheDocument();
});

test("shows the user's standing instructions read-only, with no edit controls", async () => {
  getUserMemoryMock.mockResolvedValue({ content: "", updatedAt: null });
  getUserDirectivesMock.mockResolvedValue([
    { id: "d1", content: "Always answer in metric units", createdAt: "", updatedAt: "" },
    { id: "d2", content: "Call me Jan", createdAt: "", updatedAt: "" },
  ]);

  render(<UserMemoryPanel />);

  expect(
    await screen.findByRole("region", { name: "Other instructions" }),
  ).toBeInTheDocument();
  expect(screen.getByText("Always answer in metric units")).toBeInTheDocument();
  expect(screen.getByText("Call me Jan")).toBeInTheDocument();
  // View-only: the directives are steered via chat, so no add/edit/delete buttons.
  expect(screen.queryByRole("button", { name: /add|edit|remove|delete/i })).not.toBeInTheDocument();
});

test("shows the directives empty state when there are none", async () => {
  getUserMemoryMock.mockResolvedValue({ content: "", updatedAt: null });
  getUserDirectivesMock.mockResolvedValue([]);

  render(<UserMemoryPanel />);

  expect(await screen.findByText(/No saved instructions yet/)).toBeInTheDocument();
});

test("does not show a manual refresh action", async () => {
  getUserMemoryMock.mockResolvedValue({ content: "", updatedAt: null });

  render(<UserMemoryPanel />);

  await screen.findByText(/Memories will show here/);
  expect(screen.queryByRole("button", { name: /refresh/i })).not.toBeInTheDocument();
});

test("does not show a manual edit composer", async () => {
  getUserMemoryMock.mockResolvedValue({
    content: "- Works at Acme",
    updatedAt: "2026-06-11T00:00:00Z",
  });

  render(<UserMemoryPanel />);

  await screen.findByText("Works at Acme");
  // User memories are read-only — the prompt/edit affordance lives on project
  // memories only.
  expect(screen.queryByRole("button", { name: /edit memories/i })).not.toBeInTheDocument();
});
