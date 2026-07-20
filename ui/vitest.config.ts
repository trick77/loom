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
      reporter: ["text", "lcov"],
      include: ["src/**/*.{ts,tsx}"],
      exclude: ["src/**/*.test.{ts,tsx}", "src/**/*.d.ts"],
      // Project floor, not a target. Set just under the level already achieved
      // (70.3% lines) so it pins the gain without going red on noise. Raise it
      // as coverage climbs toward 80. Patch coverage on new lines is enforced
      // separately at 80% by hack/coverage-gate.sh.
      thresholds: {
        lines: 69,
      },
    },
  },
});
