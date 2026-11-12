import "@testing-library/jest-dom/vitest";
import { act, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, expect, test, vi } from "vitest";

import {
  IdeaCaptureProvider,
  useIdeaCapture,
  useOpenCapture,
} from "./IdeaCaptureContext";
import { SelectionCapture, SETTLE_MS } from "./SelectionCapture";

function setHover(hover: boolean) {
  window.matchMedia = vi.fn().mockImplementation((query: string) => ({
    matches: query.includes("hover: hover") ? hover : !hover,
    media: query,
    addEventListener: vi.fn(),
    removeEventListener: vi.fn(),
  }));
}

// Probe shows the capture open for m1, as the answer's footer would.
function Probe() {
  const capture = useIdeaCapture();
  const open = useOpenCapture(capture, "m1");
  return <output data-testid="open">{open?.excerpt ?? ""}</output>;
}

function renderThread(threadId: string | null = "t1") {
  return render(
    <IdeaCaptureProvider threadId={threadId} threadTitle="Tech news">
      <div data-idea-prose="m1">
        <p data-testid="prose">
          Vibe Kanban is a board
          <button className="ui-source-pill" type="button">
            1
          </button>{" "}
          for coding agents.
        </p>
      </div>
      <textarea aria-label="composer" />
      <Probe />
      <SelectionCapture />
    </IdeaCaptureProvider>,
  );
}

// Selects from the start over the citation marker into "for": the marker
// drops out of the passage.
function selectProse() {
  const prose = screen.getByTestId("prose");
  const range = document.createRange();
  range.setStart(prose.firstChild!, 0);
  range.setEnd(prose.lastChild!, 4);
  const selection = document.getSelection()!;
  selection.removeAllRanges();
  selection.addRange(range);
}

beforeEach(() => {
  vi.useFakeTimers();
  // jsdom lays nothing out: the selection gets a line on screen.
  Range.prototype.getClientRects = () =>
    [{ top: 200, bottom: 220, left: 100, width: 300, height: 20 }] as never;
});

afterEach(() => {
  document.getSelection()?.removeAllRanges();
  vi.useRealTimers();
});

const pill = () => screen.queryByRole("button", { name: "Save as idea" });

test("desktop: the pill shows above the selection on mouseup and opens the capture with it", () => {
  setHover(true);
  renderThread();
  selectProse();
  // Not while dragging: only once the mouse is up.
  fireEvent(document, new Event("selectionchange"));
  expect(pill()).toBeNull();
  fireEvent.mouseUp(document);
  act(() => vi.runOnlyPendingTimers());
  const button = pill()!;
  expect(button).toHaveStyle({ top: "164px", left: "100px" });
  // Its press keeps the selection and the focus.
  const down = fireEvent.pointerDown(button);
  expect(down).toBe(false);
  fireEvent.mouseUp(button);
  fireEvent.click(button);
  act(() => vi.runOnlyPendingTimers());
  expect(screen.getByTestId("open")).toHaveTextContent(
    "Vibe Kanban is a board for",
  );
  expect(pill()).toBeNull();
});

test("desktop: a collapsed selection or Escape takes the pill away", () => {
  setHover(true);
  renderThread();
  selectProse();
  fireEvent.mouseUp(document);
  act(() => vi.runOnlyPendingTimers());
  expect(pill()).not.toBeNull();
  document.getSelection()!.removeAllRanges();
  fireEvent(document, new Event("selectionchange"));
  expect(pill()).toBeNull();
  selectProse();
  fireEvent.keyUp(document, { key: "Shift" });
  act(() => vi.runOnlyPendingTimers());
  expect(pill()).not.toBeNull();
  fireEvent.keyDown(window, { key: "Escape" });
  expect(pill()).toBeNull();
  // The Escape's own keyup does not bring it back for the same passage.
  fireEvent.keyUp(document, { key: "Escape" });
  act(() => vi.runOnlyPendingTimers());
  expect(pill()).toBeNull();
  // A new selection is offered again.
  document.getSelection()!.removeAllRanges();
  fireEvent(document, new Event("selectionchange"));
  selectProse();
  fireEvent.mouseUp(document);
  act(() => vi.runOnlyPendingTimers());
  expect(pill()).not.toBeNull();
});

test("desktop: the pill follows its selection when the transcript scrolls", () => {
  setHover(true);
  renderThread();
  selectProse();
  fireEvent.mouseUp(document);
  act(() => vi.runOnlyPendingTimers());
  expect(pill()).toHaveStyle({ top: "164px" });
  Range.prototype.getClientRects = () =>
    [{ top: 120, bottom: 140, left: 100, width: 300, height: 20 }] as never;
  fireEvent.scroll(window);
  act(() => vi.runOnlyPendingTimers());
  expect(pill()).toHaveStyle({ top: "84px" });
});

test("touch: a docked bar once the selection rests and no finger is down; it survives its own tap", () => {
  setHover(false);
  renderThread();
  fireEvent.touchStart(document);
  selectProse();
  fireEvent(document, new Event("selectionchange"));
  act(() => vi.advanceTimersByTime(SETTLE_MS + 10));
  // A handle still held.
  expect(pill()).toBeNull();
  fireEvent.touchEnd(document);
  act(() => vi.advanceTimersByTime(SETTLE_MS - 50));
  expect(pill()).toBeNull();
  act(() => vi.advanceTimersByTime(100));
  const bar = pill()!;
  // Thumb-sized.
  expect(bar.className).toContain("h-11");
  // A handle dragged again: gone at once.
  fireEvent.touchStart(document);
  expect(pill()).toBeNull();
  fireEvent.touchEnd(document);
  act(() => vi.advanceTimersByTime(SETTLE_MS + 10));
  // The tap drops the selection on its way: the bar holds what it took.
  fireEvent.touchStart(pill()!);
  fireEvent.pointerDown(pill()!);
  document.getSelection()!.removeAllRanges();
  fireEvent(document, new Event("selectionchange"));
  // The tap's own touchend arms a settle; the open capture drops it.
  fireEvent.touchEnd(pill()!);
  fireEvent.click(pill()!);
  expect(screen.getByTestId("open")).toHaveTextContent(
    "Vibe Kanban is a board for",
  );
  // Not back over the open sheet, not even for a new selection.
  selectProse();
  fireEvent(document, new Event("selectionchange"));
  act(() => vi.advanceTimersByTime(SETTLE_MS + 10));
  expect(pill()).toBeNull();
});

test("touch: no bar while typing", () => {
  setHover(false);
  renderThread();
  selectProse();
  screen.getByLabelText("composer").focus();
  fireEvent(document, new Event("selectionchange"));
  act(() => vi.advanceTimersByTime(SETTLE_MS + 10));
  expect(pill()).toBeNull();
});

test("no stored thread (incognito, a share): no offer", () => {
  setHover(true);
  renderThread(null);
  selectProse();
  fireEvent.mouseUp(document);
  act(() => vi.runOnlyPendingTimers());
  expect(pill()).toBeNull();
});
