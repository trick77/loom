import "@testing-library/jest-dom/vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { expect, test, vi } from "vitest";

import { ProjectMemoryPanel } from "./ProjectMemoryPanel";
import * as api from "../api";

vi.mock("../api", async () => {
  const actual = await vi.importActual<typeof import("../api")>("../api");
  return {
    ...actual,
    getProjectMemory: vi.fn(),
    refreshProjectMemory: vi.fn(),
  };
});

const getProjectMemoryMock = vi.mocked(api.getProjectMemory);
const refreshProjectMemoryMock = vi.mocked(api.refreshProjectMemory);

test("shows the empty state when there is no memory yet", async () => {
  getProjectMemoryMock.mockResolvedValue({ projectId: "p1", content: "", updatedAt: null });

  render(<ProjectMemoryPanel projectId="p1" />);

  expect(await screen.findByText(/Project memory will show here after a few chats/)).toBeInTheDocument();
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

test("refresh button rebuilds the memory", async () => {
  getProjectMemoryMock.mockResolvedValue({ projectId: "p1", content: "", updatedAt: null });
  refreshProjectMemoryMock.mockResolvedValue({
    projectId: "p1",
    content: "Travel month: June",
    updatedAt: "2026-06-11T00:00:00Z",
  });

  render(<ProjectMemoryPanel projectId="p1" />);
  await screen.findByText(/Project memory will show here/);

  fireEvent.click(screen.getByRole("button", { name: "Refresh" }));

  await waitFor(() => expect(refreshProjectMemoryMock).toHaveBeenCalledWith("p1"));
  expect(await screen.findByText("Travel month: June")).toBeInTheDocument();
});
