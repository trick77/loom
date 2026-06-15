import { expect, test } from "vitest";
import { splitIntoSegments } from "./streamFade";

test("preserves the full text when segments are rejoined", () => {
  const input = "Das ist ein etwas längerer Satz, der mehrere Segmente ergibt.";
  expect(splitIntoSegments(input).join("")).toBe(input);
});

test("keeps trailing whitespace inside segments so spacing survives", () => {
  const segs = splitIntoSegments("alpha beta gamma");
  expect(segs.join("")).toBe("alpha beta gamma");
  // jedes Segment außer dem letzten endet mit Whitespace
  for (const seg of segs.slice(0, -1)) {
    expect(/\s$/.test(seg)).toBe(true);
  }
});

test("breaks at clause punctuation", () => {
  const segs = splitIntoSegments("Hallo, Welt. Noch was");
  // Komma und Punkt beenden ein Segment -> erstes Segment endet auf Komma
  expect(segs[0]).toBe("Hallo, ");
});

// Kernmechanik: bereits gesetzte Segmente werden bei wachsendem Input nie
// revidiert -> stabile Präfixe -> React reused die DOM-Nodes -> kein Re-Fade.
test("settled segments form a stable prefix as the text grows", () => {
  const base = "Die Geschichte des Kaffees beginnt in Äthiopien und reicht weit zurück.";
  for (let i = 10; i < base.length; i += 7) {
    const shorter = splitIntoSegments(base.slice(0, i));
    const longer = splitIntoSegments(base.slice(0, i + 7));
    // Alle Segmente außer dem letzten (wachsenden) müssen identisch bleiben.
    const settled = shorter.slice(0, -1);
    expect(longer.slice(0, settled.length)).toEqual(settled);
  }
});

test("handles empty and whitespace-only input without throwing", () => {
  expect(splitIntoSegments("")).toEqual([""]);
  expect(splitIntoSegments("   ").join("")).toBe("   ");
});
