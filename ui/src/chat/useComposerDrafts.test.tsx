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

test("an idea's passage is staged marked outside; the same idea again replaces it", () => {
  const { result } = renderHook(() => useComposerDrafts());
  const source = { outside: true, ideaId: "i1", ideaTitle: "T", source: "S" };
  act(() => {
    result.current.addPastedText("new", "mine");
    result.current.addPastedText("new", "passage", source);
    result.current.addPastedText("new", "passage again", source);
  });
  const staged = getDraft(result.current.drafts, "new").pastedTexts;
  expect(staged.map((p) => p.text)).toEqual(["mine", "passage again"]);
  expect(staged[1]).toMatchObject(source);
  expect(staged[0].outside).toBeUndefined();
});
