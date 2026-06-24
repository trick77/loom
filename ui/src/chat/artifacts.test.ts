import { describe, expect, test } from "vitest";
import { downloadableResponse, pendingFencedArtifact } from "./artifacts";

describe("streamed downloadable artifacts", () => {
  const largeContent = "a".repeat(64 * 1024 + 1);
  const largeData = "a".repeat(16 * 1024 + 1);

  test.each([
    ["pdf", "PDF"],
  ])("detects pending %s fences and counts received bytes", (language, label) => {
    const pending = pendingFencedArtifact(`\`\`\`${language}\n${"a".repeat(1536)}`);

    expect(pending).toEqual({
      label,
      before: "",
      receivedBytes: 1536,
    });
  });

  test.each([
    ["txt", "TXT"],
    ["text", "TXT"],
    ["md", "MD"],
    ["markdown", "MD"],
    ["yaml", "YAML"],
    ["yml", "YAML"],
    ["log", "LOG"],
  ])("detects pending %s fences only after the inline threshold", (language, label) => {
    const pending = pendingFencedArtifact(`\`\`\`${language}\n${largeContent}`);

    expect(pending).toEqual({
      label,
      before: "",
      receivedBytes: 64 * 1024 + 1,
    });
  });

  test.each(["txt", "text", "md", "markdown", "yaml", "yml", "log"])(
    "keeps small pending %s fences inline",
    (language) => {
      expect(pendingFencedArtifact(`\`\`\`${language}\nsmall content`)).toBeNull();
    },
  );

  test("types completed svg fences as image/svg+xml so they render in an <img>", () => {
    const embedded = downloadableResponse('```svg\n<svg viewBox="0 0 10 10"></svg>\n```');

    expect(embedded?.artifact).toMatchObject({
      extension: "svg",
      label: "SVG",
      mimeType: "image/svg+xml;charset=utf-8",
      content: '<svg viewBox="0 0 10 10"></svg>',
    });
  });

  test.each([
    ["pdf", "PDF", "application/pdf"],
  ])("turns completed %s fences into downloadable responses", (language, label, mimeType) => {
    const embedded = downloadableResponse(`\`\`\`${language}\ncontent\n\`\`\``);

    expect(embedded?.artifact).toMatchObject({
      extension: label.toLowerCase(),
      label,
      mimeType,
      content: "content",
    });
  });

  test.each([
    ["txt", "TXT", "text/plain;charset=utf-8"],
    ["text", "TXT", "text/plain;charset=utf-8"],
    ["md", "MD", "text/markdown;charset=utf-8"],
    ["markdown", "MD", "text/markdown;charset=utf-8"],
    ["yaml", "YAML", "application/yaml;charset=utf-8"],
    ["yml", "YAML", "application/yaml;charset=utf-8"],
    ["log", "LOG", "text/plain;charset=utf-8"],
  ])("turns large completed %s fences into downloadable responses", (language, label, mimeType) => {
    const embedded = downloadableResponse(`\`\`\`${language}\n${largeContent}\n\`\`\``);

    expect(embedded?.artifact).toMatchObject({
      extension: label.toLowerCase(),
      label,
      mimeType,
      content: largeContent,
    });
  });

  test.each(["txt", "text", "md", "markdown", "yaml", "yml", "log"])(
    "keeps small completed %s fences inline",
    (language) => {
      expect(downloadableResponse(`\`\`\`${language}\nsmall content\n\`\`\``)).toBeNull();
    },
  );

  test.each(["json", "csv", "xml"])("keeps small completed %s fences inline", (language) => {
    expect(downloadableResponse(`\`\`\`${language}\nsmall content\n\`\`\``)).toBeNull();
  });

  test.each([
    ["json", "JSON", "application/json;charset=utf-8"],
    ["csv", "CSV", "text/csv;charset=utf-8"],
    ["xml", "XML", "application/xml;charset=utf-8"],
  ])("turns large completed %s fences into downloadable responses", (language, label, mimeType) => {
    const embedded = downloadableResponse(`\`\`\`${language}\n${largeData}\n\`\`\``);

    expect(embedded?.artifact).toMatchObject({
      extension: language,
      label,
      mimeType,
      content: largeData,
    });
  });

  test("keeps completed html fences downloadable regardless of size", () => {
    expect(downloadableResponse("```html\n<p>hi</p>\n```")?.artifact).toMatchObject({
      extension: "html",
      label: "HTML",
    });
  });
});

