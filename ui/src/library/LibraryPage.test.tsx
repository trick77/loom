import "@testing-library/jest-dom/vitest";
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, test, vi } from "vitest";

import { LibraryPage } from "./LibraryPage";
import * as api from "../api";
import type { Artifact } from "../api";

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

function renderPage() {
  const props = {
    onOpenSidebar: vi.fn(),
    onSessionExpired: vi.fn(),
  };
  render(<LibraryPage {...props} />);
  return props;
}

beforeEach(() => {
  listArtifactsMock.mockResolvedValue([
    artifact({
      id: "art_image",
      displayFilename: "robot.png",
      mimeType: "image/png",
      sizeBytes: 842 * 1024,
      modifiedAt: "2026-06-10T14:52:00Z",
      downloadUrl: "/api/artifacts/art_image/download",
    }),
    artifact({}),
  ]);
  downloadArtifactMock.mockResolvedValue(new Blob(["image-bytes"], { type: "image/png" }));
});

afterEach(() => {
  vi.clearAllMocks();
});

test("renders artifacts with chats-page controls and default modified descending sort", async () => {
  renderPage();

  expect(await screen.findByRole("heading", { name: "Library" })).toBeInTheDocument();
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
    limit: 1000,
  });
});

test("filters and sorts the library", async () => {
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

test("sort header clicks visibly reorder rows", async () => {
  listArtifactsMock.mockResolvedValue([
    artifact({
      id: "art_image",
      displayFilename: "robot.png",
      mimeType: "image/png",
      sizeBytes: 842 * 1024,
      modifiedAt: "2026-06-10T14:52:00Z",
      downloadUrl: "/api/artifacts/art_image/download",
    }),
    artifact({
      id: "art_alpha",
      displayFilename: "alpha.pdf",
      sizeBytes: 42 * 1024,
      modifiedAt: "2026-06-10T12:00:00Z",
      downloadUrl: "/api/artifacts/art_alpha/download",
    }),
  ]);
  renderPage();
  await screen.findByText("robot.png");

  fireEvent.click(screen.getByRole("button", { name: "Name" }));

  await waitFor(() => {
    const rowText = [...document.querySelectorAll("li")].map((row) => row.textContent ?? "");
    expect(rowText[0]).toContain("alpha.pdf");
    expect(rowText[1]).toContain("robot.png");
  });
});

test("filter clicks visibly narrow rows", async () => {
  listArtifactsMock.mockResolvedValue([
    artifact({
      id: "art_image",
      displayFilename: "robot.png",
      mimeType: "image/png",
      sizeBytes: 842 * 1024,
      modifiedAt: "2026-06-10T14:52:00Z",
      downloadUrl: "/api/artifacts/art_image/download",
    }),
    artifact({
      id: "art_alpha",
      displayFilename: "alpha.pdf",
      sizeBytes: 42 * 1024,
      modifiedAt: "2026-06-10T12:00:00Z",
      downloadUrl: "/api/artifacts/art_alpha/download",
    }),
  ]);
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
  expect(row?.querySelector(".slopr-library-row-primary")).toHaveTextContent(
    "quarterly-board-update.pdf",
  );
  expect(row?.querySelector(".slopr-library-row-primary")).toHaveTextContent("2 hours ago");
  expect(row?.querySelector(".slopr-library-row-primary")).toHaveTextContent("1.3 MB");
  expect(row?.querySelector(".slopr-library-row-secondary")).toHaveTextContent("application/pdf");
});

test("shows a limit hint when the capped artifact list is full", async () => {
  listArtifactsMock.mockResolvedValue(
    Array.from({ length: 1000 }, (_, index) =>
      artifact({
        id: `art_${index}`,
        displayFilename: `artifact-${index}.txt`,
        sizeBytes: 1024 + index,
        downloadUrl: `/api/artifacts/art_${index}/download`,
      }),
    ),
  );

  renderPage();

  expect(await screen.findByText("artifact-0.txt")).toBeInTheDocument();
  expect(screen.getByText("Showing the latest 1000 artifacts.")).toBeInTheDocument();
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
  const rowSurface = row?.querySelector(".slopr-library-row-surface");
  expect(rowSurface).not.toBeNull();
  fireEvent.click(rowSurface!);

  expect(await screen.findByRole("dialog", { name: "Preview robot.png" })).toBeInTheDocument();
});
