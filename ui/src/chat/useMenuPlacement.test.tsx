import "@testing-library/jest-dom/vitest";
import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, test, vi } from "vitest";

import { useMenuPlacement } from "./useMenuPlacement";

// jsdom lays nothing out: geometry comes from data attributes, and a menu's
// offsetParent is its parent element (the positioned anchor).
function Menu({ fitAbove, gap }: { fitAbove?: boolean; gap?: number }) {
  const { menuRef, dropUp } = useMenuPlacement({ fitAbove, gap });
  return (
    <div ref={menuRef} data-h="268" data-testid="menu">
      {dropUp ? "up" : "down"}
    </div>
  );
}

function renderAt(top: number, fitAbove?: boolean, gap?: number) {
  render(
    <div data-top={top} data-bottom={top + 28}>
      <Menu fitAbove={fitAbove} gap={gap} />
    </div>,
  );
  return screen.getByTestId("menu").textContent;
}

beforeEach(() => {
  vi.spyOn(window, "innerHeight", "get").mockReturnValue(480);
  Object.defineProperty(HTMLElement.prototype, "offsetParent", {
    configurable: true,
    get(this: HTMLElement) {
      return this.parentElement;
    },
  });
  Object.defineProperty(HTMLElement.prototype, "offsetHeight", {
    configurable: true,
    get(this: HTMLElement) {
      return Number(this.dataset.h ?? 0);
    },
  });
  vi.spyOn(HTMLElement.prototype, "getBoundingClientRect").mockImplementation(
    function (this: HTMLElement) {
      const top = Number(this.dataset.top ?? 0);
      const bottom = Number(this.dataset.bottom ?? 0);
      return { top, bottom, left: 0, right: 0, width: 0, height: 0 } as DOMRect;
    },
  );
});

afterEach(() => {
  vi.restoreAllMocks();
  delete (HTMLElement.prototype as { offsetParent?: unknown }).offsetParent;
  delete (HTMLElement.prototype as { offsetHeight?: unknown }).offsetHeight;
});

describe("useMenuPlacement", () => {
  test("opens down while the menu fits below", () => {
    expect(renderAt(100)).toBe("down");
  });

  test("flips up when there is more room above", () => {
    expect(renderAt(260)).toBe("up");
  });

  test("fitAbove keeps it down when it would not fit above either", () => {
    expect(renderAt(260, true)).toBe("down");
  });

  test("fitAbove flips once the whole menu fits above", () => {
    expect(renderAt(300, true)).toBe("up");
  });

  test("fitAbove counts the caller's gap", () => {
    // 268px menu: 272 above fits a 4px gap, not a 6px one.
    expect(renderAt(272, true)).toBe("up");
    cleanup();
    expect(renderAt(272, true, 6)).toBe("down");
  });
});
