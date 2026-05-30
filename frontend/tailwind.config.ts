import type { Config } from "tailwindcss";

export default {
  content: ["./index.html", "./src/**/*.{ts,tsx}"],
  theme: {
    extend: {
      colors: {
        bg: "var(--eve-bg)",
        panel: "var(--eve-panel)",
        active: "var(--eve-active)",
        border: "var(--eve-border)",
        ink: "var(--eve-text)",
        muted: "var(--eve-muted)",
        accent: "var(--eve-accent)",
      },
      fontFamily: {
        sans: ["var(--eve-font-sans)", "ui-sans-serif", "system-ui", "sans-serif"],
        serif: ["var(--eve-font-serif)", "Georgia", "serif"],
      },
      borderRadius: { eve: "12px" },
    },
  },
  plugins: [],
} satisfies Config;
