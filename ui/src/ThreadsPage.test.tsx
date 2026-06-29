import "@testing-library/jest-dom/vitest";
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, expect, test, vi } from "vitest";

import { ThreadsPage } from "./ThreadsPage";
import * as api from "./api";
import type { Thread } from "./api";

vi.mock("./api", async () => {
  const actual = await vi.importActual<typeof import("./api")>("./api");
  return {
    ...actual,
    listThreads: vi.fn(),
    listThreadIds: vi.fn(),
    bulkDeleteThreads: vi.fn(),
    searchThreadContent: vi.fn(),
  };
});

const listThreadsMock = vi.mocked(api.listThreads);
const listThreadIdsMock = vi.mocked(api.listThreadIds);
const bulkDeleteThreadsMock = vi.mocked(api.bulkDeleteThreads);
const searchThreadContentMock = vi.mocked(api.searchThreadContent);

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

function renderPage(overrides: Partial<Parameters<typeof ThreadsPage>[0]> = {}) {
  const props = {
    mutationVersion: 0,
    onOpenSidebar: vi.fn(),
    onNewThread: vi.fn(),
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
  render(<ThreadsPage {...props} />);
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
  // No full-text matches by default; individual tests opt in.
  searchThreadContentMock.mockResolvedValue([]);
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
  const row = rowButton.closest("li");

  expect(row).toHaveClass("border-b");
  expect(rowSurface).toHaveClass("rounded-xl");
  expect(rowSurface).not.toHaveClass("border-b");
  expect(rowSurface).toHaveClass("px-3");
  expect(rowSurface).not.toHaveClass("px-1.5");
  expect(rowSurface).toHaveClass("transition-colors");
  expect(rowSurface).toHaveClass("hover:bg-[#2a2a28]");
  expect(rowSurface).not.toBeNull();
  const timeLabel = rowSurface?.querySelector("[data-thread-row-time]");
  expect(timeLabel).toHaveClass("ml-auto");
  expect(timeLabel).toHaveClass("group-hover:hidden");
  expect(timeLabel).toHaveClass("[@media(hover:none)]:hidden");
  const actionButton = within(rowSurface!).getByRole("button", { name: "Open thread actions" });
  expect(actionButton).toHaveClass("absolute");
  expect(actionButton).toHaveClass("right-3");
  expect(actionButton).toHaveClass("[@media(hover:none)]:visible");
});

test("chat rows fade adjacent dividers behind the rounded hover surface", async () => {
  renderPage();

  const firstRow = (await screen.findByText("Greeting")).closest("li");
  const secondRow = screen.getByText("Morning greeting").closest("li");
  expect(firstRow).toHaveClass("border-[#343432]");
  expect(secondRow).toHaveClass("border-[#343432]");

  fireEvent.pointerEnter(secondRow!);

  expect(firstRow).toHaveClass("border-transparent");
  expect(secondRow).toHaveClass("border-transparent");
});

test("search input uses the standard input text size", async () => {
  renderPage();

  const searchInput = await screen.findByRole("textbox", { name: "Search threads" });

  expect(searchInput).toHaveClass("ui-composer-text");
  expect(searchInput).not.toHaveClass("ui-control-text");
});

test("search filters by title (debounced)", async () => {
  renderPage();
  await screen.findByText("Apps and websites");

  fireEvent.change(screen.getByLabelText("Search threads"), { target: { value: "greet" } });

  await waitFor(() => {
    expect(screen.queryByText("Apps and websites")).not.toBeInTheDocument();
  });
  await waitFor(() => {
    expect(titleSpan("Greeting")).toBeInTheDocument();
  });
  expect(titleSpan("Morning greeting")).toBeInTheDocument();
  expect(listThreadsMock).toHaveBeenCalledWith(expect.objectContaining({ search: "greet" }));
});

// Matches the title span by its full textContent, since an active search bolds
// the matched term and splits the title across <strong> and text nodes.
const titleSpan = (title: string) =>
  screen.getByText(
    (_, element) =>
      element?.classList.contains("truncate") === true &&
      element.classList.contains("text-[15px]") &&
      element.textContent === title,
  );

test("search surfaces full-text matches and Select all includes them", async () => {
  // A thread whose title does NOT match the query — it only matches on content,
  // so it arrives via searchThreadContent, not the title list.
  searchThreadContentMock.mockResolvedValue([
    { thread: thread("t4", "Deployment notes"), snippet: "…we «greet» the operator…" },
  ]);

  renderPage();
  await screen.findByText("Apps and websites");

  fireEvent.change(screen.getByLabelText("Search threads"), { target: { value: "greet" } });

  // The content-only row renders once the full-text search resolves.
  await waitFor(() => {
    expect(titleSpan("Deployment notes")).toBeInTheDocument();
  });
  expect(searchThreadContentMock).toHaveBeenCalledWith(
    expect.objectContaining({ query: "greet" }),
  );

  fireEvent.click(screen.getByRole("button", { name: "Select threads" }));
  fireEvent.click(screen.getByRole("button", { name: "Select all" }));

  // Two title matches (Greeting, Morning greeting) + the content-only match.
  expect(await screen.findByText("3 selected")).toBeInTheDocument();
  // Select all over a search must NOT fall back to the title-only id endpoint.
  expect(listThreadIdsMock).not.toHaveBeenCalled();
});

test("Select all selects the filtered set and toggles button states", async () => {
  renderPage();
  await screen.findByText("Greeting");

  fireEvent.click(screen.getByRole("button", { name: "Select threads" }));

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

  fireEvent.click(screen.getByRole("button", { name: "Select threads" }));
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
  fireEvent.click(screen.getByRole("button", { name: "Select threads" }));
  fireEvent.click(screen.getByRole("button", { name: "Select all" }));
  await screen.findByText("3 selected");

  fireEvent.click(screen.getByRole("button", { name: "Move to project" }));
  expect(props.onMoveSelectedToProject).toHaveBeenCalledWith(FIXTURES);
  expect(bulkDeleteThreadsMock).not.toHaveBeenCalled();
});
