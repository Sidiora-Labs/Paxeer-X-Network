import { describe, expect, it } from 'vitest';

import { recipe } from './button.recipe';

const control = recipe.variants?.variant.scan_control;

describe('button recipe, scan_control variant', () => {
  it('draws an outlined control on the surface', () => {
    expect(control).toMatchObject({
      borderWidth: '1px',
      bg: 'bg.surface',
      color: 'text.secondary',
      borderColor: 'border.divider',
    });
  });

  it('fills the selected control with the accent and mutes the disabled one', () => {
    expect(control?._selected).toMatchObject({
      bg: 'selected.control.bg',
      color: 'selected.control.text',
      borderColor: 'transparent',
    });
    expect(control?._disabled).toMatchObject({ color: 'text.muted', borderColor: 'border.divider' });
  });

  it('draws the outlined variants on a hairline ring and pads the pill like the product', () => {
    expect(recipe.base?.fontWeight).toBe(500);
    expect(recipe.variants?.variant.outline).toMatchObject({ borderWidth: '1px', borderColor: 'border.strong' });
    expect(recipe.variants?.variant.dropdown).toMatchObject({ borderWidth: '1px' });
    expect(recipe.variants?.variant.pagination).toMatchObject({ borderWidth: '1px' });
    expect(recipe.variants?.size.md).toMatchObject({ px: 6, h: 10, borderRadius: 'full' });
    expect(recipe.variants?.size.sm).toMatchObject({ px: 4, h: 8 });
  });

  it('keeps the plain variant and the default the rest of the app uses', () => {
    expect(recipe.variants?.variant.plain).toBeDefined();
    expect(recipe.defaultVariants?.variant).toBe('solid');
  });
});
