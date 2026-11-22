import { describe, expect, test } from "vitest";
import {
  downloadableResponse,
  fileTypeLabel,
  markdownToPlainText,
  markdownToSpeech,
  pendingFencedArtifact,
} from "./artifacts";

describe("streamed downloadable artifacts", () => {
  const largeContent = "a".repeat(64 * 1024 + 1);
  const largeData = "a".repeat(16 * 1024 + 1);

  test.each([["pdf", "PDF"]])(
    "detects pending %s fences and counts received bytes",
    (language, label) => {
      const pending = pendingFencedArtifact(
        `\`\`\`${language}\n${"a".repeat(1536)}`,
      );

      expect(pending).toEqual({
        label,
        before: "",
        receivedBytes: 1536,
      });
    },
  );

  test.each([
    ["txt", "TXT"],
    ["text", "TXT"],
    ["md", "MD"],
    ["markdown", "MD"],
    ["yaml", "YAML"],
    ["yml", "YAML"],
    ["log", "LOG"],
  ])(
    "detects pending %s fences only after the inline threshold",
    (language, label) => {
      const pending = pendingFencedArtifact(
        `\`\`\`${language}\n${largeContent}`,
      );

      expect(pending).toEqual({
        label,
        before: "",
        receivedBytes: 64 * 1024 + 1,
      });
    },
  );

  test.each(["txt", "text", "md", "markdown", "yaml", "yml", "log"])(
    "keeps small pending %s fences inline",
    (language) => {
      expect(
        pendingFencedArtifact(`\`\`\`${language}\nsmall content`),
      ).toBeNull();
    },
  );

  test.each([
    ["json", "JSON"],
    ["csv", "CSV"],
    ["xml", "XML"],
  ])(
    "detects pending %s fences only after the data inline threshold",
    (language, label) => {
      const pending = pendingFencedArtifact(`\`\`\`${language}\n${largeData}`);

      expect(pending).toEqual({
        label,
        before: "",
        receivedBytes: 16 * 1024 + 1,
      });
    },
  );

  test.each(["json", "csv", "xml"])(
    "keeps small pending %s fences inline",
    (language) => {
      expect(
        pendingFencedArtifact(`\`\`\`${language}\nsmall content`),
      ).toBeNull();
    },
  );

  test("types completed svg fences as image/svg+xml so they render in an <img>", () => {
    const embedded = downloadableResponse(
      '```svg\n<svg viewBox="0 0 10 10"></svg>\n```',
    );

    expect(embedded?.artifact).toMatchObject({
      extension: "svg",
      label: "SVG",
      mimeType: "image/svg+xml;charset=utf-8",
      content: '<svg viewBox="0 0 10 10"></svg>',
    });
  });

  test.each([["pdf", "PDF", "application/pdf"]])(
    "turns completed %s fences into downloadable responses",
    (language, label, mimeType) => {
      const embedded = downloadableResponse(
        `\`\`\`${language}\ncontent\n\`\`\``,
      );

      expect(embedded?.artifact).toMatchObject({
        extension: label.toLowerCase(),
        label,
        mimeType,
        content: "content",
      });
    },
  );

  test.each([
    ["txt", "TXT", "text/plain;charset=utf-8"],
    ["text", "TXT", "text/plain;charset=utf-8"],
    ["md", "MD", "text/markdown;charset=utf-8"],
    ["markdown", "MD", "text/markdown;charset=utf-8"],
    ["yaml", "YAML", "application/yaml;charset=utf-8"],
    ["yml", "YAML", "application/yaml;charset=utf-8"],
    ["log", "LOG", "text/plain;charset=utf-8"],
  ])(
    "turns large completed %s fences into downloadable responses",
    (language, label, mimeType) => {
      const embedded = downloadableResponse(
        `\`\`\`${language}\n${largeContent}\n\`\`\``,
      );

      expect(embedded?.artifact).toMatchObject({
        extension: label.toLowerCase(),
        label,
        mimeType,
        content: largeContent,
      });
    },
  );

  test.each(["txt", "text", "md", "markdown", "yaml", "yml", "log"])(
    "keeps small completed %s fences inline",
    (language) => {
      expect(
        downloadableResponse(`\`\`\`${language}\nsmall content\n\`\`\``),
      ).toBeNull();
    },
  );

  test.each(["json", "csv", "xml"])(
    "keeps small completed %s fences inline",
    (language) => {
      expect(
        downloadableResponse(`\`\`\`${language}\nsmall content\n\`\`\``),
      ).toBeNull();
    },
  );

  test.each([
    ["json", "JSON", "application/json;charset=utf-8"],
    ["csv", "CSV", "text/csv;charset=utf-8"],
    ["xml", "XML", "application/xml;charset=utf-8"],
  ])(
    "turns large completed %s fences into downloadable responses",
    (language, label, mimeType) => {
      const embedded = downloadableResponse(
        `\`\`\`${language}\n${largeData}\n\`\`\``,
      );

      expect(embedded?.artifact).toMatchObject({
        extension: language,
        label,
        mimeType,
        content: largeData,
      });
    },
  );

  test("keeps completed html fences downloadable regardless of size", () => {
    expect(
      downloadableResponse("```html\n<p>hi</p>\n```")?.artifact,
    ).toMatchObject({
      extension: "html",
      label: "HTML",
    });
  });
});

describe("fileTypeLabel", () => {
  test.each([
    ["warsaw-trip-summary.pdf", "PDF"],
    ["data.csv", "CSV"],
    ["notes.md", "MD"],
    ["sheet.xlsx", "XLSX"],
    ["photo.png", "PNG"], // deleted images fall into the non-image branch
    ["archive.name.with.dots.json", "JSON"],
  ])("derives the type label from %s", (filename, expected) => {
    expect(fileTypeLabel(filename)).toBe(expected);
  });

  test.each([
    ["README"], // no extension
    [".gitignore"], // leading dot only
    ["trailing."], // empty extension
    ["weird.extension-too-long"], // not a plausible extension
  ])("returns null for %s so callers fall back to the glyph", (filename) => {
    expect(fileTypeLabel(filename)).toBeNull();
  });
});

describe("an answer holding a diagram", () => {
  const answer =
    'The flow:\n\n```mermaid\nflowchart TD\n  a["Start"] --> b["End"]\n```\n\nThat is **all**.';

  test("copies with the diagram's source", () => {
    expect(markdownToPlainText(answer)).toContain('a["Start"] --> b["End"]');
  });

  test("is read aloud without the diagram", () => {
    expect(markdownToSpeech(answer)).toBe("The flow:\n\nThat is all.");
  });

  test("reads every other code block aloud as before", () => {
    const code = "Run:\n\n```sh\nmake check\n```";
    expect(markdownToSpeech(code)).toBe(markdownToPlainText(code));
  });

  test("drops an empty diagram fence", () => {
    expect(markdownToSpeech("A\n\n```mermaid\n```\n\nB")).toBe("A\n\nB");
    expect(
      markdownToSpeech("A\n\n```mermaid\n```\n\nB\n\n```sh\nls\n```"),
    ).toContain("B");
  });

  test("drops each diagram up to its own closing fence, keeping the prose between", () => {
    expect(
      markdownToSpeech(
        "A\n\n```mermaid\nflowchart TD\n```\n\nMiddle\n\n```mermaid\nmindmap\n```\n\nB",
      ),
    ).toBe("A\n\nMiddle\n\nB");
    expect(
      markdownToSpeech(
        "A\n\n```mermaid\nflowchart TD\n```\n\nMiddle\n\n~~~js\nx\n~~~\n\nB",
      ),
    ).toContain("Middle");
  });

  test("drops a diagram inside a quote", () => {
    expect(markdownToSpeech("A\n\n> ```mermaid\n> mindmap\n> ```\n\nB")).toBe(
      "A\n\nB",
    );
  });

  test("drops a diagram fenced with tildes or more backticks", () => {
    expect(markdownToSpeech("A\n\n~~~mermaid\nmindmap\n  root\n~~~\n\nB")).toBe(
      "A\n\nB",
    );
    expect(
      markdownToSpeech("A\n\n````mermaid\nmindmap\n  root\n````\n\nB"),
    ).toBe("A\n\nB");
  });
});
