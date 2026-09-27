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

  it('keeps the plain variant and the default the rest of the app uses', () => {
    expect(recipe.variants?.variant.plain).toBeDefined();
    expect(recipe.defaultVariants?.variant).toBe('solid');
  });
});
