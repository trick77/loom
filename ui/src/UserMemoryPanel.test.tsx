import "@testing-library/jest-dom/vitest";
import { render, screen } from "@testing-library/react";
import { expect, test, vi } from "vitest";

import { UserMemoryPanel } from "./UserMemoryPanel";
import * as api from "./api";
import { ICONS } from "./chat/Icon";

vi.mock("./api", async () => {
  const actual = await vi.importActual<typeof import("./api")>("./api");
  return {
    ...actual,
    getUserMemory: vi.fn(),
  };
});

const getUserMemoryMock = vi.mocked(api.getUserMemory);

test("shows the empty state when there is no memory yet", async () => {
  getUserMemoryMock.mockResolvedValue({ content: "", updatedAt: null });

  render(<UserMemoryPanel />);

  expect(screen.getByRole("region", { name: "Memories" })).toBeInTheDocument();
  const heading = screen.getByRole("heading", { name: "Memories" });
  expect(heading).toBeInTheDocument();
  expect(heading).toHaveTextContent(ICONS.memory);
  expect(await screen.findByText(/Memories will show here after a few chats/)).toBeInTheDocument();
  expect(screen.queryByText("Memory")).not.toBeInTheDocument();
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

test("does not show a manual refresh action", async () => {
  getUserMemoryMock.mockResolvedValue({ content: "", updatedAt: null });

  render(<UserMemoryPanel />);

  await screen.findByText(/Memories will show here/);
  expect(screen.queryByRole("button", { name: /refresh/i })).not.toBeInTheDocument();
});
