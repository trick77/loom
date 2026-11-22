import { render, screen } from "@testing-library/react";
import mermaid from "mermaid";
import { beforeAll, describe, expect, test, vi } from "vitest";

import { type DiagramIR, type MindNodeIR } from "@trick77/diagram";
import { checkIR } from "@trick77/diagram/check";
import {
  diagramKind,
  editorial,
  escapeAngles,
  fits,
  ownKinds,
  repair,
  toIR,
} from "@trick77/diagram/mermaid";

import { ProseMarkdown } from "./messages";

/**
 * The diagram corpus (testdata/diagrams): whole answers in the shape loom's
 * chat writes them, prose and markers around one ```mermaid fence, with
 * invented content. Each runs through what the chat does with it: the fence
 * reaches the drawing, mermaid's own parser accepts the source, and the kinds
 * the package draws come out as <svg> and pass its layout rules. Drawing
 * mermaid's pictures needs a browser and is checked there.
 *
 * A diagram that fails to draw in loom gets its answer dropped in here as a
 * new .txt FIRST, and the fix afterwards.
 *
 * Two kinds of answer are held apart, each asserted as it is today:
 * - refused/: mermaid's parser refuses them even after the package's
 *   repair. The prompt steers away from each; a repair that makes one parse
 *   moves it up here.
 * - declined: of a kind the package draws, but drawn by mermaid: past the
 *   package's caps, or laid out against its own rules (the package then
 *   hands the source on).
 */

const declined: ReadonlySet<string> = new Set([
  // 8 branches of 5 to 6 children: past the mind map caps.
  "mindmap-wide-branches-en.txt",
  // 11 states, 16 transitions; 12 states, 19 transitions: every layout
  // crossed edges.
  "state-dense-de.txt",
  "state-dense-en.txt",
  // 15 nodes, a long edge behind two boxes.
  "flow-focus-spaced-de.txt",
]);

// Only the drawing in the chat is stood in for (it needs a browser); the
// parser, the package's own drawing and its rules are the real ones.
const draw = vi.hoisted(() => vi.fn(() => new Promise<never>(() => {})));
vi.mock("@trick77/diagram/mermaid", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@trick77/diagram/mermaid")>()),
  draw,
}));

const answers: Record<string, string> = import.meta.glob(
  "./testdata/diagrams/*.txt",
  { query: "?raw", import: "default", eager: true },
);

const refusedAnswers: Record<string, string> = import.meta.glob(
  "./testdata/diagrams/refused/*.txt",
  { query: "?raw", import: "default", eager: true },
);

function entries(files: Record<string, string>) {
  return Object.entries(files).map(([path, text]) => ({
    name: path.slice(path.lastIndexOf("/") + 1),
    text,
    fences: [...text.matchAll(/^```mermaid\n([\s\S]*?)\n```$/gm)].map(
      (m) => m[1],
    ),
  }));
}

const corpus = entries(answers);
const refused = entries(refusedAnswers);

beforeAll(() => {
  mermaid.initialize({ startOnLoad: false, securityLevel: "strict" });
});

/** The source as draw() hands it to the parser. */
function prepared(src: string): string {
  return escapeAngles(repair(src));
}

async function irOf(src: string): Promise<DiagramIR | null> {
  const d = await mermaid.mermaidAPI.getDiagramFromText(prepared(src));
  return toIR(d.type, d.db);
}

/** focused counts the nodes a flow or a mind map frames as the focus, null
 * for the other kinds. */
function focused(ir: DiagramIR): number | null {
  if (ir.kind === "flow") return ir.nodes.filter((n) => n.focus).length;
  if (ir.kind === "mindmap") {
    const count = (n: MindNodeIR): number =>
      (n.focus ? 1 : 0) + n.children.reduce((s, c) => s + count(c), 0);
    return count(ir.root);
  }
  return null;
}

describe("the diagram corpus", () => {
  test("covers every type the prompt asks for", () => {
    const kinds = new Set(corpus.flatMap((c) => c.fences.map(diagramKind)));
    for (const k of [
      "mindmap",
      "flowchart",
      "sequenceDiagram",
      "stateDiagram-v2",
      "erDiagram",
      "timeline",
      "quadrantChart",
    ])
      expect(kinds).toContain(k);
  });

  for (const { name, text, fences } of corpus) {
    describe(name, () => {
      test("holds one diagram, which reaches the drawing", () => {
        expect(fences).toHaveLength(1);
        draw.mockClear();
        render(<ProseMarkdown>{text}</ProseMarkdown>);
        expect(screen.getAllByText("Drawing the diagram…")).toHaveLength(1);
        expect(draw).toHaveBeenCalledWith(fences[0]);
      });

      test("is accepted by mermaid's parser", async () => {
        const src = prepared(fences[0]);
        expect(
          await mermaid.parse(src, { suppressErrors: true }),
          `mermaid refuses:\n${src}`,
        ).toBeTruthy();
      });

      test("gets the drawing its kind gets", async () => {
        const src = fences[0];
        const svg = await editorial(mermaid, prepared(src), "corpus");
        if (ownKinds.has(diagramKind(src)) && !declined.has(name)) {
          expect(svg, "the package declined a kind it draws").toMatch(/^<svg /);
          const ir = await irOf(src);
          expect(ir).not.toBeNull();
          if (ir !== null && fits(ir)) expect(await checkIR(ir)).toEqual([]);
        } else {
          expect(
            svg,
            declined.has(name)
              ? "the package draws it now: drop it from declined"
              : "the package drew what mermaid should",
          ).toBeNull();
        }
      });

      test("frames as the focus exactly what it marks", async () => {
        const src = fences[0];
        if (!ownKinds.has(diagramKind(src)) || declined.has(name)) return;
        const ir = await irOf(src);
        const count = ir === null ? null : focused(ir);
        if (count === null) return;
        expect(count).toBe((src.match(/:::focus\b/g) ?? []).length);
      });
    });
  }

  for (const { name, fences } of refused) {
    test(`refused/${name} is still refused by mermaid's parser`, async () => {
      expect(fences).toHaveLength(1);
      expect(
        await mermaid.parse(prepared(fences[0]), { suppressErrors: true }),
        "it parses now: move it up to testdata/diagrams",
      ).toBe(false);
    });
  }
});
