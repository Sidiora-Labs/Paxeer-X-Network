// @vitest-environment jsdom

import { describe, it, expect, vi } from 'vitest';

vi.hoisted(() => {
  window.__envs = {
    ...window.__envs,
    NEXT_PUBLIC_NAVIGATION_LAYOUT: 'horizontal',
  };
});

import { CONTENT_MAX_WIDTH } from './utils';

describe('CONTENT_MAX_WIDTH', () => {
  it('centres the content in a 1280 pixel container under the horizontal navigation', () => {
    expect(CONTENT_MAX_WIDTH).toBe(1_280);
  });

  it('keeps the wide container under the vertical navigation', async() => {
    window.__envs = { ...window.__envs, NEXT_PUBLIC_NAVIGATION_LAYOUT: 'vertical' };
    vi.resetModules();

    const utils = await import('./utils');

    expect(utils.CONTENT_MAX_WIDTH).toBe(1_920);
  });
});
