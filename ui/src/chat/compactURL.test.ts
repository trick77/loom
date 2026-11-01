import { expect, test } from "vitest";

import { compactURL } from "./compactURL";

const LONG =
  "https://docs.example.com/guides/v2/streaming/markdown-links?utm_source=x#top";

test("a short URL stays as written", () => {
  expect(compactURL("https://example.com/a")).toBe("https://example.com/a");
});

test("a long URL keeps host and last path segment", () => {
  expect(compactURL(LONG)).toBe("docs.example.com/…/markdown-links");
});

test("scheme, www and query go first; a path that then fits stays whole", () => {
  expect(
    compactURL("https://www.example.com/news/item?ref=abcdefghijklmnop"),
  ).toBe("example.com/news/item");
});

test("a trailing slash is not a segment", () => {
  expect(
    compactURL("https://docs.example.com/guides/v2/streaming/markdown-links/"),
  ).toBe("docs.example.com/…/markdown-links");
});

test("a single segment is kept, cut only past max", () => {
  expect(
    compactURL("https://example.com/a-long-article-slug-here?x=1&y=2"),
  ).toBe("example.com/a-long-article-slug-here");
  expect(
    compactURL("https://example.com/a-very-long-article-slug-here?x=1&y=2"),
  ).toBe("example.com/a-very-long-article-slug-he…");
});

test("a partial URL mid-stream is compacted without throwing", () => {
  expect(
    compactURL("https://docs.example.com/guides/v2/streaming/markdown-li"),
  ).toBe("docs.example.com/…/markdown-li");
  expect(compactURL("https://docs.exa")).toBe("https://docs.exa");
});

test("never longer than max", () => {
  const huge = `https://${"a".repeat(60)}.example.com/${"b".repeat(80)}`;
  const out = compactURL(huge);
  expect(out.length).toBeLessThanOrEqual(40);
  expect(out.endsWith("…")).toBe(true);
  expect(compactURL(LONG, 20).length).toBeLessThanOrEqual(20);
});
