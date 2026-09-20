import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render } from "@testing-library/react";
import {
  SIDEBAR_DEFAULT,
  SIDEBAR_MAX,
  SIDEBAR_MIN,
  SidebarResizer,
  clampSidebar,
  storedSidebarWidth,
} from "./SidebarResizer";

const handle = () =>
  document.querySelector(".ui-sidebar-resizer") as HTMLElement;
const pref = () =>
  document.documentElement.style.getPropertyValue("--ui-sidebar-pref");
const stored = () => localStorage.getItem("loom:sidebar-width");
const valuenow = () => Number(handle().getAttribute("aria-valuenow"));

/** Press on the handle at the current border, so a move to x yields a width of x. */
const grab = (h: HTMLElement, pointerId = 1, pointerType = "mouse") =>
  fireEvent.pointerDown(h, {
    pointerId,
    pointerType,
    button: 0,
    clientX: valuenow(),
  });

beforeEach(() => {
  localStorage.clear();
  document.documentElement.style.removeProperty("--ui-sidebar-pref");
});
afterEach(() => {
  cleanup();
  document.body.classList.remove("resizing");
  vi.restoreAllMocks();
});

describe("clampSidebar", () => {
  it("holds the bounds and rounds", () => {
    expect(clampSidebar(400)).toBe(400);
    expect(clampSidebar(20)).toBe(SIDEBAR_MIN);
    expect(clampSidebar(9999)).toBe(SIDEBAR_MAX);
    expect(clampSidebar(362.6)).toBe(363);
  });

  // The viewport cap is the CSS clamp on --ui-sidebar-w, deliberately not here:
  // clamping to the window and storing that would lose the preference on a rotation.
  it("ignores the viewport", () => {
    expect(clampSidebar(SIDEBAR_MAX)).toBe(SIDEBAR_MAX);
  });
});

describe("storedSidebarWidth", () => {
  it("falls back to the default when nothing is stored", () => {
    expect(storedSidebarWidth()).toBe(SIDEBAR_DEFAULT);
  });

  it("clamps what it reads", () => {
    localStorage.setItem("loom:sidebar-width", "9999");
    expect(storedSidebarWidth()).toBe(SIDEBAR_MAX);
  });

  it("rejects junk", () => {
    localStorage.setItem("loom:sidebar-width", "wide please");
    expect(storedSidebarWidth()).toBe(SIDEBAR_DEFAULT);
  });

  it("survives a storage that throws, as private mode does", () => {
    vi.spyOn(Storage.prototype, "getItem").mockImplementation(() => {
      throw new Error("private mode");
    });
    expect(storedSidebarWidth()).toBe(SIDEBAR_DEFAULT);
  });
});

describe("SidebarResizer", () => {
  it("exposes the separator semantics screen readers need", () => {
    render(<SidebarResizer />);
    const h = handle();
    expect(h).toHaveAttribute("role", "separator");
    expect(h).toHaveAttribute("aria-orientation", "vertical");
    expect(h).toHaveAttribute("aria-valuenow", String(SIDEBAR_DEFAULT));
    expect(h).toHaveAttribute("aria-valuemin", String(SIDEBAR_MIN));
    expect(h).toHaveAttribute("aria-valuemax", String(SIDEBAR_MAX));
    expect(h.tabIndex).toBe(0);
  });

  // Below md the sidebar is an off-canvas drawer at its own fixed width, where a
  // resizer would drag an edge nobody can see.
  it("is hidden below the md breakpoint", () => {
    render(<SidebarResizer />);
    expect(handle().className).toContain("hidden");
    expect(handle().className).toContain("md:block");
  });

  it("paints the stored width on mount", () => {
    localStorage.setItem("loom:sidebar-width", "420");
    render(<SidebarResizer />);
    expect(pref()).toBe("420px");
    expect(valuenow()).toBe(420);
  });

  it("widens and narrows with the arrow keys, and persists", () => {
    render(<SidebarResizer />);
    fireEvent.keyDown(handle(), { key: "ArrowRight" });
    expect(pref()).toBe(SIDEBAR_DEFAULT + 16 + "px");
    expect(stored()).toBe(String(SIDEBAR_DEFAULT + 16));

    fireEvent.keyDown(handle(), { key: "ArrowLeft" });
    expect(pref()).toBe(SIDEBAR_DEFAULT + "px");
    expect(stored()).toBe(String(SIDEBAR_DEFAULT));
  });

  it("clamps at both ends instead of running away", () => {
    localStorage.setItem("loom:sidebar-width", String(SIDEBAR_MAX));
    render(<SidebarResizer />);
    fireEvent.keyDown(handle(), { key: "ArrowRight" });
    expect(valuenow()).toBe(SIDEBAR_MAX);

    cleanup();
    localStorage.setItem("loom:sidebar-width", String(SIDEBAR_MIN));
    render(<SidebarResizer />);
    fireEvent.keyDown(handle(), { key: "ArrowLeft" });
    expect(valuenow()).toBe(SIDEBAR_MIN);
  });

  it("ignores keys it does not own", () => {
    render(<SidebarResizer />);
    document.documentElement.style.removeProperty("--ui-sidebar-pref");
    fireEvent.keyDown(handle(), { key: "a" });
    expect(pref()).toBe("");
  });

  it("resets to the default on Home", () => {
    localStorage.setItem("loom:sidebar-width", "480");
    render(<SidebarResizer />);
    fireEvent.keyDown(handle(), { key: "Home" });
    expect(valuenow()).toBe(SIDEBAR_DEFAULT);
    expect(stored()).toBe(String(SIDEBAR_DEFAULT));
  });

  it("resizes on a pointer drag and stores once, on release", () => {
    render(<SidebarResizer />);
    const h = handle();
    grab(h);
    fireEvent.pointerMove(h, { pointerId: 1, clientX: 420 });
    expect(pref()).toBe("420px");
    expect(stored()).toBeNull(); // nothing written mid-drag

    fireEvent.pointerUp(h, { pointerId: 1 });
    expect(valuenow()).toBe(420);
    expect(stored()).toBe("420");
    expect(document.body.classList.contains("resizing")).toBe(false);
  });

  // The grid carries transition-[grid-template-columns]; without suppressing it the
  // column would animate behind every frame of the drag and lag the pointer.
  it("marks the body as resizing for the length of the drag", () => {
    render(<SidebarResizer />);
    const h = handle();
    grab(h);
    expect(document.body.classList.contains("resizing")).toBe(true);
    fireEvent.pointerMove(h, { pointerId: 1, clientX: 420 });
    expect(document.body.classList.contains("resizing")).toBe(true);
    fireEvent.pointerUp(h, { pointerId: 1 });
    expect(document.body.classList.contains("resizing")).toBe(false);
  });

  it("clamps a drag past the edges", () => {
    render(<SidebarResizer />);
    const h = handle();
    grab(h);
    fireEvent.pointerMove(h, { pointerId: 1, clientX: 20 });
    expect(pref()).toBe(SIDEBAR_MIN + "px");
    fireEvent.pointerMove(h, { pointerId: 1, clientX: 5000 });
    expect(pref()).toBe(SIDEBAR_MAX + "px");
    fireEvent.pointerUp(h, { pointerId: 1 });
    expect(stored()).toBe(String(SIDEBAR_MAX));
  });

  // Regression from ../transmission-ui: performance.now() is milliseconds since
  // load, so comparing against a 0 sentinel swallowed the first drag as a double tap.
  it("drags on the first interaction after load", () => {
    vi.spyOn(performance, "now").mockReturnValue(120);
    render(<SidebarResizer />);
    const h = handle();
    grab(h, 1, "touch");
    fireEvent.pointerMove(h, { pointerId: 1, clientX: 420 });
    fireEvent.pointerUp(h, { pointerId: 1 });
    expect(valuenow()).toBe(420);
  });

  // Regression: pointerup used to commit the width captured when the drag began.
  it("commits the last dragged width, not the width at pointerdown", () => {
    render(<SidebarResizer />);
    const h = handle();
    grab(h);
    fireEvent.pointerMove(h, { pointerId: 1, clientX: 420 });
    fireEvent.pointerMove(h, { pointerId: 1, clientX: 470 });
    fireEvent.pointerUp(h, { pointerId: 1 });
    expect(valuenow()).toBe(470);
    expect(stored()).toBe("470");
  });

  it("ignores a move that is not part of a drag", () => {
    render(<SidebarResizer />);
    document.documentElement.style.removeProperty("--ui-sidebar-pref");
    fireEvent.pointerMove(handle(), { pointerId: 1, clientX: 420 });
    expect(pref()).toBe("");
  });

  it("treats pointercancel like a release", () => {
    render(<SidebarResizer />);
    const h = handle();
    grab(h);
    fireEvent.pointerMove(h, { pointerId: 1, clientX: 400 });
    fireEvent.pointerCancel(h, { pointerId: 1 });
    expect(stored()).toBe("400");
    expect(document.body.classList.contains("resizing")).toBe(false);
  });

  it("ignores a non-primary mouse button", () => {
    render(<SidebarResizer />);
    const h = handle();
    document.documentElement.style.removeProperty("--ui-sidebar-pref");
    fireEvent.pointerDown(h, { pointerId: 1, pointerType: "mouse", button: 2 });
    fireEvent.pointerMove(h, { pointerId: 1, clientX: 420 });
    expect(pref()).toBe("");
  });

  // Review finding in ../transmission-ui: the drag used the raw clientX, so grabbing
  // the handle off-centre snapped the border to the pointer.
  it("moves by the grab offset, not to the pointer", () => {
    render(<SidebarResizer />);
    const h = handle();
    fireEvent.pointerDown(h, {
      pointerId: 1,
      pointerType: "mouse",
      button: 0,
      clientX: SIDEBAR_DEFAULT + 6,
    });
    fireEvent.pointerMove(h, { pointerId: 1, clientX: SIDEBAR_DEFAULT + 36 });
    expect(pref()).toBe(SIDEBAR_DEFAULT + 30 + "px");
    fireEvent.pointerUp(h, { pointerId: 1 });
    expect(valuenow()).toBe(SIDEBAR_DEFAULT + 30);
  });

  // Review finding: a touch screen emits a pixel of jitter during a plain tap, and
  // treating it as a drag disarmed the double-tap reset.
  it("ignores tap jitter, so a jittery double tap still resets", () => {
    const clock = vi.spyOn(performance, "now");
    localStorage.setItem("loom:sidebar-width", "480");
    render(<SidebarResizer />);
    const h = handle();

    clock.mockReturnValue(1000);
    fireEvent.pointerDown(h, {
      pointerId: 1,
      pointerType: "touch",
      clientX: 480,
    });
    fireEvent.pointerMove(h, { pointerId: 1, clientX: 481 });
    fireEvent.pointerUp(h, { pointerId: 1 });
    expect(valuenow()).toBe(480);

    clock.mockReturnValue(1150);
    fireEvent.pointerDown(h, {
      pointerId: 1,
      pointerType: "touch",
      clientX: 480,
    });
    expect(valuenow()).toBe(SIDEBAR_DEFAULT);
  });

  // Review finding: releasePointerCapture throws NotFoundError once the pointer is
  // gone, and the throw used to skip the commit.
  it("commits before it releases capture, and swallows a throw", () => {
    render(<SidebarResizer />);
    const h = handle();
    const order: string[] = [];
    h.hasPointerCapture = () => true;
    h.releasePointerCapture = () => {
      order.push("release");
      throw new DOMException("gone", "NotFoundError");
    };

    grab(h);
    fireEvent.pointerMove(h, { pointerId: 1, clientX: 420 });
    fireEvent.pointerCancel(h, { pointerId: 1 });

    expect(valuenow()).toBe(420);
    expect(stored()).toBe("420");
    expect(order).toEqual(["release"]);
  });

  // Review finding: a completed drag used to arm the double-tap window, so
  // re-grabbing to fine-tune snapped back to the default.
  it("does not treat a re-grab right after a drag as a double tap", () => {
    const clock = vi.spyOn(performance, "now");
    render(<SidebarResizer />);
    const h = handle();
    clock.mockReturnValue(1000);
    grab(h);
    fireEvent.pointerMove(h, { pointerId: 1, clientX: 420 });
    fireEvent.pointerUp(h, { pointerId: 1 });
    expect(valuenow()).toBe(420);

    clock.mockReturnValue(1100);
    grab(h);
    fireEvent.pointerMove(h, { pointerId: 1, clientX: 436 });
    fireEvent.pointerUp(h, { pointerId: 1 });
    expect(valuenow()).toBe(436);
  });

  // Review finding: a second finger landing mid-drag hijacked the drag state.
  it("ignores a second pointer during a drag", () => {
    render(<SidebarResizer />);
    const h = handle();
    grab(h, 1, "touch");
    fireEvent.pointerMove(h, { pointerId: 1, clientX: 400 });
    fireEvent.pointerDown(h, {
      pointerId: 2,
      pointerType: "touch",
      clientX: 400,
    });
    fireEvent.pointerMove(h, { pointerId: 2, clientX: 300 });
    fireEvent.pointerUp(h, { pointerId: 2 });
    expect(pref()).toBe("400px");
    fireEvent.pointerMove(h, { pointerId: 1, clientX: 430 });
    fireEvent.pointerUp(h, { pointerId: 1 });
    expect(valuenow()).toBe(430);
    expect(stored()).toBe("430");
  });

  // preventDefault on pointerdown kills the compatibility mousedown, and the focus
  // that comes with it.
  it("focuses the handle on grab, so the arrow keys work straight after", () => {
    render(<SidebarResizer />);
    const h = handle();
    fireEvent.pointerDown(h, {
      pointerId: 1,
      pointerType: "mouse",
      button: 0,
    });
    expect(document.activeElement).toBe(h);
  });

  it("resets on a double tap", () => {
    const clock = vi.spyOn(performance, "now");
    localStorage.setItem("loom:sidebar-width", "480");
    render(<SidebarResizer />);
    const h = handle();
    clock.mockReturnValue(1000);
    fireEvent.pointerDown(h, { pointerId: 1, pointerType: "touch" });
    fireEvent.pointerUp(h, { pointerId: 1 });
    clock.mockReturnValue(1200);
    fireEvent.pointerDown(h, { pointerId: 1, pointerType: "touch" });
    expect(valuenow()).toBe(SIDEBAR_DEFAULT);
    expect(stored()).toBe(String(SIDEBAR_DEFAULT));
  });

  // Review finding: the arrow keys stepped the preference while the drag stepped
  // the rendered edge, so with a preference above the 40vw cap the first presses
  // moved nothing on screen and aria-valuenow announced a width the layout did
  // not have. jsdom lays nothing out, so the cap is simulated by measuring.
  it("steps the rendered width, not a preference the clamp is holding back", () => {
    localStorage.setItem("loom:sidebar-width", "520");
    render(<SidebarResizer />);
    const h = handle();
    const aside = document.createElement("div");
    aside.className = "ui-sidebar-text";
    aside.getBoundingClientRect = () => ({ width: 440 }) as DOMRect;
    document.body.appendChild(aside);

    fireEvent.keyDown(h, { key: "ArrowLeft" });
    expect(valuenow()).toBe(424); // 440 - 16, the edge the user can see
    aside.remove();
  });

  // Review finding: a drag on a capped viewport used to commit the capped number
  // and throw the wider preference away, which is what the clamp exists to stop.
  it("keeps a wider stored preference when a capped drag widens", () => {
    localStorage.setItem("loom:sidebar-width", "520");
    render(<SidebarResizer />);
    const h = handle();
    const aside = document.createElement("div");
    aside.className = "ui-sidebar-text";
    aside.getBoundingClientRect = () => ({ width: 440 }) as DOMRect;
    document.body.appendChild(aside);

    fireEvent.pointerDown(h, {
      pointerId: 1,
      pointerType: "mouse",
      button: 0,
      clientX: 440,
    });
    fireEvent.pointerMove(h, { pointerId: 1, clientX: 445 });
    fireEvent.pointerUp(h, { pointerId: 1 });
    expect(stored()).toBe("520"); // the docked-monitor width survives
    aside.remove();
  });

  // A deliberate narrowing is taken at face value, cap or no cap.
  it("lowers the stored preference when the user narrows", () => {
    localStorage.setItem("loom:sidebar-width", "520");
    render(<SidebarResizer />);
    const h = handle();
    fireEvent.pointerDown(h, {
      pointerId: 1,
      pointerType: "mouse",
      button: 0,
      clientX: 520,
    });
    fireEvent.pointerMove(h, { pointerId: 1, clientX: 400 });
    fireEvent.pointerUp(h, { pointerId: 1 });
    expect(stored()).toBe("400");
  });

  // Review finding: a cancel before the slop left the double-tap window armed, so
  // the next deliberate grab inside 350ms was swallowed as a reset.
  it("does not let a cancelled tap arm the double-tap reset", () => {
    const clock = vi.spyOn(performance, "now");
    localStorage.setItem("loom:sidebar-width", "480");
    render(<SidebarResizer />);
    const h = handle();

    clock.mockReturnValue(1000);
    fireEvent.pointerDown(h, {
      pointerId: 1,
      pointerType: "touch",
      clientX: 480,
    });
    fireEvent.pointerCancel(h, { pointerId: 1 }); // system took the pointer

    clock.mockReturnValue(1100); // inside the tap window
    fireEvent.pointerDown(h, {
      pointerId: 1,
      pointerType: "touch",
      clientX: 480,
    });
    fireEvent.pointerMove(h, { pointerId: 1, clientX: 450 });
    fireEvent.pointerUp(h, { pointerId: 1 });
    expect(valuenow()).toBe(450); // dragged, not reset to 362
  });

  it("does not reset two slow taps", () => {
    const clock = vi.spyOn(performance, "now");
    localStorage.setItem("loom:sidebar-width", "480");
    render(<SidebarResizer />);
    const h = handle();
    clock.mockReturnValue(1000);
    fireEvent.pointerDown(h, { pointerId: 1, pointerType: "touch" });
    fireEvent.pointerUp(h, { pointerId: 1 });
    clock.mockReturnValue(3000);
    fireEvent.pointerDown(h, { pointerId: 1, pointerType: "touch" });
    fireEvent.pointerUp(h, { pointerId: 1 });
    expect(valuenow()).toBe(480);
  });
});
