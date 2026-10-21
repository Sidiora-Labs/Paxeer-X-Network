// @vitest-environment jsdom
import React, { act } from 'react';
import { afterEach, beforeAll, describe, expect, it } from 'vitest';
import { createRoot, type Root } from 'react-dom/client';
import ProfileLayout from './profile-layout';
import ProfileHeader, { avatarFallbackName } from './profile-header';

beforeAll(() => { (globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true; });

let mounted: { root: Root; container: HTMLDivElement } | null = null;
function mount(node: React.ReactElement): HTMLDivElement {
  const container = document.createElement('div');
  document.body.appendChild(container);
  const root = createRoot(container);
  act(() => root.render(node));
  mounted = { root, container };
  return container;
}
afterEach(() => {
  if (!mounted) return;
  act(() => mounted!.root.unmount());
  mounted.container.remove();
  mounted = null;
});

function classTokens(root: Element): string[] {
  return [root, ...Array.from(root.querySelectorAll('[class]'))].flatMap(element => (element.getAttribute('class') ?? '').split(/\s+/));
}
const strokeClass = /^(?:[a-z0-9-]+:)*(?:border|ring|divide|outline|shadow)(?:-|$)/;

const wallet = '0x1dae78b53c9f39930a2ff2b6b7aac12ec75fb154';

describe('profile layout', () => {
  it('is three flush columns: leaderboard rail from 2xl only, centre, top trades rail from lg', () => {
    const container = mount(<ProfileLayout left={<p>board</p>} center={<p>centre</p>} right={<p>trades</p>} />);
    const grid = container.querySelector('[data-profile-layout]')!;
    const tokens = classTokens(grid);
    // No gutters between the columns: the rails start at the same top as the centre.
    expect(grid.className.split(/\s+/).some(token => /(?:^|:)(?:gap|space-[xy])-/.test(token))).toBe(false);
    expect(grid.className).toContain('items-stretch');
    expect(grid.className).toContain('2xl:grid-cols-[minmax(300px,360px)_minmax(0,960px)_minmax(300px,360px)]');
    // Below 2xl (1280, 1024) the grid is centre + right only, so the centre is never crushed by the leaderboard.
    expect(grid.className).toContain('xl:max-2xl:grid-cols-[minmax(0,1fr)_360px]');
    expect(grid.className).toContain('lg:max-xl:grid-cols-[minmax(0,1fr)_320px]');
    const left = container.querySelector('aside[aria-label="Leaderboard"]')!;
    expect(left.className.split(/\s+/)).toEqual(expect.arrayContaining(['hidden', '2xl:block']));
    const right = container.querySelector('aside[aria-label="Top trades"]')!;
    expect(right.className).not.toMatch(/\bhidden\b/);
    // Columns are told apart by surface contrast only.
    expect(left.className).toContain('bg-surface-1');
    expect(right.className).toContain('bg-surface-1');
    expect(tokens.filter(token => strokeClass.test(token))).toEqual([]);
  });

  it('collapses a missing rail instead of leaving an empty gutter', () => {
    const container = mount(<ProfileLayout center={<p>centre</p>} />);
    expect(container.querySelectorAll('aside')).toHaveLength(0);
    expect(container.querySelector('[data-profile-col="center"]')).not.toBeNull();
  });
});

describe('profile header anatomy', () => {
  it('hands the wallet address to the Avatar primitive when there is no display name', () => {
    expect(avatarFallbackName(wallet, null)).toBe(wallet);
    expect(avatarFallbackName(wallet, '  ')).toBe(wallet);
    expect(avatarFallbackName(wallet, 'OxSun')).toBe('OxSun');
  });

  it('keeps banner, overlapping avatar and the action row when no profile data loaded', () => {
    const container = mount(
      <ProfileHeader
        wallet={wallet}
        displayName={null}
        bio=""
        avatarUrl={null}
        bannerUrl={null}
        links={{}}
        isOwner={false}
        actions={<button type="button">Follow</button>}
      />,
    );
    const banner = container.querySelector('[data-profile-banner]')!;
    expect(banner).not.toBeNull();
    expect(banner.className).toContain('xl:h-[240px]');
    expect(banner.className).toContain('h-[100px]');
    const avatar = container.querySelector('[data-profile-avatar]')!;
    // Overlaps the banner bottom and sits on the same row as the actions.
    expect(avatar.className).toMatch(/-mt-7/);
    expect(avatar.className).toMatch(/md:-mt-12/);
    const row = avatar.parentElement!;
    expect(row.textContent).toContain('Follow');
    // The fallback mark itself is the Avatar primitive's job (address-derived for hex names).
    expect(avatar.querySelector('[aria-label]')?.getAttribute('aria-label')).toBe(wallet);
    // The display name falls back to the 0x address on purpose.
    expect(container.querySelector('h1')?.textContent).toMatch(/^0x1dae78/);
    expect(classTokens(container).filter(token => strokeClass.test(token))).toEqual([]);
  });
});
