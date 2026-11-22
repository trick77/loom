import { act, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, test, vi } from "vitest";

import type { Message } from "../api";
import { AssistantProse, MessageBubble, ProseMarkdown } from "./messages";

// The drawing itself needs a browser (the renderer measures text with
// getBBox); here draw() is a stand-in and the tests assert on what loom does
// around it: which fences reach it, the placeholder, the failure, the card.
type Drawn = { svg: string; own?: true } | { error: string };
const draw = vi.hoisted(() =>
  vi.fn<(src: string, room?: { width?: number }) => Promise<Drawn>>(),
);
const drawnNow = vi.hoisted(() =>
  vi.fn<(src: string, room?: { width?: number }) => Drawn | null>(),
);
vi.mock("@trick77/diagram/mermaid", async (importOriginal) => {
  const real =
    await importOriginal<typeof import("@trick77/diagram/mermaid")>();
  return { ...real, draw, drawnNow, configure: vi.fn() };
});

const flow =
  'flowchart TD\n  a["Order placed"] --> b{"Paid?"}\n  b -->|yes| c["Ship"]';
const svg = '<svg viewBox="0 0 200 100" width="200"><g></g></svg>';

beforeEach(() => {
  draw.mockReset();
  draw.mockResolvedValue({ svg, own: true });
  drawnNow.mockReset();
  drawnNow.mockReturnValue(null);
});

describe("a drawing that depends on its room", () => {
  const timeline = "timeline\n  Q1 : Pilot\n  Q2 : Launch";
  const standing =
    '<svg data-orient="vertical" data-wide="600" viewBox="0 0 200 300" width="200"><g class="standing"></g></svg>';
  const flat =
    '<svg data-orient="horizontal" data-wide="600" viewBox="0 0 700 120" width="700"><g class="flat"></g></svg>';

  test("lies a timeline flat where the text has room for it", async () => {
    vi.spyOn(HTMLElement.prototype, "clientWidth", "get").mockReturnValue(800);
    draw.mockImplementation((_src, room) =>
      Promise.resolve({
        svg: room?.width !== undefined ? flat : standing,
        own: true,
      }),
    );
    render(<ProseMarkdown>{fenced(timeline)}</ProseMarkdown>);
    const img = await screen.findByRole("img", { name: "Timeline" });
    expect(draw).toHaveBeenCalledWith(timeline, { width: 800 });
    expect(img.querySelector(".flat")).not.toBeNull();
    expect(img.querySelector(".standing")).toBeNull();
  });
});

test("shows a drawing it already has at once, with no placeholder", () => {
  drawnNow.mockImplementation((src) =>
    src === flow ? { svg, own: true } : null,
  );
  draw.mockReturnValue(new Promise(() => {}));
  render(<ProseMarkdown>{fenced(flow)}</ProseMarkdown>);
  expect(screen.getByRole("img", { name: "Flow diagram" })).toBeInTheDocument();
  expect(screen.queryByText("Drawing the diagram…")).not.toBeInTheDocument();
});

afterEach(() => {
  vi.restoreAllMocks();
});

function fenced(body: string): string {
  return "Here is the flow.\n\n```mermaid\n" + body + "\n```\n\nThat is all.";
}

describe("a mermaid fence in an answer", () => {
  test("is drawn once the fence is closed", async () => {
    render(<ProseMarkdown>{fenced(flow)}</ProseMarkdown>);
    expect(
      await screen.findByRole("img", { name: "Flow diagram" }),
    ).toBeInTheDocument();
    expect(draw).toHaveBeenCalledWith(flow);
    expect(screen.queryByText(/Order placed/)).not.toBeInTheDocument();
    // The prose around it stays prose.
    expect(screen.getByText("Here is the flow.")).toBeInTheDocument();
    expect(screen.getByText("That is all.")).toBeInTheDocument();
  });

  test("is drawn when it closes while the answer still streams", async () => {
    render(<ProseMarkdown streaming>{fenced(flow)}</ProseMarkdown>);
    expect(
      await screen.findByRole("img", { name: "Flow diagram" }),
    ).toBeInTheDocument();
  });

  test("holds a placeholder, not the source, while it still streams", () => {
    render(
      <ProseMarkdown streaming>
        {'Here is the flow.\n\n```mermaid\nflowchart TD\n  a["Order'}
      </ProseMarkdown>,
    );
    expect(screen.getByText("Drawing the diagram…")).toBeInTheDocument();
    expect(screen.queryByText(/Order/)).not.toBeInTheDocument();
    expect(draw).not.toHaveBeenCalled();
  });

  test("holds the placeholder for a fence opened on the last line", () => {
    render(<ProseMarkdown streaming>{"Look:\n\n```mermaid"}</ProseMarkdown>);
    expect(screen.getByText("Drawing the diagram…")).toBeInTheDocument();
  });

  test("stays code when the answer ended inside it", () => {
    // A stream cut or a stored answer: no placeholder that waits for nothing.
    render(
      <ProseMarkdown>
        {"Look:\n\n```mermaid\nflowchart TD\n  a --> b"}
      </ProseMarkdown>,
    );
    expect(screen.queryByText("Drawing the diagram…")).not.toBeInTheDocument();
    expect(screen.getByText(/a --> b/)).toBeInTheDocument();
    expect(draw).not.toHaveBeenCalled();
  });

  test("shows the placeholder while the drawing is on its way", async () => {
    let resolve: (v: { svg: string; own: true }) => void = () => {};
    draw.mockReturnValue(new Promise((r) => (resolve = r)));
    render(<ProseMarkdown>{fenced(flow)}</ProseMarkdown>);
    expect(screen.getByText("Drawing the diagram…")).toBeInTheDocument();
    await act(async () => resolve({ svg, own: true }));
    expect(
      screen.getByRole("img", { name: "Flow diagram" }),
    ).toBeInTheDocument();
  });

  test("says so, with the source, when it cannot be drawn", async () => {
    draw.mockResolvedValue({ error: "Parse error on line 2" });
    render(<ProseMarkdown>{fenced("flowchart TD\n  a[[[ -->")}</ProseMarkdown>);
    const summary = await screen.findByText("The diagram could not be drawn");
    expect(summary.closest("details")).toHaveTextContent("a[[[ -->");
  });

  test("shows an unsafe source as code, without the package's reason", async () => {
    const unsafe =
      'flowchart TD\n  a["Start"] --> b["End"]\n  click a "https://example.com"';
    draw.mockResolvedValue({ error: "unsafe: a link" });
    render(<ProseMarkdown>{fenced(unsafe)}</ProseMarkdown>);
    const summary = await screen.findByText("The diagram could not be drawn");
    const details = summary.closest("details");
    expect(details).toHaveTextContent('click a "https://example.com"');
    expect(details).not.toHaveTextContent("unsafe");
    expect(screen.queryByRole("img")).not.toBeInTheDocument();
  });

  test("leaves every other code block a code block", () => {
    render(<ProseMarkdown>{"```js\nconst a = 1;\n```"}</ProseMarkdown>);
    expect(screen.getByText(/const/)).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "Copy code" }),
    ).toBeInTheDocument();
    expect(draw).not.toHaveBeenCalled();
  });

  test("is drawn inside a full answer, not taken for a download", async () => {
    render(<AssistantProse>{fenced(flow)}</AssistantProse>);
    expect(
      await screen.findByRole("img", { name: "Flow diagram" }),
    ).toBeInTheDocument();
  });

  test("is named in the reader's language", async () => {
    render(
      <ProseMarkdown>{fenced("sequenceDiagram\n  A->>B: hi")}</ProseMarkdown>,
    );
    expect(
      await screen.findByRole("img", { name: "Sequence diagram" }),
    ).toBeInTheDocument();
  });

  test("names a class diagram in either spelling", async () => {
    render(
      <ProseMarkdown>{fenced("classDiagram-v2\n  class Order")}</ProseMarkdown>,
    );
    expect(
      await screen.findByRole("img", { name: "Class diagram" }),
    ).toBeInTheDocument();
  });

  test("falls back to a generic name for a kind it has no word for", async () => {
    render(<ProseMarkdown>{fenced("gantt\n  title Plan")}</ProseMarkdown>);
    expect(
      await screen.findByRole("img", { name: "Diagram" }),
    ).toBeInTheDocument();
  });
});

describe("an answer with a diagram, read aloud and copied", () => {
  const message: Message = {
    id: "m1",
    threadId: "t1",
    role: "assistant",
    content: fenced(flow),
    createdAt: "2026-06-14T00:00:00Z",
  };

  test("reads the prose aloud, not the diagram's source", async () => {
    const spoken: string[] = [];
    class Utterance {
      text: string;
      constructor(text: string) {
        this.text = text;
      }
      addEventListener() {}
    }
    vi.stubGlobal("SpeechSynthesisUtterance", Utterance);
    vi.stubGlobal("speechSynthesis", {
      speaking: false,
      speak: (u: Utterance) => spoken.push(u.text),
      cancel: () => {},
    });
    render(<MessageBubble message={message} retryMessage={null} />);
    await screen.findByRole("img", { name: "Flow diagram" });
    fireEvent.click(screen.getByRole("button", { name: "Read aloud" }));
    expect(spoken).toEqual(["Here is the flow.\n\nThat is all."]);
    vi.unstubAllGlobals();
  });

  test("copies the answer with the diagram's source", async () => {
    const writeText = vi.fn(() => Promise.resolve());
    Object.defineProperty(navigator, "clipboard", {
      configurable: true,
      value: { writeText },
    });
    render(<MessageBubble message={message} retryMessage={null} />);
    await screen.findByRole("img", { name: "Flow diagram" });
    await act(async () => {
      fireEvent.click(screen.getByRole("button", { name: "Copy response" }));
    });
    expect(writeText).toHaveBeenCalledWith(
      expect.stringContaining('a["Order placed"]'),
    );
  });
});

describe("the diagram card", () => {
  async function drawn() {
    render(<ProseMarkdown>{fenced(flow)}</ProseMarkdown>);
    await screen.findByRole("img", { name: "Flow diagram" });
  }

  test("opens the full view, which Escape closes", async () => {
    await drawn();
    fireEvent.click(
      screen.getByRole("button", { name: "Flow diagram: full view" }),
    );
    const dialog = screen.getByRole("dialog", { name: "Flow diagram" });
    expect(dialog).toBeInTheDocument();
    expect(dialog).toContainElement(document.activeElement as HTMLElement);
    fireEvent.keyDown(window, { key: "Escape" });
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  });

  test("closes the full view on the backdrop and the ×, not on the sheet", async () => {
    await drawn();
    const open = screen.getByRole("button", {
      name: "Flow diagram: full view",
    });
    fireEvent.click(open);
    fireEvent.click(screen.getByRole("dialog"));
    expect(screen.getByRole("dialog")).toBeInTheDocument();
    fireEvent.click(screen.getByTestId("diagram-backdrop"));
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    fireEvent.click(open);
    fireEvent.click(screen.getByRole("button", { name: "Close" }));
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  });

  test("keeps Tab inside the full view", async () => {
    await drawn();
    fireEvent.click(
      screen.getByRole("button", { name: "Flow diagram: full view" }),
    );
    const dialog = screen.getByRole("dialog");
    const buttons = dialog.querySelectorAll<HTMLElement>("button");
    const first = buttons[0];
    const last = buttons[buttons.length - 1];
    last.focus();
    fireEvent.keyDown(dialog, { key: "Tab" });
    expect(document.activeElement).toBe(first);
    first.focus();
    fireEvent.keyDown(dialog, { key: "Tab", shiftKey: true });
    expect(document.activeElement).toBe(last);
  });

  test("downloads the drawing as an SVG file", async () => {
    let saved: Blob | null = null;
    let name = "";
    Object.defineProperty(URL, "createObjectURL", {
      configurable: true,
      value: (b: Blob) => {
        saved = b;
        return "blob:x";
      },
    });
    Object.defineProperty(URL, "revokeObjectURL", {
      configurable: true,
      value: () => {},
    });
    vi.spyOn(HTMLAnchorElement.prototype, "click").mockImplementation(function (
      this: HTMLAnchorElement,
    ) {
      name = this.download;
    });
    await drawn();
    fireEvent.click(
      screen.getByRole("button", { name: "Flow diagram: download SVG" }),
    );
    expect(name).toBe("loom-flowchart-diagram.svg");
    expect(await (saved as unknown as Blob).text()).toContain("<svg");
  });
});
