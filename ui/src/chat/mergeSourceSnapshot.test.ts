import { describe, expect, it } from "vitest";

import type { Citation } from "../api";
import { mergeSourceSnapshot } from "./ThreadShell";

const doc = (index: number, filename: string): Citation => ({
  documentId: `d${index}`,
  filename,
  snippet: "x",
  score: 1,
  index,
});

const web = (index: number, filename: string): Citation => ({
  documentId: "",
  filename,
  snippet: "x",
  score: 1,
  index,
  url: `https://${filename}/page`,
});

describe("mergeSourceSnapshot", () => {
  it("keeps documents when the first web snapshot arrives", () => {
    const merged = mergeSourceSnapshot(
      [doc(1, "runbook.md")],
      [web(2, "kubernetes.io"), web(3, "reddit.com")],
      true,
    );
    expect(merged.map((c) => c.index)).toEqual([1, 2, 3]);
  });

  // The regression guard: a later round replaces the *web* sources wholesale (it is
  // a full snapshot of that kind) but must not take the documents with it — that is
  // what unresolved the document [n] pills mid-answer.
  it("replaces only the web sources on a later round", () => {
    const merged = mergeSourceSnapshot(
      [doc(1, "runbook.md"), web(2, "kubernetes.io")],
      [web(2, "kubernetes.io"), web(3, "reddit.com")],
      true,
    );
    expect(merged.map((c) => c.filename)).toEqual([
      "runbook.md",
      "kubernetes.io",
      "reddit.com",
    ]);
  });

  it("puts documents ahead of already-gathered web sources", () => {
    const merged = mergeSourceSnapshot(
      [web(2, "kubernetes.io")],
      [doc(1, "runbook.md")],
      false,
    );
    expect(merged.map((c) => c.index)).toEqual([1, 2]);
  });
});
