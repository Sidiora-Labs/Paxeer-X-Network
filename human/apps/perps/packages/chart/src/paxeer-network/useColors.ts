import { useMemo } from "react";
import { getThemeColors } from "../utils/theme";

export const useColors = (colors?: { profit: string; loss: string }) => {
  const _colors = useMemo(() => {
    const themeColors = getThemeColors();
    return {
      profit: colors?.profit || themeColors.profit,
      loss: colors?.loss || themeColors.loss,
      primary: themeColors.primary,
      primaryLight: themeColors.primaryLight,
    };
  }, [colors]);

  return _colors;
};
