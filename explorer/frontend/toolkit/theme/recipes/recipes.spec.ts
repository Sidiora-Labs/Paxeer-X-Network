import { describe, expect, it } from 'vitest';

import { radii } from '../foundations/borders';
import shadows from '../foundations/shadows';
import { recipe as badgeRecipe } from './badge.recipe';
import { recipe as buttonRecipe } from './button.recipe';
import { recipe as statRecipe } from './stat.recipe';
import { recipe as tableRecipe } from './table.recipe';
import { recipe as tabsRecipe } from './tabs.recipe';
import { recipe as tagRecipe } from './tag.recipe';

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
