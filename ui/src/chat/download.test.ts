import { afterEach, expect, test, vi } from "vitest";

import { downloadBlob } from "./download";

afterEach(() => {
  vi.useRealTimers();
  vi.restoreAllMocks();
});

test("downloadBlob clicks a temporary anchor and revokes the URL on the next tick", () => {
  vi.useFakeTimers();
  const createObjectURL = vi.fn(() => "blob:test");
  const revokeObjectURL = vi.fn();
  Object.assign(URL, { createObjectURL, revokeObjectURL });
  const click = vi
    .spyOn(HTMLAnchorElement.prototype, "click")
    .mockImplementation(() => {});

  downloadBlob(new Blob(["x"]), "notes.txt");

  expect(click).toHaveBeenCalledTimes(1);
  expect(document.querySelector("a[download]")).toBeNull();
  expect(revokeObjectURL).not.toHaveBeenCalled();
  vi.runAllTimers();
  expect(revokeObjectURL).toHaveBeenCalledWith("blob:test");
});
