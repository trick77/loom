import "@testing-library/jest-dom/vitest";

// jsdom has no ResizeObserver; provide a no-op stub for components that use it.
if (typeof globalThis.ResizeObserver === "undefined") {
  globalThis.ResizeObserver = class {
    observe() {}
    unobserve() {}
    disconnect() {}
  };
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
