import plugin from "tailwindcss/plugin";

/**
 * paxeer-network Design System v2 dark palette — the SDK default dark theme.
 *
 * Uses the Manrope type stack. Built-in gradient utilities receive matching
 * RGB values at both stops, so brand buttons and gradient text appear solid
 * under this preset. Treatments with different stop opacities can still fade.
 * For color-shifting gradients, use `PURPLE_DARK_THEME_CSS_VARS` or define
 * custom `cssVars`.
 */
export const DARK_THEME_CSS_VARS = {
  // Font stack for SDK text, with platform and CJK fallbacks.
  "--oui-font-family":
    '"Manrope", "PingFang SC", "Noto Sans CJK SC", "Noto Sans", sans-serif',

  // Main brand accent for primary actions and emphasis.
  "--oui-color-primary": "156 117 255",
  // Primary-light slot; intentionally equal to primary in this flat palette.
  "--oui-color-primary-light": "156 117 255",
  // Primary button fill, distinct from the main brand accent.
  "--oui-color-primary-darken": "103 0 206",
  // Text and icons displayed on a primary-colored surface.
  "--oui-color-primary-contrast": "255 255 255",

  // Default link accent.
  "--oui-color-link": "156 117 255",
  // Light link accent slot; equal to the default in this flat palette.
  "--oui-color-link-light": "156 117 255",

  // Supporting foreground color for secondary text and controls.
  "--oui-color-secondary": "255 255 255",
  // Muted foreground color for tertiary text and icons.
  "--oui-color-tertiary": "132 131 136",
  // More subdued foreground color for quaternary details.
  "--oui-color-quaternary": "98 96 102",

  // Error or loss status color.
  "--oui-color-danger": "233 77 122",
  // Light danger slot; equal to danger in this flat palette.
  "--oui-color-danger-light": "233 77 122",
  // Danger-darken slot; intentionally equal to danger in this flat palette.
  "--oui-color-danger-darken": "233 77 122",
  // Text and icons displayed on danger-colored surfaces.
  "--oui-color-danger-contrast": "9 6 16",

  // Success or gain status color.
  "--oui-color-success": "2 181 137",
  // Light success slot; equal to success in this flat palette.
  "--oui-color-success-light": "2 181 137",
  // Success-darken slot; intentionally equal to success in this flat palette.
  "--oui-color-success-darken": "2 181 137",
  // Text and icons displayed on success-colored surfaces.
  "--oui-color-success-contrast": "9 6 16",

  // Warning status color.
  "--oui-color-warning": "217 171 82",
  // Light warning slot; equal to warning in this flat palette.
  "--oui-color-warning-light": "217 171 82",
  // Warning-darken slot; intentionally equal to warning in this flat palette.
  "--oui-color-warning-darken": "217 171 82",
  // Text and icons displayed on warning-colored surfaces.
  "--oui-color-warning-contrast": "9 6 16",

  // Reserved fill surface for consumers; no built-in component currently uses it.
  "--oui-color-fill": "29 25 41",
  // Reserved active fill surface; no built-in component currently uses it.
  "--oui-color-fill-active": "43 39 54",

  // Neutral surface slots progress from lighter details to darker backgrounds.
  // These are common uses, not fixed component roles.
  // 1: prominent small surfaces, such as switch tracks.
  "--oui-color-base-1": "66 61 75",
  // 2: buttons, avatars, and other raised details.
  "--oui-color-base-2": "57 53 65",
  // 3: muted or disabled controls.
  "--oui-color-base-3": "54 50 64",
  // 4: interactive control surfaces, including buttons.
  "--oui-color-base-4": "51 47 62",
  // 5: selected, hovered, or disabled control surfaces.
  "--oui-color-base-5": "43 39 54",
  // 6: input fields, tooltips, and other control surfaces.
  "--oui-color-base-6": "29 25 41",
  // 7: tabs, selectors, and secondary content surfaces.
  "--oui-color-base-7": "25 22 36",
  // 8: sheets, popovers, dropdowns, and similar containers.
  "--oui-color-base-8": "22 18 32",
  // 9: larger panels, tables, and content sections.
  "--oui-color-base-9": "19 14 29",
  // 10: the deepest page background and overlays.
  "--oui-color-base-10": "9 6 16",

  // Theme foreground used by base-contrast text and icon classes.
  "--oui-color-base-foreground": "255 255 255",
  // Source color for translucent borders and dividers.
  "--oui-color-line": "255 255 255",

  // Light color used for text and fixed-color surfaces across built-in themes.
  "--oui-color-base-static": "255 255 255",
  // Dark text and icons for base-static-colored surfaces.
  "--oui-color-base-static-contrast": "0 0 0",

  // Sell-side and trading-loss color; a distinct semantic token from danger.
  "--oui-color-trading-loss": "233 77 122",
  // Contrast text and icons for trading-loss-colored surfaces.
  "--oui-color-trading-loss-contrast": "9 6 16",
  // Buy-side and trading-profit color; a distinct semantic token from success.
  "--oui-color-trading-profit": "2 181 137",
  // Contrast text and icons for trading-profit-colored surfaces.
  "--oui-color-trading-profit-contrast": "9 6 16",

  // Gradient utilities read space-separated RGB start and end colors.
  // Matching RGB values appear solid at equal opacity; callers can still
  // create a fade by varying opacity or a color shift by using different RGBs.
  // Primary gradient start: main brand accent.
  "--oui-gradient-primary-start": "156 117 255",
  // Primary gradient end: match the start for a solid primary fill.
  "--oui-gradient-primary-end": "156 117 255",

  // Secondary gradient start: brand accent in this preset.
  "--oui-gradient-secondary-start": "156 117 255",
  // Secondary gradient end: match the start for a solid fill.
  "--oui-gradient-secondary-end": "156 117 255",

  // Success gradient start: positive status color.
  "--oui-gradient-success-start": "2 181 137",
  // Success gradient end: match the start for a solid fill.
  "--oui-gradient-success-end": "2 181 137",

  // Danger gradient start: negative status color.
  "--oui-gradient-danger-start": "233 77 122",
  // Danger gradient end: match the start for a solid fill.
  "--oui-gradient-danger-end": "233 77 122",

  // Warning gradient start: caution status color.
  "--oui-gradient-warning-start": "217 171 82",
  // Warning gradient end: match the start for a solid fill.
  "--oui-gradient-warning-end": "217 171 82",

  // Neutral gradient start: subdued surface color.
  "--oui-gradient-neutral-start": "29 25 41",
  // Neutral gradient end: match the start for a solid fill.
  "--oui-gradient-neutral-end": "29 25 41",

  // Brand gradient start: brand accent used by gradient treatments.
  "--oui-gradient-brand-start": "156 117 255",
  // Brand gradient end: match the start for a solid brand fill.
  "--oui-gradient-brand-end": "156 117 255",
  // Position of the brand gradient's first color stop.
  "--oui-gradient-brand-stop-start": "6.62%",
  // Position of its last color stop; positions do not affect a solid fill.
  "--oui-gradient-brand-stop-end": "86.5%",
  // Direction of the brand gradient; irrelevant when both colors match.
  "--oui-gradient-brand-angle": "17.44deg",

  // Small corner radius for compact controls.
  "--oui-rounded-sm": "2px",
  // Default corner radius.
  "--oui-rounded": "4px",
  // Medium corner radius.
  "--oui-rounded-md": "6px",
  // Large corner radius.
  "--oui-rounded-lg": "8px",
  // Extra-large corner radius for panels and controls.
  "--oui-rounded-xl": "12px",
  // 2XL corner radius for prominent panels.
  "--oui-rounded-2xl": "16px",
  // Pill or circular radius.
  "--oui-rounded-full": "9999px",

  // Extra-small maximum content width (20rem, typically 320px).
  "--oui-spacing-xs": "20rem",
  // Small maximum content width (22.5rem, typically 360px).
  "--oui-spacing-sm": "22.5rem",
  // Medium maximum content width (26.25rem, typically 420px).
  "--oui-spacing-md": "26.25rem",
  // Large maximum content width (30rem, typically 480px).
  "--oui-spacing-lg": "30rem",
  // Extra-large maximum content width (33.75rem, typically 540px).
  "--oui-spacing-xl": "33.75rem",
};

export type ThemeCssVars = typeof DARK_THEME_CSS_VARS;

export const darkThemePlugin = () =>
  plugin(function ({ addBase }) {
    addBase({
      ":root": DARK_THEME_CSS_VARS,
    });
  });
