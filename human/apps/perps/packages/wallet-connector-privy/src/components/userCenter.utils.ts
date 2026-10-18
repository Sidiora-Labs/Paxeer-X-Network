export type ThemeMode = "dark" | "light";

/**
 * Resolves the wallet icon color from a theme contrast token.
 * CSS variable references cannot be resolved in JavaScript, so they fall back
 * to the theme mode instead of throwing during render.
 */
export const shouldUseBlackIcon = (
  contrastToken: string | undefined,
  mode?: ThemeMode,
) => {
  const normalizedToken = contrastToken?.trim();

  if (normalizedToken === "0 0 0") return true;
  if (normalizedToken === "255 255 255") return false;

  return mode === "light";
};
