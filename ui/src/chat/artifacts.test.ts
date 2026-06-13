import { describe, expect, test } from "vitest";
import {
  artifactToolLabel,
  downloadableResponse,
  pendingArtifactLabels,
  pendingFencedArtifact,
} from "./artifacts";

describe("streamed downloadable artifacts", () => {
  const largeContent = "a".repeat(64 * 1024 + 1);

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
});

describe("artifactToolLabel", () => {
  test("maps known artifact tools to a label", () => {
    expect(artifactToolLabel("create_pdf_file")).toBe("PDF");
    expect(artifactToolLabel("generate_image")).toBe("image");
    expect(artifactToolLabel("create_xlsx_file")).toBe("spreadsheet");
  });

  test("returns null for non-artifact tools", () => {
    expect(artifactToolLabel("web_search")).toBeNull();
  });
});

describe("pendingArtifactLabels", () => {
  const running = (name: string) => ({ type: "tool", status: "running", name });

  test("lists running artifact tools not yet covered by an arrived artifact", () => {
    expect(pendingArtifactLabels([running("create_pdf_file")], 0)).toEqual(["PDF"]);
  });

  test("drops as many as have already arrived", () => {
    expect(pendingArtifactLabels([running("create_pdf_file")], 1)).toEqual([]);
  });

  test("ignores done/failed and non-artifact tools", () => {
    const trace = [
      { type: "tool", status: "done", name: "create_pdf_file" },
      { type: "tool", status: "running", name: "web_search" },
      { type: "reasoning", status: "running" },
    ];
    expect(pendingArtifactLabels(trace, 0)).toEqual([]);
  });
});
