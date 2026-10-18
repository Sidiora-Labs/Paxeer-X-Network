import { useEffect, useState } from "react";
import { useThemeAttribute } from "@paxeer-network/ui";

// because tradingview only supports hex colors, so we need to convert the css variables to hex colors
const cssVar2Hex = (color: string) => {
  const [r, g, b] = color.trim().split(/\s+/).map(Number);
  const toHex = (n: number) =>
    Math.min(255, Math.max(0, n)).toString(16).padStart(2, "0");
  return `#${toHex(r)}${toHex(g)}${toHex(b)}`;
};

export const useCssVariables = (
  theme: "dark" | "light",
  themeConfig?: unknown,
) => {
  const [cssVariables, setCssVariables] = useState<Record<string, string>>({});
  const themeAttribute = useThemeAttribute();

  useEffect(() => {
    const rootStyle = getComputedStyle(document.documentElement);

    const base9 = rootStyle.getPropertyValue("--oui-color-base-9").trim();
    const primaryVar = rootStyle.getPropertyValue("--oui-color-primary").trim();
    const warningLightVar = rootStyle
      .getPropertyValue("--oui-color-warning-light")
      .trim();

    // const profitColor = rootStyle
    //   .getPropertyValue("--oui-color-trading-profit")
    //   .trim();

    // const lossColor = rootStyle
    //   .getPropertyValue("--oui-color-trading-loss")
    //   .trim();

    setCssVariables({
      chartBG: cssVar2Hex(base9),
      primary: cssVar2Hex(primaryVar),
      /** For liquidation line; same as Position list Liq. Price (--oui-color-warning-light). */
      warningLight: warningLightVar ? cssVar2Hex(warningLightVar) : "",
      // upColor: cssVar2Hex(profitColor),
      // downColor: cssVar2Hex(lossColor),
    });
  }, [theme, themeAttribute, themeConfig]);

  return cssVariables;
};
