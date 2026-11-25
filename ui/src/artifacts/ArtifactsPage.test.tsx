import "@testing-library/jest-dom/vitest";
import {
  act,
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import { afterEach, beforeEach, expect, test, vi } from "vitest";

import { ArtifactsPage } from "./ArtifactsPage";
import * as api from "../api";
import type { Artifact, ArtifactSummary, Page } from "../api";

vi.mock("../api", async () => {
  const actual = await vi.importActual<typeof import("../api")>("../api");
  return {
    ...actual,
    deleteArtifact: vi.fn(),
    downloadArtifact: vi.fn(),
    getArtifactSummary: vi.fn(),
    listArtifacts: vi.fn(),
  };
});

const listArtifactsMock = vi.mocked(api.listArtifacts);
const summaryMock = vi.mocked(api.getArtifactSummary);
const deleteArtifactMock = vi.mocked(api.deleteArtifact);
const downloadArtifactMock = vi.mocked(api.downloadArtifact);

// Captures the live IntersectionObserver callbacks so tests can simulate the
// sentinel scrolling into view and triggering a "load more".
let intersectionCallbacks: Array<
  (entries: Array<{ isIntersecting: boolean }>) => void
> = [];

class MockIntersectionObserver {
  constructor(callback: (entries: Array<{ isIntersecting: boolean }>) => void) {
    intersectionCallbacks.push(callback);
  }
  observe() {}
  unobserve() {}
  disconnect() {}
  takeRecords() {
    return [];
  }
}

function triggerIntersection() {
  for (const callback of intersectionCallbacks) {
    callback([{ isIntersecting: true }]);
  }
}

function page(
  items: Artifact[],
  nextCursor: string | null = null,
): Page<Artifact> {
  return { items, nextCursor };
}

function summary(
  olderCount = 0,
  months: ArtifactSummary["months"] = [],
): ArtifactSummary {
  return { olderCount, months };
}

// Relative to "now" so the row's relative-time label ("… ago") is deterministic
// regardless of when the suite runs.
const recentIso = new Date(Date.now() - 3 * 60 * 60 * 1000).toISOString();

function artifact(overrides: Partial<Artifact>): Artifact {
  return {
    id: "art_file",
    threadId: "thread_1",
    displayFilename: "quarterly-board-update.pdf",
    mimeType: "application/pdf",
    sizeBytes: 1_400_000,
    modifiedAt: recentIso,
    downloadUrl: "/api/artifacts/art_file/download",
    ...overrides,
  };
}

const robot = artifact({
  id: "art_image",
  displayFilename: "robot.png",
  mimeType: "image/png",
  sizeBytes: 842 * 1024,
  downloadUrl: "/api/artifacts/art_image/download",
  thumbnailUrl: "/api/artifacts/art_image/thumbnail",
});

// A non-PDF, non-image file: it downloads on click (the preview modal is
// reserved for images and PDFs).
const textFile = artifact({
  id: "art_text",
  displayFilename: "notes.txt",
  mimeType: "text/plain",
  downloadUrl: "/api/artifacts/art_text/download",
});

function pageProps() {
  return {
    onOpenSidebar: vi.fn(),
    onSessionExpired: vi.fn(),
    onUseInThread: vi.fn(),
    onOpenThread: vi.fn(),
  };
}

function renderPage() {
  const props = pageProps();
  const view = render(<ArtifactsPage {...props} />);
  return { ...props, rerender: view.rerender, props };
}

// Pins the clock (Date only: timers stay real for the search debounce and
// findBy polling) to local midday, so no zone moves "now" to another day.
const now = new Date(2026, 10, 25, 12, 0, 0);
function pinClock() {
  vi.useFakeTimers({ toFake: ["Date"] });
  vi.setSystemTime(now);
}

function daysAgo(days: number, hour = 10): string {
  return new Date(
    now.getFullYear(),
    now.getMonth(),
    now.getDate() - days,
    hour,
  ).toISOString();
}

const iso = (y: number, m: number, d = 1) => new Date(y, m, d).toISOString();

// A tile names its file twice (hover overlay, touch caption): find it by its
// button instead of its text.
const findRobotTile = () =>
  screen.findByRole("button", { name: "Preview robot.png" });

beforeEach(() => {
  intersectionCallbacks = [];
  vi.stubGlobal("IntersectionObserver", MockIntersectionObserver);
  // PdfLightbox turns the fetched blob into a `blob:` object URL; jsdom doesn't
  // implement these, so provide no-op stubs.
  URL.createObjectURL = vi.fn(() => "blob:mock");
  URL.revokeObjectURL = vi.fn();
  listArtifactsMock.mockResolvedValue(page([robot, artifact({})]));
  summaryMock.mockResolvedValue(summary());
  deleteArtifactMock.mockResolvedValue(undefined);
  downloadArtifactMock.mockResolvedValue(
    new Blob(["image-bytes"], { type: "image/png" }),
  );
});

afterEach(() => {
  vi.useRealTimers();
  vi.clearAllMocks();
  vi.unstubAllGlobals();
});

test("the default view asks for the last 30 days in one request", async () => {
  pinClock();
  renderPage();

  expect(
    await screen.findByRole("heading", { name: "Artifacts" }),
  ).toBeInTheDocument();
  expect(
    screen.getByRole("textbox", { name: "Search file names and chats" }),
  ).toHaveAttribute("placeholder", "Search file names and chats...");
  expect(screen.getByRole("button", { name: "All" })).toHaveAttribute(
    "aria-pressed",
    "true",
  );
  expect(screen.getByRole("button", { name: "Last 30 days" })).toHaveAttribute(
    "aria-pressed",
    "true",
  );
  await screen.findByText("quarterly-board-update.pdf");
  const since = iso(2026, 9, 26);
  expect(listArtifactsMock).toHaveBeenCalledTimes(1);
  expect(listArtifactsMock).toHaveBeenCalledWith({
    type: "all",
    since,
    limit: 500,
    cursor: null,
  });
  expect(summaryMock).toHaveBeenCalledWith({ since, type: "all" });
  // No sortable column header any more.
  expect(screen.queryByRole("button", { name: "Name" })).toBeNull();
  expect(screen.queryByRole("button", { name: "Size" })).toBeNull();
});

test("the window groups into today, this week and this month", async () => {
  pinClock();
  listArtifactsMock.mockResolvedValue(
    page([
      artifact({
        id: "a",
        displayFilename: "today.txt",
        modifiedAt: daysAgo(0),
      }),
      artifact({
        id: "b",
        displayFilename: "week.txt",
        modifiedAt: daysAgo(3),
      }),
      artifact({
        id: "c",
        displayFilename: "old.txt",
        modifiedAt: daysAgo(20),
      }),
      artifact({
        id: "d",
        displayFilename: "older.txt",
        modifiedAt: daysAgo(29),
      }),
    ]),
  );
  renderPage();

  const today = await screen.findByRole("region", { name: "Today" });
  const week = screen.getByRole("region", { name: "This week" });
  const month = screen.getByRole("region", { name: "This month" });
  expect(within(today).getByText("today.txt")).toBeInTheDocument();
  expect(within(week).getByText("week.txt")).toBeInTheDocument();
  expect(within(month).getByText("old.txt")).toBeInTheDocument();
  expect(within(month).getByText("older.txt")).toBeInTheDocument();
  // The count hint sits beside each heading.
  expect(within(month).getByText("2 artifacts")).toBeInTheDocument();
  expect(within(today).getByText("1 artifact")).toBeInTheDocument();
  // In page order: Today, This week, This month.
  expect(
    screen.getAllByRole("heading", { level: 2 }).map((h) => h.textContent),
  ).toEqual(["Today", "This week", "This month"]);
});

test("an empty group is not rendered", async () => {
  pinClock();
  listArtifactsMock.mockResolvedValue(
    page([artifact({ displayFilename: "old.txt", modifiedAt: daysAgo(12) })]),
  );
  renderPage();
  await screen.findByRole("region", { name: "This month" });
  expect(screen.queryByRole("region", { name: "Today" })).toBeNull();
  expect(screen.queryByRole("region", { name: "This week" })).toBeNull();
});

test("images render as tiles before the file rows", async () => {
  listArtifactsMock.mockResolvedValue(page([textFile, robot]));
  renderPage();

  const tileButton = await screen.findByRole("button", {
    name: "Preview robot.png",
  });
  const tile = tileButton.closest("li");
  expect(tile).toHaveClass("ui-artifact-tile");
  expect(
    within(tile!).getByRole("button", { name: "Actions for robot.png" }),
  ).toBeInTheDocument();
  // The thumbnail uses the small thumbnail endpoint.
  expect(
    within(tile!).getByRole("img", { name: "robot.png thumbnail" }),
  ).toHaveAttribute("src", "/api/artifacts/art_image/thumbnail");

  const row = screen.getByRole("button", { name: "Download notes.txt" });
  expect(row.closest("li")).not.toHaveClass("ui-artifact-tile");
  // Tiles come first, even though the server sent the file first.
  expect(
    tile!.compareDocumentPosition(row) & Node.DOCUMENT_POSITION_FOLLOWING,
  ).toBeTruthy();
});

test("a row shows age and size, and its chat as a link that opens it", async () => {
  const withChat = artifact({
    id: "art_chat",
    displayFilename: "budget.txt",
    mimeType: "text/plain",
    threadId: "thread_9",
    threadTitle: "Budget talk",
  });
  listArtifactsMock.mockResolvedValue(page([withChat, textFile]));
  const { onOpenThread } = renderPage();

  const chatRow = (await screen.findByText("budget.txt")).closest("li")!;
  const secondary = chatRow.querySelector(".ui-artifacts-row-secondary");
  expect(secondary).toHaveTextContent("from chat Budget talk");
  expect(chatRow.querySelector(".ui-artifacts-row-primary")).toHaveTextContent(
    /ago/,
  );
  expect(chatRow.querySelector(".ui-artifacts-row-primary")).toHaveTextContent(
    "1.3 MB",
  );

  fireEvent.click(
    within(chatRow).getByRole("button", { name: "Open chat Budget talk" }),
  );
  expect(onOpenThread).toHaveBeenCalledWith("thread_9");
  // The chat link does not also download the file.
  expect(downloadArtifactMock).not.toHaveBeenCalled();

  // Without a chat title the row keeps showing the file type.
  const plainRow = screen.getByText("notes.txt").closest("li")!;
  expect(
    plainRow.querySelector(".ui-artifacts-row-secondary"),
  ).toHaveTextContent("text/plain");
});

test("a chat title shows as typed, markup characters and all", async () => {
  const title = "Vec<T> & <b>x</b> <chat>y</chat>";
  listArtifactsMock.mockResolvedValue(
    page([{ ...textFile, threadId: "thread_3", threadTitle: title }]),
  );
  renderPage();
  const link = await screen.findByRole("button", {
    name: `Open chat ${title}`,
  });
  expect(link).toHaveTextContent(title, { normalizeWhitespace: false });
  expect(link.closest(".ui-artifacts-row-secondary")).toHaveTextContent(
    `from chat ${title}`,
  );
});

test("a tile on touch names its chat in a caption that opens it", async () => {
  listArtifactsMock.mockResolvedValue(
    page([{ ...robot, threadId: "thread_7", threadTitle: "Robots" }]),
  );
  const { onOpenThread } = renderPage();
  const tile = (
    await screen.findByRole("button", { name: "Preview robot.png" })
  ).closest("li")!;
  fireEvent.click(
    within(tile).getByRole("button", { name: "Open chat Robots" }),
  );
  expect(onOpenThread).toHaveBeenCalledWith("thread_7");
});

test("search spans all time, ungrouped, and clearing it returns to the period", async () => {
  pinClock();
  listArtifactsMock.mockResolvedValue(
    page([
      artifact({ displayFilename: "today.txt", modifiedAt: daysAgo(0) }),
      artifact({
        id: "x",
        displayFilename: "ancient.txt",
        modifiedAt: iso(2019, 1),
      }),
    ]),
  );
  renderPage();
  await screen.findByRole("region", { name: "Today" });

  const box = screen.getByRole("textbox", {
    name: "Search file names and chats",
  });
  fireEvent.change(box, { target: { value: "robot" } });
  await waitFor(() => {
    expect(listArtifactsMock).toHaveBeenLastCalledWith({
      type: "all",
      search: "robot",
      limit: 200,
      cursor: null,
    });
  });
  await screen.findByText("ancient.txt");
  expect(screen.queryByRole("heading", { level: 2 })).toBeNull();
  // A search lists every period, so none is marked as the one shown.
  const recentChip = screen.getByRole("button", { name: "Last 30 days" });
  expect(recentChip).toHaveAttribute("aria-pressed", "false");

  fireEvent.change(box, { target: { value: "" } });
  await waitFor(() => {
    expect(recentChip).toHaveAttribute("aria-pressed", "true");
  });
  await waitFor(() => {
    expect(listArtifactsMock).toHaveBeenLastCalledWith({
      type: "all",
      since: iso(2026, 9, 26),
      limit: 500,
      cursor: null,
    });
  });
  expect(
    await screen.findByRole("region", { name: "Today" }),
  ).toBeInTheDocument();
});

test("a year or a month period lists by calendar month", async () => {
  pinClock();
  summaryMock.mockResolvedValue(
    summary(3, [
      { month: "2026-11", count: 2 },
      { month: "2025-12", count: 2 },
      { month: "2025-10", count: 1 },
    ]),
  );
  listArtifactsMock.mockImplementation(async (params) => {
    if (params?.since === iso(2025, 0, 1))
      return page([
        artifact({
          id: "dec",
          displayFilename: "dec.txt",
          modifiedAt: iso(2025, 11, 20),
        }),
        artifact({
          id: "oct",
          displayFilename: "oct.txt",
          modifiedAt: iso(2025, 9, 3),
        }),
      ]);
    return page([]);
  });
  renderPage();

  fireEvent.click(await screen.findByRole("button", { name: "2025" }));
  await waitFor(() => {
    expect(listArtifactsMock).toHaveBeenLastCalledWith({
      type: "all",
      since: iso(2025, 0, 1),
      until: iso(2026, 0, 1),
      limit: 200,
      cursor: null,
    });
  });
  expect(screen.getByRole("button", { name: "2025" })).toHaveAttribute(
    "aria-pressed",
    "true",
  );
  expect(screen.getByRole("button", { name: "Last 30 days" })).toHaveAttribute(
    "aria-pressed",
    "false",
  );
  const december = await screen.findByRole("region", {
    name: "December 2025",
  });
  expect(within(december).getByText("dec.txt")).toBeInTheDocument();
  expect(
    within(screen.getByRole("region", { name: "October 2025" })).getByText(
      "oct.txt",
    ),
  ).toBeInTheDocument();

  const picker = screen.getByRole("combobox", { name: "Pick month" });
  expect(
    within(picker).getByRole("option", { name: "October 2025 (1)" }),
  ).toBeInTheDocument();
  fireEvent.change(picker, { target: { value: "2025-10" } });
  await waitFor(() => {
    expect(listArtifactsMock).toHaveBeenLastCalledWith({
      type: "all",
      since: iso(2025, 9, 1),
      until: iso(2025, 10, 1),
      limit: 200,
      cursor: null,
    });
  });
  expect(picker).toHaveValue("2025-10");
  expect(screen.getByRole("button", { name: "2025" })).toHaveAttribute(
    "aria-pressed",
    "false",
  );
  expect(
    await screen.findByText("No artifacts in this period."),
  ).toBeInTheDocument();

  fireEvent.click(screen.getByRole("button", { name: "Last 30 days" }));
  await waitFor(() => {
    expect(listArtifactsMock).toHaveBeenLastCalledWith(
      expect.objectContaining({ since: iso(2026, 9, 26), limit: 500 }),
    );
  });
  expect(picker).toHaveValue("");
});

test("picking a period while searching clears the search", async () => {
  summaryMock.mockResolvedValue(summary(0, [{ month: "2024-05", count: 1 }]));
  renderPage();
  const box = await screen.findByRole("textbox", {
    name: "Search file names and chats",
  });
  fireEvent.change(box, { target: { value: "robot" } });
  await waitFor(() => {
    expect(listArtifactsMock).toHaveBeenLastCalledWith(
      expect.objectContaining({ search: "robot" }),
    );
  });
  fireEvent.click(screen.getByRole("button", { name: "2024" }));
  expect(box).toHaveValue("");
  await waitFor(() => {
    expect(listArtifactsMock).toHaveBeenLastCalledWith(
      expect.objectContaining({
        since: iso(2024, 0, 1),
        until: iso(2025, 0, 1),
      }),
    );
  });
  // The cleared box ends the search at once: the search is not sent again
  // while the debounce catches up.
  await new Promise((resolve) => setTimeout(resolve, 300));
  const searches = listArtifactsMock.mock.calls.filter(
    ([params]) => params?.search === "robot",
  );
  expect(searches).toHaveLength(1);
  expect(listArtifactsMock).toHaveBeenLastCalledWith(
    expect.objectContaining({ since: iso(2024, 0, 1) }),
  );
});

test("after a type switch the empty text waits for that type's summary", async () => {
  listArtifactsMock.mockResolvedValue(page([]));
  summaryMock.mockResolvedValueOnce(
    summary(2, [{ month: "2024-02", count: 2 }]),
  );
  renderPage();
  expect(
    await screen.findByText(
      "Nothing in the last 30 days. Older artifacts are reachable via search or a period.",
    ),
  ).toBeInTheDocument();

  let resolveImages: (value: ArtifactSummary) => void = () => {};
  summaryMock.mockReturnValueOnce(
    new Promise((resolve) => {
      resolveImages = resolve;
    }),
  );
  fireEvent.click(screen.getByRole("button", { name: "Images" }));
  await waitFor(() => {
    expect(listArtifactsMock).toHaveBeenLastCalledWith(
      expect.objectContaining({ type: "images" }),
    );
  });
  await new Promise((resolve) => setTimeout(resolve, 20));
  // The other type's older count must not speak for this one.
  expect(screen.queryByText(/Nothing in the last 30 days/)).toBeNull();
  expect(screen.queryByText("No artifacts yet.")).toBeNull();
  expect(screen.queryByRole("button", { name: "Show 2024" })).toBeNull();
  // The period bar keeps the last months meanwhile instead of flickering.
  expect(screen.getByRole("button", { name: "2024" })).toBeInTheDocument();

  resolveImages(summary());
  expect(await screen.findByText("No artifacts yet.")).toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "2024" })).toBeNull();
});

test("the footer names older artifacts and jumps to their newest year", async () => {
  pinClock();
  summaryMock.mockResolvedValue(
    summary(4, [
      { month: "2026-11", count: 2 },
      { month: "2025-03", count: 4 },
    ]),
  );
  renderPage();
  expect(
    await screen.findByText(
      "4 older artifacts are not loaded. Find them via search or a period.",
    ),
  ).toBeInTheDocument();

  fireEvent.click(screen.getByRole("button", { name: "Show 2025" }));
  await waitFor(() => {
    expect(listArtifactsMock).toHaveBeenLastCalledWith(
      expect.objectContaining({
        since: iso(2025, 0, 1),
        until: iso(2026, 0, 1),
      }),
    );
  });
  // Only the default window carries the note.
  expect(screen.queryByText(/older artifacts? (is|are) not loaded/)).toBeNull();
});

test("no footer note when nothing is older", async () => {
  renderPage();
  await findRobotTile();
  await waitFor(() => expect(summaryMock).toHaveBeenCalled());
  expect(screen.queryByText(/not loaded/)).toBeNull();
});

test("empty states tell the recent window, no artifacts and no match apart", async () => {
  pinClock();
  listArtifactsMock.mockResolvedValue(page([]));
  summaryMock.mockResolvedValue(summary(2, [{ month: "2024-02", count: 2 }]));
  const { unmount } = render(<ArtifactsPage {...pageProps()} />);
  expect(
    await screen.findByText(
      "Nothing in the last 30 days. Older artifacts are reachable via search or a period.",
    ),
  ).toBeInTheDocument();
  expect(screen.getByRole("button", { name: "Show 2024" })).toBeInTheDocument();
  expect(screen.queryByText("No artifacts yet.")).toBeNull();
  unmount();

  summaryMock.mockResolvedValue(summary());
  render(<ArtifactsPage {...pageProps()} />);
  expect(await screen.findByText("No artifacts yet.")).toBeInTheDocument();

  fireEvent.change(
    screen.getByRole("textbox", { name: "Search file names and chats" }),
    { target: { value: "zzz" } },
  );
  expect(
    await screen.findByText("No artifacts match your search."),
  ).toBeInTheDocument();
});

test("the type filter goes to both the list and the summary", async () => {
  renderPage();
  await findRobotTile();

  fireEvent.click(screen.getByRole("button", { name: "Images" }));
  await waitFor(() => {
    expect(listArtifactsMock).toHaveBeenLastCalledWith(
      expect.objectContaining({ type: "images" }),
    );
    expect(summaryMock).toHaveBeenLastCalledWith(
      expect.objectContaining({ type: "images" }),
    );
  });
  expect(screen.getByRole("button", { name: "Images" })).toHaveAttribute(
    "aria-pressed",
    "true",
  );
});

test("a delete drops the item and refreshes the summary", async () => {
  listArtifactsMock.mockResolvedValue(page([robot, textFile]));
  renderPage();
  await screen.findByText("notes.txt");
  await waitFor(() => expect(summaryMock).toHaveBeenCalledTimes(1));

  fireEvent.click(
    screen.getByRole("button", { name: "Actions for notes.txt" }),
  );
  fireEvent.click(screen.getByRole("menuitem", { name: "Delete" }));
  const dialog = await screen.findByRole("dialog");
  fireEvent.click(within(dialog).getByRole("button", { name: "Delete" }));

  await waitFor(() => {
    expect(screen.queryByText("notes.txt")).toBeNull();
  });
  expect(deleteArtifactMock).toHaveBeenCalledWith("art_text");
  await waitFor(() => expect(summaryMock).toHaveBeenCalledTimes(2));
  expect(listArtifactsMock).toHaveBeenCalledTimes(1);
});

test("the window start stays put across re-renders (no refetch loop)", async () => {
  pinClock();
  const { rerender, props } = renderPage();
  await findRobotTile();
  vi.setSystemTime(new Date(2026, 10, 26, 12));
  rerender(<ArtifactsPage {...props} onOpenSidebar={vi.fn()} />);
  rerender(<ArtifactsPage {...props} onUseInThread={vi.fn()} />);
  await findRobotTile();
  expect(listArtifactsMock).toHaveBeenCalledTimes(1);
  expect(summaryMock).toHaveBeenCalledTimes(1);
});

test("a failed summary leaves the list; an expired session signs out", async () => {
  summaryMock.mockRejectedValueOnce(new Error("boom"));
  const first = render(<ArtifactsPage {...pageProps()} />);
  expect(await findRobotTile()).toBeInTheDocument();
  expect(screen.queryByRole("alert")).toBeNull();
  first.unmount();

  summaryMock.mockRejectedValueOnce(new api.AuthExpiredError());
  const { onSessionExpired } = renderPage();
  await waitFor(() => expect(onSessionExpired).toHaveBeenCalled());
});

test("a failed list says so; an expired session signs out", async () => {
  listArtifactsMock.mockRejectedValue(new Error("boom"));
  const first = render(<ArtifactsPage {...pageProps()} />);
  expect(await screen.findByRole("alert")).toHaveTextContent(
    "Artifacts failed to load.",
  );
  first.unmount();

  listArtifactsMock.mockRejectedValue(new api.AuthExpiredError());
  const { onSessionExpired } = renderPage();
  await waitFor(() => expect(onSessionExpired).toHaveBeenCalled());
  expect(screen.queryByRole("alert")).toBeNull();
});

test("the type filter switches between files, images and all", async () => {
  renderPage();
  await findRobotTile();
  for (const [label, type] of [
    ["Files", "files"],
    ["All", "all"],
  ] as const) {
    fireEvent.click(screen.getByRole("button", { name: label }));
    await waitFor(() => {
      expect(listArtifactsMock).toHaveBeenLastCalledWith(
        expect.objectContaining({ type }),
      );
    });
  }
});

test("failed rename and delete say so in the dialog; cancel closes it", async () => {
  const renameMock = vi
    .spyOn(api, "renameArtifact")
    .mockRejectedValue(new Error("boom"));
  deleteArtifactMock.mockRejectedValueOnce(new Error("boom"));
  listArtifactsMock.mockResolvedValue(page([textFile]));
  const { onSessionExpired } = renderPage();

  fireEvent.click(
    await screen.findByRole("button", { name: "Actions for notes.txt" }),
  );
  fireEvent.click(screen.getByRole("menuitem", { name: "Rename" }));
  const input = await screen.findByRole("textbox", {
    name: "Artifact filename",
  });
  fireEvent.change(input, { target: { value: "memo" } });
  fireEvent.submit(input.closest("form")!);
  expect(await screen.findByText("Rename failed.")).toBeInTheDocument();
  fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
  expect(
    screen.queryByRole("textbox", { name: "Artifact filename" }),
  ).toBeNull();

  fireEvent.click(
    screen.getByRole("button", { name: "Actions for notes.txt" }),
  );
  fireEvent.click(screen.getByRole("menuitem", { name: "Delete" }));
  const dialog = await screen.findByRole("dialog");
  fireEvent.click(within(dialog).getByRole("button", { name: "Delete" }));
  expect(await within(dialog).findByText("Delete failed.")).toBeInTheDocument();
  expect(
    screen.getByRole("button", { name: "Download notes.txt" }),
  ).toBeInTheDocument();
  fireEvent.click(within(dialog).getByRole("button", { name: "Cancel" }));
  expect(screen.queryByRole("dialog")).toBeNull();
  // A failed delete leaves the summary alone.
  expect(summaryMock).toHaveBeenCalledTimes(1);

  deleteArtifactMock.mockRejectedValueOnce(new api.AuthExpiredError());
  fireEvent.click(
    screen.getByRole("button", { name: "Actions for notes.txt" }),
  );
  fireEvent.click(screen.getByRole("menuitem", { name: "Delete" }));
  fireEvent.click(
    within(await screen.findByRole("dialog")).getByRole("button", {
      name: "Delete",
    }),
  );
  await waitFor(() => expect(onSessionExpired).toHaveBeenCalled());
  renameMock.mockRestore();
});

test("a failed download says so; an expired session signs out", async () => {
  listArtifactsMock.mockResolvedValue(page([textFile]));
  downloadArtifactMock.mockRejectedValueOnce(new Error("failed"));
  const props = renderPage();
  fireEvent.click(
    await screen.findByRole("button", { name: "Download notes.txt" }),
  );
  expect(await screen.findByText("Download failed")).toBeInTheDocument();
  expect(props.onSessionExpired).not.toHaveBeenCalled();

  downloadArtifactMock.mockRejectedValueOnce(new api.AuthExpiredError());
  fireEvent.click(screen.getByRole("button", { name: "Download notes.txt" }));
  await waitFor(() => expect(props.onSessionExpired).toHaveBeenCalled());
});

test("artifact rows fade dividers behind the rounded hover surface", async () => {
  listArtifactsMock.mockResolvedValue(page([textFile, artifact({})]));
  renderPage();
  await screen.findByText("notes.txt");

  const row = screen.getByText("quarterly-board-update.pdf").closest("li");
  const previousRow = screen.getByText("notes.txt").closest("li");
  expect(row).toHaveClass("border-[#343432]");
  expect(previousRow).toHaveClass("border-[#343432]");

  fireEvent.pointerEnter(row!);

  expect(row).toHaveClass("border-transparent");
  expect(previousRow).toHaveClass("border-transparent");
  const rowSurface = row?.querySelector(".ui-artifacts-row-surface");
  expect(rowSurface).toHaveClass("rounded-xl");
  expect(rowSurface).toHaveClass("bg-[#2a2a28]");

  fireEvent.pointerLeave(row!);
  expect(row).toHaveClass("border-[#343432]");
});

test("loads further pages via the infinite-scroll sentinel", async () => {
  const first = artifact({
    id: "art_first",
    displayFilename: "first.txt",
    mimeType: "text/plain",
    downloadUrl: "/api/artifacts/art_first/download",
  });
  const second = artifact({
    id: "art_second",
    displayFilename: "second.txt",
    mimeType: "text/plain",
    downloadUrl: "/api/artifacts/art_second/download",
  });
  listArtifactsMock
    .mockResolvedValueOnce(page([first], "cursor-1"))
    .mockResolvedValueOnce(page([second], null));

  renderPage();
  expect(await screen.findByText("first.txt")).toBeInTheDocument();
  expect(screen.queryByText("second.txt")).not.toBeInTheDocument();

  triggerIntersection();

  expect(await screen.findByText("second.txt")).toBeInTheDocument();
  expect(listArtifactsMock).toHaveBeenLastCalledWith(
    expect.objectContaining({ cursor: "cursor-1" }),
  );
});

test("a tile opens the full image; a plain file row downloads", async () => {
  listArtifactsMock.mockResolvedValue(page([robot, textFile]));
  renderPage();

  fireEvent.click(
    await screen.findByRole("button", { name: "Preview robot.png" }),
  );
  const dialog = await screen.findByRole("dialog", {
    name: "Preview robot.png",
  });
  expect(
    within(dialog).getByRole("img", { name: "robot.png" }),
  ).toHaveAttribute("src", "/api/artifacts/art_image/download");
  expect(downloadArtifactMock).not.toHaveBeenCalled();
  fireEvent.click(within(dialog).getByRole("button", { name: /close/i }));
  expect(screen.queryByRole("dialog")).toBeNull();

  fireEvent.click(screen.getByRole("button", { name: "Download notes.txt" }));
  await waitFor(() => {
    expect(downloadArtifactMock).toHaveBeenCalledWith(
      "/api/artifacts/art_text/download",
    );
  });
});

test("a PDF row previews on click, also on empty row space", async () => {
  listArtifactsMock.mockResolvedValue(page([artifact({})]));
  renderPage();
  await screen.findByText("quarterly-board-update.pdf");

  const rowSurface = screen
    .getByText("quarterly-board-update.pdf")
    .closest("li")
    ?.querySelector(".ui-artifacts-row-surface");
  fireEvent.click(rowSurface!);
  expect(
    await screen.findByRole("dialog", {
      name: "Preview quarterly-board-update.pdf",
    }),
  ).toBeInTheDocument();
  // The preview is rendered from the fetched blob, not the attachment-disposition URL.
  await waitFor(() => {
    expect(downloadArtifactMock).toHaveBeenCalledWith(
      "/api/artifacts/art_file/download",
    );
  });
});

test("a deleted PDF downloads instead of previewing", async () => {
  listArtifactsMock.mockResolvedValue(page([artifact({ deleted: true })]));
  renderPage();
  fireEvent.click(
    await screen.findByRole("button", {
      name: "Download quarterly-board-update.pdf",
    }),
  );
  await waitFor(() => {
    expect(downloadArtifactMock).toHaveBeenCalledWith(
      "/api/artifacts/art_file/download",
    );
  });
  expect(screen.queryByRole("dialog")).toBeNull();
});

test("downloads an artifact from the row actions menu", async () => {
  renderPage();
  await screen.findByText("quarterly-board-update.pdf");

  fireEvent.click(
    screen.getByRole("button", {
      name: "Actions for quarterly-board-update.pdf",
    }),
  );
  const items = screen.getAllByRole("menuitem");
  // "Download" is the first menu entry.
  expect(items[0]).toHaveTextContent("Download");

  fireEvent.click(items[0]);
  await waitFor(() => {
    expect(downloadArtifactMock).toHaveBeenCalledWith(
      "/api/artifacts/art_file/download",
    );
  });
  expect(screen.queryByRole("menu")).toBeNull();
});

test("a tile's menu closes on Escape and offers rename", async () => {
  const renameMock = vi.spyOn(api, "renameArtifact").mockResolvedValue();
  listArtifactsMock.mockResolvedValue(page([robot]));
  renderPage();
  const actions = await screen.findByRole("button", {
    name: "Actions for robot.png",
  });

  fireEvent.click(actions);
  expect(actions).toHaveAttribute("aria-expanded", "true");
  fireEvent.keyDown(document, { key: "Escape" });
  expect(screen.queryByRole("menu")).toBeNull();

  fireEvent.click(actions);
  fireEvent.click(screen.getByRole("menuitem", { name: "Rename" }));
  const input = await screen.findByRole("textbox", {
    name: "Artifact filename",
  });
  fireEvent.change(input, { target: { value: "android" } });
  fireEvent.submit(input.closest("form")!);
  expect(
    await screen.findByRole("button", { name: "Preview android.png" }),
  ).toBeInTheDocument();
  expect(renameMock).toHaveBeenCalledWith("art_image", "android.png");
  renameMock.mockRestore();
});

test('offers "Use in thread" for an image artifact and reports the chosen artifact', async () => {
  const props = renderPage();
  await screen.findByText("quarterly-board-update.pdf");

  fireEvent.click(
    screen.getByRole("button", { name: "Actions for robot.png" }),
  );
  const useInThread = screen.getByRole("menuitem", { name: "Use in thread" });
  fireEvent.click(useInThread);
  expect(props.onUseInThread).toHaveBeenCalledTimes(1);
  expect(props.onUseInThread).toHaveBeenCalledWith(
    expect.objectContaining({ id: "art_image" }),
  );
});

test('hides "Use in thread" for a non-image file artifact', async () => {
  renderPage();
  await screen.findByText("quarterly-board-update.pdf");

  fireEvent.click(
    screen.getByRole("button", {
      name: "Actions for quarterly-board-update.pdf",
    }),
  );
  expect(screen.getByRole("menuitem", { name: "Rename" })).toBeInTheDocument();
  expect(
    screen.queryByRole("menuitem", { name: "Use in thread" }),
  ).not.toBeInTheDocument();
});

test('hides "Use in thread" for a deleted image artifact', async () => {
  listArtifactsMock.mockResolvedValue(
    page([artifact({ ...robot, deleted: true })]),
  );
  renderPage();

  fireEvent.click(
    await screen.findByRole("button", { name: "Actions for robot.png" }),
  );
  expect(
    screen.queryByRole("menuitem", { name: "Use in thread" }),
  ).not.toBeInTheDocument();
});

test("a tile's menu opens its chat; an item without one has no entry", async () => {
  // On a desktop the tile's caption is hidden and its overlay inert, so the
  // menu is the way to the chat.
  listArtifactsMock.mockResolvedValue(
    page([{ ...robot, threadId: "thread_7", threadTitle: "Robots" }, textFile]),
  );
  const { onOpenThread } = renderPage();
  fireEvent.click(
    await screen.findByRole("button", { name: "Actions for robot.png" }),
  );
  fireEvent.click(screen.getByRole("menuitem", { name: "Open chat" }));
  expect(onOpenThread).toHaveBeenCalledWith("thread_7");
  expect(screen.queryByRole("menu")).toBeNull();

  fireEvent.click(
    screen.getByRole("button", { name: "Actions for notes.txt" }),
  );
  expect(screen.getByRole("menuitem", { name: "Rename" })).toBeInTheDocument();
  expect(screen.queryByRole("menuitem", { name: "Open chat" })).toBeNull();
});

test("a cleared search never comes back when typing resumes", async () => {
  renderPage();
  const box = await screen.findByRole("textbox", {
    name: "Search file names and chats",
  });
  fireEvent.change(box, { target: { value: "robot" } });
  await waitFor(() => {
    expect(listArtifactsMock).toHaveBeenLastCalledWith(
      expect.objectContaining({ search: "robot" }),
    );
  });
  // Cleared, then typed again before the debounce caught up with the clear.
  fireEvent.change(box, { target: { value: "" } });
  fireEvent.change(box, { target: { value: "x" } });
  await waitFor(() => {
    expect(listArtifactsMock).toHaveBeenLastCalledWith(
      expect.objectContaining({ search: "x" }),
    );
  });
  const searches = listArtifactsMock.mock.calls.filter(
    ([params]) => params?.search === "robot",
  );
  expect(searches).toHaveLength(1);
});

test("a failed summary never claims there are no artifacts", async () => {
  listArtifactsMock.mockResolvedValue(page([]));
  summaryMock.mockRejectedValue(new Error("boom"));
  renderPage();
  expect(
    await screen.findByText(
      "Nothing in the last 30 days. Older artifacts are reachable via search or a period.",
    ),
  ).toBeInTheDocument();
  expect(screen.queryByText("No artifacts yet.")).toBeNull();
  // Nothing known about older items: no year to offer.
  expect(screen.queryByRole("button", { name: /^Show \d{4}$/ })).toBeNull();

  // A type switch asks again.
  summaryMock.mockResolvedValue(summary());
  fireEvent.click(screen.getByRole("button", { name: "Images" }));
  expect(await screen.findByText("No artifacts yet.")).toBeInTheDocument();
});

test("a later page appends below the rows already on screen", async () => {
  const notes = { ...textFile, displayFilename: "a-notes.txt" };
  const late = {
    ...robot,
    id: "art_late",
    displayFilename: "a-late.png",
    downloadUrl: "/api/artifacts/art_late/download",
  };
  listArtifactsMock.mockImplementation(async (params) => {
    if (params?.search !== "a") return page([]);
    return params.cursor === "c1"
      ? page([late])
      : page([{ ...robot, displayFilename: "a-robot.png" }, notes], "c1");
  });
  renderPage();
  fireEvent.change(
    await screen.findByRole("textbox", { name: "Search file names and chats" }),
    { target: { value: "a" } },
  );
  const row = (await screen.findByText("a-notes.txt")).closest("li")!;
  triggerIntersection();
  const lateTile = (
    await screen.findByRole("button", { name: "Preview a-late.png" })
  ).closest("li")!;
  expect(
    row.compareDocumentPosition(lateTile) & Node.DOCUMENT_POSITION_FOLLOWING,
  ).toBeTruthy();
});

test("a month heading counts from the summary, not from what is loaded", async () => {
  pinClock();
  const dec = (id: string, day: number) =>
    artifact({
      id,
      displayFilename: `${id}.txt`,
      mimeType: "text/plain",
      modifiedAt: iso(2025, 11, day),
    });
  summaryMock.mockResolvedValue(
    summary(6, [
      { month: "2026-11", count: 1 },
      { month: "2025-12", count: 5 },
      { month: "2025-10", count: 1 },
    ]),
  );
  listArtifactsMock.mockImplementation(async (params) => {
    if (params?.since !== iso(2025, 0, 1)) return page([]);
    if (params.cursor === "c1") return page([dec("dec3", 3)]);
    return page([dec("dec9", 9), dec("dec8", 8)], "c1");
  });
  renderPage();
  fireEvent.click(await screen.findByRole("button", { name: "2025" }));
  const december = await screen.findByRole("region", {
    name: "December 2025",
  });
  // Two loaded, five in the month.
  expect(within(december).getByText("5 artifacts")).toBeInTheDocument();
});

test("a month the summary does not know shows no count until fully loaded", async () => {
  pinClock();
  summaryMock.mockResolvedValue(summary(1, [{ month: "2025-10", count: 1 }]));
  listArtifactsMock.mockImplementation(async (params) => {
    if (params?.since !== iso(2025, 0, 1)) return page([]);
    if (params.cursor === "c1") return page([]);
    return page(
      [
        artifact({
          id: "dec",
          displayFilename: "dec.txt",
          mimeType: "text/plain",
          modifiedAt: iso(2025, 11, 9),
        }),
      ],
      "c1",
    );
  });
  renderPage();
  fireEvent.click(await screen.findByRole("button", { name: "2025" }));
  const december = await screen.findByRole("region", {
    name: "December 2025",
  });
  expect(within(december).queryByText(/artifacts?$/)).toBeNull();
  triggerIntersection();
  expect(await within(december).findByText("1 artifact")).toBeInTheDocument();
});

test("a tab shown again on a later day moves the window", async () => {
  pinClock();
  renderPage();
  await findRobotTile();
  await waitFor(() => expect(summaryMock).toHaveBeenCalledTimes(1));

  // Shown again the same day: nothing moves.
  const visibility = vi
    .spyOn(document, "visibilityState", "get")
    .mockReturnValue("visible");
  vi.setSystemTime(new Date(2026, 10, 25, 23, 0));
  document.dispatchEvent(new Event("visibilitychange"));
  // Hidden on a later day: not yet.
  vi.setSystemTime(new Date(2026, 10, 26, 9, 0));
  visibility.mockReturnValue("hidden");
  document.dispatchEvent(new Event("visibilitychange"));
  await new Promise((resolve) => setTimeout(resolve, 20));
  expect(listArtifactsMock).toHaveBeenCalledTimes(1);
  expect(summaryMock).toHaveBeenCalledTimes(1);

  // Shown on a later day: the window starts a day later.
  visibility.mockReturnValue("visible");
  document.dispatchEvent(new Event("visibilitychange"));
  await waitFor(() => {
    expect(listArtifactsMock).toHaveBeenLastCalledWith({
      type: "all",
      since: iso(2026, 9, 27),
      limit: 500,
      cursor: null,
    });
  });
  expect(summaryMock).toHaveBeenLastCalledWith({
    since: iso(2026, 9, 27),
    type: "all",
  });
  expect(listArtifactsMock).toHaveBeenCalledTimes(2);
  visibility.mockRestore();
});

test("a foreground tab moves the window at local midnight", async () => {
  // Intervals fake too, so a tick is the test's to give; timeouts stay real
  // for findBy polling.
  vi.useFakeTimers({ toFake: ["Date", "setInterval", "clearInterval"] });
  vi.setSystemTime(new Date(2026, 10, 25, 23, 58, 30));
  const visibility = vi
    .spyOn(document, "visibilityState", "get")
    .mockReturnValue("visible");
  const setIntervalSpy = vi.spyOn(window, "setInterval");
  const clearIntervalSpy = vi.spyOn(window, "clearInterval");
  const { unmount } = render(<ArtifactsPage {...pageProps()} />);
  await findRobotTile();
  await waitFor(() => expect(summaryMock).toHaveBeenCalledTimes(1));
  // jsdom runs intervals of its own: the page's is the minute one.
  const tickIndex = setIntervalSpy.mock.calls.findIndex(
    ([, ms]) => ms === 60_000,
  );
  expect(tickIndex).toBeGreaterThanOrEqual(0);
  const tickID = setIntervalSpy.mock.results[tickIndex].value as number;

  // A tick on the same day moves nothing.
  act(() => vi.advanceTimersByTime(60_000));
  // Past midnight while hidden: not yet, the tab moves when shown.
  visibility.mockReturnValue("hidden");
  act(() => vi.advanceTimersByTime(60_000));
  await new Promise((resolve) => setTimeout(resolve, 20));
  expect(listArtifactsMock).toHaveBeenCalledTimes(1);
  expect(summaryMock).toHaveBeenCalledTimes(1);

  // In the foreground the next tick moves it a day.
  visibility.mockReturnValue("visible");
  act(() => vi.advanceTimersByTime(60_000));
  await waitFor(() => {
    expect(listArtifactsMock).toHaveBeenLastCalledWith({
      type: "all",
      since: iso(2026, 9, 27),
      limit: 500,
      cursor: null,
    });
  });
  // Later ticks that day leave it.
  act(() => vi.advanceTimersByTime(5 * 60_000));
  await new Promise((resolve) => setTimeout(resolve, 20));
  expect(listArtifactsMock).toHaveBeenCalledTimes(2);
  expect(summaryMock).toHaveBeenCalledTimes(2);

  expect(clearIntervalSpy).not.toHaveBeenCalledWith(tickID);
  unmount();
  expect(clearIntervalSpy).toHaveBeenCalledWith(tickID);
  setIntervalSpy.mockRestore();
  clearIntervalSpy.mockRestore();
  visibility.mockRestore();
});

// More than one request holds the window: a later page must not put its tiles
// above rows on screen, and the older note waits for the window's end.
test("a recent window in pages appends below and notes older ones at its end", async () => {
  pinClock();
  summaryMock.mockResolvedValue(summary(4, [{ month: "2025-03", count: 4 }]));
  const late = {
    ...robot,
    id: "art_late",
    displayFilename: "late.png",
    modifiedAt: daysAgo(0, 8),
    downloadUrl: "/api/artifacts/art_late/download",
  };
  listArtifactsMock.mockImplementation(async (params) =>
    params?.cursor === "c1"
      ? page([late])
      : page([{ ...textFile, modifiedAt: daysAgo(0, 9) }], "c1"),
  );
  renderPage();
  const row = (await screen.findByText("notes.txt")).closest("li")!;
  await waitFor(() => expect(summaryMock).toHaveBeenCalled());
  expect(screen.queryByText(/not loaded/)).toBeNull();

  triggerIntersection();
  const lateTile = (
    await screen.findByRole("button", { name: "Preview late.png" })
  ).closest("li")!;
  // Both in Today, the late tile below the row, also now that all is in.
  const today = screen.getByRole("region", { name: "Today" });
  expect(today).toContainElement(lateTile);
  expect(
    row.compareDocumentPosition(lateTile) & Node.DOCUMENT_POSITION_FOLLOWING,
  ).toBeTruthy();
  expect(
    await screen.findByText(
      "4 older artifacts are not loaded. Find them via search or a period.",
    ),
  ).toBeInTheDocument();
});

test("in search results a file row tells its age on phones too", async () => {
  const withChat = artifact({
    id: "art_chat",
    displayFilename: "budget.txt",
    mimeType: "text/plain",
    threadId: "thread_9",
    threadTitle: "Budget talk",
  });
  listArtifactsMock.mockResolvedValue(page([withChat, textFile]));
  renderPage();
  const secondaryOf = (name: string) =>
    screen
      .getByText(name)
      .closest("li")!
      .querySelector<HTMLElement>(".ui-artifacts-row-secondary")!;
  // In a group its heading tells the age.
  await screen.findByText("budget.txt");
  expect(secondaryOf("budget.txt")).toHaveTextContent(
    /^from chat Budget talk$/,
  );

  fireEvent.change(
    screen.getByRole("textbox", { name: "Search file names and chats" }),
    { target: { value: "b" } },
  );
  await waitFor(() => {
    expect(listArtifactsMock).toHaveBeenLastCalledWith(
      expect.objectContaining({ search: "b" }),
    );
  });
  await waitFor(() => {
    expect(secondaryOf("budget.txt")).toHaveTextContent(
      /^3 hours ago · from chat Budget talk$/,
    );
  });
  // Only below sm: wider screens have the age column.
  const chatAge = within(secondaryOf("budget.txt")).getByText("3 hours ago");
  expect(chatAge).toHaveClass("sm:hidden");
  // A row without a chat tells it before its type.
  const typeLine = secondaryOf("notes.txt");
  expect(typeLine).toHaveTextContent(/^3 hours ago · text\/plain$/);
  expect(within(typeLine).getByText("3 hours ago")).toHaveClass("sm:hidden");
});

test("a later day leaves a year view's loaded pages alone", async () => {
  pinClock();
  summaryMock.mockResolvedValue(summary(1, [{ month: "2025-10", count: 1 }]));
  renderPage();
  fireEvent.click(await screen.findByRole("button", { name: "2025" }));
  await waitFor(() => {
    expect(listArtifactsMock).toHaveBeenLastCalledWith(
      expect.objectContaining({ since: iso(2025, 0, 1) }),
    );
  });
  const listCalls = listArtifactsMock.mock.calls.length;
  const visibility = vi
    .spyOn(document, "visibilityState", "get")
    .mockReturnValue("visible");
  vi.setSystemTime(new Date(2026, 10, 26, 9, 0));
  document.dispatchEvent(new Event("visibilitychange"));
  // The summary still counts against the moved window...
  await waitFor(() => {
    expect(summaryMock).toHaveBeenLastCalledWith({
      since: iso(2026, 9, 27),
      type: "all",
    });
  });
  // ...but the year's list is not reloaded.
  expect(listArtifactsMock).toHaveBeenCalledTimes(listCalls);
  visibility.mockRestore();
});
