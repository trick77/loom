import { act, renderHook, waitFor } from "@testing-library/react";
import { beforeEach, expect, test, vi } from "vitest";

import * as api from "../api";
import { useIdeaCounts, useOpenIdeasCount } from "./useOpenIdeasCount";

vi.mock("../api", async () => {
  const actual = await vi.importActual<typeof import("../api")>("../api");
  return { ...actual, countIdeas: vi.fn() };
});

const countIdeas = vi.mocked(api.countIdeas);

beforeEach(() => {
  countIdeas.mockReset();
  countIdeas.mockResolvedValue({ open: 3, done: 1 });
});

function useBoth() {
  return { open: useOpenIdeasCount(), counts: useIdeaCounts() };
}

test("readers mounted together share one count, a change one recount", async () => {
  const { result, unmount } = renderHook(useBoth);
  await waitFor(() => expect(result.current.open).toBe(3));
  expect(result.current.counts).toEqual({ open: 3, done: 1 });
  expect(countIdeas).toHaveBeenCalledTimes(1);

  countIdeas.mockResolvedValue({ open: 2, done: 2 });
  act(() => api.announce(api.IDEAS_CHANGED));
  await waitFor(() => expect(result.current.open).toBe(2));
  expect(result.current.counts).toEqual({ open: 2, done: 2 });
  expect(countIdeas).toHaveBeenCalledTimes(2);

  unmount();
});

test("the counts go with the last reader, a late answer is dropped", async () => {
  let answer: (counts: api.IdeaCounts) => void = () => {};
  countIdeas.mockReturnValueOnce(
    new Promise((resolve) => {
      answer = resolve;
    }),
  );
  const first = renderHook(useIdeaCounts);
  first.unmount();
  await act(async () => answer({ open: 9, done: 9 }));

  const second = renderHook(useIdeaCounts);
  expect(second.result.current).toBeNull();
  await waitFor(() =>
    expect(second.result.current).toEqual({ open: 3, done: 1 }),
  );
  second.unmount();
});
