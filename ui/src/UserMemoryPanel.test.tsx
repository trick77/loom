import "@testing-library/jest-dom/vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { expect, test, vi } from "vitest";

import { UserMemoryPanel } from "./UserMemoryPanel";
import * as api from "./api";

vi.mock("./api", async () => {
  const actual = await vi.importActual<typeof import("./api")>("./api");
  return {
    ...actual,
    getUserMemory: vi.fn(),
    refreshUserMemory: vi.fn(),
  };
});

const getUserMemoryMock = vi.mocked(api.getUserMemory);
const refreshUserMemoryMock = vi.mocked(api.refreshUserMemory);

test("shows the empty state when there is no memory yet", async () => {
  getUserMemoryMock.mockResolvedValue({ content: "", updatedAt: null });

  render(<UserMemoryPanel />);

  expect(await screen.findByText(/Memory will show here after a few chats/)).toBeInTheDocument();
});

test("renders the discrete facts as a list, stripping bullet markers", async () => {
  getUserMemoryMock.mockResolvedValue({
    content: "- Works at Acme\n- Lives in Zurich",
    updatedAt: "2026-06-11T00:00:00Z",
  });

  render(<UserMemoryPanel />);

  const items = await screen.findAllByRole("listitem");
  expect(items).toHaveLength(2);
  expect(items[0]).toHaveTextContent("Works at Acme");
  expect(items[1]).toHaveTextContent("Lives in Zurich");
  // Bullet markers from the stored content are not rendered as text.
  expect(items[0]).not.toHaveTextContent("- Works");
});

test("refresh button rebuilds the memory", async () => {
  getUserMemoryMock.mockResolvedValue({ content: "", updatedAt: null });
  refreshUserMemoryMock.mockResolvedValue({
    content: "- Lives in Bern",
    updatedAt: "2026-06-11T00:00:00Z",
  });

  render(<UserMemoryPanel />);
  await screen.findByText(/Memory will show here/);

  fireEvent.click(screen.getByRole("button", { name: "Refresh" }));

  await waitFor(() => expect(refreshUserMemoryMock).toHaveBeenCalled());
  expect(await screen.findByText("Lives in Bern")).toBeInTheDocument();
});

test("shows an error when refresh fails", async () => {
  getUserMemoryMock.mockResolvedValue({ content: "", updatedAt: null });
  refreshUserMemoryMock.mockRejectedValue(new Error("502"));

  render(<UserMemoryPanel />);
  await screen.findByText(/Memory will show here/);

  fireEvent.click(screen.getByRole("button", { name: "Refresh" }));

  expect(await screen.findByRole("alert")).toHaveTextContent(/Couldn.t refresh memory/);
});
