import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";
import path from "node:path";

export default defineConfig({
  plugins: [react(), tailwindcss()],
  server: { proxy: { "/api": "http://127.0.0.1:8080" } },
  build: {
    outDir: path.resolve(import.meta.dirname, "../backend/web/dist"),
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
});
