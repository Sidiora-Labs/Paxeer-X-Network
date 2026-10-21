// @vitest-environment jsdom
import React, { act } from 'react';
import { describe, it, expect, beforeAll, vi } from 'vitest';
import { renderToStaticMarkup } from 'react-dom/server';
import { createRoot, type Root } from 'react-dom/client';
import { Star } from 'lucide-react';
import {
  Avatar, AvatarStack, Chip, PercentPill, StatusBadge, RankBadge, StageBadge, SegmentedControl, TabStrip, UnderlineTabs,
  DropdownMenu, SelectPill, Button, IconButton, StatTile, CompactNumber, MoneyValue, Sparkline, ProgressBar, PairedBar,
  ModalFrame, Tooltip, Skeleton, EmptyState, UnavailableState, ErrorState, LoadMore, DisclosureRow, NoticeBar, FormField,
  Toggle, Slider, SortHeader, ResizeHandle, ShortAddress, AgeChip, CountdownProgress, SectionHeader, CoinCard, KeyValueRow, CountPill,
  isPluralTitle, notAvailableTitle, humanError, humanErrorText,
} from './index';
import { StateNotice } from './states';
import { initialsOf, isAddressName, addressMarkOf } from './avatar';

beforeAll(() => {
  (globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
});

const html = (node: React.ReactElement) => renderToStaticMarkup(node);
const BORDER = /\b(border(?!-0\b|-none\b)(-[a-z0-9/\[\]-]+)?|divide-[a-z]|ring-(?!offset)[a-z0-9]|outline-(?!none|focus|2|offset)[a-z]+)\b/;
const noop = () => {};
const addr = '0x1234567890abcdef1234567890abcdef12345678';
const now = Date.parse('2026-10-12T12:00:00Z');

function mount(node: React.ReactElement): { root: Root; container: HTMLDivElement } {
  const container = document.createElement('div');
  document.body.appendChild(container);
  const root = createRoot(container);
  act(() => root.render(node));
  return { root, container };
}

function unmount(root: Root, container: HTMLDivElement) {
  act(() => root.unmount());
  container.remove();
}

describe('primitives render in several states without border utilities', () => {
  const cases: Array<[string, React.ReactElement[]]> = [
    ['Avatar', [<Avatar key="a" src={null} name="Ada Lovelace" />, <Avatar key="b" src="/icons/token-fallback.svg" name="Coin" shape="square" size="xl" />, <Avatar key="c" src={null} name="Loading" loading />]],
    ['AvatarStack', [<AvatarStack key="a" items={[{ src: null, name: 'A' }, { src: null, name: 'B' }]} label="2 quote assets" />, <AvatarStack key="b" items={Array.from({ length: 7 }, (_, i) => ({ src: null, name: `N${i}` }))} max={3} label="7 quote assets" />]],
    ['Chip', [<Chip key="a">Label</Chip>, <Chip key="b" selected onClick={noop}>Label</Chip>, <Chip key="c" pressed={false} onClick={noop} count={12}>Label</Chip>, <Chip key="d" href="/explore" variant="preset" selected>Label</Chip>]],
    ['PercentPill', [<PercentPill key="a" value={12.3} arrow />, <PercentPill key="b" value={-4.5} variant="text" />, <PercentPill key="c" value={0} />, <PercentPill key="d" value={null} />]],
    ['StatusBadge', [<StatusBadge key="a" status="open" />, <StatusBadge key="b" status="paid" />, <StatusBadge key="c" status="live" dot />, <StatusBadge key="d" status="recent" dot />]],
    ['RankBadge', [<RankBadge key="a" rank={1} />, <RankBadge key="b" rank={12} />, <RankBadge key="c" rank={2} variant="media" />, <RankBadge key="d" rank={5} variant="disc" />]],
    ['StageBadge', [<StageBadge key="a" stage="graduated" />, <StageBadge key="b" stage="soon" decorative />]],
    ['SegmentedControl', [<SegmentedControl key="a" label="Window" options={[{ value: 'a', label: '1H' }, { value: 'b', label: '24H', count: 3 }]} value="a" onChange={noop} />, <SegmentedControl key="b" label="Side" tone="trade" options={[{ value: 'buy', label: 'Buy' }, { value: 'sell', label: 'Sell' }]} value="sell" onChange={noop} />]],
    ['TabStrip', [<TabStrip key="a" label="Sections" tabs={[{ id: 'x', label: 'First', count: 4 }, { id: 'y', label: 'Second', locked: true }]} active="x" onChange={noop} idPrefix="t" />, <UnderlineTabs key="b" label="Sections" tabs={[{ id: 'x', label: 'First' }, { id: 'y', label: 'Second', disabled: true }]} active="y" onChange={noop} idPrefix="u" />]],
    ['DropdownMenu', [<DropdownMenu key="a" label="More actions" items={[{ id: 'a', label: 'Open' }]} />, <DropdownMenu key="b" label="Sort" showLabel trigger={<span>Sort</span>} items={[{ id: 'a', label: 'A', selected: true }]} />]],
    ['SelectPill', [<SelectPill key="a" label="Sort by" showLabel options={[{ value: 'a', label: 'Label' }]} value="a" onChange={noop} />, <SelectPill key="b" label="Filter" options={[]} value={null} onChange={noop} error="No longer valid" />, <SelectPill key="c" label="Loading" options={[]} value={null} onChange={noop} loading disabled />]],
    ['Button', [<Button key="a" variant="primary">Label</Button>, <Button key="b" variant="secondary" loading>Label</Button>, <Button key="c" variant="danger" disabled>Label</Button>, <Button key="d" variant="link" href="/explore">Label</Button>, <Button key="e" variant="ghost" size="sm" pill icon={<Star />}>Label</Button>]],
    ['IconButton', [<IconButton key="a" label="Star" icon={<Star />} />, <IconButton key="b" label="Star" icon={<Star />} pressed variant="raised" />, <IconButton key="c" label="Busy" icon={<Star />} loading variant="round" />, <IconButton key="d" label="Mute" icon={<Star />} variant="on-media" disabled />]],
    ['StatTile', [<dl key="a"><StatTile label="Label" value="Value" /></dl>, <dl key="b"><StatTile label="Label" value={null} /></dl>, <dl key="c"><StatTile label="Label" value="x" loading /></dl>, <dl key="d"><StatTile label="Label" value="x" masked tone="positive" size="inline" /></dl>]],
    ['CompactNumber', [<CompactNumber key="a" value={1234} />, <CompactNumber key="b" value={null} />, <CompactNumber key="c" value="12.5" kind="percent" />]],
    ['MoneyValue', [<MoneyValue key="a" raw="1500000" />, <MoneyValue key="b" raw="-1500000" tone="auto" />, <MoneyValue key="c" raw={null} />, <MoneyValue key="d" raw="1000000" masked />]],
    ['Sparkline', [<Sparkline key="a" data={[1, 2, 3]} />, <Sparkline key="b" data={[3, 2, 1]} />, <Sparkline key="c" data={[1]} />, <Sparkline key="d" data={[]} loading />]],
    ['ProgressBar', [<ProgressBar key="a" value={56} label="of all-time high" showValue />, <ProgressBar key="b" value={97} label="of all-time high" />, <ProgressBar key="c" value={null} label="of all-time high" showValue />]],
    ['PairedBar', [<PairedBar key="a" left={5} right={3} leftLabel="buys" rightLabel="sells" />, <PairedBar key="b" left={0} right={0} leftLabel="buys" rightLabel="sells" />]],
    ['ModalFrame', [<div key="a"><ModalFrame open={false} onClose={noop} title="Title">Body</ModalFrame></div>]],
    ['Tooltip', [<Tooltip key="a" content="Label"><button type="button">Trigger</button></Tooltip>]],
    ['Skeleton', [<Skeleton key="a" />, <Skeleton key="b" variant="circle" label="profile" />, <Skeleton key="c" variant="rows" rows={2} />, <Skeleton key="d" variant="block" height={40} />]],
    ['EmptyState', [<EmptyState key="a" title="Nothing here" body="Body" />, <EmptyState key="b" title="Nothing" variant="inline" announce />, <table key="c"><tbody><EmptyState title="Nothing" variant="table" colSpan={3} /></tbody></table>]],
    ['UnavailableState', [<UnavailableState key="a" operation="IF_LEADERBOARDS" status={404} title="Leaderboards" onRetry={noop} />, <UnavailableState key="b" operation="IF_BOUNTIES" status={null} variant="inline" title="Bounties" />]],
    ['ErrorState', [<ErrorState key="a" onRetry={noop} errorId="ref-1" />, <ErrorState key="b" variant="inline" message="Failed" />, <ErrorState key="c" onRetry={noop} retrying />]],
    ['LoadMore', [<LoadMore key="a" onLoadMore={noop} hasMore />, <LoadMore key="b" onLoadMore={noop} hasMore={false} />, <LoadMore key="c" onLoadMore={noop} hasMore loading />, <LoadMore key="d" onLoadMore={noop} hasMore error />]],
    ['DisclosureRow', [<DisclosureRow key="a" id="d1" label="Label" open={false} onToggle={noop}>Content</DisclosureRow>, <DisclosureRow key="b" id="d2" label="Label" optional open onToggle={noop}>Content</DisclosureRow>, <DisclosureRow key="c" id="d3" label="Rules" href="/docs/terms" />]],
    ['NoticeBar', [<NoticeBar key="a" tone="info">Label</NoticeBar>, <NoticeBar key="b" tone="warning" variant="card" title="Title" onDismiss={noop} announce>Label</NoticeBar>, <NoticeBar key="c">Label</NoticeBar>]],
    ['FormField', [<FormField key="a" id="f1" label="Label" helper="Helper" inputProps={{ placeholder: 'Placeholder' }} />, <FormField key="b" id="f2" label="Label" error="Required" inputProps={{ defaultValue: 'x' }} />, <FormField key="c" id="f3" label="Label" optional multiline counter={{ count: 95, max: 100 }} textareaProps={{ defaultValue: 'text' }} />, <FormField key="d" id="f4" label="Label" inputProps={{ disabled: true }} loading />]],
    ['Toggle', [<Toggle key="a" checked={false} onChange={noop} label="Label" />, <Toggle key="b" checked onChange={noop} label="Label" />, <Toggle key="c" checked onChange={noop} label="Label" disabled size="sm" />, <Toggle key="d" checked={false} onChange={noop} label="Label" loading />]],
    ['Slider', [<Slider key="a" value={25} onChange={noop} label="Slippage" unit="%" ticks={[25, 50, 100]} />, <Slider key="b" value={60} onChange={noop} label="Slippage" outOfRange />, <Slider key="c" value={0} onChange={noop} label="Slippage" disabled />]],
    ['SortHeader', [<table key="a"><thead><tr><SortHeader label="Label" onSort={noop} /><SortHeader label="Label" sort="desc" onSort={noop} align="right" /><SortHeader label="Label" /></tr></thead></table>]],
    ['ResizeHandle', [<ResizeHandle key="a" value={380} min={320} max={800} onChange={noop} defaultValue={380} />, <ResizeHandle key="b" orientation="vertical" inverted value={312} min={280} max={600} onChange={noop} label="Resize right column" />]],
    ['ShortAddress', [<ShortAddress key="a" address={addr} />, <ShortAddress key="b" address={addr} copy={false} explorerHref={`https://paxscan.paxeer.app/address/${addr}`} />]],
    ['AgeChip', [<AgeChip key="a" iso="2026-10-12T11:55:00Z" now={now} />, <AgeChip key="b" iso={null} />, <AgeChip key="c" iso={1760270100} compact={false} now={now} />]],
    ['CountdownProgress', [<CountdownProgress key="a" endsAt="2026-10-26T12:00:00Z" now={now} />, <CountdownProgress key="b" endsAt="2026-10-13T12:00:00Z" startsAt="2026-10-01T00:00:00Z" variant="bar" now={now} />, <CountdownProgress key="c" endsAt="2026-10-01T12:00:00Z" variant="icon" now={now} />]],
    ['SectionHeader', [<SectionHeader key="a" title="Title" count={3} />, <SectionHeader key="b" title="Title" description="Description" action={<Button size="sm">Label</Button>} loading level={3} />]],
    ['CoinCard', [
      <CoinCard key="a" image={null} name="Name" symbol="SYM" mcapRaw={null} percentChange={null} createdAt={null} href={`/coin/${addr}`} starred={false} onToggleStar={noop} stage="new" footerStats={[{ label: 'Holders', value: null }]} />,
      <CoinCard key="b" image={null} name="Name" symbol="SYM" mcapRaw="143000000000" percentChange={12} createdAt="2026-10-12T11:00:00Z" href={`/coin/${addr}`} starred onToggleStar={noop} variant="screener" rank={1} />,
      <CoinCard key="c" image={null} name="Name" symbol="SYM" mcapRaw={null} percentChange={null} createdAt={null} href="/coin" loading />,
    ]],
    ['KeyValueRow', [<dl key="a"><KeyValueRow label="Label" value="Value" /></dl>, <dl key="b"><KeyValueRow label="Label" value={null} /></dl>, <dl key="c"><KeyValueRow label="Label" value="x" masked /></dl>, <dl key="d"><KeyValueRow label="Label" value="x" loading /></dl>]],
    ['CountPill', [<CountPill key="a" count={12} />, <CountPill key="b" count={12345} label="holders" />]],
  ];

  for (const [name, nodes] of cases) {
    it(`${name} renders ${nodes.length} states`, () => {
      for (const node of nodes) {
        const out = html(node);
        expect(out.length).toBeGreaterThan(0);
        expect(out).not.toMatch(BORDER);
        expect(out).not.toMatch(/#[0-9a-fA-F]{6}\b/);
      }
    });
  }
});

describe('semantics', () => {
  it('ResizeHandle vertical orientation resizes on the x axis and inverted grows toward the start', () => {
    const values: number[] = [];
    const { root, container } = mount(<ResizeHandle orientation="vertical" inverted value={312} min={280} max={600} step={16} onChange={value => values.push(value)} defaultValue={312} label="Resize right column" />);
    const handle = container.querySelector('[role="separator"]') as HTMLElement;
    expect(handle.getAttribute('aria-orientation')).toBe('vertical');
    expect(handle.className).toContain('cursor-col-resize');
    act(() => { handle.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowLeft', bubbles: true })); });
    act(() => { handle.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowRight', bubbles: true })); });
    act(() => { handle.dispatchEvent(new KeyboardEvent('keydown', { key: 'End', bubbles: true })); });
    expect(values).toEqual([328, 296, 600]);
    unmount(root, container);
    const horizontal = mount(<ResizeHandle value={380} min={320} max={800} onChange={value => values.push(value)} />);
    const row = horizontal.container.querySelector('[role="separator"]') as HTMLElement;
    expect(row.getAttribute('aria-orientation')).toBe('horizontal');
    act(() => { row.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowDown', bubbles: true })); });
    expect(values.at(-1)).toBe(396);
    unmount(horizontal.root, horizontal.container);
  });

  it('UnavailableState shows plain copy and keeps the operation id and status out of the visible text', () => {
    const panel = mount(<UnavailableState operation="IF_LEADERBOARDS" status={404} title="Leaderboards" message="HTTP 404: NOT_FOUND" />);
    const state = panel.container.querySelector('[role="status"]') as HTMLElement;
    expect(state.textContent).toContain('Leaderboards is not available yet');
    expect(state.textContent).not.toMatch(/IF_LEADERBOARDS|status 404|NOT_FOUND|HTTP 404/);
    expect(state.getAttribute('data-operation')).toBeNull();
    expect(state.getAttribute('title')).toBeNull();
    unmount(panel.root, panel.container);
    const inline = mount(<UnavailableState operation="GET /bff/token/{p}" status={null} variant="inline" title="This coin" />);
    expect(inline.container.textContent).toBe('This coin is not available yet');
    expect(inline.container.querySelector('[data-operation]')).toBeNull();
    unmount(inline.root, inline.container);
  });

  it('UnavailableState never renders developer detail', () => {
    vi.stubEnv('NODE_ENV', 'production');
    try {
      const out = html(<UnavailableState operation="IF_POOL_STATS.athUsdlRaw" status={null} variant="inline" title="All-time-high valuation" />);
      expect(out).toContain('All-time-high valuation is not available yet');
      expect(out).not.toMatch(/IF_POOL_STATS|no response|data-operation/);
    } finally { vi.unstubAllEnvs(); }
  });

  it('UnavailableState agrees the verb with plural titles when asked, and keeps "is" by default', () => {
    expect(html(<UnavailableState operation="IF_X" status={null} variant="inline" title="Leaderboards" />)).toContain('Leaderboards is not available yet');
    expect(html(<UnavailableState operation="IF_X" status={null} variant="inline" title="Top trades" plural />)).toContain('Top trades are not available yet');
    const auto = (title: string) => html(<UnavailableState operation="IF_X" status={null} variant="inline" title={title} plural="auto" />);
    expect(auto('Callouts')).toContain('Callouts are not available yet');
    expect(auto('Price and 1h change')).toContain('Price and 1h change are not available yet');
    expect(auto('This coin')).toContain('This coin is not available yet');
    expect(auto('Bounties service')).toContain('Bounties service is not available yet');
    expect(auto('Status')).toContain('Status is not available yet');
    expect(html(<UnavailableState operation="IF_X" status={null} title="Live stats" plural="auto" />)).toContain('Live stats are not available yet');
    expect(isPluralTitle('Holder-reward distributions')).toBe(true);
    expect(isPluralTitle('Settings sync')).toBe(false);
    expect(isPluralTitle('Address')).toBe(false);
    expect(notAvailableTitle('Replies', true)).toBe('Replies are not available yet');
  });

  it('never shows a raw browser fetch failure: UnavailableState, ErrorState and NoticeBar reword it', () => {
    const fetchError = new TypeError('Failed to fetch');
    const unavailable = html(<UnavailableState operation="IF_PROFILE" status={null} title="Profile" message="Failed to fetch" />);
    expect(unavailable).not.toMatch(/Failed to fetch/i);
    expect(unavailable).toContain('Connection problem');
    const fromError = html(<UnavailableState operation="IF_PROFILE" status={null} title="Profile" error={fetchError} />);
    expect(fromError.replace(/title="[^"]*"|data-operation="[^"]*"/g, '')).not.toMatch(/Failed to fetch/i);
    expect(fromError).toContain('Connection problem');
    for (const node of [
      <ErrorState key="a" message="Failed to fetch" />,
      <ErrorState key="b" variant="inline" message="Top trades are unavailable right now: Failed to fetch" />,
      <ErrorState key="c" error={fetchError} />,
      <ErrorState key="d" title="Failed to fetch" error={fetchError} variant="inline" />,
      <NoticeBar key="e" tone="danger">Published profile unavailable: Failed to fetch</NoticeBar>,
      <NoticeBar key="f" tone="danger">{'Published profile unavailable: '}{'Failed to fetch'}</NoticeBar>,
    ]) {
      const out = html(node).replace(/title="[^"]*"|data-error-detail="[^"]*"/g, '');
      expect(out).not.toMatch(/Failed to fetch/i);
      expect(out).toMatch(/Connection problem|Something went wrong/);
    }
    // Node's undici wording and a stringified TypeError are caught too.
    expect(humanErrorText('fetch failed')).toBe("Connection problem, check your internet and try again");
    expect(humanError(new TypeError('fetch failed'))).toBe("Connection problem, check your internet and try again");
    expect(html(<ErrorState error={new TypeError('fetch failed')} />).replace(/title="[^"]*"|data-error-detail="[^"]*"/g, '')).not.toMatch(/fetch failed/i);
    expect(html(<UnavailableState operation="IF_X" status={null} title="Profile" message="TypeError: fetch failed" />)).not.toMatch(/fetch failed/i);
    // Sentences written for people pass through untouched.
    expect(html(<NoticeBar tone="danger">Your session expired. Sign in again.</NoticeBar>)).toContain('Your session expired. Sign in again.');
    expect(html(<NoticeBar tone="info">Failed to fetch is shown verbatim only on info notices</NoticeBar>)).toContain('Failed to fetch');
  });

  it('ErrorState never renders the error detail, in any build', () => {
    const err = Object.assign(new Error('Failed to fetch'), { code: 'NETWORK_ERROR' });
    expect(html(<ErrorState error={err} variant="inline" />)).not.toMatch(/data-error-detail|NETWORK_ERROR/);
    vi.stubEnv('NODE_ENV', 'production');
    try {
      const out = html(<ErrorState error={err} variant="inline" />);
      expect(out).not.toMatch(/Failed to fetch|NETWORK_ERROR|data-error-detail/);
    } finally { vi.unstubAllEnvs(); }
  });

  it('ErrorState rewords transport codes passed as the message', () => {
    const out = html(<ErrorState title="Trending is unavailable right now" message="NETWORK_INTERRUPTED" />);
    expect(out).toContain('Connection problem');
    expect(out).not.toContain('NETWORK_INTERRUPTED');
    expect(html(<ErrorState message="Check your wallet and try again." />)).toContain('Check your wallet and try again.');
  });

  it('PercentPill speaks the direction and never relies on colour alone', () => {
    expect(html(<PercentPill value={5} arrow />)).toContain('<span class="sr-only">up</span>');
    expect(html(<PercentPill value={-5} />)).toContain('-5.00%');
    expect(html(<PercentPill value={null} />)).toContain('—');
  });

  it('SegmentedControl is a radiogroup with one checked radio', () => {
    const out = html(<SegmentedControl label="Window" options={[{ value: 'a', label: '1H' }, { value: 'b', label: '24H' }]} value="b" onChange={noop} />);
    expect(out).toContain('role="radiogroup"');
    expect(out.match(/aria-checked="true"/g)?.length).toBe(1);
    expect(out.match(/role="radio"/g)?.length).toBe(2);
  });

  it('TabStrip exposes tablist semantics and stable ids', () => {
    const out = html(<TabStrip label="Sections" tabs={[{ id: 'x', label: 'First' }, { id: 'y', label: 'Second' }]} active="x" onChange={noop} idPrefix="t" />);
    expect(out).toContain('role="tablist"');
    expect(out).toContain('id="t-tab-x"');
    expect(out).toContain('aria-controls="t-panel-x"');
    expect(out).toContain('aria-selected="true"');
  });

  it('Button keeps its label while loading and sets aria-busy', () => {
    const out = html(<Button loading>Save</Button>);
    expect(out).toContain('aria-busy="true"');
    expect(out).toContain('Save');
  });

  it('SortHeader renders aria-sort', () => {
    const out = html(<table><thead><tr><SortHeader label="Volume" sort="desc" onSort={noop} /></tr></thead></table>);
    expect(out).toContain('aria-sort="descending"');
    expect(out).toContain('<button');
  });

  it('StatTile shows an em dash for missing data and a dl-compatible structure', () => {
    const out = html(<dl><StatTile label="Volume" value={null} /></dl>);
    expect(out).toContain('<dt');
    expect(out).toContain('<dd');
    expect(out).toContain('—');
  });

  it('CoinCard names the link by the coin and keeps the star outside it', () => {
    const out = html(<CoinCard image={null} name="Name" symbol="SYM" mcapRaw="143000000000" percentChange={12} createdAt={null} href={`/coin/${addr}`} starred={false} onToggleStar={noop} />);
    expect(out).toContain(`href="/coin/${addr}"`);
    expect(out).toContain('aria-pressed="false"');
    expect(out).toContain('Add Name to watchlist');
    expect(out).toContain('143K USDL');
  });

  it('Sparkline computes direction and a spoken label', () => {
    expect(html(<Sparkline data={[1, 2]} label="24h price" />)).toContain('aria-label="24h price, up 100.0 percent"');
    expect(html(<Sparkline data={[1]} label="24h price" />)).toContain('not enough data');
    expect(html(<Sparkline data={[1, 2]} />)).not.toContain('linearGradient');
  });

  it('FormField wires label, helper and error ids', () => {
    const out = html(<FormField id="name" label="Name" error="Required" inputProps={{ defaultValue: '' }} />);
    expect(out).toContain('for="name"');
    expect(out).toContain('aria-describedby="name-error"');
    expect(out).toContain('aria-invalid="true"');
    expect(out).toContain('role="alert"');
  });

  it('CountdownProgress formats remaining time and the ended state', () => {
    expect(html(<CountdownProgress endsAt="2026-10-26T10:00:00Z" now={now} />)).toContain('13d 22h left');
    expect(html(<CountdownProgress endsAt="2026-10-01T12:00:00Z" now={now} />)).toContain('Ended');
  });
});

describe('interactive behaviour (jsdom)', () => {
  it('ModalFrame portals a dialog, closes on Escape and restores focus', () => {
    const opener = document.createElement('button');
    document.body.appendChild(opener);
    opener.focus();
    let closed = 0;
    const { root, container } = mount(
      <ModalFrame open onClose={() => { closed += 1; }} title="Sign in" subtitle="Subtitle">
        <button type="button">Inside</button>
      </ModalFrame>,
    );
    const dialog = document.body.querySelector('[role="dialog"]');
    expect(dialog).not.toBeNull();
    expect(dialog?.getAttribute('aria-modal')).toBe('true');
    expect(document.body.style.overflow).toBe('hidden');
    act(() => {
      dialog?.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }));
    });
    expect(closed).toBe(1);
    act(() => root.render(<ModalFrame open={false} onClose={noop} title="Sign in">x</ModalFrame>));
    expect(document.body.querySelector('[role="dialog"]')).toBeNull();
    expect(document.activeElement).toBe(opener);
    unmount(root, container);
    opener.remove();
  });

  it('DropdownMenu opens a menu with items and closes on Escape', () => {
    let picked = '';
    const { root, container } = mount(
      <DropdownMenu label="More actions" items={[{ id: 'a', label: 'Alpha', onSelect: () => { picked = 'a'; } }, { id: 'b', label: 'Beta', destructive: true }]} />,
    );
    const trigger = container.querySelector('button[aria-haspopup="menu"]') as HTMLButtonElement;
    expect(trigger.getAttribute('aria-expanded')).toBe('false');
    act(() => trigger.click());
    const menu = document.body.querySelector('[role="menu"]');
    expect(menu).not.toBeNull();
    expect(menu?.querySelectorAll('[role="menuitem"]').length).toBe(2);
    act(() => (menu?.querySelector('[role="menuitem"]') as HTMLButtonElement).click());
    expect(picked).toBe('a');
    expect(document.body.querySelector('[role="menu"]')).toBeNull();
    act(() => trigger.click());
    act(() => {
      document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }));
    });
    expect(document.body.querySelector('[role="menu"]')).toBeNull();
    unmount(root, container);
  });

  it('SelectPill opens a listbox and selects an option', () => {
    let value = 'a';
    const { root, container } = mount(
      <SelectPill label="Sort" options={[{ value: 'a', label: 'Alpha' }, { value: 'b', label: 'Beta' }]} value={value} onChange={v => { value = v; }} />,
    );
    const trigger = container.querySelector('button[aria-haspopup="listbox"]') as HTMLButtonElement;
    act(() => trigger.click());
    const options = document.body.querySelectorAll('[role="option"]');
    expect(options.length).toBe(2);
    act(() => (options[1] as HTMLLIElement).click());
    expect(value).toBe('b');
    expect(document.body.querySelector('[role="listbox"]')).toBeNull();
    unmount(root, container);
  });

  it('Tooltip appears on focus and links via aria-describedby', () => {
    const { root, container } = mount(<Tooltip content="Explore"><button type="button">Trigger</button></Tooltip>);
    const trigger = container.querySelector('button') as HTMLButtonElement;
    act(() => { trigger.focus(); trigger.dispatchEvent(new FocusEvent('focusin', { bubbles: true })); });
    const tip = document.body.querySelector('[role="tooltip"]');
    expect(tip).not.toBeNull();
    expect(trigger.getAttribute('aria-describedby')).toBe(tip?.id);
    act(() => { trigger.dispatchEvent(new FocusEvent('focusout', { bubbles: true })); });
    expect(document.body.querySelector('[role="tooltip"]')).toBeNull();
    unmount(root, container);
  });

  it('Toggle flips through onChange and DisclosureRow reveals its panel', () => {
    let checked = false;
    const { root, container } = mount(<Toggle checked={checked} onChange={v => { checked = v; }} label="Hide balances" />);
    act(() => (container.querySelector('[role="switch"]') as HTMLButtonElement).click());
    expect(checked).toBe(true);
    unmount(root, container);

    let open = false;
    const d = mount(<DisclosureRow id="opt" label="Options" open={open} onToggle={v => { open = v; }}>Hidden content</DisclosureRow>);
    expect(d.container.querySelector('#opt-panel')?.hasAttribute('hidden')).toBe(true);
    act(() => (d.container.querySelector('#opt') as HTMLButtonElement).click());
    expect(open).toBe(true);
    unmount(d.root, d.container);
  });

  it('ShortAddress copies without navigating the parent', async () => {
    let written = '';
    let parentClicks = 0;
    Object.defineProperty(navigator, 'clipboard', { value: { writeText: async (text: string) => { written = text; } }, configurable: true });
    const { root, container } = mount(<div onClick={() => { parentClicks += 1; }}><ShortAddress address={addr} /></div>);
    const button = container.querySelector('button') as HTMLButtonElement;
    await act(async () => { button.click(); });
    expect(written).toBe(addr);
    expect(parentClicks).toBe(0);
    unmount(root, container);
  });
});

describe('shared states: plural titles, raw-error suppression, panel and chip variants', () => {
  const strip = (out: string) => out.replace(/title="[^"]*"|data-operation="[^"]*"|data-error-detail="[^"]*"/g, '');

  it('the panel variant renders a full frame with an agreeing title, plain body and retry', () => {
    let retried = 0;
    const view = mount(<UnavailableState operation="IF_LIVE" status={503} title="Live streams" plural="auto" message="Failed to fetch" onRetry={() => { retried += 1; }} />);
    const state = view.container.querySelector('[role="status"]') as HTMLElement;
    expect(state.querySelector('h3')?.textContent).toBe('Live streams are not available yet');
    expect(state.querySelector('svg')).not.toBeNull();
    expect(strip(state.outerHTML)).not.toMatch(/Failed to fetch|IF_LIVE|503/);
    expect(state.textContent).toContain('Connection problem');
    const button = Array.from(state.querySelectorAll('button')).find(b => b.textContent === 'Try again') as HTMLButtonElement;
    act(() => { button.click(); });
    expect(retried).toBe(1);
    expect(state.className).not.toMatch(BORDER);
    unmount(view.root, view.container);
  });

  it('the chip variant renders a compact pill with the agreeing title and keeps the frame to the caller', () => {
    let retried = 0;
    const view = mount(<div data-frame="rail"><UnavailableState variant="chip" operation="IF_STATS" status={null} title="Buy and sell counts for 24h" plural="auto" onRetry={() => { retried += 1; }} /></div>);
    const chip = view.container.querySelector('[data-state="unavailable"]') as HTMLElement;
    expect(chip.tagName).toBe('SPAN');
    expect(chip.getAttribute('role')).toBe('status');
    expect(chip.className).toMatch(/\brounded-pill\b/);
    expect(chip.className).toMatch(/\bh-6\b/);
    expect(chip.className).toMatch(/\bbg-surface-2\b/);
    expect(chip.className).not.toMatch(BORDER);
    expect(chip.querySelector('h3')).toBeNull();
    expect(chip.textContent).toBe('Buy and sell counts for 24h are not available yetTry again');
    expect(chip.getAttribute('data-operation')).toBeNull();
    act(() => { (chip.querySelector('button') as HTMLButtonElement).click(); });
    expect(retried).toBe(1);
    expect((view.container.firstChild as HTMLElement).getAttribute('data-frame')).toBe('rail');
    unmount(view.root, view.container);
    expect(html(<UnavailableState variant="chip" operation="IF_X" status={null} title="Buyers versus sellers" plural="auto" />)).toContain('Buyers versus sellers are not available yet');
    expect(html(<UnavailableState variant="chip" operation="IF_X" status={null} title="This coin" plural="auto" />)).toContain('This coin is not available yet');
    // Default stays "is" for every variant, as before.
    expect(html(<UnavailableState variant="chip" operation="IF_X" status={null} title="Leaderboards" />)).toContain('Leaderboards is not available yet');
  });

  it('the chip variant drops developer detail in production', () => {
    vi.stubEnv('NODE_ENV', 'production');
    try {
      const out = html(<UnavailableState variant="chip" operation="IF_POOL_STATS" status={404} title="Holder shares" plural />);
      expect(out).toContain('Holder shares are not available yet');
      expect(out).not.toMatch(/IF_POOL_STATS|404|data-operation/);
      const err = html(<ErrorState variant="chip" error={Object.assign(new TypeError('Failed to fetch'), { code: 'NETWORK_ERROR' })} />);
      expect(err).not.toMatch(/Failed to fetch|NETWORK_ERROR|data-error-detail/);
    } finally { vi.unstubAllEnvs(); }
  });

  it('ErrorState chip and StateNotice never surface raw transport text', () => {
    const fetchError = new TypeError('Failed to fetch');
    let retried = 0;
    const view = mount(<ErrorState variant="chip" error={fetchError} onRetry={() => { retried += 1; }} />);
    const chip = view.container.querySelector('[data-state="error"]') as HTMLElement;
    expect(chip.getAttribute('role')).toBe('alert');
    expect(chip.textContent).toBe("Connection problem, check your internet and try againTry again");
    act(() => { (chip.querySelector('button') as HTMLButtonElement).click(); });
    expect(retried).toBe(1);
    unmount(view.root, view.container);
    for (const node of [
      <ErrorState key="a" variant="chip" message="Failed to fetch" />,
      <ErrorState key="b" variant="chip" message="HTTP 503: NETWORK_INTERRUPTED" />,
      <StateNotice key="c" kind="error" error={fetchError} />,
      <StateNotice key="d" kind="error">Trades did not load: Failed to fetch</StateNotice>,
      <StateNotice key="e" kind="error">{'Trades did not load: '}{'fetch failed'}</StateNotice>,
      <StateNotice key="f" kind="unavailable">TypeError: Failed to fetch</StateNotice>,
    ]) {
      const out = strip(html(node));
      expect(out).not.toMatch(/Failed to fetch|fetch failed|NETWORK_INTERRUPTED|TypeError|HTTP 503/i);
      expect(out).toContain('Connection problem');
    }
  });

  it('StateNotice builds plural-aware unavailable copy, empty and loading pills, and sits one surface up', () => {
    const view = mount(<StateNotice kind="unavailable" title="Live streams" plural="auto" detail="IF_LIVE (status 404)" surface={2} />);
    const chip = view.container.querySelector('[data-state]') as HTMLElement;
    expect(chip.textContent).toBe('Live streams are not available yet');
    expect(chip.className).toMatch(/\bbg-surface-3\b/);
    expect(chip.getAttribute('data-operation')).toBeNull();
    expect(chip.querySelector('button')).toBeNull();
    unmount(view.root, view.container);
    const empty = mount(<StateNotice kind="empty">No replies yet</StateNotice>);
    expect(empty.container.querySelector('[data-state="empty"]')?.textContent).toBe('No replies yet');
    unmount(empty.root, empty.container);
    const loading = mount(<StateNotice kind="loading" />);
    const busy = loading.container.querySelector('[data-state="loading"]') as HTMLElement;
    expect(busy.getAttribute('aria-busy')).toBe('true');
    expect(busy.textContent).toBe('Loading...');
    unmount(loading.root, loading.container);
    // Sentences written for people pass through; elements are left as given.
    expect(html(<StateNotice kind="error">Sign in to see your trades.</StateNotice>)).toContain('Sign in to see your trades.');
    expect(html(<StateNotice kind="unavailable"><b>Charts</b> are paused</StateNotice>)).toContain('<b>Charts</b> are paused');
    for (const out of [html(<StateNotice />), html(<StateNotice kind="error" onRetry={noop} />), html(<StateNotice kind="loading" surface={3} />)]) {
      expect(out).not.toMatch(BORDER);
      expect(out).not.toMatch(/shadow/);
    }
  });
});

describe('Avatar fallback for address names', () => {
  const full = '0xAbCdEf0123456789aBcDeF0123456789AbCd1f2e';
  const other = '0x1234567890abcdef1234567890abcdef12345678';

  it('never derives "0X" initials from an address, full or truncated', () => {
    for (const name of [full, full.toLowerCase(), '0x1234…5678', '$0xdeadbeef', '0X12ab']) {
      const view = mount(<Avatar src={null} name={name} />);
      const mark = view.container.querySelector('[role="img"]') as HTMLElement;
      expect(mark.textContent).not.toMatch(/^0x/i);
      expect(mark.textContent).toMatch(/^[0-9a-f]{1,4}$/);
      expect(mark.getAttribute('aria-label')).toBe(name);
      expect(mark.className).toMatch(/\bfont-mono\b/);
      expect(mark.className).not.toMatch(BORDER);
      unmount(view.root, view.container);
      expect(initialsOf(name)).not.toMatch(/0x/i);
    }
    expect(html(<Avatar src={null} name={full} />)).toContain('>1f2e<');
    expect(html(<Avatar src={null} name={full} size="xs" decorative />)).toContain('>2e<');
    expect(html(<Avatar src={null} name="0x1234…5678" />)).toContain('>5678<');
    expect(initialsOf(full)).toBe('2e');
  });

  it('gives the same address the same mark and hue, and different addresses different marks', () => {
    const style = (name: string) => {
      const view = mount(<Avatar src={null} name={name} size="lg" />);
      const mark = view.container.querySelector('[data-address-mark]') as HTMLElement;
      const out = { text: mark.textContent, bg: mark.style.backgroundColor };
      unmount(view.root, view.container);
      return out;
    };
    const a = style(full);
    expect(style(full)).toEqual(a);
    expect(style(full.toLowerCase())).toEqual(a);
    expect(a.bg).not.toBe('');
    const b = style(other);
    expect(b.text).toBe('5678');
    expect(addressMarkOf(full).hue).not.toBe(addressMarkOf(other).hue);
    expect(addressMarkOf(full)).toEqual(addressMarkOf(full.toUpperCase().replace('0X', '0x')));
  });

  it('normal names still give letter initials, and images still win over the mark', () => {
    expect(isAddressName('Ada Lovelace')).toBe(false);
    expect(isAddressName('Oxford coin')).toBe(false);
    expect(isAddressName(full)).toBe(true);
    const view = mount(<Avatar src={null} name="Ada Lovelace" />);
    expect(view.container.querySelector('[role="img"]')?.textContent).toBe('AL');
    expect(view.container.querySelector('[data-address-mark]')).toBeNull();
    unmount(view.root, view.container);
    expect(html(<Avatar src={null} name="$PEPE" />)).toContain('>PE<');
    expect(initialsOf('Oxford coin')).toBe('OC');
    expect(html(<Avatar src="/icons/token-fallback.svg" name={full} />)).toContain('<img');
    expect(html(<AvatarStack items={[{ src: null, name: full }, { src: null, name: 'Bob' }]} label="2 holders" />)).not.toMatch(/>0X</);
  });
});

describe('UnavailableState surface and heading options, NoticeBar flush and data attributes', () => {
  it('chip passes surface through to the pill', () => {
    const on = (surface: 1 | 2 | 3) => {
      const view = mount(<UnavailableState variant="chip" surface={surface} operation="IF_LIVE" status={null} title="Live streams" plural="auto" />);
      const cls = (view.container.querySelector('[data-state="unavailable"]') as HTMLElement).className;
      unmount(view.root, view.container);
      return cls;
    };
    expect(on(1)).toMatch(/\bbg-surface-2\b/);
    expect(on(2)).toMatch(/\bbg-surface-3\b/);
    expect(on(2)).not.toMatch(/\bbg-surface-2\b/);
    expect(on(3)).toMatch(/\bbg-surface-hover\b/);
  });

  it('panel heading level is h3 by default and configurable', () => {
    const level = (headingLevel?: 2 | 3 | 4) => {
      const view = mount(<UnavailableState operation="IF_X" status={null} title="Live streams" plural="auto" headingLevel={headingLevel} />);
      const heading = view.container.querySelector('h2, h3, h4') as HTMLElement;
      const out = [heading.tagName, heading.textContent];
      unmount(view.root, view.container);
      return out;
    };
    expect(level()).toEqual(['H3', 'Live streams are not available yet']);
    expect(level(2)).toEqual(['H2', 'Live streams are not available yet']);
    expect(level(4)[0]).toBe('H4');
  });

  it('panel retry button takes the next surface layer up', () => {
    const retry = (surface?: 1 | 2 | 3) => {
      const view = mount(<UnavailableState operation="IF_X" status={null} title="Feed" onRetry={noop} surface={surface} />);
      const button = Array.from(view.container.querySelectorAll('button')).find(b => b.textContent === 'Try again') as HTMLButtonElement;
      const cls = button.className;
      unmount(view.root, view.container);
      return cls;
    };
    expect(retry()).toMatch(/\bbg-surface-2\b/);
    expect(retry(2)).toMatch(/\bbg-surface-3\b/);
    expect(retry(2)).not.toMatch(/\bbg-surface-2\b/);
    expect(retry(3)).toMatch(/\bbg-surface-hover\b/);
    expect(retry(2)).not.toMatch(BORDER);
  });

  it('NoticeBar forwards data-* attributes and the flush variant drops rounding without a wrapper', () => {
    const view = mount(<NoticeBar tone="warning" flush data-testid="profile-notice" data-section="wallets">Linked wallets did not load: Failed to fetch</NoticeBar>);
    const root = view.container.firstElementChild as HTMLElement;
    expect(root.getAttribute('data-testid')).toBe('profile-notice');
    expect(root.getAttribute('data-section')).toBe('wallets');
    expect(root.className).toMatch(/\brounded-none\b/);
    expect(root.className).toMatch(/\bw-full\b/);
    expect(root.className).not.toMatch(/\brounded-md\b/);
    expect(root.className).toMatch(/\bpx-4\b/);
    expect(root.className).not.toMatch(BORDER);
    expect(root.textContent).not.toMatch(/Failed to fetch/);
    unmount(view.root, view.container);
    const plain = html(<NoticeBar tone="info">Heads up</NoticeBar>);
    expect(plain).toContain('rounded-md');
    expect(plain).not.toMatch(/rounded-none|data-/);
  });
});

describe('StateNotice wrap', () => {
  it('wraps the chip text to two lines with the action on a wrapping row; default stays one truncated line', () => {
    const view = mount(<ErrorState variant="chip" wrap error={new TypeError('Failed to fetch')} onRetry={noop} />);
    const chip = view.container.querySelector('[data-state="error"]') as HTMLElement;
    const text = chip.querySelector('span:not([data-state])') as HTMLElement;
    expect(chip.className).toMatch(/\bmin-h-6\b/);
    expect(chip.className).toMatch(/\bflex-wrap\b/);
    expect(chip.className).not.toMatch(/(^|\s)h-6(\s|$)/);
    expect(text.className).toMatch(/\bline-clamp-2\b/);
    expect(text.className).not.toMatch(/\btruncate\b/);
    expect(text.textContent).toBe("Connection problem, check your internet and try again");
    expect(chip.querySelector('button')?.textContent).toBe('Try again');
    expect(chip.className).not.toMatch(BORDER);
    unmount(view.root, view.container);
    expect(html(<UnavailableState variant="chip" wrap surface={2} operation="IF_X" status={null} title="Top traders" plural="auto" />)).toMatch(/line-clamp-2[^"]*">Top traders are not available yet/);
    const plain = mount(<StateNotice kind="error" error={new TypeError('Failed to fetch')} />);
    const chip1 = plain.container.querySelector('[data-state]') as HTMLElement;
    expect(chip1.className).toMatch(/\bh-6\b/);
    expect(chip1.className).toMatch(/\brounded-pill\b/);
    expect(chip1.querySelector('span:not([data-state])')?.className).toMatch(/\btruncate\b/);
    unmount(plain.root, plain.container);
  });
});
