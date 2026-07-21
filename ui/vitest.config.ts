import { defineConfig } from "vitest/config";
import react from "@vitejs/plugin-react";

export default defineConfig({
  plugins: [react()],
  test: {
    environment: "jsdom",
    globals: true,
    setupFiles: ["./vitest.setup.ts"],
    coverage: {
      provider: "v8",
      // json-summary is what hack/coverage-gate.sh reads (the project floor);
      // lcov is what diff-cover reads in hack/patch-coverage.sh (patch
      // coverage); text-summary is for humans reading the CI log.
      reporter: ["text-summary", "json-summary", "lcov"],
      // Repo-root coverage/ui, matching the sibling repos so one gate script
      // works everywhere.
      reportsDirectory: "../coverage/ui",
      include: ["src/**/*.{ts,tsx}"],
      exclude: ["src/**/*.test.{ts,tsx}", "src/**/*.d.ts", "src/main.tsx"],
      // No `thresholds` here on purpose. The project floor now lives in
      // hack/coverage-floors and is enforced by hack/coverage-gate.sh. Two
      // competing definitions of the same floor is exactly the drift this
      // harmonization removes.
    },
  },
});
