import { defineConfig } from "vitest/config";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";
import type { ChunkingContext } from "rolldown";

// eager says a module loads with the page: a chain of static imports leads to
// it from an entry. Walked up the importers; a cycle met on the way decides
// nothing, so an answer that passed one is not remembered.
const eagerModules = new Map<string, boolean>();
function eager(id: string, ctx: ChunkingContext): boolean {
  const walking = new Set<string>();
  function up(at: string): { eager: boolean; cut: boolean } {
    const known = eagerModules.get(at);
    if (known !== undefined) return { eager: known, cut: false };
    if (walking.has(at)) return { eager: false, cut: true };
    const info = ctx.getModuleInfo(at);
    if (info === null) return { eager: false, cut: false };
    if (info.isEntry) {
      eagerModules.set(at, true);
      return { eager: true, cut: false };
    }
    walking.add(at);
    let cut = false;
    for (const importer of info.importers) {
      const r = up(importer);
      if (r.eager) {
        walking.delete(at);
        eagerModules.set(at, true);
        return { eager: true, cut: false };
      }
      cut ||= r.cut;
    }
    walking.delete(at);
    if (!cut) eagerModules.set(at, false);
    return { eager: false, cut };
  }
  return up(id).eager;
}

export default defineConfig({
  plugins: [react(), tailwindcss()],
  server: { proxy: { "/api": "http://127.0.0.1:8080" } },
  build: {
    outDir: "../backend/web/dist",
    // ship empties the dir but for its .gitkeep before the build; vite's own
    // emptying would take the .gitkeep with it.
    emptyOutDir: false,
    rolldownOptions: {
      output: {
        // Split heavy, rarely-changing vendor code out of the app chunk so no
        // single chunk crosses Vite's 500 kB warning threshold and the browser
        // can cache these independently across deploys. The markdown + syntax
        // highlighting stack (highlight.js via lowlight) is the largest outlier.
        advancedChunks: {
          groups: [
            {
              name: "markdown",
              test: /node_modules[\\/](highlight\.js|lowlight|react-markdown|rehype-|remark-|micromark|mdast-|hast-|hastscript|unist-|unified|vfile|property-information|character-entities|decode-named-character-reference|devlop)/,
              priority: 20,
            },
            {
              name: "react",
              test: /node_modules[\\/](react|react-dom|scheduler)[\\/]/,
              priority: 10,
            },
            {
              // Only what the page loads at once. A library reached only
              // through an import() stays in the lazy chunks the split gives
              // it: the diagram renderer (mermaid, its layout engine, ~5 MB)
              // loads on the first diagram, and a catch-all here took it into
              // every page load.
              debugName: "vendor",
              name: (id, ctx) =>
                /node_modules/.test(id) && eager(id, ctx) ? "vendor" : null,
              priority: 1,
            },
          ],
        },
      },
    },
  },
  test: {
    environment: "jsdom",
    globals: true,
    setupFiles: ["./src/test/setup.ts"],
    coverage: {
      provider: "v8",
      // lcov is what ship check reads (the UI floor and the patch gate; its
      // ui.test passes the reporters itself); text-summary is for humans.
      reporter: ["text-summary", "lcov"],
      reportsDirectory: "../coverage/ui",
      include: ["src/**/*.{ts,tsx}"],
      exclude: [
        "src/**/*.test.{ts,tsx}",
        "src/**/*.d.ts",
        "src/main.tsx",
        "src/test/**",
      ],
    },
  },
});
