// @vitest-environment jsdom
import React, { act } from 'react';
import { describe, it, expect, beforeAll, beforeEach } from 'vitest';
import { createRoot, type Root } from 'react-dom/client';
import { BalanceVisibilityProvider, BalanceVisibilityToggle, MaskedAmount, readStoredBalanceVisibility } from './balance-visibility';

beforeAll(() => { (globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true; });
beforeEach(() => { window.localStorage.clear(); });

function mount(node: React.ReactElement): { root: Root; container: HTMLDivElement } {
  const container = document.createElement('div');
  document.body.appendChild(container);
  const root = createRoot(container);
  act(() => root.render(node));
  return { root, container };
}

const page = (
  <BalanceVisibilityProvider>
    <BalanceVisibilityToggle />
    <p data-testid="value"><MaskedAmount>1,843.41 USDL</MaskedAmount></p>
  </BalanceVisibilityProvider>
);

describe('balance visibility', () => {
  it('shows amounts by default and masks every MaskedAmount when toggled, persisting the choice', () => {
    const { root, container } = mount(page);
    const toggle = container.querySelector('button') as HTMLButtonElement;
    expect(toggle.getAttribute('aria-label')).toBe('Hide balances');
    expect(container.querySelector('[data-testid="value"]')?.textContent).toBe('1,843.41 USDL');
    expect(container.querySelector('[data-masked]')).toBeNull();

    act(() => toggle.click());
    expect(toggle.getAttribute('aria-pressed')).toBe('true');
    expect(toggle.getAttribute('aria-label')).toBe('Show balances');
    const masked = container.querySelector('[data-masked]') as HTMLElement;
    expect(masked).not.toBeNull();
    expect(masked.querySelector('[aria-label="Hidden"]')?.textContent).toBe('••••••');
    expect(masked.querySelector('[aria-hidden="true"]')?.textContent).toBe('1,843.41 USDL');
    expect(readStoredBalanceVisibility()).toBe(true);

    act(() => toggle.click());
    expect(container.querySelector('[data-masked]')).toBeNull();
    expect(readStoredBalanceVisibility()).toBe(false);
    act(() => root.unmount());
    container.remove();
  });
  it('restores a stored preference on mount', () => {
    window.localStorage.setItem('sidiora_balances_hidden', '1');
    const { root, container } = mount(page);
    expect(container.querySelector('[data-masked]')).not.toBeNull();
    act(() => root.unmount());
    container.remove();
  });
  it('renders unmasked without a provider', () => {
    const { root, container } = mount(<MaskedAmount>12 USDL</MaskedAmount>);
    expect(container.textContent).toBe('12 USDL');
    expect(container.querySelector('[data-masked]')).toBeNull();
    act(() => root.unmount());
    container.remove();
  });
});
