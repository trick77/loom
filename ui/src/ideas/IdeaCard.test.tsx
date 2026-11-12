import "@testing-library/jest-dom/vitest";
import {
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import { beforeEach, expect, test, vi } from "vitest";

import * as api from "../api";
import type { IdeaCardData } from "../api";
import { makeIdea } from "../test/ideas";
import { forgetCardOutcomes, IdeaCard } from "./IdeaCard";

vi.mock("../api", async () => {
  const actual = await vi.importActual<typeof import("../api")>("../api");
  return {
    ...actual,
    confirmIdea: vi.fn(),
    deleteIdea: vi.fn(),
    restoreIdea: vi.fn(),
    updateIdea: vi.fn(),
    getIdea: vi.fn(),
  };
});

const card = (over: Partial<IdeaCardData> = {}): IdeaCardData => ({
  ideaId: "i1",
  action: "save",
  state: "proposed",
  title: "Vibe Kanban",
  reason: "Lab week topic",
  dueOn: null,
  remindOn: null,
  ...over,
});

beforeEach(() => {
  forgetCardOutcomes();
  vi.clearAllMocks();
});

// Rule 1: nothing is saved until the user clicks; the card shows in full
// what the click commits, as plain text.
test("a save card asks first, then saves on the click", async () => {
  vi.mocked(api.confirmIdea).mockResolvedValue(makeIdea({ state: "open" }));
  const { container } = render(
    <IdeaCard
      card={card({
        title: "**Bold** <img src=x>",
        reason: "[link](https://evil.example)",
      })}
    />,
  );
  expect(screen.getByText("Save this idea?")).toBeInTheDocument();
  expect(screen.getByText("**Bold** <img src=x>")).toBeInTheDocument();
  expect(container.querySelector("img, a, strong")).toBeNull();
  expect(api.confirmIdea).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole("button", { name: "Save idea" }));
  expect(await screen.findByText("Idea saved")).toBeInTheDocument();
  expect(api.confirmIdea).toHaveBeenCalledWith("i1");
  expect(screen.getByRole("button", { name: "View" })).toBeInTheDocument();
});

test("dismiss discards the proposal", async () => {
  vi.mocked(api.deleteIdea).mockResolvedValue();
  render(<IdeaCard card={card()} />);
  fireEvent.click(screen.getByRole("button", { name: "Dismiss" }));
  expect(await screen.findByText("Idea not saved")).toBeInTheDocument();
});

// The click's outcome outlives the card: the stored answer that replaces
// the streamed one shows it, not the question again.
test("an outcome survives the card being replaced", async () => {
  vi.mocked(api.confirmIdea).mockResolvedValue(makeIdea({ state: "open" }));
  const { unmount } = render(<IdeaCard card={card()} />);
  fireEvent.click(screen.getByRole("button", { name: "Save idea" }));
  await screen.findByText("Idea saved");
  unmount();
  render(<IdeaCard card={card()} />);
  expect(screen.getByText("Idea saved")).toBeInTheDocument();
});

test("a second click on a handled proposal reads as gone, not failed", async () => {
  vi.mocked(api.confirmIdea).mockRejectedValue(new api.IdeaGoneError());
  render(<IdeaCard card={card()} />);
  fireEvent.click(screen.getByRole("button", { name: "Save idea" }));
  expect(
    await screen.findByText("This idea is no longer there"),
  ).toBeInTheDocument();
  expect(screen.queryByText("That did not work. Try again.")).toBeNull();
});

test("a remove card deletes only on the click, undo restores", async () => {
  vi.mocked(api.deleteIdea).mockResolvedValue();
  vi.mocked(api.restoreIdea).mockResolvedValue(makeIdea({ state: "open" }));
  render(<IdeaCard card={card({ action: "remove", state: "open" })} />);
  expect(screen.getByText("Delete this idea?")).toBeInTheDocument();
  fireEvent.click(screen.getByRole("button", { name: "Delete" }));
  expect(await screen.findByText("Idea deleted")).toBeInTheDocument();
  expect(screen.getByText("Vibe Kanban")).toHaveClass("line-through");
  fireEvent.click(screen.getByRole("button", { name: "Undo" }));
  await waitFor(() => expect(api.restoreIdea).toHaveBeenCalledWith("i1"));
});

test("keep changes nothing on the server and outlives the streamed card", () => {
  const { unmount } = render(
    <IdeaCard card={card({ action: "remove", state: "open" })} />,
  );
  fireEvent.click(screen.getByRole("button", { name: "Keep" }));
  expect(screen.getByText("Kept the idea")).toBeInTheDocument();
  expect(api.deleteIdea).not.toHaveBeenCalled();
  unmount();
  render(<IdeaCard card={card({ action: "remove", state: "open" })} />);
  expect(screen.getByText("Kept the idea")).toBeInTheDocument();
});

test("a done card marks done on the click", async () => {
  vi.mocked(api.updateIdea).mockResolvedValue(makeIdea({ state: "done" }));
  render(<IdeaCard card={card({ action: "done", state: "open" })} />);
  fireEvent.click(screen.getByRole("button", { name: "Mark as done" }));
  expect(await screen.findByText("Marked done")).toBeInTheDocument();
  expect(api.updateIdea).toHaveBeenCalledWith("i1", { done: true });
});

test("a gone idea is a quiet line", () => {
  render(<IdeaCard card={card({ action: "remove", state: "gone" })} />);
  expect(screen.getByText("This idea is no longer there")).toBeInTheDocument();
  expect(screen.queryByRole("button")).toBeNull();
});

// A click's outcome holds only while the card still shows the state it was
// clicked in: a reload with a newer state (changed on the ideas page) wins.
test("a newer server state replaces a kept click", async () => {
  vi.mocked(api.confirmIdea).mockResolvedValue(makeIdea({ state: "open" }));
  const { rerender } = render(<IdeaCard card={card()} />);
  fireEvent.click(screen.getByRole("button", { name: "Save idea" }));
  await screen.findByText("Idea saved");
  rerender(<IdeaCard card={card({ state: "deleted" })} />);
  expect(screen.getByText("Idea deleted")).toBeInTheDocument();
});

test("an edit shows its fields, dates too", async () => {
  vi.mocked(api.getIdea).mockResolvedValue(
    makeIdea({ id: "i1", title: "Vibe Kanban", reason: "Lab week topic" }),
  );
  vi.mocked(api.updateIdea).mockResolvedValue(
    makeIdea({ id: "i1", title: "Renamed", dueOn: "2099-03-01" }),
  );
  render(<IdeaCard card={card({ state: "open" })} />);
  fireEvent.click(screen.getByRole("button", { name: "Edit" }));
  const dialog = await screen.findByRole("dialog");
  fireEvent.click(within(dialog).getByRole("button", { name: "Save" }));
  expect(await screen.findByText("Renamed")).toBeInTheDocument();
  expect(screen.getByText(/Due Mar 1/)).toBeInTheDocument();
});
