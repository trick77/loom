import "@testing-library/jest-dom/vitest";
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, expect, test, vi } from "vitest";

import { ArtifactsPage } from "./ArtifactsPage";
import * as api from "../api";
import type { Artifact, Page } from "../api";

vi.mock("../api", async () => {
  const actual = await vi.importActual<typeof import("../api")>("../api");
  return {
    ...actual,
    downloadArtifact: vi.fn(),
    listArtifacts: vi.fn(),
  };
});

const listArtifactsMock = vi.mocked(api.listArtifacts);
const downloadArtifactMock = vi.mocked(api.downloadArtifact);

// Captures the live IntersectionObserver callbacks so tests can simulate the
// sentinel scrolling into view and triggering a "load more".
let intersectionCallbacks: Array<(entries: Array<{ isIntersecting: boolean }>) => void> = [];

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

function page(items: Artifact[], nextCursor: string | null = null): Page<Artifact> {
  return { items, nextCursor };
}

function artifact(overrides: Partial<Artifact>): Artifact {
  return {
    id: "art_file",
    threadId: "thread_1",
    displayFilename: "quarterly-board-update.pdf",
    mimeType: "application/pdf",
    sizeBytes: 1_400_000,
    modifiedAt: "2026-06-10T12:00:00Z",
    downloadUrl: "/api/artifacts/art_file/download",
    ...overrides,
  };
}

const robot = artifact({
  id: "art_image",
  displayFilename: "robot.png",
  mimeType: "image/png",
  sizeBytes: 842 * 1024,
  modifiedAt: "2026-06-10T14:52:00Z",
  downloadUrl: "/api/artifacts/art_image/download",
});

function renderPage() {
  const props = {
    onOpenSidebar: vi.fn(),
    onSessionExpired: vi.fn(),
  };
  render(<ArtifactsPage {...props} />);
  return props;
}

beforeEach(() => {
  intersectionCallbacks = [];
  vi.stubGlobal("IntersectionObserver", MockIntersectionObserver);
  listArtifactsMock.mockResolvedValue(page([robot, artifact({})]));
  downloadArtifactMock.mockResolvedValue(new Blob(["image-bytes"], { type: "image/png" }));
});

afterEach(() => {
  vi.clearAllMocks();
  vi.unstubAllGlobals();
});

test("renders artifacts with chats-page controls and default modified descending sort", async () => {
  renderPage();

  expect(await screen.findByRole("heading", { name: "Artifacts" })).toBeInTheDocument();
  expect(screen.getByRole("textbox", { name: "Search filenames" })).toHaveClass("slopr-composer-text");
  expect(screen.getByRole("button", { name: "All" })).toHaveAttribute("aria-pressed", "true");
  expect(screen.getByText("robot.png")).toBeInTheDocument();
  expect(screen.getByText("842 KB")).toBeInTheDocument();
  expect(screen.getByText("1.3 MB")).toBeInTheDocument();
  expect(listArtifactsMock).toHaveBeenCalledWith({
    type: "all",
    sort: "modified",
    order: "desc",
    search: "",
    limit: 50,
    cursor: null,
  });
});

test("requests the right server-side filter/sort/search params", async () => {
  renderPage();
  await screen.findByText("robot.png");

  fireEvent.click(screen.getByRole("button", { name: "Images" }));
  await waitFor(() => {
    expect(listArtifactsMock).toHaveBeenLastCalledWith(
      expect.objectContaining({ type: "images", sort: "modified", order: "desc" }),
    );
  });

  fireEvent.click(screen.getByRole("button", { name: "Name" }));
  await waitFor(() => {
    expect(listArtifactsMock).toHaveBeenLastCalledWith(
      expect.objectContaining({ type: "images", sort: "name", order: "asc" }),
    );
  });

  fireEvent.change(screen.getByRole("textbox", { name: "Search filenames" }), {
    target: { value: "robot" },
  });
  await waitFor(() => {
    expect(listArtifactsMock).toHaveBeenLastCalledWith(expect.objectContaining({ search: "robot" }));
  });
});

test("renders rows in the order the server returns (no client re-sort)", async () => {
  const alpha = artifact({
    id: "art_alpha",
    displayFilename: "alpha.pdf",
    sizeBytes: 42 * 1024,
    downloadUrl: "/api/artifacts/art_alpha/download",
  });
  // The server owns ordering; reflect that by sorting per the requested params.
  listArtifactsMock.mockImplementation(async (params) => {
    const items = [robot, alpha];
    if (params?.sort === "name") {
      return page(
        [...items].sort((a, b) => a.displayFilename.localeCompare(b.displayFilename)),
      );
    }
    return page(items);
  });
  renderPage();
  await screen.findByText("robot.png");

  fireEvent.click(screen.getByRole("button", { name: "Name" }));

  await waitFor(() => {
    const rowText = [...document.querySelectorAll("li")].map((row) => row.textContent ?? "");
    expect(rowText[0]).toContain("alpha.pdf");
    expect(rowText[1]).toContain("robot.png");
  });
});

test("reflects server-side type filtering", async () => {
  const alpha = artifact({
    id: "art_alpha",
    displayFilename: "alpha.pdf",
    sizeBytes: 42 * 1024,
    downloadUrl: "/api/artifacts/art_alpha/download",
  });
  listArtifactsMock.mockImplementation(async (params) => {
    let items = [robot, alpha];
    if (params?.type === "images") {
      items = items.filter((item) => item.mimeType.startsWith("image/"));
    }
    return page(items);
  });
  renderPage();
  await screen.findByText("robot.png");

  fireEvent.click(screen.getByRole("button", { name: "Images" }));

  await waitFor(() => {
    expect(screen.getByText("robot.png")).toBeInTheDocument();
    expect(screen.queryByText("alpha.pdf")).not.toBeInTheDocument();
  });
});

test("keeps row metadata on the filename line", async () => {
  renderPage();
  await screen.findByText("robot.png");

  const row = screen.getByText("quarterly-board-update.pdf").closest("li");
  expect(row).not.toBeNull();
  expect(row?.querySelector(".slopr-artifacts-row-primary")).toHaveTextContent(
    "quarterly-board-update.pdf",
  );
  // Relative timestamps are clock-dependent; assert a relative label is present.
  expect(row?.querySelector(".slopr-artifacts-row-primary")).toHaveTextContent(/ago/);
  expect(row?.querySelector(".slopr-artifacts-row-primary")).toHaveTextContent("1.3 MB");
  expect(row?.querySelector(".slopr-artifacts-row-secondary")).toHaveTextContent("application/pdf");
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

test("opens image previews and downloads file rows", async () => {
  renderPage();

  expect(
    await screen.findByRole("img", { name: "robot.png thumbnail" }),
  ).toHaveAttribute("src", "/api/artifacts/art_image/download");
  expect(downloadArtifactMock).not.toHaveBeenCalled();

  fireEvent.click(await screen.findByRole("button", { name: "Preview robot.png" }));
  const dialog = await screen.findByRole("dialog", { name: "Preview robot.png" });
  expect(within(dialog).getByRole("img", { name: "robot.png" })).toHaveAttribute(
    "src",
    "/api/artifacts/art_image/download",
  );

  fireEvent.click(screen.getByRole("button", { name: "Download quarterly-board-update.pdf" }));
  await waitFor(() => {
    expect(downloadArtifactMock).toHaveBeenCalledWith("/api/artifacts/art_file/download");
  });
});

test("opens image previews when clicking empty row space", async () => {
  renderPage();

  expect(await screen.findByRole("img", { name: "robot.png thumbnail" })).toBeInTheDocument();
  const row = screen.getByText("robot.png").closest("li");
  expect(row).not.toBeNull();
  const rowSurface = row?.querySelector(".slopr-artifacts-row-surface");
  expect(rowSurface).not.toBeNull();
  fireEvent.click(rowSurface!);

  expect(await screen.findByRole("dialog", { name: "Preview robot.png" })).toBeInTheDocument();
});
