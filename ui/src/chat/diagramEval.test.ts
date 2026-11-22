import { readdirSync, readFileSync } from "node:fs";
import { join } from "node:path";

import mermaid from "mermaid";
import { expect, test } from "vitest";

import { checkIR } from "@trick77/diagram/check";
import {
  diagramKind,
  editorial,
  escapeAngles,
  fits,
  repair,
  toIR,
} from "@trick77/diagram/mermaid";

/**
 * The parser half of the diagram eval: the answers the backend's
 * TestDiagramStyleEval wrote to LOOM_DIAGRAM_EVAL_DIR, each fence run through
 * mermaid's parser as draw() runs it, then through the package's own
 * drawing: drawn by the package (and held to its layout rules) or left to
 * mermaid. Skipped without the variable. The counts are the result, printed
 * and asserted only for at least one fence.
 *
 *   LOOM_DIAGRAM_EVAL_DIR=/tmp/loom-diagram-eval npx vitest run src/chat/diagramEval.test.ts
 */
const dir = process.env.LOOM_DIAGRAM_EVAL_DIR ?? "";

test.skipIf(dir === "")(
  "the eval's fences parse",
  async () => {
    mermaid.initialize({ startOnLoad: false, securityLevel: "strict" });
    const lines: string[] = [];
    const kinds = new Map<string, number>();
    let fences = 0;
    let parsed = 0;
    let own = 0;
    for (const file of readdirSync(dir).filter(
      (f) => f.endsWith(".txt") && f !== "summary.txt",
    )) {
      const text = readFileSync(join(dir, file), "utf8");
      for (const m of text.matchAll(
        /^[ \t]*```mermaid[^\n]*\n([\s\S]*?)\n[ \t]*```/gm,
      )) {
        fences++;
        const kind = diagramKind(m[1]);
        kinds.set(kind, (kinds.get(kind) ?? 0) + 1);
        const src = escapeAngles(repair(m[1]));
        const ok = await mermaid.parse(src, { suppressErrors: true });
        if (!ok) {
          lines.push(`REFUSED  ${file} (${kind})`);
          continue;
        }
        parsed++;
        const svg = await editorial(mermaid, src, "eval").catch(() => null);
        if (svg === null) {
          lines.push(`mermaid  ${file} (${kind})`);
          continue;
        }
        own++;
        const d = await mermaid.mermaidAPI.getDiagramFromText(src);
        const ir = toIR(d.type, d.db);
        const broken = ir !== null && fits(ir) ? await checkIR(ir) : [];
        lines.push(
          `own      ${file} (${kind})${broken.length ? ` RULES: ${broken.join("; ")}` : ""}`,
        );
      }
    }
    console.log(
      `${parsed} of ${fences} fences parse, ${own} drawn by the package, ${parsed - own} by mermaid\n` +
        `kinds: ${[...kinds].map(([k, n]) => `${k} ${n}`).join(", ")}\n` +
        lines.join("\n"),
    );
    expect(fences).toBeGreaterThan(0);
  },
  120_000,
);
