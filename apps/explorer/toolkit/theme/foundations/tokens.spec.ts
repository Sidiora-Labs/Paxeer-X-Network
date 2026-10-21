import fs from 'node:fs';

import { describe, expect, it } from 'vitest';

import globalCss from '../globalCss';
import { recipe as badgeRecipe } from '../recipes/badge.recipe';
import { recipe as buttonRecipe } from '../recipes/button.recipe';
import { recipe as dialogRecipe } from '../recipes/dialog.recipe';
import { recipe as inputRecipe } from '../recipes/input.recipe';
import { recipe as tableRecipe } from '../recipes/table.recipe';
import { recipe as tabsRecipe } from '../recipes/tabs.recipe';
import { radii } from './borders';
import colors from './colors';
import semanticTokens from './semanticTokens';
import shadows from './shadows';
import { fonts, textStyles } from './typography';

const PRESETS = [ 'configs/envs/paxeer-x.env', 'configs/envs/paxeer-x-dev.env' ];

function walk(source: unknown, path: string): unknown {
  return path.split('.').reduce<unknown>((node, key) => {
    if (typeof node !== 'object' || node === null) {
      throw new Error(`"${ path }" stops at "${ key }": the branch above it is not an object`);
    }
    return (node as Record<string, unknown>)[key];
  }, source);
}

function value(source: unknown, path: string): string {
  const token = walk(source, path);

  if (typeof token !== 'object' || token === null || typeof (token as { value?: unknown }).value !== 'string') {
    throw new Error(`"${ path }" is not a token with a string value`);
  }

  return (token as { value: string }).value;
}

// A theme token may point at another token of the base palette, e.g. "{colors.gray.900}".
function resolve(raw: string): string {
  const reference = /^\{colors\.(.+)\}$/.exec(raw);

  return reference ? resolve(value(colors, reference[1])) : raw;
}

function color(path: string): string {
  return resolve(value(colors.theme, path));
}

function leaves(node: unknown, prefix: string): Array<[string, string]> {
  if (typeof node !== 'object' || node === null) {
    return [];
  }

  const record = node as Record<string, unknown>;

  if (typeof record.value === 'string') {
    return [ [ prefix, record.value ] ];
  }

  return Object.entries(record).flatMap(([ key, child ]) => leaves(child, prefix ? `${ prefix }.${ key }` : key));
}

function readPreset(path: string): Record<string, string> {
  const line = fs.readFileSync(path, 'utf-8')
    .split('\n')
    .find((entry) => entry.startsWith('NEXT_PUBLIC_COLOR_THEME_OVERRIDES='));

  if (!line) {
    throw new Error(`${ path } declares no NEXT_PUBLIC_COLOR_THEME_OVERRIDES`);
  }

  const overrides: unknown = JSON.parse(line.slice('NEXT_PUBLIC_COLOR_THEME_OVERRIDES='.length).replace(/'/g, '"'));

  return Object.fromEntries(leaves(overrides, ''));
}

describe('product colour tokens', () => {
  it('carries the on-surface pill primary button and its hover', () => {
    expect(color('button.primary._light')).toBe('#121317');
    expect(color('button.primary.text._light')).toBe('#FFFFFF');
    expect(color('button.primary.hover._light')).toBe('#2B2D31');
    expect(color('button.primary._dark')).toBe('#F8F9FC');
    expect(color('button.primary.text._dark')).toBe('#121317');
  });

  it('carries the accent with its strong and soft variants', () => {
    expect(color('accent.primary._light')).toBe('#2B4FDA');
    expect(color('accent.strong._light')).toBe('#1E2867');
    expect(color('accent.soft._light')).toBe('#DCF1FF');
    expect(color('link.primary._light')).toBe('#2B4FDA');
    expect(color('hover._light')).toBe('#1E2867');
    expect(color('accent.primary._dark')).toBe('#9DD2FF');
  });

  it('carries the page background and the two surfaces', () => {
    expect(color('bg.primary._light')).toBe('#F8F9FC');
    expect(color('bg.surface._light')).toBe('#FFFFFF');
    expect(color('bg.sunken._light')).toBe('#EFF2F7');
    expect(color('bg.overlay._light')).toBe('#FFFFFF');
    expect(color('bg.primary._dark')).toBe('#121317');
    expect(color('bg.surface._dark')).toBe('#18191D');
    expect(color('bg.sunken._dark')).toBe('#212226');
  });

  it('carries the two border tones', () => {
    expect(color('border.divider._light')).toBe('rgba(33, 34, 38, 0.12)');
    expect(color('border.strong._light')).toBe('#BDC1C6');
    expect(color('border.divider._dark')).toBe('rgba(230, 234, 240, 0.12)');
  });

  it('carries the three text tones', () => {
    expect(color('text.primary._light')).toBe('#121317');
    expect(color('text.secondary._light')).toBe('#45474D');
    expect(color('text.muted._light')).toBe('#45474D');
    expect(color('text.primary._dark')).toBe('#F8F9FC');
    expect(color('text.secondary._dark')).toBe('#B2BBC5');
  });

  it('carries the success, destructive and warning pairs', () => {
    expect(color('feedback.success.fg._light')).toBe('#005143');
    expect(color('feedback.success.bg._light')).toBe('#DAF9D4');
    expect(color('feedback.error.fg._light')).toBe('#BE1C1B');
    expect(color('feedback.error.bg._light')).toBe('#FFEEF7');
    expect(color('feedback.warning.fg._light')).toBe('#9E3D02');
    expect(color('feedback.warning.bg._light')).toBe('#FCFFAD');
  });

  it('resolves every token in both appearances', () => {
    const paths = leaves(colors.theme, '').map(([ path ]) => path);
    const appearances = paths.filter((path) => path.endsWith('._light'));

    expect(appearances.length).toBeGreaterThan(0);

    const unpaired = appearances.filter((path) => !paths.includes(path.replace(/\._light$/, '._dark')));
    const unresolved = paths.filter((path) => !/\S/.test(color(path)));

    expect(unpaired).toEqual([]);
    expect(unresolved).toEqual([]);
  });
});

describe('product shape tokens', () => {
  it('carries the product radii', () => {
    expect(value(radii, 'xs')).toBe('4px');
    expect(value(radii, 'sm')).toBe('8px');
    expect(value(radii, 'base')).toBe('12px');
    expect(value(radii, 'md')).toBe('16px');
    expect(value(radii, 'lg')).toBe('24px');
    expect(value(radii, 'xl')).toBe('36px');
    expect(value(radii, '2xl')).toBe('48px');
    expect(value(radii, 'full')).toBe('9999px');
  });

  it('rests the card flat and lifts the overlay on the product elevation', () => {
    expect(value(shadows, 'card')).toBe('none');
    expect(value(shadows, 'overlay')).toBe('0 1px 2px 0 rgba(60, 64, 67, 0.3), 0 1px 3px 1px rgba(60, 64, 67, 0.15)');
  });

  it('sets the headings at the product weight and tracking on the line boxes the pills read', () => {
    expect(walk(textStyles, 'heading.xl.value')).toMatchObject({ fontSize: '32px', fontWeight: '500', letterSpacing: '-0.01067em' });
    expect(walk(textStyles, 'heading.md.value')).toMatchObject({ lineHeight: '24px', fontWeight: '500' });
    expect(walk(textStyles, 'text.sm.value')).toMatchObject({ lineHeight: '20px', letterSpacing: '0.01143em' });
    expect(walk(textStyles, 'text.xs.value')).toMatchObject({ lineHeight: '16px', letterSpacing: '0.00917em' });
  });

  it('carries the product fallback typefaces on both font tokens', () => {
    expect(value(fonts, 'heading')).toContain('ui-sans-serif, system-ui, -apple-system, "Segoe UI", sans-serif');
    expect(value(fonts, 'body')).toContain('ui-sans-serif, system-ui, -apple-system, "Segoe UI", sans-serif');
  });
});

describe('semantic tokens', () => {
  it('routes the foundations at the product tokens', () => {
    expect(walk(semanticTokens, 'colors.bg.surface.value')).toEqual({
      _light: '{colors.theme.bg.surface._light}',
      _dark: '{colors.theme.bg.surface._dark}',
    });
    expect(walk(semanticTokens, 'colors.border.strong.value')).toEqual({
      _light: '{colors.theme.border.strong._light}',
      _dark: '{colors.theme.border.strong._dark}',
    });
    expect(walk(semanticTokens, 'colors.heading.DEFAULT.value')).toBe('{colors.text.primary}');
    expect(walk(semanticTokens, 'colors.text.error.value')).toBe('{colors.feedback.error.fg}');
  });

  it('routes the components at the product tokens', () => {
    expect(walk(semanticTokens, 'colors.button.solid.bg.hover.value')).toEqual({
      _light: '{colors.theme.button.primary.hover._light}',
      _dark: '{colors.theme.button.primary.hover._dark}',
    });
    expect(walk(semanticTokens, 'colors.dialog.bg.DEFAULT.value')).toBe('{colors.bg.overlay}');
    expect(walk(semanticTokens, 'colors.input.bg.DEFAULT.value')).toBe('{colors.bg.surface}');
    expect(walk(semanticTokens, 'colors.input.border.focus.value')).toBe('{colors.accent.strong}');
    expect(walk(semanticTokens, 'colors.badge.green.bg.value')).toBe('{colors.feedback.success.bg}');
    expect(walk(semanticTokens, 'colors.badge.red.fg.value')).toBe('{colors.feedback.error.fg}');
    expect(walk(semanticTokens, 'colors.badge.orange.bg.value')).toBe('{colors.feedback.warning.bg}');
    expect(walk(semanticTokens, 'colors.badge.purple.bg.value')).toEqual({
      _light: '{colors.datavis.purple.lowest}',
      _dark: '{colors.datavis.purple.highest}',
    });
    expect(walk(semanticTokens, 'colors.card.border.value')).toEqual({
      _light: 'rgba(47, 48, 52, 0.15)',
      _dark: '{colors.border.divider}',
    });
    expect(walk(semanticTokens, 'colors.header.sticky.bg.value')).toEqual({
      _light: 'rgba(255, 255, 255, 0.9)',
      _dark: 'rgba(18, 19, 23, 0.95)',
    });
    expect(walk(semanticTokens, 'colors.table.row.hover.value')).toEqual({
      _light: 'rgba(47, 48, 52, 0.1)',
      _dark: 'rgba(230, 234, 240, 0.06)',
    });
    expect(walk(semanticTokens, 'shadows.popover.DEFAULT.value')).toEqual({
      _light: '{shadows.overlay}',
      _dark: '{shadows.dark-lg}',
    });
  });
});

describe('global styles and recipes', () => {
  it('dresses the page from the theme', () => {
    expect(walk(globalCss, 'body.bg')).toBe('global.body.bg');
    expect(walk(globalCss, 'body.color')).toBe('global.body.fg');
    expect(walk(globalCss, 'body.fontFamily')).toBe('body');
  });

  it('makes the primary button a pill that hovers on the product tone', () => {
    expect(walk(buttonRecipe, 'base.borderRadius')).toBe('full');
    expect(walk(buttonRecipe, 'variants.variant.solid.bg')).toBe('button.solid.bg');
    expect(walk(buttonRecipe, 'variants.variant.solid._hover.bg')).toBe('button.solid.bg.hover');
    expect(walk(buttonRecipe, 'variants.variant.solid._expanded.bg')).toBe('button.solid.bg.hover');
    expect(walk(buttonRecipe, 'variants.size.md.borderRadius')).toBe('full');
  });

  it('dresses the fields, tables, tabs, badges and dialogs from the theme', () => {
    expect(walk(inputRecipe, 'base.borderRadius')).toBe('base');
    expect(walk(inputRecipe, 'variants.variant.outline.borderWidth')).toBe('1px');
    expect(walk(inputRecipe, 'variants.variant.outline._focus.boxShadow')).toBe('overlay');
    expect(walk(tableRecipe, 'variants.variant.line.row.bg')).toBe('bg.surface');
    expect(walk(tableRecipe, 'variants.variant.line.columnHeader.backgroundColor')).toBe('table.header.bg');
    expect(walk(tableRecipe, 'variants.variant.line.columnHeader._first.borderTopLeftRadius')).toBe('sm');
    expect(walk(tabsRecipe, 'variants.variant.solid.trigger.borderRadius')).toBe('full');
    expect(walk(badgeRecipe, 'base.borderRadius')).toBe('full');
    expect(walk(badgeRecipe, 'base.fontWeight')).toBe('500');
    expect(walk(dialogRecipe, 'base.content.boxShadow')).toBe('overlay');
    expect(walk(dialogRecipe, 'base.content.borderRadius')).toBe('lg');
  });
});

describe('the Paxeer X presets', () => {
  it.each(PRESETS)('%s declares the colours the theme declares', (path) => {
    const preset = readPreset(path);
    const declared = leaves(colors.theme, '');

    expect(declared.length).toBeGreaterThan(0);

    const undeclared = declared
      .map(([ tokenPath ]) => tokenPath)
      .filter((tokenPath) => preset[tokenPath] === undefined);
    const drifted = declared
      .map(([ tokenPath ]) => tokenPath)
      .filter((tokenPath) => preset[tokenPath] !== undefined)
      .filter((tokenPath) => preset[tokenPath].toLowerCase() !== color(tokenPath).toLowerCase());

    expect(undeclared).toEqual([]);
    expect(drifted).toEqual([]);
  });

  it.each(PRESETS)('%s points the marks and the typeface at the product', (path) => {
    const preset = fs.readFileSync(path, 'utf-8');

    expect(preset).toContain('NEXT_PUBLIC_NETWORK_LOGO=file:///app/public/static/paxeer-x/logo.svg');
    expect(preset).toContain('NEXT_PUBLIC_NETWORK_LOGO_DARK=file:///app/public/static/paxeer-x/logo-dark.svg');
    expect(preset).toContain('NEXT_PUBLIC_NETWORK_ICON=file:///app/public/static/paxeer-x/icon.svg');
    expect(preset).toContain('NEXT_PUBLIC_NETWORK_ICON_DARK=file:///app/public/static/paxeer-x/icon-dark.svg');
    expect(preset).toContain('NEXT_PUBLIC_FONT_FAMILY_HEADING=');
    expect(preset).toContain('NEXT_PUBLIC_FONT_FAMILY_BODY=');
    expect(preset).toMatch(/NEXT_PUBLIC_FONT_FAMILY_HEADING=\{'name':'Manrope'/);
    expect(preset).toMatch(/NEXT_PUBLIC_FONT_FAMILY_BODY=\{'name':'Manrope'/);
    expect(preset).toMatch(/^NEXT_PUBLIC_COLOR_THEME_DEFAULT=light$/m);
  });
});
