import plugin from "tailwindcss/plugin";

export const basePlugin = () =>
  plugin(function ({ addComponents, addBase }) {
    addBase({
      html: {
        // fontSize: "var(--paxeer-network-font-size-base)",
        backgroundColor: "rgb(var(--oui-color-base-10))",
        color: "rgb(var(--oui-color-base-foreground) / 0.98)",
        fontFamily: "var(--oui-font-family)",
      },
      body: {
        // fontSize: "var(--oui-font-size-base)",
        backgroundColor: "rgb(var(--oui-color-base-10))",
        color: "rgb(var(--oui-color-base-foreground) / 0.98)",
        fontFamily: "var(--oui-font-family)",
        fontWeight: "500",
        // Atyp BL stylistic sets; no-op for fonts without these features.
        fontFeatureSettings: '"ss02" 1, "ss03" 1, "ss05" 1, "ss06" 1',
      },
      "h1, h2, h3, h4, h5, h6, [role='heading']": {
        fontWeight: "600",
      },
      // Zero-specificity so explicit font-weight utilities still win.
      ":where(.oui-text-2xl, .oui-text-3xl, .oui-text-4xl, .oui-text-5xl)": {
        fontWeight: "600",
      },
      ":where(.oui-text-2xs, .oui-text-xs, .oui-text-sm)": {
        fontWeight: "500",
      },
    });
  });
