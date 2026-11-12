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
import type { Message } from "../api";
import { MessageBubble } from "../chat/messages";
import { makeIdea } from "../test/ideas";
import { forgetCaptureDrafts } from "./IdeaCapture";
import { IdeaCaptureProvider, SAVED_TOAST_MS } from "./IdeaCaptureContext";
import { DRAFT_DEBOUNCE_MS } from "./useIdeaDraft";

vi.mock("../api", async () => {
  const actual = await vi.importActual<typeof import("../api")>("../api");
  return { ...actual, createIdea: vi.fn(), draftIdea: vi.fn() };
});

const answer = {
  id: "m1",
  role: "assistant",
  content: "**Vibe Kanban** is a board for [coding agents](https://x.example).",
  createdAt: "2026-11-12T08:00:00Z",
} as Message;

function setNarrow(narrow: boolean) {
  window.matchMedia = vi.fn().mockImplementation((query: string) => ({
    matches: query.includes("max-width") ? narrow : !narrow,
    media: query,
    addEventListener: vi.fn(),
    removeEventListener: vi.fn(),
  }));
}

function renderBubble({
  provided = true,
  publicView = false,
  onOpenIdea = vi.fn(),
} = {}) {
  const bubble = (
    <MessageBubble
      message={answer}
      retryMessage={null}
      publicView={publicView}
    />
  );
  render(
    provided ? (
      <IdeaCaptureProvider
        threadId="t1"
        threadTitle="Tech news week 45"
        onOpenIdea={onOpenIdea}
      >
        {bubble}
      </IdeaCaptureProvider>
    ) : (
      bubble
    ),
  );
  return { onOpenIdea };
}

beforeEach(() => {
  // Here, not after: the last test's unmount keeps its capture's draft.
  forgetCaptureDrafts();
  setNarrow(false);
  vi.mocked(api.draftIdea).mockResolvedValue({
    title: "Vibe Kanban",
    dueOn: null,
  });
});

afterEach(() => {
  vi.clearAllMocks();
  vi.useRealTimers();
});

test("no lightbulb without a stored thread (incognito) or on a share", () => {
  renderBubble({ provided: false });
  expect(screen.queryByRole("button", { name: "Save as idea" })).toBeNull();
  renderBubble({ publicView: true });
  expect(screen.queryByRole("button", { name: "Save as idea" })).toBeNull();
});

test("capture: the reason is required, the title is proposed, save stores it with its source", async () => {
  vi.mocked(api.createIdea).mockResolvedValue(makeIdea({ id: "i9" }));
  const changed = vi.fn();
  window.addEventListener(api.IDEAS_CHANGED, changed);
  const { onOpenIdea } = renderBubble();
  fireEvent.click(screen.getByRole("button", { name: "Save as idea" }));
  const dialog = screen.getByRole("dialog");
  // The passage is the answer's plain text, without link targets.
  expect(
    within(dialog).getByText("Vibe Kanban is a board for coding agents."),
  ).toBeInTheDocument();
  expect(api.draftIdea).toHaveBeenCalledWith(
    "Vibe Kanban is a board for coding agents.",
    "",
    expect.any(AbortSignal),
  );
  expect(
    await within(dialog).findByDisplayValue("Vibe Kanban"),
  ).toBeInTheDocument();
  expect(
    within(dialog).getByText("Source: “Tech news week 45”"),
  ).toBeInTheDocument();
  const save = within(dialog).getByRole("button", { name: "Save" });
  expect(save).toBeDisabled();
  const reason = within(dialog).getByLabelText(
    /Why do you want to follow this up/,
  );
  // A pointer device gets the reason focused.
  expect(reason).toHaveFocus();
  fireEvent.change(reason, { target: { value: "Lab week topic" } });
  fireEvent.click(save);
  await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
  expect(api.createIdea).toHaveBeenCalledWith({
    title: "Vibe Kanban",
    reason: "Lab week topic",
    excerpt: "Vibe Kanban is a board for coding agents.",
    threadId: "t1",
    messageId: "m1",
    dueOn: null,
    remindOn: null,
  });
  expect(changed).toHaveBeenCalled();
  window.removeEventListener(api.IDEAS_CHANGED, changed);
  expect(screen.getByRole("status")).toHaveTextContent("Idea saved");
  fireEvent.click(screen.getByRole("button", { name: "View" }));
  expect(onOpenIdea).toHaveBeenCalledWith("i9");
});

test("a title the user typed is never overwritten by the proposal", async () => {
  let answerDraft: (value: api.IdeaDraft) => void = () => {};
  vi.mocked(api.draftIdea).mockReturnValue(
    new Promise((resolve) => {
      answerDraft = resolve;
    }),
  );
  renderBubble();
  fireEvent.click(screen.getByRole("button", { name: "Save as idea" }));
  const title = screen.getByLabelText("Title");
  fireEvent.change(title, { target: { value: "Mine" } });
  await act(async () => answerDraft({ title: "Proposed", dueOn: null }));
  expect(title).toHaveValue("Mine");
});

// A local 'YYYY-MM-DD' n days from now.
function dayFromNow(n: number): string {
  const d = new Date();
  d.setDate(d.getDate() + n);
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, "0")}-${String(d.getDate()).padStart(2, "0")}`;
}

test("a reason naming a deadline brings the due date and a follow-up a week before; the user can move or drop it", async () => {
  vi.mocked(api.createIdea).mockResolvedValue(makeIdea({ id: "i9" }));
  const due = dayFromNow(30);
  renderBubble();
  fireEvent.click(screen.getByRole("button", { name: "Save as idea" }));
  const dialog = screen.getByRole("dialog");
  await within(dialog).findByDisplayValue("Vibe Kanban");
  expect(within(dialog).getByRole("button", { name: "Set follow-up" }));
  vi.mocked(api.draftIdea).mockResolvedValue({
    title: "Other title",
    dueOn: due,
  });
  vi.useFakeTimers();
  fireEvent.change(within(dialog).getByLabelText(/Why do you want/), {
    target: { value: "lab week, submit by then" },
  });
  // Debounced: asked once the typing pauses, with the reason alone (the
  // passage is outside text and is never where a date comes from).
  expect(api.draftIdea).toHaveBeenCalledTimes(1);
  await act(async () => {
    vi.advanceTimersByTime(DRAFT_DEBOUNCE_MS + 10);
  });
  vi.useRealTimers();
  expect(api.draftIdea).toHaveBeenLastCalledWith(
    "",
    "lab week, submit by then",
    expect.any(AbortSignal),
  );
  const chip = await within(dialog).findByRole("button", {
    name: /Follow-up .* · 1 week before\. Change follow-up/,
  });
  // A proposed title stays put once there.
  expect(within(dialog).getByLabelText(/^Title/)).toHaveValue("Vibe Kanban");
  fireEvent.click(chip);
  fireEvent.click(
    within(dialog).getByRole("menuitem", { name: "2 weeks before" }),
  );
  fireEvent.click(within(dialog).getByRole("button", { name: "Save" }));
  await waitFor(() => expect(api.createIdea).toHaveBeenCalled());
  expect(api.createIdea).toHaveBeenCalledWith(
    expect.objectContaining({ dueOn: due, remindOn: dayFromNow(16) }),
  );
});

test("dates the user dropped stay dropped when the next proposal comes", async () => {
  vi.mocked(api.createIdea).mockResolvedValue(makeIdea({ id: "i9" }));
  vi.mocked(api.draftIdea).mockResolvedValue({
    title: "Vibe Kanban",
    dueOn: dayFromNow(30),
  });
  vi.useFakeTimers();
  renderBubble();
  fireEvent.click(screen.getByRole("button", { name: "Save as idea" }));
  const dialog = screen.getByRole("dialog");
  const reason = within(dialog).getByLabelText(/Why do you want/);
  fireEvent.change(reason, { target: { value: "by then" } });
  await act(async () => {
    vi.advanceTimersByTime(DRAFT_DEBOUNCE_MS + 10);
  });
  fireEvent.click(within(dialog).getByRole("button", { name: "No due date" }));
  fireEvent.click(within(dialog).getByRole("button", { name: "No follow-up" }));
  fireEvent.change(reason, { target: { value: "by then, really" } });
  await act(async () => {
    vi.advanceTimersByTime(DRAFT_DEBOUNCE_MS + 10);
  });
  vi.useRealTimers();
  expect(
    within(dialog).queryByRole("button", { name: "No due date" }),
  ).toBeNull();
  fireEvent.click(within(dialog).getByRole("button", { name: "Save" }));
  await waitFor(() =>
    expect(api.createIdea).toHaveBeenCalledWith(
      expect.objectContaining({ dueOn: null, remindOn: null }),
    ),
  );
});

test("a closed capture keeps its reason until reopened for the same message", () => {
  renderBubble();
  const bulb = screen.getByRole("button", { name: "Save as idea" });
  fireEvent.click(bulb);
  fireEvent.change(screen.getByLabelText(/Why do you want/), {
    target: { value: "half typed" },
  });
  fireEvent.keyDown(document, { key: "Escape" });
  expect(screen.queryByRole("dialog")).toBeNull();
  fireEvent.click(bulb);
  expect(screen.getByLabelText(/Why do you want/)).toHaveValue("half typed");
});

// The bulb toggles: its own press is not an outside click that closes the
// capture only for the click to open it again.
test("the bulb closes an open capture, and a reopen asks for no new title", async () => {
  renderBubble();
  const bulb = screen.getByRole("button", { name: "Save as idea" });
  fireEvent.click(bulb);
  expect(await screen.findByDisplayValue("Vibe Kanban")).toBeInTheDocument();
  fireEvent.pointerDown(bulb);
  fireEvent.click(bulb);
  expect(screen.queryByRole("dialog")).toBeNull();
  fireEvent.click(bulb);
  // The kept proposal, not a new one.
  expect(screen.getByDisplayValue("Vibe Kanban")).toBeInTheDocument();
  expect(api.draftIdea).toHaveBeenCalledTimes(1);
  // A press elsewhere closes it.
  fireEvent.pointerDown(document.body);
  expect(screen.queryByRole("dialog")).toBeNull();
});

test("on a narrow screen it is a bottom sheet: no keyboard until tapped, backdrop closes", () => {
  setNarrow(true);
  renderBubble();
  fireEvent.click(screen.getByRole("button", { name: "Save as idea" }));
  const sheet = screen.getByRole("dialog", { name: "Save idea" });
  expect(sheet.parentElement).toBe(document.body);
  const reason = within(sheet).getByLabelText(/Why do you want/);
  expect(reason).not.toHaveFocus();
  // iOS zooms into inputs under 16px.
  expect(reason.className).toContain("text-base");
  fireEvent.click(document.querySelector('[aria-hidden="true"].fixed')!);
  expect(screen.queryByRole("dialog")).toBeNull();
});

test("the saved toast leaves by itself", async () => {
  vi.mocked(api.createIdea).mockResolvedValue(makeIdea({ id: "i9" }));
  renderBubble();
  fireEvent.click(screen.getByRole("button", { name: "Save as idea" }));
  fireEvent.change(screen.getByLabelText(/Why do you want/), {
    target: { value: "r" },
  });
  vi.useFakeTimers();
  fireEvent.click(screen.getByRole("button", { name: "Save" }));
  await act(async () => {});
  expect(screen.getByRole("status")).toBeInTheDocument();
  act(() => {
    vi.advanceTimersByTime(SAVED_TOAST_MS + 10);
  });
  expect(screen.queryByRole("status")).toBeNull();
});
