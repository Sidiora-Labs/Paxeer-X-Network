import { describe, expect, it } from 'vitest';

import { recipe } from './stat.recipe';

const scan = recipe.variants?.variant.scan;

describe('stat recipe, scan variant', () => {
  it('stacks the card on the surface inside the divider border', () => {
    expect(scan?.root).toMatchObject({
      flexDirection: 'column',
      alignItems: 'flex-start',
      bg: 'bg.surface',
      borderWidth: '1px',
      borderColor: 'border.divider',
      borderRadius: 'md',
      boxShadow: 'card',
    });
  });

  it('sets the label in muted small caps above a heading-sized value', () => {
    expect(scan?.label).toMatchObject({ color: 'text.muted', textStyle: 'xs', textTransform: 'uppercase' });
    expect(scan?.valueText).toMatchObject({ color: 'text.primary', textStyle: 'heading.sm' });
  });

  it('colours a rising delta apart from a falling one', () => {
    expect(scan?.valueText?.['& [data-delta=up]']).toMatchObject({ color: 'stat.indicator.up' });
    expect(scan?.valueText?.['& [data-delta=down]']).toMatchObject({ color: 'stat.indicator.down' });
    expect(scan?.valueText?.['& [data-secondary]']).toMatchObject({ color: 'text.muted' });
  });

  it('is declared after the orientation variant so the column layout wins over the default row', () => {
    const order = Object.keys(recipe.variants ?? {});
    expect(recipe.defaultVariants?.orientation).toBe('horizontal');
    expect(order.indexOf('variant')).toBeGreaterThan(order.indexOf('orientation'));
  });
});
