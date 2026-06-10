import "@testing-library/jest-dom/vitest";
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, expect, test, vi } from "vitest";

import { ChatsPage } from "./ChatsPage";
import * as api from "./api";
import type { Thread } from "./api";

vi.mock("./api", async () => {
  const actual = await vi.importActual<typeof import("./api")>("./api");
  return {
    ...actual,
    listThreads: vi.fn(),
    listThreadIds: vi.fn(),
    bulkDeleteThreads: vi.fn(),
  };
});

const listThreadsMock = vi.mocked(api.listThreads);
const listThreadIdsMock = vi.mocked(api.listThreadIds);
const bulkDeleteThreadsMock = vi.mocked(api.bulkDeleteThreads);

function thread(id: string, title: string): Thread {
  return {
    id,
    title,
    starred: false,
    createdAt: "2026-06-01T00:00:00Z",
    updatedAt: "2026-06-04T15:00:00Z",
  };
}

const FIXTURES = [thread("t1", "Greeting"), thread("t2", "Morning greeting"), thread("t3", "Apps and websites")];

function renderPage(overrides: Partial<Parameters<typeof ChatsPage>[0]> = {}) {
  const props = {
    mutationVersion: 0,
    onOpenSidebar: vi.fn(),
    onNewChat: vi.fn(),
    onSelectThread: vi.fn(),
    onRenameThread: vi.fn(),
    onDeleteThread: vi.fn(),
    onStarThread: vi.fn(),
    projectsAvailable: true,
    onMoveSelectedToProject: vi.fn(),
    onAfterBulkDelete: vi.fn(),
    onSessionExpired: vi.fn(),
    ...overrides,
  };
  render(<ChatsPage {...props} />);
  return props;
}

function matching(search: string): Thread[] {
  const term = search.toLowerCase();
  return FIXTURES.filter((item) => item.title.toLowerCase().includes(term));
}

beforeEach(() => {
  listThreadsMock.mockImplementation(async (params) => ({
    items: matching(params?.search ?? ""),
    nextCursor: null,
  }));
  // "Select all" resolves the full matching id set from the server.
  listThreadIdsMock.mockImplementation(async (params) =>
    matching(params?.search ?? "").map((item) => item.id),
  );
  bulkDeleteThreadsMock.mockResolvedValue({ deleted: 0 });
});

afterEach(() => {
  vi.clearAllMocks();
});

test("renders all chats with a relative time label", async () => {
  renderPage();
  expect(await screen.findByText("Greeting")).toBeInTheDocument();
  expect(screen.getByText("Morning greeting")).toBeInTheDocument();
  expect(screen.getByText("Apps and websites")).toBeInTheDocument();
});

test("chat rows use the sidebar hover surface", async () => {
  renderPage();

  const rowButton = await screen.findByRole("button", { name: /Greeting/ });
  const rowSurface = rowButton.closest("div");

  expect(rowSurface).toHaveClass("rounded-md");
  expect(rowSurface).toHaveClass("transition-colors");
  expect(rowSurface).toHaveClass("hover:bg-[#2a2a28]");
});

test("search input uses the standard input text size", async () => {
  renderPage();

  const searchInput = await screen.findByRole("textbox", { name: "Search chats" });

  expect(searchInput).toHaveClass("slopr-composer-text");
  expect(searchInput).not.toHaveClass("slopr-control-text");
});

test("search filters by title (debounced)", async () => {
  renderPage();
  await screen.findByText("Apps and websites");

  fireEvent.change(screen.getByLabelText("Search chats"), { target: { value: "greet" } });

  await waitFor(() => {
    expect(screen.queryByText("Apps and websites")).not.toBeInTheDocument();
  });
  expect(screen.getByText("Greeting")).toBeInTheDocument();
  expect(screen.getByText("Morning greeting")).toBeInTheDocument();
  expect(listThreadsMock).toHaveBeenCalledWith(expect.objectContaining({ search: "greet" }));
});

test("Select all selects the filtered set and toggles button states", async () => {
  renderPage();
  await screen.findByText("Greeting");

  fireEvent.click(screen.getByRole("button", { name: "Select chats" }));

  // Nothing selected yet: destructive actions are disabled (muted).
  expect(screen.getByText("0 selected")).toBeInTheDocument();
  const deleteButton = screen.getByRole("button", { name: "Delete" });
  const moveButton = screen.getByRole("button", { name: "Move to project" });
  expect(deleteButton).toBeDisabled();
  expect(moveButton).toBeDisabled();

  fireEvent.click(screen.getByRole("button", { name: "Select all" }));

  // Selecting all resolves the full id set from the server asynchronously.
  expect(await screen.findByText("3 selected")).toBeInTheDocument();
  expect(deleteButton).toBeEnabled();
  expect(moveButton).toBeEnabled();
});

test("bulk delete confirms then calls the API with the selected ids", async () => {
  const props = renderPage();
  await screen.findByText("Greeting");

  fireEvent.click(screen.getByRole("button", { name: "Select chats" }));
  fireEvent.click(screen.getByRole("button", { name: "Select all" }));
  await screen.findByText("3 selected");
  fireEvent.click(screen.getByRole("button", { name: "Delete" }));

  const dialog = await screen.findByRole("dialog");
  fireEvent.click(within(dialog).getByRole("button", { name: "Delete" }));

  await waitFor(() => {
    expect(bulkDeleteThreadsMock).toHaveBeenCalledWith(["t1", "t2", "t3"]);
  });
  await waitFor(() => {
    expect(props.onAfterBulkDelete).toHaveBeenCalled();
  });
});

test("Move to project sends selected chats to the move handler", async () => {
  const props = renderPage();
  await screen.findByText("Greeting");
  fireEvent.click(screen.getByRole("button", { name: "Select chats" }));
  fireEvent.click(screen.getByRole("button", { name: "Select all" }));
  await screen.findByText("3 selected");

  fireEvent.click(screen.getByRole("button", { name: "Move to project" }));
  expect(props.onMoveSelectedToProject).toHaveBeenCalledWith(FIXTURES);
  expect(bulkDeleteThreadsMock).not.toHaveBeenCalled();
});
