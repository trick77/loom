import { expect, test } from "vitest";

import {
  clearDraft,
  composeContent,
  draftScopeKey,
  getDraft,
  setDraftPastedTexts,
  setDraftText,
  type ComposerDrafts,
} from "./composerDrafts";
import { createPastedText } from "./pastedText";

test("each surface owns its own scope", () => {
  expect(draftScopeKey({ view: "new" }, false)).toBe("new");
  expect(draftScopeKey({ view: "thread", threadID: "t1" }, false)).toBe(
    "thread:t1",
  );
  expect(draftScopeKey({ view: "project", projectID: "p1" }, false)).toBe(
    "project:p1",
  );
  expect(draftScopeKey({ view: "threads" }, false)).toBe("view:threads");
});

test("incognito wins over the route it was entered from", () => {
  expect(draftScopeKey({ view: "new" }, true)).toBe("incognito");
  expect(draftScopeKey({ view: "thread", threadID: "t1" }, true)).toBe(
    "incognito",
  );
});

test("an unsent draft does not follow you to another thread", () => {
  const drafts = setDraftText({}, "thread:a", "half a question");

  expect(getDraft(drafts, "thread:a").text).toBe("half a question");
  expect(getDraft(drafts, "thread:b").text).toBe("");
  expect(getDraft(drafts, "new").text).toBe("");
});

test("clearing one scope leaves the others alone", () => {
  let drafts: ComposerDrafts = setDraftText({}, "thread:a", "keep me");
  drafts = setDraftText(drafts, "thread:b", "send me");
  drafts = clearDraft(drafts, "thread:b");

  expect(getDraft(drafts, "thread:a").text).toBe("keep me");
  expect(getDraft(drafts, "thread:b").text).toBe("");
});

test("staged pastes are scoped too", () => {
  const pasted = createPastedText("a wall of text");
  const drafts = setDraftPastedTexts({}, "thread:a", [pasted]);

  expect(getDraft(drafts, "thread:a").pastedTexts).toHaveLength(1);
  expect(getDraft(drafts, "thread:b").pastedTexts).toHaveLength(0);
});

test("emptied drafts are pruned rather than accumulating per thread visited", () => {
  let drafts = setDraftText({}, "thread:a", "typing");
  drafts = setDraftText(drafts, "thread:a", "");
  expect(Object.keys(drafts)).toEqual([]);

  // A scope that never held anything stays absent.
  expect(setDraftText(drafts, "thread:b", "")).toBe(drafts);
});

test("a draft with only pastes survives an empty textarea", () => {
  let drafts = setDraftPastedTexts({}, "thread:a", [
    createPastedText("pasted only"),
  ]);
  drafts = setDraftText(drafts, "thread:a", "");

  expect(getDraft(drafts, "thread:a").pastedTexts).toHaveLength(1);
});

test("content folds the trimmed draft together with its pastes", () => {
  expect(
    composeContent({
      text: "  look at this  ",
      pastedTexts: [createPastedText("first"), createPastedText("second")],
    }),
  ).toBe("look at this\n\nfirst\n\nsecond");

  expect(
    composeContent({ text: "   ", pastedTexts: [createPastedText("only")] }),
  ).toBe("only");

  expect(composeContent({ text: "", pastedTexts: [] })).toBe("");
});
