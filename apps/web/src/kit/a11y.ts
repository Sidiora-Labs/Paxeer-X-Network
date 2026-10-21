import { createElement, type HTMLAttributes, type ReactNode } from "react";

export const MINIMUM_TEXT_CONTRAST = 4.5;

export const SEMANTIC_CONTRAST_PAIRS = [
  ["--foreground", "--background"],
  ["--foreground", "--surface"],
  ["--foreground-secondary", "--background"],
  ["--foreground-secondary", "--surface"],
  ["--muted-foreground", "--background"],
  ["--muted-foreground", "--surface"],
  ["--faint-foreground", "--background"],
  ["--faint-foreground", "--surface"],
  ["--primary-foreground", "--primary"],
  ["--accent-foreground", "--accent"],
  ["--accent", "--background"],
  ["--accent", "--surface"],
  ["--accent-strong", "--accent-soft"],
  ["--success", "--background"],
  ["--success", "--surface"],
  ["--success", "--success-soft"],
  ["--success-foreground", "--success"],
  ["--destructive", "--background"],
  ["--destructive", "--surface"],
  ["--destructive", "--destructive-soft"],
  ["--destructive-foreground", "--destructive"],
  ["--warning", "--background"],
  ["--warning", "--surface"],
  ["--warning", "--warning-soft"],
  ["--warning-foreground", "--warning"],
] as const;

const ACCESSIBILITY_CONTRACT = Object.freeze({
  minimumTextContrast: MINIMUM_TEXT_CONTRAST,
  semanticContrastPairs: SEMANTIC_CONTRAST_PAIRS,
  minimumTouchTargetCssPixels: 44,
  supportedTextExpansion: 1.5,
});

export function a11y() {
  return ACCESSIBILITY_CONTRACT;
}

export interface ExplicitCurrencyAmountOptions {
  readonly currency: string;
  readonly locale: string;
  readonly decimals?: number;
  readonly signed?: boolean;
}

export interface SemanticContrastResult {
  readonly background: string;
  readonly foreground: string;
  readonly ratio: number;
}

function channelToLinear(value: number): number {
  const channel = value / 255;
  return channel <= 0.04045 ? channel / 12.92 : ((channel + 0.055) / 1.055) ** 2.4;
}

function hexChannels(color: string): readonly [number, number, number] {
  const compact = color.trim().toLowerCase();
  const expanded = /^#[0-9a-f]{3}$/u.test(compact)
    ? `#${compact.slice(1, 2).repeat(2)}${compact.slice(2, 3).repeat(2)}${compact.slice(3, 4).repeat(2)}`
    : compact;
  if (!/^#[0-9a-f]{6}$/u.test(expanded)) {
    throw new Error(`Expected a hexadecimal color, received ${color}`);
  }
  return [
    Number.parseInt(expanded.slice(1, 3), 16),
    Number.parseInt(expanded.slice(3, 5), 16),
    Number.parseInt(expanded.slice(5, 7), 16),
  ];
}

function luminance(color: string): number {
  const [red, green, blue] = hexChannels(color);
  return (
    channelToLinear(red) * 0.2126 +
    channelToLinear(green) * 0.7152 +
    channelToLinear(blue) * 0.0722
  );
}

export function contrastRatio(foreground: string, background: string): number {
  const foregroundLuminance = luminance(foreground);
  const backgroundLuminance = luminance(background);
  return (
    (Math.max(foregroundLuminance, backgroundLuminance) + 0.05) /
    (Math.min(foregroundLuminance, backgroundLuminance) + 0.05)
  );
}

export function semanticTokenValues(stylesheet: string): ReadonlyMap<string, string> {
  const values = new Map<string, string>();
  for (const match of stylesheet.matchAll(/(--[a-z-]+)\s*:\s*(#[0-9a-f]{3,8})\s*;/giu)) {
    const name = match[1];
    const value = match[2];
    if (name !== undefined && value !== undefined) {
      values.set(name, value);
    }
  }
  return values;
}

export function semanticContrastResults(stylesheet: string): readonly SemanticContrastResult[] {
  const tokens = semanticTokenValues(stylesheet);
  return SEMANTIC_CONTRAST_PAIRS.map(([foreground, background]) => {
    const foregroundValue = tokens.get(foreground);
    const backgroundValue = tokens.get(background);
    if (foregroundValue === undefined || backgroundValue === undefined) {
      throw new Error(`Missing semantic color token pair ${foreground} on ${background}`);
    }
    return {
      foreground,
      background,
      ratio: contrastRatio(foregroundValue, backgroundValue),
    };
  });
}

export function assertSemanticTokenContrast(stylesheet: string): void {
  const failure = semanticContrastResults(stylesheet).find(
    ({ ratio }) => ratio < MINIMUM_TEXT_CONTRAST,
  );
  if (failure !== undefined) {
    throw new Error(
      `${failure.foreground} on ${failure.background} has ${failure.ratio.toFixed(2)}:1 contrast`,
    );
  }
}

export function formatExplicitCurrencyAmount(
  value: number,
  { currency, locale, decimals = 2, signed = true }: ExplicitCurrencyAmountOptions,
): string {
  if (!Number.isFinite(value)) {
    throw new Error("Amounts must be finite numbers");
  }
  if (!/^[A-Z]{3}$/u.test(currency)) {
    throw new Error("Currency must be an explicit uppercase ISO 4217 code");
  }
  const [canonicalLocale] = Intl.getCanonicalLocales(locale);
  if (canonicalLocale === undefined) {
    throw new Error("A locale is required for amount formatting");
  }
  return new Intl.NumberFormat(canonicalLocale, {
    style: "currency",
    currency,
    currencyDisplay: "code",
    signDisplay: signed ? "always" : "auto",
    minimumFractionDigits: decimals,
    maximumFractionDigits: decimals,
  }).format(value);
}

export interface LiveRegionProps extends Omit<HTMLAttributes<HTMLSpanElement>, "role"> {
  readonly children?: ReactNode;
  readonly priority?: "assertive" | "polite";
}

export function LiveRegion({
  children,
  priority = "polite",
  className,
  ...props
}: LiveRegionProps) {
  return createElement(
    "span",
    {
      ...props,
      className: ["sr-only", className].filter(Boolean).join(" "),
      role: priority === "assertive" ? "alert" : "status",
      "aria-live": priority,
      "aria-atomic": true,
    },
    children,
  );
}
