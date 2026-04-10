import type { Config } from "tailwindcss";

const config: Config = {
  content: [
    "./app/**/*.{js,ts,jsx,tsx}",
    "./components/**/*.{js,ts,jsx,tsx}",
  ],
  theme: {
    extend: {
      fontFamily: {
        sora: ["var(--font-sora)", "Inter", "sans-serif"],
        figtree: ["var(--font-figtree)", "Roboto", "sans-serif"],
      },
      colors: {
        prism: {
          navy:           "#1E3A5F",
          "navy-light":   "#2A4F7A",
          deep:           "#0D1B2A",
          mint:           "#00C9A7",
          "mint-dark":    "#0D8B7D",
          coral:          "#FF6B4A",
          surface:        "#F6F8FB",
          border:         "#E3E8EF",
          "border-light": "#F3F5F8",
          muted:          "#8896A6",
          secondary:      "#4A5568",
        },
      },
    },
  },
  plugins: [],
};

export default config;
