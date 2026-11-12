import { afterEach, beforeEach, expect, test } from "vitest";

import { selectionTarget } from "./selectionTarget";

beforeEach(() => {
  document.body.innerHTML = `
    <div id="m1">
      <div data-idea-prose="m1"><p id="a">Vibe Kanban is a board for coding agents.</p></div>
      <div id="trace">Searched the web</div>
      <div data-idea-prose="m1"><p id="b">Each agent works in its own worktree.<button class="ui-source-pill" id="pill">1</button> Diffs are reviewed on the card.</p>
        <pre><code id="code">go test ./...</code><button id="copy">Copy</button></pre></div>
      <div id="footer"><button id="bulb">Save as idea</button></div>
    </div>
    <div class="ui-user-message"><p id="user">What is Vibe Kanban?</p></div>
    <div data-idea-prose="m2"><p id="c">Another answer entirely.</p></div>
  `;
});

afterEach(() => {
  document.getSelection()?.removeAllRanges();
});

const text = (id: string) => document.getElementById(id)!.firstChild!;

function select(
  start: Node,
  startOffset: number,
  end: Node,
  endOffset: number,
): Selection {
  const selection = document.getSelection()!;
  selection.removeAllRanges();
  const range = document.createRange();
  range.setStart(start, startOffset);
  range.setEnd(end, endOffset);
  selection.addRange(range);
  return selection;
}

test("a passage inside one answer's prose is offered, trimmed", () => {
  expect(selectionTarget(select(text("a"), 0, text("a"), 12))).toEqual({
    messageId: "m1",
    text: "Vibe Kanban",
  });
});

test("across two text blocks of the same answer it counts, without the trace between them", () => {
  expect(selectionTarget(select(text("a"), 0, text("b"), 10))).toEqual({
    messageId: "m1",
    text: "Vibe Kanban is a board for coding agents.\n\nEach agent",
  });
});

// A citation marker renders as a button holding only its number, right
// behind the word: its text is no part of the passage, and an end on it
// still counts.
test("citation markers drop out of the passage; an end on one counts", () => {
  const b = document.getElementById("b")!;
  const after = b.childNodes[2];
  expect(selectionTarget(select(text("b"), 0, after, 6))?.text).toBe(
    "Each agent works in its own worktree. Diffs",
  );
  expect(selectionTarget(select(text("b"), 5, text("pill"), 1))?.text).toBe(
    "agent works in its own worktree.",
  );
});

test("a triple-click ending at the start of what follows counts", () => {
  const footer = document.getElementById("footer")!;
  // From the last prose block's start to offset 0 of the footer.
  expect(selectionTarget(select(text("b"), 0, footer, 0))?.messageId).toBe(
    "m1",
  );
  // Something actually selected outside the prose does not.
  const trace = document.getElementById("trace")!;
  expect(
    selectionTarget(select(text("a"), 0, trace.firstChild!, 5)),
  ).toBeNull();
});

test("two answers, a user's bubble, a control or too little text: nothing", () => {
  expect(selectionTarget(select(text("a"), 0, text("c"), 5))).toBeNull();
  expect(selectionTarget(select(text("user"), 0, text("user"), 10))).toBeNull();
  // Only a button's label: nothing of the passage.
  expect(selectionTarget(select(text("copy"), 0, text("copy"), 4))).toBeNull();
  expect(selectionTarget(select(text("a"), 0, text("a"), 2))).toBeNull();
  expect(selectionTarget(select(text("a"), 4, text("a"), 7))).toBeNull();
  // Code inside the prose is prose.
  expect(selectionTarget(select(text("code"), 0, text("code"), 7))?.text).toBe(
    "go test",
  );
});

test("code keeps its indentation and brackets; formulas and table cells read right", () => {
  document.body.innerHTML = `
    <div data-idea-prose="m1">
      <pre><code id="py">def f(a):
    return a[0]</code></pre>
      <p id="math">Area is <span class="katex"><span class="katex-mathml">x^2 x2</span><span class="katex-html">x²</span></span> here.</p>
      <table><tbody><tr id="row"><td>Bob</td><td>42</td></tr></tbody></table>
    </div>`;
  const py = document.getElementById("py")!.firstChild!;
  expect(
    selectionTarget(select(py, 0, py, (py.textContent ?? "").length))?.text,
  ).toBe("def f(a):\n    return a[0]");
  const math = document.getElementById("math")!;
  expect(
    selectionTarget(select(math.firstChild!, 0, math.lastChild!, 5))?.text,
  ).toBe("Area is x² here");
  const row = document.getElementById("row")!;
  expect(selectionTarget(select(row, 0, row, 2))?.text).toBe("Bob 42");
});

test("no selection or a collapsed one: nothing", () => {
  expect(selectionTarget(null)).toBeNull();
  expect(selectionTarget(select(text("a"), 3, text("a"), 3))).toBeNull();
});
