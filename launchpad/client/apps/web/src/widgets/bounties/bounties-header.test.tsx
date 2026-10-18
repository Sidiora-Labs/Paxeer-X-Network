// @vitest-environment jsdom
import React, { act } from 'react';
import { afterEach, beforeAll, describe, expect, it, vi } from 'vitest';
import { createRoot, type Root } from 'react-dom/client';
import type { Loadable } from '@/core/market/unsupported';
import { closeAuthModal, useAuthModal } from '@/shell/auth-modal-store';
import { BountiesHeader, CreateBountyFab, createBlockedNotice, createBountySignInReason, useCreateBountyGate } from './bounties-header';
import { BountiesStats, bountyTotals } from './bounties-stats';
import { BountiesTabs, bountyTabCounts } from './bounties-tabs';
import type { BountiesStatsResponse } from './bounties-contract';

vi.mock('next/image', async importOriginal => ({ default: ((await importOriginal()) as { default: unknown }).default }));

beforeAll(() => {
  (globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
});

const BORDER = /\b(border(?!-0\b|-none\b)(-[a-z0-9/[\]-]+)?|divide-[a-z]|ring-(?!offset)[a-z0-9]|shadow(-[a-z0-9]+)?)\b/;
type Availability = Loadable<{ generatedAt: string }>;
const ready: Availability = { state: 'ready', data: { generatedAt: '2026-10-13T00:00:00Z' } };
const failed: Availability = { state: 'error', error: new TypeError('Failed to fetch'), retry: () => {} };
const missing: Availability = { state: 'unsupported', operation: 'IF_BOUNTY_LIST', status: 404, message: 'not served' };

const mounted: Array<{ root: Root; container: HTMLDivElement }> = [];
function mount(node: React.ReactElement): HTMLDivElement {
  const container = document.createElement('div');
  document.body.appendChild(container);
  const root = createRoot(container);
  act(() => root.render(node));
  mounted.push({ root, container });
  return container;
}
afterEach(() => {
  for (const { root, container } of mounted.splice(0)) { act(() => root.unmount()); container.remove(); }
});

function Harness({ availability, fab = false, signedIn = true }: { availability: Availability; fab?: boolean; signedIn?: boolean }) {
  const gate = useCreateBountyGate(availability, signedIn);
  return fab ? <CreateBountyFab gate={gate} /> : <BountiesHeader tab="open" gate={gate} stats={<span>chips</span>} inlineStats={<span>inline</span>} />;
}

function AuthModalProbe() {
  const { open, reason } = useAuthModal();
  return <output data-testid="auth-modal">{open ? reason : 'closed'}</output>;
}

function button(container: HTMLElement, label: string): HTMLButtonElement | HTMLAnchorElement {
  const found = [...container.querySelectorAll<HTMLButtonElement | HTMLAnchorElement>('button, a')].find(el => el.textContent === label);
  if (!found) throw new Error(`No control labelled ${label}`);
  return found;
}

const statsReady: Loadable<BountiesStatsResponse> = {
  state: 'ready',
  data: {
    data: { openCount: 74, submissionsCount: 893, unclaimedPoolUsdlRaw: '13000000000', paidOutUsdlRaw: '1000000000000' },
    generatedAt: '2026-10-13T00:00:00Z', nextCursor: null, quality: 'complete', requestId: 'r', revision: null, watermark: null,
  } as BountiesStatsResponse,
};

describe('create bounty call to action', () => {
  it('explains a blocked create for every non-ready service state and nothing when ready', () => {
    expect(createBlockedNotice(ready)).toBeNull();
    expect(createBlockedNotice({ state: 'loading' })?.message).toMatch(/Still checking/);
    expect(createBlockedNotice(failed)?.message).toBe("Can't create a bounty right now, try again");
    expect(createBlockedNotice(missing)?.message).toMatch(/isn't available yet/);
  });

  it('is never disabled while the service is down and shows the honest notice only after a click', () => {
    const container = mount(<Harness availability={failed} />);
    const cta = button(container, 'Create bounty') as HTMLButtonElement;
    expect(cta.tagName).toBe('BUTTON');
    expect(cta.disabled).toBe(false);
    expect(cta.getAttribute('aria-disabled')).toBeNull();
    expect(container.textContent).not.toMatch(/Can't create/);
    act(() => cta.click());
    const notice = container.querySelector('[role="status"]');
    expect(notice?.textContent).toMatch(/Can't create a bounty right now/);
    expect(notice?.textContent).not.toMatch(/Failed to fetch/);
    act(() => container.querySelector<HTMLButtonElement>('button[aria-label="Dismiss"]')?.click());
    expect(container.querySelector('[role="status"]')).toBeNull();
  });

  it('links straight to the create page when the service answered', () => {
    const container = mount(<Harness availability={ready} />);
    const cta = button(container, 'Create bounty');
    expect(cta.tagName).toBe('A');
    expect(cta.getAttribute('href')).toBe('/bounties/new');
    expect(cta.getAttribute('aria-disabled')).toBeNull();
  });

  it('opens the shared sign-in modal instead of disabling when the service answered but no session is signed in', () => {
    closeAuthModal();
    const container = mount(<><Harness availability={ready} signedIn={false} /><AuthModalProbe /></>);
    const cta = button(container, 'Create bounty') as HTMLButtonElement;
    expect(cta.tagName).toBe('BUTTON');
    expect(cta.disabled).toBe(false);
    expect(container.querySelector('[data-testid="auth-modal"]')?.textContent).toBe('closed');
    act(() => cta.click());
    expect(container.querySelector('[data-testid="auth-modal"]')?.textContent).toBe(createBountySignInReason);
    expect(container.querySelector('[role="status"]')).toBeNull();
    act(() => closeAuthModal());
  });

  it('sends a signed-out phone Post bounty tap to the same sign-in modal', () => {
    closeAuthModal();
    const container = mount(<><Harness availability={ready} signedIn={false} fab /><AuthModalProbe /></>);
    act(() => (button(container, 'Post bounty') as HTMLButtonElement).click());
    expect(container.querySelector('[data-testid="auth-modal"]')?.textContent).toBe(createBountySignInReason);
    act(() => closeAuthModal());
  });

  it('renders the phone Post bounty pill fixed above the bottom navigation and gates it the same way', () => {
    const container = mount(<Harness availability={missing} fab />);
    const fab = container.querySelector('[data-testid="post-bounty-fab"]');
    expect(fab?.className).toMatch(/\bfixed\b/);
    expect(fab?.className).toMatch(/\bmd:hidden\b/);
    const pill = button(container, 'Post bounty') as HTMLButtonElement;
    expect(pill.className).toMatch(/rounded-pill/);
    expect(pill.disabled).toBe(false);
    act(() => pill.click());
    expect(mount(<Harness availability={ready} fab />).querySelector('a')?.getAttribute('href')).toBe('/bounties/new');
  });

  it('keeps the title at the reference scale and puts chips and the CTA on the title row from md', () => {
    const container = mount(<Harness availability={ready} />);
    const h1 = container.querySelector('h1');
    expect(h1?.textContent).toBe('Open bounties');
    expect(h1?.className).toMatch(/md:text-\[28px\]/);
    expect(h1?.className).not.toMatch(/extrabold|text-2xl/);
    const row = h1?.closest('div')?.parentElement;
    expect(row?.textContent).toContain('chips');
    expect(row?.textContent).toContain('Create bounty');
    expect(container.innerHTML).not.toMatch(BORDER);
  });
});

describe('bounty totals', () => {
  it('renders four compact chips on one row with real values', () => {
    const container = mount(<BountiesStats stats={statsReady} openCount={null} />);
    const dl = container.querySelector('dl');
    expect(dl?.className).toMatch(/flex-nowrap/);
    expect(dl?.className).not.toMatch(/grid|flex-wrap\b/);
    const chips = [...container.querySelectorAll('[data-stat]')];
    expect(chips.map(chip => chip.getAttribute('data-stat'))).toEqual(['live', 'pool', 'submissions', 'paid']);
    for (const chip of chips) expect(chip.className).toMatch(/py-1\.5/);
    expect(chips[0].textContent).toContain('74');
    expect(chips[2].textContent).toContain('893');
    expect(container.innerHTML).not.toMatch(BORDER);
  });

  it('shows dashes instead of endless skeletons when the service is down, with no repeated error copy', () => {
    const container = mount(<BountiesStats stats={{ state: 'loading' }} openCount={null} openLoading serviceDown />);
    expect(container.querySelector('.sl-skeleton')).toBeNull();
    expect(container.querySelectorAll('[aria-label="No data"]').length).toBe(4);
    expect(container.textContent).not.toMatch(/unavailable/i);
  });

  it('reads "Bounty totals are not available yet" when the totals route is not served, in both variants', () => {
    const notServed = { state: 'unsupported', operation: 'IF_BOUNTIES_STATS', status: 404, message: 'not served' } as Loadable<BountiesStatsResponse>;
    for (const variant of ['chips', 'inline'] as const) {
      const container = mount(<BountiesStats stats={notServed} openCount={null} variant={variant} />);
      expect(container.textContent).toMatch(/Bounty totals are not available yet/);
      expect(container.textContent).not.toMatch(/Bounty totals is not/);
      expect(container.innerHTML).not.toMatch(BORDER);
    }
  });

  it('keeps loading placeholders while the service is still answering', () => {
    const totals = bountyTotals({ stats: { state: 'loading' }, openCount: null, openLoading: true });
    expect(totals.every(total => total.loading)).toBe(true);
  });

  it('falls back to a complete open-list count for Live only', () => {
    const totals = bountyTotals({ stats: missing as Loadable<BountiesStatsResponse>, openCount: 3 });
    expect(totals.map(total => total.value)).toEqual(['3', null, null, null]);
  });

  it('renders the inline variant as two lines of text for narrow headers', () => {
    const container = mount(<BountiesStats stats={statsReady} openCount={null} variant="inline" />);
    const lines = container.querySelectorAll('dl > div');
    expect(lines.length).toBe(2);
    expect(lines[0].textContent).toMatch(/live74.*unclaimed rewards13K USDL/);
    expect(lines[1].textContent).toMatch(/submissions across all893.*paid out1M USDL/);
  });
});

describe('bounty tabs', () => {
  it('shows count badges only for counts that were passed', () => {
    const container = mount(<BountiesTabs active="open" onChange={() => {}} counts={{ open: 74, paid: 12 }} mineLocked />);
    const tabs = [...container.querySelectorAll('[role="tab"]')];
    expect(tabs.map(tab => tab.textContent)).toEqual(['Bounties74', 'Reviewing', 'Paid12', 'Mine, sign in required']);
  });
});


describe('tab counts', () => {
  it('uses only real totals: stats first, then complete lists', () => {
    const partial = { complete: false, items: [1, 2] };
    const complete = { complete: true, items: [1, 2, 3] };
    expect(bountyTabCounts({ statsOpenCount: 74, open: complete, paid: partial, active: { tab: 'open', complete: true, count: 3 } })).toEqual({ open: 74 });
    expect(bountyTabCounts({ statsOpenCount: null, open: complete, paid: complete, active: { tab: 'reviewing', complete: true, count: 0 } })).toEqual({ open: 3, paid: 3, reviewing: 0 });
    expect(bountyTabCounts({ statsOpenCount: null, open: partial, paid: partial, active: { tab: 'paid', complete: false, count: 2 } })).toEqual({});
  });
});
