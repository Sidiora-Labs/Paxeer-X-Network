import { describe, expect, it } from 'vitest';

import { recipe } from './table.recipe';

const scan = recipe.variants?.variant.scan;

describe('table recipe, scan variant', () => {
  it('draws the header row on the sunken surface with the divider border', () => {
    expect(scan?.columnHeader).toMatchObject({
      color: 'table.header.fg',
      backgroundColor: 'bg.sunken',
      borderBottomWidth: '1px',
      borderColor: 'border.divider',
    });
  });

  it('separates every row with the divider border and drops it on the last one', () => {
    expect(scan?.cell).toMatchObject({ borderBottomWidth: '1px', borderColor: 'border.divider' });
    expect(scan?.row).toMatchObject({ bg: 'bg.surface', _hover: { bg: 'bg.sunken' } });
    expect(scan?.row?._last).toEqual({ '& td': { borderBottomWidth: '0' } });
  });

  it('leaves the line variant the rest of the app uses untouched', () => {
    expect(recipe.variants?.variant.line).toBeDefined();
    expect(recipe.defaultVariants?.variant).toBe('line');
  });
});
