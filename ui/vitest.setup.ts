import "@testing-library/jest-dom/vitest";

// Initialize the i18n singleton for every test, mirroring runtime where main.tsx
// imports it before any component renders. Without this, components using
// useTranslation would render raw keys. Tests assert English copy, so pin `en`.
import i18n from "./src/i18n";
void i18n.changeLanguage("en");

// jsdom has no ResizeObserver; provide a no-op stub for components that use it.
if (typeof globalThis.ResizeObserver === "undefined") {
  globalThis.ResizeObserver = class {
    observe() {}
    unobserve() {}
    disconnect() {}
  };
}

// jsdom implements no layout, so it has no scrollIntoView at all; a no-op stub lets
// components that scroll a selection into view mount and be asserted on.
if (typeof Element.prototype.scrollIntoView !== "function") {
  Element.prototype.scrollIntoView = function scrollIntoView() {};
}

// jsdom has no IntersectionObserver; provide a no-op stub so infinite-scroll
// sentinels mount without crashing. Tests that exercise "load more" install a
// controllable mock of their own.
if (typeof globalThis.IntersectionObserver === "undefined") {
  globalThis.IntersectionObserver = class {
    readonly root = null;
    readonly rootMargin = "";
    readonly thresholds: ReadonlyArray<number> = [];
    observe() {}
    unobserve() {}
    disconnect() {}
    takeRecords(): IntersectionObserverEntry[] {
      return [];
    }
  } as unknown as typeof IntersectionObserver;
}
