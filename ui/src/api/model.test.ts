import { afterEach, expect, test, vi } from "vitest";
import { getModelInfo, resetModelInfoForTest } from "./model";

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
