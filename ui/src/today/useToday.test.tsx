import { act, renderHook, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, expect, test, vi } from "vitest";

import * as api from "../api";
import { makeIdea } from "../test/ideas";
import { todayFixture } from "../test/today";
import { useToday } from "./useToday";

vi.mock("../api", async () => {
  const actual = await vi.importActual<typeof import("../api")>("../api");
  return { ...actual, getToday: vi.fn(), updateIdea: vi.fn() };
});

const idea = makeIdea({ id: "i1" });

beforeEach(() => {
  vi.mocked(api.getToday).mockResolvedValue(todayFixture({ ideas: [idea] }));
});

afterEach(() => {
  vi.clearAllMocks();
});

async function ready() {
  const hook = renderHook(() => useToday());
  await waitFor(() => expect(hook.result.current.state.status).toBe("ready"));
  return hook;
}

const shownIdeas = (state: ReturnType<typeof useToday>["state"]) =>
  state.status === "ready" ? state.today.ideas : [];

test("a hidden follow-up leaves at once, is saved, and tells the app", async () => {
  vi.mocked(api.updateIdea).mockResolvedValue(idea);
  const changed = vi.fn();
  window.addEventListener(api.IDEAS_CHANGED, changed);
  const { result } = await ready();
  expect(shownIdeas(result.current.state)).toHaveLength(1);
  act(() =>
    result.current.settleIdea("i1", { remindDismissed: true }, vi.fn()),
  );
  expect(shownIdeas(result.current.state)).toHaveLength(0);
  expect(api.updateIdea).toHaveBeenCalledWith("i1", { remindDismissed: true });
  await waitFor(() => expect(changed).toHaveBeenCalled());
  window.removeEventListener(api.IDEAS_CHANGED, changed);
  // One reload once saved (not one more for its own announcement): the
  // next follow-up moves up; the id is let go after it.
  await waitFor(() => expect(api.getToday).toHaveBeenCalledTimes(2));
  expect(api.getToday).toHaveBeenCalledTimes(2);
});

test("after the reload a card hidden here shows again when the server lists it again", async () => {
  vi.mocked(api.updateIdea).mockResolvedValue(idea);
  const { result } = await ready();
  vi.mocked(api.getToday).mockResolvedValue(todayFixture({ ideas: [] }));
  act(() => result.current.settleIdea("i1", { done: true }, vi.fn()));
  await waitFor(() => expect(api.getToday).toHaveBeenCalledTimes(2));
  // Reopened with a new follow-up elsewhere: listed again, shown again.
  vi.mocked(api.getToday).mockResolvedValue(todayFixture({ ideas: [idea] }));
  await act(async () => {
    await result.current.reload();
  });
  expect(shownIdeas(result.current.state)).toHaveLength(1);
});

test("a failed change brings the card back; an expired session signs out", async () => {
  vi.mocked(api.updateIdea).mockRejectedValue(new api.AuthExpiredError());
  const expired = vi.fn();
  const { result } = await ready();
  act(() => result.current.settleIdea("i1", { done: true }, expired));
  await waitFor(() => expect(shownIdeas(result.current.state)).toHaveLength(1));
  expect(expired).toHaveBeenCalled();
});

test("an idea changed elsewhere reloads the follow-ups", async () => {
  await ready();
  act(() => {
    window.dispatchEvent(new Event(api.IDEAS_CHANGED));
  });
  await waitFor(() => expect(api.getToday).toHaveBeenCalledTimes(2));
});
