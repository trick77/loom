import { afterEach, expect, test, vi } from "vitest";

import {
  contextWindowFor,
  getModelInfo,
  modelNameFor,
  peekModelInfo,
  resetModelInfoForTest,
  type ModelInfo,
} from "./model";

afterEach(() => {
  vi.unstubAllGlobals();
  resetModelInfoForTest();
});

test("getModelInfo reads /api/model once and shares the answer", async () => {
  const fetchMock = vi.fn().mockResolvedValue({
    ok: true,
    json: async () => ({ model: "some-model", contextWindow: 1_000_000 }),
  });
  vi.stubGlobal("fetch", fetchMock);

  const [a, b] = await Promise.all([getModelInfo(), getModelInfo()]);

  expect(a).toEqual({ model: "some-model", contextWindow: 1_000_000 });
  expect(b).toBe(a);
  expect(fetchMock).toHaveBeenCalledTimes(1);
  expect(fetchMock).toHaveBeenCalledWith("/api/model");
});

test("getModelInfo resolves null on failure and retries on the next call", async () => {
  const fetchMock = vi
    .fn()
    .mockResolvedValueOnce({ ok: false })
    .mockResolvedValueOnce({
      ok: true,
      json: async () => ({ model: "some-model", contextWindow: 1_000_000 }),
    });
  vi.stubGlobal("fetch", fetchMock);

  expect(await getModelInfo()).toBeNull();
  expect(await getModelInfo()).toEqual({
    model: "some-model",
    contextWindow: 1_000_000,
  });
});

const base = { model: "fast", displayName: "Fast", contextWindow: 1000 };

test("an answer names its model only when more than one model can answer", () => {
  const twoSlots: ModelInfo = {
    ...base,
    slots: [
      {
        key: "standard",
        model: "fast",
        displayName: "Fast",
        contextWindow: 1000,
      },
      { key: "deep", model: "deep", displayName: "Deep", contextWindow: 2000 },
    ],
  };
  expect(modelNameFor(twoSlots, "deep")).toBe("Deep");
  expect(modelNameFor(twoSlots, "gone")).toBeUndefined();
  expect(modelNameFor(null, "deep")).toBeUndefined();

  // The deep slot on the chat model itself: one model, nothing to tell apart.
  const sameModel: ModelInfo = {
    ...base,
    slots: [
      {
        key: "standard",
        model: "fast",
        displayName: "Fast",
        contextWindow: 1000,
      },
      { key: "deep", model: "fast", displayName: "Fast", contextWindow: 1000 },
    ],
  };
  expect(modelNameFor(sameModel, "fast")).toBeUndefined();
});

test("the vision model an image turn falls back to is named too", () => {
  const info: ModelInfo = {
    ...base,
    slots: [
      {
        key: "standard",
        model: "fast",
        displayName: "Fast",
        contextWindow: 1000,
      },
    ],
    answering: [
      { model: "fast", displayName: "Fast", contextWindow: 1000 },
      { model: "eyes", displayName: "Eyes", contextWindow: 3000 },
    ],
  };
  expect(modelNameFor(info, "eyes")).toBe("Eyes");
  expect(contextWindowFor(info, "eyes")).toBe(3000);
  expect(contextWindowFor(info, "gone")).toBe(1000);
});

test("a settled answer is there synchronously for the next caller", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn().mockResolvedValue({ ok: true, json: async () => base }),
  );
  expect(peekModelInfo()).toBeNull();
  await getModelInfo();
  expect(peekModelInfo()).toEqual(base);
});
