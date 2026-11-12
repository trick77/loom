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

import * as api from "../api";
import { makeIdea } from "../test/ideas";
import { IdeasPage, UNDO_MS } from "./IdeasPage";

vi.mock("../api", async () => {
  const actual = await vi.importActual<typeof import("../api")>("../api");
  return {
    ...actual,
    listIdeas: vi.fn(),
    countIdeas: vi.fn(),
    getIdea: vi.fn(),
    createIdea: vi.fn(),
    updateIdea: vi.fn(),
    deleteIdea: vi.fn(),
    restoreIdea: vi.fn(),
  };
});

const props = () => ({
  initialIdeaID: null as string | null,
  onOpenSidebar: vi.fn(),
  onStartThread: vi.fn(),
  onOpenMessage: vi.fn(),
  onSessionExpired: vi.fn(),
});

const kanban = makeIdea();
const rye = makeIdea({
  id: "i2",
  title: "Rye at 30 %",
  reason: "The last loaf was too mild.",
  excerpt: "",
  threadId: null,
  messageId: null,
  threadTitle: "",
  createdAt: new Date(Date.now() - 49 * 24 * 3600 * 1000).toISOString(),
});
const passkeys = makeIdea({
  id: "i3",
  title: "Passkeys instead of OIDC",
  threadId: null,
  messageId: null,
  threadTitle: "Passkeys vs. OIDC",
  threadDeleted: true,
  dueOn: "2099-01-12",
  remindOn: "2099-01-05",
});

beforeEach(() => {
  vi.mocked(api.listIdeas).mockResolvedValue([kanban, rye, passkeys]);
  vi.mocked(api.countIdeas).mockResolvedValue({ open: 3, done: 1 });
});

afterEach(() => {
  vi.clearAllMocks();
  vi.useRealTimers();
});

test("lists ideas with reason, source and age", async () => {
  render(<IdeasPage {...props()} />);
  expect(await screen.findByText("Vibe Kanban")).toBeInTheDocument();
  expect(screen.getByRole("tab", { name: "Open 3" })).toBeInTheDocument();
  expect(screen.getByRole("tab", { name: "Done 1" })).toBeInTheDocument();
  expect(screen.getByText("Tech news week 45")).toBeInTheDocument();
  expect(screen.getByText("noted without a thread")).toBeInTheDocument();
  expect(screen.getByText("open for 7 weeks")).toHaveClass("text-unread-ink");
  expect(screen.getByText("(thread deleted)")).toBeInTheDocument();
  expect(screen.getByText("Passkeys vs. OIDC")).toHaveClass("line-through");
  expect(screen.getByText("Due Jan 12, follow-up Jan 5")).toBeInTheDocument();
});

// Outside text is never markup: a passage with a link or an image is
// shown as the characters it is.
test("an open idea shows its passage as plain text", async () => {
  const hostile = makeIdea({
    excerpt:
      "![x](https://evil.example/beacon.png) <b>bold</b> [link](https://evil.example)",
    reason: "**not bold**",
  });
  vi.mocked(api.listIdeas).mockResolvedValue([hostile]);
  const { container } = render(<IdeasPage {...props()} />);
  fireEvent.click(await screen.findByRole("button", { name: /Vibe Kanban/ }));
  expect(
    screen.getByText(/!\[x\]\(https:\/\/evil\.example\/beacon\.png\)/),
  ).toBeInTheDocument();
  expect(container.querySelector("img")).toBeNull();
  expect(container.querySelector("a")).toBeNull();
  expect(container.querySelector("b")).toBeNull();
  expect(container.querySelector("strong")).toBeNull();
});

test("jump to the passage opens the thread at the message", async () => {
  const p = props();
  render(<IdeasPage {...p} />);
  fireEvent.click(await screen.findByRole("button", { name: /Vibe Kanban/ }));
  fireEvent.click(screen.getByRole("button", { name: "Jump to the passage" }));
  expect(p.onOpenMessage).toHaveBeenCalledWith("t1", "m1");
});

test("jump is disabled once the thread is gone, absent without one", async () => {
  render(<IdeasPage {...props()} />);
  fireEvent.click(
    await screen.findByRole("button", { name: /Passkeys instead of OIDC/ }),
  );
  expect(
    screen.getByRole("button", { name: "Jump to the passage" }),
  ).toBeDisabled();
  fireEvent.click(screen.getByRole("button", { name: /Rye at 30 %/ }));
  expect(
    screen.queryByRole("button", { name: "Jump to the passage" }),
  ).not.toBeInTheDocument();
});

test("start a thread fills a draft and never sends", async () => {
  const p = props();
  render(<IdeasPage {...p} />);
  fireEvent.click(await screen.findByRole("button", { name: /Vibe Kanban/ }));
  fireEvent.click(
    screen.getByRole("button", { name: "Start a thread about it" }),
  );
  expect(p.onStartThread).toHaveBeenCalledWith(
    "Vibe Kanban\n\nWhy: Topic for the AI lab week, submit by 12 January.",
  );
});

test("delete is optimistic, the toast undoes it, and it goes away by itself", async () => {
  vi.mocked(api.deleteIdea).mockResolvedValue();
  vi.mocked(api.restoreIdea).mockResolvedValue(kanban);
  const changed = vi.fn();
  window.addEventListener(api.IDEAS_CHANGED, changed);
  render(<IdeasPage {...props()} />);
  const row = (await screen.findByText("Vibe Kanban")).closest("li")!;
  fireEvent.click(within(row).getByRole("button", { name: "Idea actions" }));
  fireEvent.click(screen.getByRole("menuitem", { name: "Delete" }));
  expect(screen.queryByText("Vibe Kanban")).not.toBeInTheDocument();
  expect(screen.getByRole("status")).toHaveTextContent("“Vibe Kanban” deleted");
  await waitFor(() => expect(api.deleteIdea).toHaveBeenCalledWith("i1"));
  await waitFor(() => expect(changed).toHaveBeenCalled());
  fireEvent.click(screen.getByRole("button", { name: "Undo" }));
  expect(await screen.findByText("Vibe Kanban")).toBeInTheDocument();
  expect(api.restoreIdea).toHaveBeenCalledWith("i1");
  // Back in its place, first.
  expect(screen.getAllByRole("listitem")[0]).toHaveTextContent("Vibe Kanban");
  window.removeEventListener(api.IDEAS_CHANGED, changed);

  vi.useFakeTimers();
  fireEvent.click(
    within(screen.getAllByRole("listitem")[1]).getByRole("button", {
      name: "Idea actions",
    }),
  );
  fireEvent.click(screen.getByRole("menuitem", { name: "Delete" }));
  expect(screen.getByRole("status")).toBeInTheDocument();
  act(() => {
    vi.advanceTimersByTime(UNDO_MS + 10);
  });
  expect(screen.queryByRole("status")).not.toBeInTheDocument();
});

// On a slow link the undo must not overtake the delete: the restore would
// 404 and the delete would win.
test("undo waits for the delete to land", async () => {
  let land: () => void = () => {};
  vi.mocked(api.deleteIdea).mockReturnValue(
    new Promise<void>((resolve) => {
      land = resolve;
    }),
  );
  vi.mocked(api.restoreIdea).mockResolvedValue(kanban);
  render(<IdeasPage {...props()} />);
  fireEvent.click(await screen.findByRole("button", { name: /Vibe Kanban/ }));
  fireEvent.click(screen.getByRole("button", { name: "Delete" }));
  fireEvent.click(screen.getByRole("button", { name: "Undo" }));
  await Promise.resolve();
  expect(api.restoreIdea).not.toHaveBeenCalled();
  await act(async () => land());
  await waitFor(() => expect(api.restoreIdea).toHaveBeenCalledWith("i1"));
  expect(await screen.findByText("Vibe Kanban")).toBeInTheDocument();
});

test("a second delete joins the toast, undo brings both back", async () => {
  vi.mocked(api.deleteIdea).mockResolvedValue();
  vi.mocked(api.restoreIdea).mockImplementation(async (id) =>
    id === "i1" ? kanban : rye,
  );
  render(<IdeasPage {...props()} />);
  for (const title of ["Vibe Kanban", "Rye at 30 %"]) {
    const row = (await screen.findByText(title)).closest("li")!;
    fireEvent.click(within(row).getByRole("button", { name: "Idea actions" }));
    fireEvent.click(screen.getByRole("menuitem", { name: "Delete" }));
  }
  expect(screen.getByRole("status")).toHaveTextContent("2 ideas deleted");
  fireEvent.click(screen.getByRole("button", { name: "Undo" }));
  expect(await screen.findByText("Vibe Kanban")).toBeInTheDocument();
  expect(await screen.findByText("Rye at 30 %")).toBeInTheDocument();
  expect(api.restoreIdea).toHaveBeenCalledTimes(2);
});

test("escape dismisses the undo toast", async () => {
  vi.mocked(api.deleteIdea).mockResolvedValue();
  render(<IdeasPage {...props()} />);
  fireEvent.click(await screen.findByRole("button", { name: /Vibe Kanban/ }));
  fireEvent.click(screen.getByRole("button", { name: "Delete" }));
  expect(screen.getByRole("status")).toBeInTheDocument();
  fireEvent.keyDown(document, { key: "Escape" });
  expect(screen.queryByRole("status")).not.toBeInTheDocument();
});

test("a failed delete puts the idea back", async () => {
  vi.mocked(api.deleteIdea).mockRejectedValue(new Error("boom"));
  render(<IdeasPage {...props()} />);
  fireEvent.click(await screen.findByRole("button", { name: /Vibe Kanban/ }));
  fireEvent.click(screen.getByRole("button", { name: "Delete" }));
  expect(
    await screen.findByText("That did not work. Try again."),
  ).toBeInTheDocument();
  expect(screen.getByText("Vibe Kanban")).toBeInTheDocument();
});

test("done moves the idea off the open list", async () => {
  vi.mocked(api.updateIdea).mockResolvedValue({ ...kanban, state: "done" });
  render(<IdeasPage {...props()} />);
  fireEvent.click(await screen.findByRole("button", { name: /Vibe Kanban/ }));
  fireEvent.click(screen.getByRole("button", { name: "Done" }));
  await waitFor(() =>
    expect(screen.queryByText("Vibe Kanban")).not.toBeInTheDocument(),
  );
  expect(api.updateIdea).toHaveBeenCalledWith("i1", { done: true });
});

test("a deep link opens its idea, on the done tab when it is done", async () => {
  const doneIdea = { ...kanban, state: "done" as const };
  vi.mocked(api.getIdea).mockResolvedValue(doneIdea);
  vi.mocked(api.listIdeas).mockResolvedValue([doneIdea]);
  render(<IdeasPage {...props()} initialIdeaID="i1" />);
  expect(await screen.findByText("Passage in the thread")).toBeInTheDocument();
  expect(api.listIdeas).toHaveBeenCalledWith("done");
  expect(screen.getByRole("tab", { name: "Done 1" })).toHaveAttribute(
    "aria-selected",
    "true",
  );
  expect(screen.getByRole("button", { name: "Reopen" })).toBeInTheDocument();
});

test("empty page explains how to capture", async () => {
  vi.mocked(api.listIdeas).mockResolvedValue([]);
  vi.mocked(api.countIdeas).mockResolvedValue({ open: 0, done: 0 });
  render(<IdeasPage {...props()} />);
  expect(await screen.findByText("No ideas yet")).toBeInTheDocument();
  expect(
    screen.getByText(/Tap the lightbulb under an answer/),
  ).toBeInTheDocument();
});

test("new idea needs a reason, then lands on top", async () => {
  const created = makeIdea({ id: "i9", title: "Fresh", reason: "because" });
  vi.mocked(api.createIdea).mockResolvedValue(created);
  render(<IdeasPage {...props()} />);
  await screen.findByText("Vibe Kanban");
  fireEvent.click(screen.getByRole("button", { name: "New idea" }));
  const dialog = screen.getByRole("dialog", { name: "New idea" });
  const save = within(dialog).getByRole("button", { name: "Save" });
  expect(save).toBeDisabled();
  fireEvent.change(within(dialog).getByLabelText("Title"), {
    target: { value: "Fresh" },
  });
  fireEvent.change(
    within(dialog).getByLabelText(/Why do you want to follow this up/),
    { target: { value: "because" } },
  );
  expect(save).toBeEnabled();
  fireEvent.click(save);
  await waitFor(() =>
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument(),
  );
  expect(api.createIdea).toHaveBeenCalledWith({
    title: "Fresh",
    reason: "because",
    excerpt: "",
  });
  expect(screen.getAllByRole("listitem")[0]).toHaveTextContent("Fresh");
});

test("edit saves the changed fields", async () => {
  vi.mocked(api.updateIdea).mockResolvedValue({ ...kanban, title: "Kanban!" });
  render(<IdeasPage {...props()} />);
  const row = (await screen.findByText("Vibe Kanban")).closest("li")!;
  fireEvent.click(within(row).getByRole("button", { name: "Idea actions" }));
  fireEvent.click(screen.getByRole("menuitem", { name: "Edit" }));
  const dialog = screen.getByRole("dialog", { name: "Edit idea" });
  fireEvent.change(within(dialog).getByLabelText("Title"), {
    target: { value: "Kanban!" },
  });
  fireEvent.click(within(dialog).getByRole("button", { name: "Save" }));
  expect(await screen.findByText("Kanban!")).toBeInTheDocument();
});
