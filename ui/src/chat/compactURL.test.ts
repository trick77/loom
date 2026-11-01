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

test("the first query parameter stays when it fits: it may be the page", () => {
  expect(compactURL("https://www.youtube.com/watch?v=dQw4w9WgXcQ")).toBe(
    "youtube.com/watch?v=dQw4w9WgXcQ",
  );
  expect(
    compactURL("https://www.youtube.com/watch?v=dQw4w9WgXcQ&list=PL0123456789"),
  ).toBe("youtube.com/watch?v=dQw4w9WgXcQ");
});

test("as many trailing path segments as fit are kept", () => {
  expect(
    compactURL("https://github.com/trick77/loom/blob/master/README.md"),
  ).toBe("github.com/…/loom/blob/master/README.md");
});

test("credentials never reach the label", () => {
  expect(
    compactURL("https://admin:s3cret@internal.example.com/a/b/c/d/e"),
  ).toBe("internal.example.com/a/b/c/d/e");
});

test("a cut never splits a surrogate pair", () => {
  const out = compactURL(`https://example.com/${"a".repeat(37)}😀😀`);
  expect(out).toBe(`example.com/${"a".repeat(27)}…`);
  const emoji = compactURL(`https://example.com/${"😀".repeat(40)}`);
  expect(Array.from(emoji)).toHaveLength(40);
  expect(emoji).not.toMatch(/[\uD800-\uDBFF](?![\uDC00-\uDFFF])/);
});

test("a single segment is kept, cut only past max", () => {
  expect(
    compactURL("https://example.com/a-long-article-slug-here?x=1&y=2"),
  ).toBe("example.com/a-long-article-slug-here?x=1");
  expect(
    compactURL("https://example.com/a-very-long-article-slug-here?x=1&y=2"),
  ).toBe("example.com/a-very-long-article-slug-he…");
});

test("a partial URL mid-stream is compacted without throwing", () => {
  expect(
    compactURL("https://docs.example.com/guides/v2/streaming/markdown-li"),
  ).toBe("docs.example.com/…/streaming/markdown-li");
  expect(compactURL("https://docs.exa")).toBe("https://docs.exa");
});

test("never longer than max", () => {
  const huge = `https://${"a".repeat(60)}.example.com/${"b".repeat(80)}`;
  const out = compactURL(huge);
  expect(out.length).toBeLessThanOrEqual(40);
  expect(out.endsWith("…")).toBe(true);
  expect(compactURL(LONG, 20).length).toBeLessThanOrEqual(20);
});
