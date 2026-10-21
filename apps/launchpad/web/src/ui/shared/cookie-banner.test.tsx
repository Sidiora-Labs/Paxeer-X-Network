// @vitest-environment jsdom
import React, { act } from 'react';
import { afterEach, beforeAll, beforeEach, describe, expect, it } from 'vitest';
import { createRoot, type Root } from 'react-dom/client';
import CookieBanner, { COOKIE_BANNER_PHONE_BOTTOM_CLASS } from './cookie-banner';
import { RULES_AGREED_KEY } from './platform-rules-modal';
import { BOTTOM_NAV_BREAKPOINT_PX, BOTTOM_NAV_HEIGHT_PX } from '@/shell/mobile-bottom-nav';

beforeAll(() => { (globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true; });

/** jsdom has no layout or matchMedia: evaluate min/max-width queries against a fake viewport width. */
function setViewportWidth(width: number) {
  Object.defineProperty(window, 'innerWidth', { configurable: true, value: width });
  window.matchMedia = (query: string) => {
    const max = /max-width:\s*([\d.]+)px/.exec(query);
    const min = /min-width:\s*([\d.]+)px/.exec(query);
    const matches = (!max || width <= Number(max[1])) && (!min || width >= Number(min[1]));
    return { matches, media: query, onchange: null, addEventListener() {}, removeEventListener() {}, addListener() {}, removeListener() {}, dispatchEvent: () => false } as MediaQueryList;
  };
}

const mounted: Array<{ root: Root; container: HTMLElement }> = [];
function mount() {
  const container = document.createElement('div');
  document.body.appendChild(container);
  const root = createRoot(container);
  act(() => root.render(<CookieBanner />));
  mounted.push({ root, container });
}
const banner = () => document.querySelector<HTMLElement>('[data-placement]');

beforeEach(() => {
  window.localStorage.clear();
  window.localStorage.setItem(RULES_AGREED_KEY, Date.now().toString());
});
afterEach(() => {
  for (const { root, container } of mounted.splice(0)) { act(() => root.unmount()); container.remove(); }
  window.localStorage.clear();
});

describe('cookie banner placement', () => {
  it('sits above the bottom nav (bar height plus safe-area inset) on phones', () => {
    setViewportWidth(390);
    mount();
    const el = banner()!;
    expect(el).not.toBeNull();
    expect(el.dataset.placement).toBe('above-bottom-nav');
    expect(COOKIE_BANNER_PHONE_BOTTOM_CLASS).toBe(`bottom-[calc(${BOTTOM_NAV_HEIGHT_PX}px+env(safe-area-inset-bottom,0px))]`);
    expect(el.classList).toContain('fixed');
    expect(el.classList).toContain('bottom-[calc(56px+env(safe-area-inset-bottom,0px))]');
    expect(el.classList).not.toContain('bottom-0');
  });

  it('keeps the phone offset one pixel below the breakpoint', () => {
    setViewportWidth(BOTTOM_NAV_BREAKPOINT_PX - 1);
    mount();
    expect(banner()!.dataset.placement).toBe('above-bottom-nav');
  });

  it('stays flush with the viewport bottom from the breakpoint up', () => {
    for (const width of [BOTTOM_NAV_BREAKPOINT_PX, 1440]) {
      setViewportWidth(width);
      mount();
      const el = banner()!;
      expect(el.dataset.placement).toBe('viewport-bottom');
      expect(el.classList).toContain('fixed');
      expect(el.classList).toContain('bottom-0');
      expect(el.className).not.toContain('safe-area');
      act(() => mounted.pop()!.root.unmount());
    }
  });
});
