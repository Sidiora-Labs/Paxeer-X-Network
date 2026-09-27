import { defaultConfig } from '@chakra-ui/react';

import { textStyles } from '../foundations/typography';

export type PillTextStyle = 'xs' | 'sm' | 'md';

export interface PillMetrics {
  textStyle: PillTextStyle;
  paddingY: string;
  borderWidth?: number;
}

const ROOT_FONT_SIZE = 16;

type ValueScale = Record<string, { value?: string } | undefined>;
type TextStyleScale = Record<string, { value?: { lineHeight?: string } } | undefined>;

export function pillPixels(value: string, name = 'the pill length'): number {
  const amount = Number.parseFloat(value);

  if (Number.isNaN(amount)) {
    throw new Error(`${ name } is not a length the pill sizing can read: ${ value }`);
  }

  return value.trim().endsWith('rem') ? amount * ROOT_FONT_SIZE : amount;
}

// A recipe names a text style by its top level key, so the pill reads the line box off the
// same entry the renderer resolves: the product override when it carries one, the framework
// scale otherwise.
export function pillLineHeight(textStyle: PillTextStyle): string {
  const own = textStyles as unknown as TextStyleScale | undefined;
  const base = defaultConfig.theme?.textStyles as unknown as TextStyleScale | undefined;
  const lineHeight = own?.[textStyle]?.value?.lineHeight ?? base?.[textStyle]?.value?.lineHeight;

  if (!lineHeight) {
    throw new Error(`the text style ${ textStyle } declares no line height`);
  }

  return lineHeight;
}

export function pillLineBox(textStyle: PillTextStyle): number {
  return pillPixels(pillLineHeight(textStyle), `the line height of the text style ${ textStyle }`);
}

export function productLineBox(textStyle: PillTextStyle): number {
  const group = textStyles?.text as unknown as TextStyleScale | undefined;
  const lineHeight = group?.[textStyle]?.value?.lineHeight;

  if (!lineHeight) {
    throw new Error(`the product text style text.${ textStyle } declares no line height`);
  }

  return pillPixels(lineHeight, `the line height of the product text style text.${ textStyle }`);
}

export function pillSpacing(token: string): number {
  const scale = defaultConfig.theme?.tokens?.spacing as unknown as ValueScale | undefined;
  const value = scale?.[token]?.value;

  if (!value) {
    throw new Error(`the spacing scale carries no ${ token } step`);
  }

  return pillPixels(value, `the spacing step ${ token }`);
}

export function pillSize(token: string): number {
  const scale = defaultConfig.theme?.tokens?.sizes as unknown as ValueScale | undefined;
  const value = scale?.[token]?.value;

  if (!value) {
    throw new Error(`the size scale carries no ${ token } step`);
  }

  return pillPixels(value, `the size step ${ token }`);
}

export function pillHeight(metrics: PillMetrics): number {
  return pillLineBox(metrics.textStyle) + pillSpacing(metrics.paddingY) * 2 + (metrics.borderWidth ?? 0) * 2;
}

export function pillMinHeight(metrics: PillMetrics): string {
  return `${ pillHeight(metrics) }px`;
}

export function pillPaddingY(metrics: PillMetrics): string {
  return `${ pillSpacing(metrics.paddingY) }px`;
}
