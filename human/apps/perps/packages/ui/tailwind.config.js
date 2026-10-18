import { withTV } from "tailwind-variants/transformer";
import {
  darkThemePlugin,
  basePlugin,
  sizePlugin,
  chartPlugin,
  gradientPlugin,
  positionPlugin,
  componentsPlugin,
  scrollBarPlugin,
} from "./src/tailwind";

/** @type {import('tailwindcss').Config} */
module.exports = withTV({
  mode: "jit",
  darkMode: ["class"],
  content: [
    "./src/**/*.{ts,js,tsx,jsx}",
    "../ui-connector/src/**/*.{ts,js,tsx,jsx}",
    "../ui-orders/src/**/*.{ts,js,tsx,jsx}",
    "../ui-order-entry/src/**/*.{ts,js,tsx,jsx}",
    "../ui-positions/src/**/*.{ts,js,tsx,jsx}",
    "../ui-leverage/src/**/*.{ts,js,tsx,jsx}",
    "../ui-share/src/**/*.{ts,js,tsx,jsx}",
    "../trading-rewards/src/**/*.{ts,js,tsx,jsx}",
    "../portfolio/src/**/*.{ts,js,tsx,jsx}",
    "../ui-scaffold/src/**/*.{ts,js,tsx,jsx}",
    "../affiliate/src/**/*.{ts,js,tsx,jsx}",
    "../markets/src/**/*.{ts,js,tsx,jsx}",
    "../vaults/src/**/*.{ts,js,tsx,jsx}",
    "../chart/src/**/*.{ts,js,tsx,jsx}",
    "../ui-transfer/src/**/*.{ts,js,tsx,jsx}",
    "../trading/src/**/*.{ts,js,tsx,jsx}",
    "../ui-order-entry/src/**/*.{ts,js,tsx,jsx}",
    "../ui-tpsl/src/**/*.{ts,js,tsx,jsx}",
    "../ui-tradingview/src/**/*.{ts,js,tsx,jsx}",
    "../ui-chain-selector/src/**/*.{ts,js,tsx,jsx}",
    "../wallet-connector/src/**/*.{ts,js,tsx,jsx}",
    "../wallet-connector-privy/src/**/*.{ts,js,tsx,jsx}",
    "../trading-leaderboard/src/**/*.{ts,js,tsx,jsx}",
    "../app/src/**/*.{ts,js,tsx,jsx}",
    "../ui-notification/src/**/*.{ts,js,tsx,jsx}",
  ],
  prefix: "oui-",
  theme: {
    screens: {
      sm: "375px",
      md: "480px",
      lg: "768px",
      xl: "1024px",
      "2xl": "1280px",
      "3xl": "1440px",
      "4xl": "1680px",
      "5xl": "1920px",
    },
    fontSize: {
      "3xs": ["var(--oui-font-size-3xs, 0.625rem)", "0.625rem"], // 10px
      "2xs": ["var(--oui-font-size-2xs, 0.75rem)", "1.125rem"], // 12px
      xs: ["var(--oui-font-size-xs, calc(0.875rem - 1px))", "1.25rem"], // 13px
      sm: ["var(--oui-font-size-sm,0.875rem)", "1.25rem"], // 14px
      base: ["var(--oui-font-size-base, 1rem)", "1.5rem"], // 16px
      lg: ["var(--oui-font-size-lg, 1.125rem)", "1.625rem"], // 18px
      xl: ["var(--oui-font-size-xl, 1.25rem)", "1.75rem"], // 20px
      "2xl": ["var(--oui-font-size-2xl, 1.5rem)", "2rem"], // 24px
      "3xl": ["var(--oui-font-size-3xl, 1.75rem)", "2.25rem"], // 28px
      "4xl": ["var(--oui-font-size-4xl, 1.875rem)", "2.375rem"], // 30px
      "5xl": ["var(--oui-font-size-5xl, 2.25rem)", "2.75rem"], // 36px
      "6xl": ["var(--oui-font-size-6xl, 3rem)", "3rem"], // 48px
    },
    borderRadius: {
      none: "0",
      sm: "var(--oui-rounded-sm, 0.125rem)", // 2px
      DEFAULT: "var(--oui-rounded, 0.25rem)", // 4px
      md: "var(--oui-rounded-md, 0.375rem)", // 6px
      lg: "var(--oui-rounded-lg, 0.5rem)", // 8px
      xl: "var(--oui-rounded-xl, 0.75rem)", // 12px
      "2xl": "var(--oui-rounded-2xl, 1rem)", // 16px
      full: "var(--oui-rounded-full, 9999px)",
    },
    // boxShadow: {
    //   sm: "var(--oui-shadow-sm, 0 1px 2px 0 rgb(0 0 0 / 0.05))",
    //   DEFAULT:
    //     "var(--oui-shadow, 0 1px 3px 0 rgb(0 0 0 / 0.1), 0 1px 2px -1px rgb(0 0 0 / 0.1))",
    //   md: "var(--oui-shadow-md, 0 4px 6px -1px rgb(0 0 0 / 0.1), 0 2px 4px -2px rgb(0 0 0 / 0.1))",
    //   lg: "var(--oui-shadow-lg, 0 10px 15px -3px rgb(0 0 0 / 0.1), 0 4px 6px -4px rgb(0 0 0 / 0.1))",
    //   xl: "var(--oui-shadow-xl, 0 20px 25px -5px rgb(0 0 0 / 0.1), 0 8px 10px -6px rgb(0 0 0 / 0.1))",
    //   "2xl": "var(--oui-shadow-2xl, 0 25px 50px -12px rgb(0 0 0 / 0.25))",
    // },
    extend: {
      spacing: {
        13: "3.25rem",
      },
      cursor: {
        "svg-edit":
          'url("data:image/svg+xml,%3Csvg%20xmlns%3D%22http%3A%2F%2Fwww.w3.org%2F2000%2Fsvg%22%20width%3D%2214%22%20height%3D%2214%22%20viewBox%3D%220%200%2014%2014%22%20fill%3D%22none%22%3E%3Cg%20clip-path%3D%22url%28%23clip0_22040_41827%29%22%3E%3Cpath%20d%3D%22M11.4548%200.693201C11.1139%200.352266%2010.5611%200.352266%2010.2202%200.693201L8.98558%201.92784L12.0722%205.01442L13.3068%203.77979C13.6477%203.43885%2013.6477%202.88609%2013.3068%202.54515L11.4548%200.693201Z%22%20fill%3D%22white%22%20fill-opacity%3D%220.98%22%2F%3E%3Cpath%20d%3D%22M11.1462%205.9404L8.0596%202.85381L0.960453%209.95296L0.443602%2013.0541C0.394363%2013.3495%200.650496%2013.6056%200.945935%2013.5564L4.04704%2013.0395L11.1462%205.9404Z%22%20fill%3D%22white%22%20fill-opacity%3D%220.98%22%2F%3E%3C%2Fg%3E%3Cdefs%3E%3CclipPath%20id%3D%22clip0_22040_41827%22%3E%3Crect%20width%3D%2214%22%20height%3D%2214%22%20fill%3D%22white%22%2F%3E%3C%2FclipPath%3E%3C%2Fdefs%3E%3C%2Fsvg%3E") 2 12, auto',
      },
      maxWidth: {
        xs: "var(--oui-spacing-xs, 20rem)", // 320px
        sm: "var(--oui-spacing-sm, 22.5rem)", // 360px
        md: "var(--oui-spacing-md, 26.25rem)", // 420px
        lg: "var(--oui-spacing-lg, 30rem)", // 480px
        xl: "var(--oui-spacing-xl, 33.75rem)", // 540px
      },
      colors: {
        primary: {
          DEFAULT: "rgb(var(--oui-color-primary) / <alpha-value>)",
          light: "rgb(var(--oui-color-primary-light) / <alpha-value>)",
          darken: "rgb(var(--oui-color-primary-darken) / <alpha-value>)",
          contrast: "rgb(var(--oui-color-primary-contrast) / <alpha-value>)",
        },
        secondary: {
          DEFAULT: "rgb(var(--oui-color-secondary) / <alpha-value>)",
        },
        tertiary: {
          DEFAULT: "rgb(var(--oui-color-tertiary) / <alpha-value>)",
        },

        quaternary: {
          DEFAULT: "rgb(var(--oui-color-quaternary) / <alpha-value>)",
        },
        link: {
          DEFAULT: "rgb(var(--oui-color-link) / <alpha-value>)",
          light: "rgb(var(--oui-color-link-light) / <alpha-value>)",
        },
        danger: {
          DEFAULT: "rgb(var(--oui-color-danger) / <alpha-value>)",
          light: "rgb(var(--oui-color-danger-light) / <alpha-value>)",
          darken: "rgb(var(--oui-color-danger-darken) / <alpha-value>)",
          contrast: "rgb(var(--oui-color-danger-contrast) / <alpha-value>)",
        },
        warning: {
          DEFAULT: "rgb(var(--oui-color-warning) / <alpha-value>)",
          light: "rgb(var(--oui-color-warning-light) / <alpha-value>)",
          darken: "rgb(var(--oui-color-warning-darken) / <alpha-value>)",
          contrast: "rgb(var(--oui-color-warning-contrast) / <alpha-value>)",
        },
        success: {
          DEFAULT: "rgb(var(--oui-color-success) / <alpha-value>)",
          light: "rgb(var(--oui-color-success-light) / <alpha-value>)",
          darken: "rgb(var(--oui-color-success-darken) / <alpha-value>)",
          contrast: "rgb(var(--oui-color-success-contrast) / <alpha-value>)",
        },
        fill: {
          DEFAULT: "rgb(var(--oui-color-fill) / <alpha-value>)",
          active: "rgb(var(--oui-color-fill-active) / <alpha-value>)",
        },
        base: {
          1: "rgb(var(--oui-color-base-1) / <alpha-value>)",
          2: "rgb(var(--oui-color-base-2) / <alpha-value>)",
          3: "rgb(var(--oui-color-base-3) / <alpha-value>)",
          4: "rgb(var(--oui-color-base-4) / <alpha-value>)",
          5: "rgb(var(--oui-color-base-5) / <alpha-value>)",
          6: "rgb(var(--oui-color-base-6) / <alpha-value>)",
          7: "rgb(var(--oui-color-base-7) / <alpha-value>)",
          8: "rgb(var(--oui-color-base-8) / <alpha-value>)",
          9: "rgb(var(--oui-color-base-9) / <alpha-value>)",
          10: "rgb(var(--oui-color-base-10) / <alpha-value>)",
          contrast: {
            // DEFAULT:"rgb(var(--oui-color-base-foreground) / <alpha-value>)",
            DEFAULT: "rgb(var(--oui-color-base-foreground) / 0.98)",
            80: "rgb(var(--oui-color-base-foreground) / 0.80)",
            54: "rgb(var(--oui-color-base-foreground) / 0.54)",
            36: "rgb(var(--oui-color-base-foreground) / 0.36)",
            30: "rgb(var(--oui-color-base-foreground) / 0.30)",
            20: "rgb(var(--oui-color-base-foreground) / 0.2)",
            16: "rgb(var(--oui-color-base-foreground) / 0.16)",
            12: "rgb(var(--oui-color-base-foreground) / 0.12)",
            10: "rgb(var(--oui-color-base-foreground) / 0.10)",
            8: "rgb(var(--oui-color-base-foreground) / 0.08)",
            6: "rgb(var(--oui-color-base-foreground) / 0.06)",
            4: "rgb(var(--oui-color-base-foreground) / 0.04)",
          },
          static: {
            DEFAULT: "rgb(var(--oui-color-base-static) / 0.98)",
            80: "rgb(var(--oui-color-base-static) / 0.80)",
            54: "rgb(var(--oui-color-base-static) / 0.54)",
            36: "rgb(var(--oui-color-base-static) / 0.36)",
            20: "rgb(var(--oui-color-base-static) / 0.2)",
            contrast: {
              DEFAULT: "rgb(var(--oui-color-base-static-contrast) / 0.98)",
              80: "rgb(var(--oui-color-base-static-contrast) / 0.80)",
              54: "rgb(var(--oui-color-base-static-contrast) / 0.54)",
              36: "rgb(var(--oui-color-base-static-contrast) / 0.36)",
              20: "rgb(var(--oui-color-base-static-contrast) / 0.2)",
            },
          },
        },
        line: {
          4: "rgb(var(--oui-color-line, 255 255 255) / 0.04)",
          6: "rgb(var(--oui-color-line, 255 255 255) / 0.06)",
          DEFAULT: "rgb(var(--oui-color-line, 255 255 255) / 0.08)",
          10: "rgb(var(--oui-color-line, 255 255 255) / 0.10)",
          12: "rgb(var(--oui-color-line, 255 255 255) / 0.12)",
          16: "rgb(var(--oui-color-line, 255 255 255) / 0.16)",
        },
        trade: {
          loss: {
            DEFAULT: "rgb(var(--oui-color-trading-loss) / <alpha-value>)",
            contrast:
              "rgb(var(--oui-color-trading-loss-contrast) / <alpha-value>)",
          },

          profit: {
            DEFAULT: "rgb(var(--oui-color-trading-profit) / <alpha-value>)",
            contrast:
              "rgb(var(--oui-color-trading-profit-contrast) / <alpha-value>)",
          },
        },
        // gradient: {
        //   success: 'linear-gradient(var(--oui-linear-degree, 90deg), var(--oui-color-success) 0%, var(--oui-color-success-darken) 100%)',
        // }
      },
      animation: {
        "rotate-360": "rotate 1s linear infinite",
        "accordion-down": "accordion-down 0.2s ease-out",
        "accordion-up": "accordion-up 0.2s ease-out",
        "collapsible-down": "collapsible-down 0.2s ease-out",
        "collapsible-up": "collapsible-up 0.2s ease-out",
        "scan-qr-code": "scan-qr-code 6s infinite linear",
        pop: "pop 0.2s ease-out",
      },
      keyframes: {
        rotate: {
          "0%": { transform: "rotate(0deg)" },
          "50%": { transform: "rotate(240deg)" },
          "100%": { transform: "rotate(360deg)" },
        },
        "accordion-down": {
          from: { height: 0 },
          to: { height: "var(--radix-accordion-content-height)" },
        },
        "accordion-up": {
          from: { height: "var(--radix-accordion-content-height)" },
          to: { height: 0 },
        },
        "collapsible-down": {
          from: { height: 0 },
          to: { height: "var(--radix-collapsible-content-height)" },
        },
        "collapsible-up": {
          from: { height: "var(--radix-collapsible-content-height)" },
          to: { height: 0 },
        },
        "scan-qr-code": {
          "0%": {
            transform: "translateY(0)",
          },
          "50%": {
            transform: "translateY(320px)",
          },
          "100%": {
            transform: "translateY(0)",
          },
        },
        pop: {
          "0%": {
            transform: "scale(1)",
          },
          "100%": {
            transform: "scale(1.05)",
          },
        },
      },
    },
  },
  plugins: [
    darkThemePlugin(),
    basePlugin(),
    sizePlugin(),
    chartPlugin(),
    gradientPlugin(),
    positionPlugin(),
    componentsPlugin(),
    scrollBarPlugin(),
    require("tailwindcss-animate"),
  ],
});
