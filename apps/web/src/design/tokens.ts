export const layerxUiContract = Object.freeze({
  package: "@layerx/ui",
  packageVersion: "0.1.0",
  stylesheet: "@layerx/ui/styles.css",
  tokens: Object.freeze([
    "--background",
    "--surface",
    "--surface-sunken",
    "--foreground",
    "--foreground-secondary",
    "--muted-foreground",
    "--border",
    "--border-strong",
    "--primary",
    "--accent",
    "--success",
    "--destructive",
    "--warning",
    "--radius-sheet",
    "--shadow-card",
    "--shadow-overlay",
    "--font-sans",
  ]),
  styleFeatures: Object.freeze([
    "borders",
    "dividers",
    "shadows",
    "gradients",
    "layered-surfaces",
    "responsive-patterns",
  ]),
});

export function human_design_tokens() {
  return layerxUiContract;
}
