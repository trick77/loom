import { defineConfig } from "vitest/config";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";
import path from "node:path";

export default defineConfig({
  plugins: [react(), tailwindcss()],
  server: { proxy: { "/api": "http://127.0.0.1:8080" } },
  build: {
    outDir: path.resolve(import.meta.dirname, "dist"),
    emptyOutDir: true,
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
              name: "vendor",
              test: /node_modules/,
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
      // json-summary is what scripts/coverage-gate.sh reads (the project floor);
      // lcov is what diff-cover reads in scripts/patch-coverage.sh (patch
      // coverage); text-summary is for humans reading the CI log.
      reporter: ["text-summary", "json-summary", "lcov"],
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
