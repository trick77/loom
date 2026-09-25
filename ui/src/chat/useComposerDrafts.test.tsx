import { act, renderHook } from "@testing-library/react";
import { expect, test } from "vitest";

import { getDraft } from "./composerDrafts";
import { useComposerDrafts } from "./useComposerDrafts";

test("drafts and pasted chips stay with the scope they were typed in", () => {
  const { result } = renderHook(() => useComposerDrafts());

  act(() => {
    result.current.setDraftText("thread:a", "hello");
    result.current.addPastedText("thread:a", "a long paste\nwith lines");
  });
  expect(getDraft(result.current.drafts, "thread:a").text).toBe("hello");
  expect(getDraft(result.current.drafts, "thread:a").pastedTexts).toHaveLength(
    1,
  );
  expect(getDraft(result.current.drafts, "thread:b").pastedTexts).toHaveLength(
    0,
  );

  const pastedID = getDraft(result.current.drafts, "thread:a").pastedTexts[0]
    ?.id;
  act(() => {
    result.current.removePastedText("thread:a", pastedID ?? "");
  });
  expect(getDraft(result.current.drafts, "thread:a").pastedTexts).toHaveLength(
    0,
  );
  expect(getDraft(result.current.drafts, "thread:a").text).toBe("hello");
});

test("requestFocus bumps the focus tick", () => {
  const { result } = renderHook(() => useComposerDrafts());
  const before = result.current.focusTick;
  act(() => {
    result.current.requestFocus();
  });
  expect(result.current.focusTick).toBe(before + 1);
});
