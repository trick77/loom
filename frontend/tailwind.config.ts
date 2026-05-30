import type { Config } from "tailwindcss";

export default {
  content: ["./index.html", "./src/**/*.{ts,tsx}"],
  theme: {
    extend: {
      colors: {
        bg: "var(--spark-bg)",
        panel: "var(--spark-panel)",
        active: "var(--spark-active)",
        border: "var(--spark-border)",
        ink: "var(--spark-text)",
        muted: "var(--spark-muted)",
        accent: "var(--spark-accent)",
      },
      fontFamily: {
        sans: ["var(--spark-font-sans)", "ui-sans-serif", "system-ui", "sans-serif"],
        serif: ["var(--spark-font-serif)", "Georgia", "serif"],
      },
      borderRadius: { spark: "12px" },
    },
  },
  plugins: [],
} satisfies Config;
