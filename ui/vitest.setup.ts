import "@testing-library/jest-dom/vitest";

// jsdom has no ResizeObserver; provide a no-op stub for components that use it.
if (typeof globalThis.ResizeObserver === "undefined") {
  globalThis.ResizeObserver = class {
    observe() {}
    unobserve() {}
    disconnect() {}
  };
}
