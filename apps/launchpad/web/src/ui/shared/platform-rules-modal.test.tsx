// @vitest-environment jsdom
import React, { act } from 'react';
import { afterEach, beforeAll, beforeEach, describe, expect, it } from 'vitest';
import { createRoot, type Root } from 'react-dom/client';
import { readFileSync } from 'node:fs';
import { join } from 'node:path';
import PlatformRulesModal, { RULES_AGREED_KEY } from './platform-rules-modal';
import CookieBanner from './cookie-banner';

beforeAll(() => { (globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true; });

const mounted: Array<{ root: Root; container: HTMLElement }> = [];
function mount(node: React.ReactNode) {
  const container = document.createElement('div');
  document.body.appendChild(container);
  const root = createRoot(container);
  act(() => root.render(node));
  mounted.push({ root, container });
  return container;
}

beforeEach(() => {
  window.localStorage.clear();
  // Phone viewport from the production screenshot pass.
  Object.defineProperty(window, 'innerWidth', { configurable: true, value: 390 });
  Object.defineProperty(window, 'innerHeight', { configurable: true, value: 844 });
  window.dispatchEvent(new Event('resize'));
});
afterEach(() => {
  for (const { root, container } of mounted.splice(0)) { act(() => root.unmount()); container.remove(); }
  window.localStorage.clear();
});

const dialogOf = () => document.querySelector<HTMLElement>('[role="dialog"]');
const cta = () => Array.from(document.querySelectorAll('button')).find(button => button.textContent?.includes('I understand'));

describe('platform rules modal', () => {
  it('renders a composed Sidiora header in markup and no legacy Kindle image', () => {
    mount(<PlatformRulesModal />);
    const dialog = dialogOf()!;
    const banner = dialog.querySelector('[data-slot="dialog-banner"]')!;
    expect(banner.textContent).toContain('Launch, earn, enjoy');
    expect(banner.textContent).toContain('Sidiora');
    expect(banner.className).toContain('bg-surface-2');
    expect(dialog.className).toContain('bg-surface-1');
    expect(dialog.querySelector('img')).toBeNull();
    expect(dialog.outerHTML).not.toMatch(/kindle/i);
    expect(dialog.outerHTML).not.toContain('Welcome_banner');
    expect(readFileSync(join(process.cwd(), 'src/ui/shared/platform-rules-modal.tsx'), 'utf8')).not.toContain('Welcome_banner');
  });

  it('is a labelled modal dialog', () => {
    mount(<PlatformRulesModal />);
    const dialog = dialogOf()!;
    expect(dialog.getAttribute('aria-modal')).toBe('true');
    const title = document.getElementById(dialog.getAttribute('aria-labelledby')!);
    expect(title?.textContent?.trim()).toBe('Platform rules');
    expect(document.getElementById(dialog.getAttribute('aria-describedby')!)?.textContent).toContain('Please review');
  });

  it('pins the CTA and footnote in a footer outside the scrolling body at 390x844', () => {
    mount(<PlatformRulesModal />);
    expect(window.innerWidth).toBe(390);
    expect(window.innerHeight).toBe(844);
    const dialog = dialogOf()!;
    expect(dialog.className).toMatch(/\bflex-col\b/);
    expect(dialog.className).toContain('max-h-[calc(100dvh-2rem)]');
    expect(dialog.className).toContain('overflow-hidden');
    expect(dialog.className).not.toContain('overflow-y-auto');
    const body = dialog.querySelector<HTMLElement>('[data-slot="dialog-body"]')!;
    const footer = dialog.querySelector<HTMLElement>('[data-slot="dialog-footer"]')!;
    expect(body.className).toMatch(/\bmin-h-0\b/);
    expect(body.className).toMatch(/\bflex-1\b/);
    expect(body.className).toContain('overflow-y-auto');
    expect(footer.className).toMatch(/\bshrink-0\b/);
    expect(footer.parentElement).toBe(dialog);
    expect(body.nextElementSibling).toBe(footer);
    const button = cta()!;
    expect(dialog.contains(button)).toBe(true);
    expect(footer.contains(button)).toBe(true);
    expect(body.contains(button)).toBe(false);
    expect(footer.textContent).toContain("You'll only see this once");
    expect(getComputedStyle(footer).display).not.toBe('none');
    expect(getComputedStyle(button).visibility).not.toBe('hidden');
  });

  it('uses no glow, shadow, gradient or border separation anywhere in the modal', () => {
    mount(<PlatformRulesModal />);
    const overlay = dialogOf()!.parentElement!;
    for (const element of [overlay, ...Array.from(overlay.querySelectorAll<HTMLElement>('*'))]) {
      const className = element.getAttribute('class') ?? '';
      expect(className).not.toMatch(/shadow|gradient|glow|drop-shadow|\bring\b|ring-|\bborder\b|border-(?!0\b|none\b)/);
      const style = element.getAttribute('style') ?? '';
      expect(style).not.toMatch(/box-shadow|gradient|background-image/);
      expect(element.style.boxShadow).toBe('');
      expect(element.style.backgroundImage).toBe('');
    }
  });

  it('keeps focus inside, ignores Escape, and closes only on agreement', () => {
    const outside = document.createElement('button');
    outside.textContent = 'outside';
    document.body.appendChild(outside);
    try {
      mount(<PlatformRulesModal />);
      const dialog = dialogOf()!;
      expect(dialog.contains(document.activeElement)).toBe(true);
      expect(outside.hasAttribute('inert')).toBe(true);

      act(() => { outside.focus(); });
      expect(dialog.contains(document.activeElement)).toBe(true);

      const button = cta()!;
      button.focus();
      act(() => { button.dispatchEvent(new KeyboardEvent('keydown', { key: 'Tab', bubbles: true, cancelable: true })); });
      expect(dialog.contains(document.activeElement)).toBe(true);
      expect(document.activeElement).not.toBe(button);

      const escape = new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true });
      act(() => { dialog.dispatchEvent(escape); });
      expect(escape.defaultPrevented).toBe(true);
      expect(dialogOf()).not.toBeNull();

      act(() => { button.click(); });
      expect(dialogOf()).toBeNull();
      expect(window.localStorage.getItem(RULES_AGREED_KEY)).toBeTruthy();
      expect(outside.hasAttribute('inert')).toBe(false);
    } finally { outside.remove(); }
  });

  it('does not render once agreed', () => {
    window.localStorage.setItem(RULES_AGREED_KEY, '1');
    mount(<PlatformRulesModal />);
    expect(dialogOf()).toBeNull();
  });
});

describe('cookie banner with the rules modal', () => {
  const cookieText = () => document.body.textContent?.includes('We use cookies') ?? false;

  it('stays hidden while the rules modal is open and appears after agreement', () => {
    mount(<><PlatformRulesModal /><CookieBanner /></>);
    expect(dialogOf()).not.toBeNull();
    expect(cookieText()).toBe(false);
    act(() => { cta()!.click(); });
    expect(dialogOf()).toBeNull();
    expect(cookieText()).toBe(true);
  });

  it('shows immediately for a visitor who already agreed to the rules', () => {
    window.localStorage.setItem(RULES_AGREED_KEY, '1');
    mount(<CookieBanner />);
    expect(cookieText()).toBe(true);
  });
});
