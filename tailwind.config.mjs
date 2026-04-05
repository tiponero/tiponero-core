/** @type {import('tailwindcss').Config} */
module.exports = {
  darkMode: "class",
  content: ["./internal/views/**/*.templ"],
  theme: {
    extend: {
      colors: {
        // Primary: Monero orange
        "primary":                      "var(--brand-500, #FF6600)",
        "primary-container":            "var(--brand-500, #FF6600)",
        "on-primary":                   "#000000",
        "on-primary-container":         "#000000",
        "primary-fixed":                "#ffdbcd",
        "primary-fixed-dim":            "#FF6600",
        "on-primary-fixed":             "#000000",
        "on-primary-fixed-variant":     "#7c2e00",
        "inverse-primary":              "#a33e00",

        // Secondary: Warm grays
        "secondary":                    "#c8c6c5",
        "secondary-container":          "#474746",
        "on-secondary":                 "#303030",
        "on-secondary-container":       "#b7b5b4",
        "secondary-fixed":              "#e5e2e1",
        "secondary-fixed-dim":          "#c8c6c5",
        "on-secondary-fixed":           "#1b1c1c",
        "on-secondary-fixed-variant":   "#474746",

        // Tertiary: Cool blue
        "tertiary":                     "#9ccaff",
        "tertiary-container":           "#009cfc",
        "on-tertiary":                  "#003256",
        "on-tertiary-container":        "#003155",
        "tertiary-fixed":               "#d0e4ff",
        "tertiary-fixed-dim":           "#9ccaff",
        "on-tertiary-fixed":            "#001d35",
        "on-tertiary-fixed-variant":    "#00497b",

        // Error: Light red
        "error":                        "#ffb4ab",
        "error-container":              "#93000a",
        "on-error":                     "#690005",
        "on-error-container":           "#ffdad6",

        // Surface and background
        "surface":                      "#131313",
        "surface-dim":                  "#131313",
        "surface-bright":               "#393939",
        "surface-container-lowest":     "#0e0e0e",
        "surface-container-low":        "#1c1b1b",
        "surface-container":            "#201f1f",
        "surface-container-high":       "#2a2a2a",
        "surface-container-highest":    "#353534",
        "surface-variant":              "#353534",
        "surface-tint":                 "#FF6600",

        // On-surface text
        "on-surface":                   "#e5e2e1",
        "on-surface-variant":           "#e3bfb1",
        "on-background":                "#e5e2e1",
        "background":                   "#131313",
        "inverse-surface":              "#e5e2e1",
        "inverse-on-surface":           "#313030",

        // Outlines
        "outline":                      "#aa8a7d",
        "outline-variant":              "#5a4136",

        // Utility
        "monero-gray":                  "#4d4d4d",

        // Widget-specific
        "brand": {
          50:  "#fff7ed",
          100: "#ffedd5",
          200: "#fed7aa",
          300: "#fdba74",
          400: "#fb923c",
          500: "var(--brand-500, #ff6600)",
          600: "var(--brand-600, #ea580c)",
          700: "#c2410c",
          800: "#9a3412",
          900: "#7c2d12",
        },
      },
      fontFamily: {
        "headline": ['"Space Grotesk"', "sans-serif"],
        "body":     ['"Inter"', "sans-serif"],
        "label":    ['"Inter"', "sans-serif"],
        "mono":     ['"JetBrains Mono"', "monospace"],
      },
      borderRadius: {
        "DEFAULT": "0.125rem",
        "sm":      "0.125rem",
        "md":      "0.375rem",
        "lg":      "0.25rem",
        "xl":      "0.5rem",
        "2xl":     "0.75rem",
        "full":    "9999px",
      },
    },
  },
  plugins: [],
};
