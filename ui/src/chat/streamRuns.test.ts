import { expect, test } from "vitest";

import {
  anyStreaming,
  beginRun,
  endRun,
  isStreaming,
  patchRun,
  rekeyRun,
  selectRun,
  type StreamRuns,
} from "./streamRuns";

const A = "thread:a";
const B = "thread:b";

test("an absent key is idle and renders the empty run", () => {
  const runs: StreamRuns = {};
  expect(isStreaming(runs, A)).toBe(false);
  expect(isStreaming(runs, null)).toBe(false);
  expect(selectRun(runs, A).status).toBe("idle");
  expect(selectRun(runs, A).blocks).toEqual([]);
  expect(selectRun(runs, null)).toBe(selectRun(runs, A));
});

test("two threads stream independently", () => {
  let runs = beginRun({}, A);
  runs = beginRun(runs, B);
  runs = patchRun(runs, A, { toolPending: true });

  expect(isStreaming(runs, A)).toBe(true);
  expect(isStreaming(runs, B)).toBe(true);
  expect(selectRun(runs, A).toolPending).toBe(true);
  expect(selectRun(runs, B).toolPending).toBe(false);

  runs = endRun(runs, A, { keepFailedTurnVisible: false });
  expect(isStreaming(runs, A)).toBe(false);
  expect(isStreaming(runs, B)).toBe(true);
  expect(anyStreaming(runs)).toBe(true);

  runs = endRun(runs, B, { keepFailedTurnVisible: false });
  expect(anyStreaming(runs)).toBe(false);
  expect(runs).toEqual({});
});

test("a failed turn keeps its partial blocks and error until the next send", () => {
  let runs = beginRun({}, A);
  runs = patchRun(runs, A, { error: "Message failed to send." });
  runs = endRun(runs, A, { keepFailedTurnVisible: true });

  expect(isStreaming(runs, A)).toBe(false);
  expect(selectRun(runs, A).status).toBe("failed");
  expect(selectRun(runs, A).error).toBe("Message failed to send.");

  runs = beginRun(runs, A);
  expect(selectRun(runs, A).error).toBe("");
  expect(selectRun(runs, A).status).toBe("streaming");
});

test("a patch arriving after the run ended does not resurrect it", () => {
  let runs = beginRun({}, A);
  runs = endRun(runs, A, { keepFailedTurnVisible: false });
  const after = patchRun(runs, A, { toolPending: true });

  expect(after).toBe(runs);
  expect(selectRun(after, A).status).toBe("idle");
});

test("rekey moves a provisional run onto its new thread id", () => {
  let runs = beginRun({}, "new:1");
  runs = patchRun(runs, "new:1", { toolPending: true });
  runs = rekeyRun(runs, "new:1", A);

  expect(runs["new:1"]).toBeUndefined();
  expect(isStreaming(runs, A)).toBe(true);
  expect(selectRun(runs, A).toolPending).toBe(true);
});

test("concurrent start-screen sends get their own provisional keys", () => {
  let runs = beginRun({}, "new:1");
  runs = beginRun(runs, "new:2");
  runs = rekeyRun(runs, "new:1", A);

  // The second send is untouched by the first one learning its thread id.
  expect(isStreaming(runs, A)).toBe(true);
  expect(isStreaming(runs, "new:2")).toBe(true);
});

test("rekeying an unknown or unchanged key is a no-op", () => {
  const runs = beginRun({}, A);
  expect(rekeyRun(runs, "new:9", B)).toBe(runs);
  expect(rekeyRun(runs, A, A)).toBe(runs);
});

test("ending an unknown key is a no-op", () => {
  const runs = beginRun({}, A);
  expect(endRun(runs, B, { keepFailedTurnVisible: false })).toBe(runs);
});
