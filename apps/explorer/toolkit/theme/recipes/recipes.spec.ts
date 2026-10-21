import { describe, expect, it } from 'vitest';

import { radii } from '../foundations/borders';
import shadows from '../foundations/shadows';
import { BADGE_PILL_METRICS, recipe as badgeRecipe } from './badge.recipe';
import { recipe as buttonRecipe } from './button.recipe';
import type { PillMetrics } from './pillSizing';
import { pillHeight, pillLineBox, pillSize, pillSpacing, productLineBox } from './pillSizing';
import { recipe as statRecipe } from './stat.recipe';
import { recipe as tableRecipe } from './table.recipe';
import { recipe as tabsRecipe } from './tabs.recipe';
import { recipe as tagRecipe, TAG_PILL_METRICS } from './tag.recipe';

const SCAN_VARIANTS: Array<[ string, unknown ]> = [
  [ 'table.scan', tableRecipe.variants?.variant.scan ],
  [ 'tabs.pill', tabsRecipe.variants?.variant.pill ],
  [ 'badge.direction', badgeRecipe.variants?.variant.direction ],
  [ 'tag.outlined', tagRecipe.variants?.variant.outlined ],
  [ 'stat.scan', statRecipe.variants?.variant.scan ],
  [ 'button.scan_control', buttonRecipe.variants?.variant.scan_control ],
];

const LITERAL_COLOUR = /#[0-9a-f]{3,8}\b|\b(?:rgba?|hsla?)\(/i;

function entries(node: unknown, path = ''): Array<[ string, string ]> {
  if (typeof node === 'string') {
    return [ [ path, node ] ];
  }

  if (typeof node !== 'object' || node === null) {
    return [];
  }

  return Object.entries(node).flatMap(([ key, child ]) => entries(child, path ? `${ path }.${ key }` : key));
}

describe('the scan layout variants', () => {
  it.each(SCAN_VARIANTS)('declares %s', (_name, variant) => {
    expect(variant).toBeDefined();
  });

  it.each(SCAN_VARIANTS)('writes no literal colour into %s', (name, variant) => {
    for (const [ path, value ] of entries(variant)) {
      expect(`${ name }.${ path } = ${ value }`).not.toMatch(LITERAL_COLOUR);
    }
  });

  it.each(SCAN_VARIANTS)('writes no font family into %s', (name, variant) => {
    for (const [ path ] of entries(variant)) {
      expect(`${ name }.${ path }`).not.toMatch(/fontFamily/);
    }
  });

  it.each(SCAN_VARIANTS)('names every radius of %s from the radius scale', (_name, variant) => {
    const scale = Object.keys(radii ?? {});
    const outside = entries(variant)
      .filter(([ path, value ]) => path.includes('Radius') && !scale.includes(value))
      .map(([ path, value ]) => `${ path } = ${ value }`);

    expect(outside).toEqual([]);
  });

  it.each(SCAN_VARIANTS)('names every shadow of %s from the shadow scale', (_name, variant) => {
    const scale = Object.keys(shadows ?? {});
    const outside = entries(variant)
      .filter(([ path, value ]) => path.endsWith('boxShadow') && !scale.includes(value))
      .map(([ path, value ]) => `${ path } = ${ value }`);

    expect(outside).toEqual([]);
  });
});

const BADGE_SIZES = Object.entries(BADGE_PILL_METRICS) as Array<[ string, PillMetrics ]>;
const TAG_SIZES = Object.entries(TAG_PILL_METRICS) as Array<[ string, PillMetrics ]>;

type Declarations = Record<string, unknown>;

const badgeSizes = badgeRecipe.variants?.size as unknown as Record<string, Declarations>;
const tagSizes = tagRecipe.variants?.size as unknown as Record<string, Record<string, Declarations>>;

function badgeSize(size: string): Declarations {
  return badgeSizes[size];
}

function tagSizeRoot(size: string): Declarations {
  return tagSizes[size].root;
}

describe('the pill and chip sizing', () => {
  it.each([
    { size: 'sm', textStyle: 'xs', lineBox: 16, padding: 2, height: 20 },
    { size: 'md', textStyle: 'sm', lineBox: 20, padding: 2, height: 24 },
    { size: 'lg', textStyle: 'sm', lineBox: 20, padding: 4, height: 28 },
  ] as const)('keeps the $size badge at its independently specified metrics', ({ size, textStyle, lineBox, padding, height }) => {
    const metrics = BADGE_PILL_METRICS[size];
    const declarations = badgeSize(size);

    expect(metrics.textStyle).toBe(textStyle);
    expect(pillLineBox(metrics.textStyle)).toBe(lineBox);
    expect(pillSpacing(metrics.paddingY)).toBe(padding);
    expect(metrics.borderWidth ?? 0).toBe(0);
    expect(pillHeight(metrics)).toBe(height);
    expect(declarations.py).toBe(`${ padding }px`);
    expect(declarations.minH).toBe(`${ height }px`);
    expect(height).toBe(lineBox + padding * 2);
  });

  it.each([
    { size: 'md', lineBox: 20, padding: 2, border: 1, height: 26 },
    { size: 'lg', lineBox: 20, padding: 6, border: 1, height: 34 },
  ] as const)('keeps the $size chip at its independently specified metrics', ({ size, lineBox, padding, border, height }) => {
    const metrics = TAG_PILL_METRICS[size];
    const root = tagSizeRoot(size);

    expect(metrics.textStyle).toBe('sm');
    expect(pillLineBox(metrics.textStyle)).toBe(lineBox);
    expect(pillSpacing(metrics.paddingY)).toBe(padding);
    expect(metrics.borderWidth).toBe(border);
    expect(pillHeight(metrics)).toBe(height);
    expect(root.py).toBe(`${ padding }px`);
    expect(root.minH).toBe(`${ height }px`);
    expect(height).toBe(lineBox + padding * 2 + border * 2);
  });

  it('keeps tag decorations from shrinking with their label', () => {
    const base = tagRecipe.base as unknown as Record<string, Declarations>;

    expect(base.startElement.flexShrink).toBe(0);
    expect(base.endElement.flexShrink).toBe(0);
    expect(base.label.lineClamp).toBeUndefined();
  });

  it.each([ 'xs', 'sm', 'md' ] as const)('reads the %s pill text off a line box the product typography agrees with', (textStyle) => {
    expect(pillLineBox(textStyle)).toBe(productLineBox(textStyle));
  });

  it.each(BADGE_SIZES)('reads the %s badge height off its own line box and padding', (size, metrics) => {
    const declarations = badgeSize(size);

    expect(declarations.textStyle).toBe(metrics.textStyle);
    expect(declarations.py).toBe(`${ pillSpacing(metrics.paddingY) }px`);
    expect(declarations.minH).toBe(`${ pillHeight(metrics) }px`);
  });

  it.each(BADGE_SIZES)('leaves the %s badge room for its whole line box', (size, metrics) => {
    expect(pillHeight(metrics)).toBeGreaterThanOrEqual(pillLineBox(metrics.textStyle));
  });

  it.each(BADGE_SIZES)('pins no height on the %s badge that its text could outgrow', (size) => {
    const declarations = badgeSize(size);

    expect(declarations.h).toBeUndefined();
    expect(declarations.height).toBeUndefined();
    expect(declarations.maxH).toBeUndefined();
  });

  it('lets every badge grow with its text and keeps that text on one line', () => {
    const base = badgeRecipe.base as unknown as Declarations;

    expect(base.height).toBe('auto');
    expect(base.minWidth).toBe('auto');
    expect(base.whiteSpace).toBe('nowrap');
    expect(base.overflow).toBe('hidden');
  });

  it('shrinks a badge label to an ellipsis and never its icon', () => {
    const base = badgeRecipe.base as unknown as Record<string, Declarations>;

    expect(base['& > span']).toEqual({
      minWidth: 0,
      overflow: 'hidden',
      textOverflow: 'ellipsis',
      whiteSpace: 'nowrap',
    });
    expect(base['& > svg']).toEqual({ flexShrink: 0 });
  });

  it.each(TAG_SIZES)('reads the %s chip height off its line box, its padding and its border', (size, metrics) => {
    const root = tagSizeRoot(size);

    expect(root.py).toBe(`${ pillSpacing(metrics.paddingY) }px`);
    expect(root.minH).toBe(`${ pillHeight(metrics) }px`);
    expect(pillHeight(metrics)).toBeGreaterThanOrEqual(pillLineBox(metrics.textStyle) + 2);
  });

  it.each(TAG_SIZES)('pins no height on the %s chip that its text could outgrow', (size) => {
    const root = tagSizeRoot(size);

    expect(root.h).toBeUndefined();
    expect(root.height).toBeUndefined();
    expect(root.maxH).toBeUndefined();
  });

  it.each(TAG_SIZES)('labels the %s chip with the text style its height was read from', (size, metrics) => {
    const label = tagSizes[size].label;

    expect(label.textStyle).toBe(metrics.textStyle);
  });

  it('lets every chip grow with its label and truncates that label on one line', () => {
    const base = tagRecipe.base as unknown as Record<string, Declarations>;

    expect(base.root.height).toBe('auto');
    expect(base.root.minWidth).toBe('auto');
    expect(base.root.overflow).toBe('hidden');
    expect(base.label.display).toBe('block');
    expect(base.label.minWidth).toBe(0);
    expect(base.label.overflow).toBe('hidden');
    expect(base.label.whiteSpace).toBe('nowrap');
    expect(base.label.textOverflow).toBe('ellipsis');
  });

  it('reserves the outlined chip border inside the height every chip size carries', () => {
    const outlined = (tagRecipe.variants?.variant.outlined as unknown as Record<string, Declarations>).root;

    expect(outlined.borderWidth).toBe('1px');

    for (const [ , metrics ] of TAG_SIZES) {
      expect(metrics.borderWidth).toBe(1);
    }
  });

  it('lets a pill tab grow with its label instead of holding a height that clips it', () => {
    const trigger = (tabsRecipe.variants?.variant.pill as unknown as Record<string, Declarations>).trigger;

    expect(trigger.height).toBe('auto');
    expect(trigger.minH).toBe('var(--tabs-height)');
    expect(trigger.minW).toBe('auto');
    expect(trigger.whiteSpace).toBe('nowrap');
    expect(trigger.borderWidth).toBe('1px');
  });

  it('runs the pill tab strip at a height that holds its line box, its padding and its border', () => {
    const size = tabsRecipe.variants?.size.sm as unknown as Record<string, Declarations>;
    const strip = String(size.root['--tabs-height']).replace('sizes.', '');
    const padding = pillSpacing(String(size.trigger.py));

    expect(size.trigger.textStyle).toBe('sm');
    expect(pillLineBox('sm')).toBe(20);
    expect(padding).toBe(4);
    expect(pillSize(strip)).toBe(32);
    expect(pillSize(strip)).toBeGreaterThanOrEqual(20 + 8 + 2);
    expect(pillSize(strip)).toBeGreaterThanOrEqual(pillLineBox('sm') + padding * 2 + 2);
  });
});
