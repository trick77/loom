import "@testing-library/jest-dom/vitest";
import { render, screen, within } from "@testing-library/react";
import { beforeEach, expect, test, vi } from "vitest";

import { UserMemoryPanel } from "./UserMemoryPanel";
import * as api from "./api";
import { ICONS } from "./chat/Icon";

vi.mock("./api", async () => {
  const actual = await vi.importActual<typeof import("./api")>("./api");
  return {
    ...actual,
    getUserFacts: vi.fn(),
    getUserDirectives: vi.fn(),
  };
});

const getUserFactsMock = vi.mocked(api.getUserFacts);
const getUserDirectivesMock = vi.mocked(api.getUserDirectives);

function fact(
  id: string,
  content: string,
  bucket: api.UserFactBucket,
  category: api.UserFact["category"] = "work",
): api.UserFact {
  const now = new Date().toISOString();
  return {
    id,
    content,
    bucket,
    category,
    durable: bucket === "long_term",
    firstSeen: now,
    lastSeen: now,
  };
}

beforeEach(() => {
  getUserDirectivesMock.mockResolvedValue([]);
});

test("shows the empty state when there are no facts yet", async () => {
  getUserFactsMock.mockResolvedValue([]);

  render(<UserMemoryPanel />);

  expect(screen.getByRole("region", { name: "Memories" })).toBeInTheDocument();
  const heading = screen.getByRole("heading", { name: "Memories" });
  expect(heading).toHaveTextContent(ICONS.memory);
  expect(
    await screen.findByText(/Memories will show here after a few threads/),
  ).toBeInTheDocument();
});

test("groups facts under their time bucket in server order", async () => {
  getUserFactsMock.mockResolvedValue([
    fact("l", "Lives Zurich", "long_term", "personal"),
    fact("t", "Shipping v2", "top_of_mind"),
    fact("r", "Started climbing", "recent", "interest"),
    fact("e", "Old project", "earlier"),
  ]);

  render(<UserMemoryPanel />);

  const headings = await screen.findAllByRole("heading", { level: 3 });
  expect(headings.map((h) => h.textContent)).toEqual([
    "Long-term background",
    "Top of mind",
    "Recent months",
    "Earlier context",
  ]);
  const items = screen.getAllByRole("listitem");
  expect(items[0]).toHaveTextContent("Lives Zurich");
  expect(within(items[0]).getByText(/Personal · last mentioned/)).toBeInTheDocument();
  expect(items[2]).toHaveTextContent("Started climbing");
  expect(within(items[2]).getByText(/Interests/)).toBeInTheDocument();
});

test("is read-only: no delete, edit, forget or refresh controls", async () => {
  getUserFactsMock.mockResolvedValue([fact("t", "Shipping v2", "top_of_mind")]);

  render(<UserMemoryPanel />);

  await screen.findByText("Shipping v2");
  expect(screen.queryByRole("button")).not.toBeInTheDocument();
  expect(screen.getByText(/tell Loom in chat/)).toBeInTheDocument();
});

test("shows the user's standing instructions read-only", async () => {
  getUserFactsMock.mockResolvedValue([]);
  getUserDirectivesMock.mockResolvedValue([
    {
      id: "d1",
      content: "Always answer in metric units",
      createdAt: "",
      updatedAt: "",
    },
    { id: "d2", content: "Call me Jan", createdAt: "", updatedAt: "" },
  ]);

  render(<UserMemoryPanel />);

  expect(
    await screen.findByRole("region", { name: "Other instructions" }),
  ).toBeInTheDocument();
  expect(screen.getByText("Always answer in metric units")).toBeInTheDocument();
  expect(screen.getByText("Call me Jan")).toBeInTheDocument();
  expect(screen.queryByRole("button")).not.toBeInTheDocument();
});

test("shows the directives empty state when there are none", async () => {
  getUserFactsMock.mockResolvedValue([]);

  render(<UserMemoryPanel />);

  expect(
    await screen.findByText(/No saved instructions yet/),
  ).toBeInTheDocument();
});

test("shows the empty state when loading the facts fails", async () => {
  getUserFactsMock.mockRejectedValue(new Error("boom"));

  render(<UserMemoryPanel />);

  expect(
    await screen.findByText(/Memories will show here after a few threads/),
  ).toBeInTheDocument();
});
