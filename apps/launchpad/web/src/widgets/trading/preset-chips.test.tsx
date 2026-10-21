// @vitest-environment jsdom
import React, { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, describe, expect, it } from 'vitest';
import { BUY_PRESETS, BuyPresets, PresetChip, SellPcts, presetAmountText } from './trade-panel-parts';

(globalThis as unknown as { IS_REACT_ACT_ENVIRONMENT: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

const noop = () => {};
let mounted: { root: Root; container: HTMLDivElement } | null = null;

function render(node: React.ReactElement): HTMLDivElement {
  const container = document.createElement('div');
  document.body.appendChild(container);
  const root = createRoot(container);
  act(() => { root.render(node); });
  mounted = { root, container };
  return container;
}

afterEach(() => {
  if (!mounted) return;
  const { root, container } = mounted;
  act(() => { root.unmount(); });
  container.remove();
  mounted = null;
});

const CLIP = /^(truncate|overflow-|text-ellipsis|line-clamp|break-|whitespace-(?!nowrap))/;
const BORDER = /\b(border(?!-0\b|-none\b)(-[a-z0-9/\[\]-]+)?|divide-[a-z]|ring-(?!offset)[a-z0-9]|outline-(?!none|focus|2|offset)[a-z]+)\b/;

/** A chip is single-line by construction: nowrap, nothing that clips or ellipsises, and one word of text. */
function expectSingleLine(chip: HTMLButtonElement) {
  const classes = chip.className.split(/\s+/);
  expect(classes).toContain('whitespace-nowrap');
  expect(classes).toContain('min-w-0');
  expect(classes.filter(name => CLIP.test(name))).toEqual([]);
  expect(classes.filter(name => BORDER.test(name))).toEqual([]);
  expect(chip.querySelector('br')).toBeNull();
  expect(chip.textContent).not.toMatch(/\s/);
}

describe('quick-amount chips never wrap or clip at the right-rail width', () => {
  it('Buy panel: amount-only chips with the unit shown once in the row, full amounts as accessible names', () => {
    const container = render(<BuyPresets amount="100" onSelect={noop} onMax={noop} />);
    const group = container.querySelector<HTMLElement>('[role="group"][aria-label="Quick USDL amounts"]')!;
    const chips = Array.from(group.querySelectorAll<HTMLButtonElement>('button'));
    expect(chips.map(chip => chip.textContent)).toEqual(['25', '100', '250', 'Max']);
    expect(chips.map(chip => chip.getAttribute('aria-label'))).toEqual(['25 USDL', '100 USDL', '250 USDL', 'Use full USDL balance']);
    expect(chips.map(chip => chip.getAttribute('title'))).toEqual(['25 USDL', '100 USDL', '250 USDL', 'Use full USDL balance']);
    expect(chips.map(chip => chip.getAttribute('aria-pressed'))).toEqual(['false', 'true', 'false', 'false']);
    chips.forEach(expectSingleLine);
    // The unit appears once, as the row's leading label, never inside a chip.
    expect(group.textContent?.match(/USDL/g)).toHaveLength(1);
    const unit = group.firstElementChild as HTMLElement;
    expect(unit.textContent).toBe('USDL');
    expect(unit.className.split(/\s+/)).toContain('shrink-0');
    // The four chips share the remaining width in equal columns.
    const grid = chips[0].parentElement!;
    expect(grid.className.split(/\s+/)).toEqual(expect.arrayContaining(['grid', 'grid-cols-4', 'min-w-0', 'flex-1']));
    expect(BUY_PRESETS.every(preset => presetAmountText(preset) === preset)).toBe(true);
  });

  it('Buy panel: Max stays disabled without a wallet and still renders on one line', () => {
    const container = render(<BuyPresets amount="" onSelect={noop} />);
    const max = container.querySelector<HTMLButtonElement>('button[aria-label="Use full USDL balance"]')!;
    expect(max.disabled).toBe(true);
    expectSingleLine(max);
  });

  it('Sell panel: percent chips share the same single-line chip', () => {
    const container = render(<SellPcts tokenBalance={1000n} tokenDecimals={0} amount="" onSelect={noop} />);
    const chips = Array.from(container.querySelectorAll<HTMLButtonElement>('[role="group"] button'));
    expect(chips.map(chip => chip.textContent)).toEqual(['25%', '50%', '75%', '100%']);
    chips.forEach(expectSingleLine);
  });

  it('Terminal quick-trade card: the xs chip is single-line with tighter type', () => {
    const container = render(<PresetChip size="xs" active={false} onClick={noop} label="Set 1000000.5 USDL">{presetAmountText('1000000.5')}</PresetChip>);
    const chip = container.querySelector('button')!;
    expectSingleLine(chip);
    expect(chip.className.split(/\s+/)).toEqual(expect.arrayContaining(['px-1', 'text-xs']));
    expect(chip.textContent).toBe('1M');
    expect(chip.getAttribute('aria-label')).toBe('Set 1000000.5 USDL');
  });

  it('long user presets get a short chip text; the exact amount stays in the accessible name', () => {
    const cases: Array<[string, string]> = [
      ['25', '25'], ['1000.5', '1000.5'], ['12.345678', '12.346'], ['100.000001', '100'],
      ['2500000', '2.5M'], ['999999999999.123456', '1T'], ['0.000001', '<0.01'],
    ];
    for (const [amount, text] of cases) {
      expect(presetAmountText(amount)).toBe(text);
      expect(presetAmountText(amount).length).toBeLessThanOrEqual(6);
    }
  });
});
