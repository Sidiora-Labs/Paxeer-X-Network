// @vitest-environment jsdom
import React from 'react';
import { describe, expect, it } from 'vitest';
import { renderToStaticMarkup } from 'react-dom/server';
import { webcrypto } from 'node:crypto';
import { verifyCost } from './creation-domain';
import { creationFunding, type FundingRead } from './creation-adapters';
import { validateCreationDraft, type CreationDraftV2 } from './creation-storage';
import { MEDIA_MAX_BYTES, MEDIA_RULES, checkMediaRules, dimensionWarnings, sha256Hex, validateStagedMediaDescriptor } from './media-validation';
import { strategyFromQuery } from './use-create-prefill';
import { linkProblem, linksFromSlots, slotsFromLinks } from './social-links-disclosure';
import { STRATEGY_MUTABILITY_NOTE, StrategyPicker, feeStrategies } from './strategy-picker';
import { MediaRuleLegend } from './media-rule-legend';
import { CoinPreviewCard } from './coin-preview-card';
import { FeeSummary, fundingCopy } from './fee-summary';
import { LaunchProgress } from './launch-progress';
import { CreatePageFallback } from '@/page-modules/create';

const html = (node: React.ReactElement) => renderToStaticMarkup(node);
const BORDER = /\b(border(?!-0\b|-none\b)(-[a-z0-9/\[\]-]+)?|divide-[a-z]|ring-(?!offset)[a-z0-9]|outline-(?!none|focus|2|offset)[a-z]+)\b/;
const creator = '0x1234567890abcdef1234567890abcdef12345678';
const noop = () => {};

function draft(overrides: Partial<CreationDraftV2> = {}): CreationDraftV2 {
  return { draftId: 'd-1', version: 2, creator, chainId: 125, name: 'Paxe', symbol: 'PAXE', description: '', tags: [], socialLinks: [], uploadRefs: [], firstBuyRaw: '0', opticalSelection: 'standard', updatedAt: Date.now(), feeStrategy: 0, ...overrides };
}

describe('media rules', () => {
  it('rejects a 7 MiB PNG with the legend rule text and accepts a small WebP', () => {
    const big = checkMediaRules({ size: 7 * 1024 * 1024, type: 'image/png' });
    expect(big.ok).toBe(false);
    if (!big.ok) expect(big.reason).toContain(MEDIA_RULES.size);
    expect(checkMediaRules({ size: MEDIA_MAX_BYTES, type: 'image/webp' })).toEqual({ ok: true });
    const video = checkMediaRules({ size: 10, type: 'video/mp4' });
    expect(video.ok).toBe(false);
    if (!video.ok) expect(video.reason).toContain(MEDIA_RULES.video);
    expect(checkMediaRules({ size: 0, type: 'image/jpeg' }).ok).toBe(false);
  });

  it('warns, without rejecting, for a 900x900 image and for non-square images', () => {
    expect(dimensionWarnings({ width: 1000, height: 1000 })).toEqual([]);
    const soft = dimensionWarnings({ width: 900, height: 900 });
    expect(soft).toHaveLength(1);
    expect(soft[0]).toContain('1000 x 1000');
    expect(dimensionWarnings({ width: 1200, height: 800 })).toHaveLength(2);
    expect(dimensionWarnings(null)).toHaveLength(1);
  });

  it('fingerprints the real file bytes with SHA-256', async () => {
    const bytes = new Uint8Array([137, 80, 78, 71, 13, 10, 26, 10, 1, 2, 3]);
    const file = new File([bytes], 'coin.png', { type: 'image/png' });
    const expected = '0x' + Array.from(new Uint8Array(await webcrypto.subtle.digest('SHA-256', bytes)), b => b.toString(16).padStart(2, '0')).join('');
    expect(await sha256Hex(file)).toBe(expected);
  });

  it('validates staged descriptors strictly inside the draft validator', () => {
    const sha256 = '0x' + 'ab'.repeat(32);
    const good = { name: 'coin.png', size: 1024, type: 'image/png' as const, sha256, width: 1000, height: 1000 };
    expect(validateStagedMediaDescriptor(good)).toBeNull();
    expect(validateCreationDraft(draft({ stagedMedia: good }))).toEqual({});
    expect(validateCreationDraft(draft())).toEqual({});
    for (const bad of [
      { ...good, size: MEDIA_MAX_BYTES + 1 },
      { ...good, type: 'image/gif' },
      { ...good, sha256: sha256.toUpperCase() },
      { ...good, sha256: '0x12' },
      { ...good, width: 0 },
      { ...good, extra: true },
      'not-an-object',
    ]) {
      expect(validateStagedMediaDescriptor(bad)).not.toBeNull();
      expect(validateCreationDraft(draft({ stagedMedia: bad as unknown as CreationDraftV2['stagedMedia'] })).stagedMedia).toBeTruthy();
    }
  });
});

describe('query prefill and link slots', () => {
  it('maps ?strategy= aliases onto fee strategies', () => {
    expect(strategyFromQuery('airdrop')).toBe(2);
    expect(strategyFromQuery('Holders')).toBe(2);
    expect(strategyFromQuery('claim')).toBe(0);
    expect(strategyFromQuery('burn')).toBe(1);
    expect(strategyFromQuery('lp')).toBe(3);
    expect(strategyFromQuery('constructor')).toBeNull();
    expect(strategyFromQuery(null)).toBeNull();
  });

  it('round-trips social links through typed slots without losing extras', () => {
    const links = ['https://x.com/paxe', 'https://t.me/paxe', 'https://paxe.example', 'https://docs.paxe.example'];
    const slots = slotsFromLinks(links);
    expect(slots).toEqual({ x: links[0], telegram: links[1], website: links[2], others: [links[3]] });
    expect(linksFromSlots(slots)).toEqual(links);
    expect(linksFromSlots({ x: '', telegram: ' https://t.me/a ', website: '', others: [] })).toEqual(['https://t.me/a']);
    expect(linkProblem('')).toBeNull();
    expect(linkProblem('https://x.com/paxe')).toBeNull();
    expect(linkProblem('http://x.com/paxe')).toContain('https');
    expect(linkProblem('https://user:pw@x.com')).toContain('password');
    expect(linkProblem('x.com')).toContain('https');
  });
});

describe('create components render honestly without border utilities', () => {
  it('renders the strategy picker with the mutability note and all four strategies on reveal', () => {
    const twoUp = html(<StrategyPicker value={2} onChange={noop} />);
    expect(twoUp).toContain('Holders');
    expect(twoUp).toContain('Creator');
    expect(twoUp).toContain('aria-checked="true"');
    expect(twoUp).toContain(STRATEGY_MUTABILITY_NOTE.replace(/'/g, '&#x27;'));
    expect(twoUp).not.toContain('LP rewards');
    const fourUp = html(<StrategyPicker value={3} onChange={noop} />);
    expect(fourUp).toContain('LP rewards');
    expect(fourUp).toContain('Burn');
    expect(feeStrategies.map(s => s.value).sort()).toEqual([0, 1, 2, 3]);
    expect(twoUp).not.toMatch(BORDER);
    expect(fourUp).not.toMatch(BORDER);
  });

  it('renders the legend with our rules, not the original 15 MB / 30 MB limits', () => {
    const legend = html(<MediaRuleLegend />);
    expect(legend).toContain('PNG, JPEG or WebP');
    expect(legend).toContain('6 MB');
    expect(legend).not.toContain('15 MB');
    expect(legend).not.toMatch(BORDER);
  });

  it('renders the preview as an empty sentence and then as a coin card', () => {
    const empty = html(<CoinPreviewCard name="" symbol="" description="" image={null} strategy={0} socialLinks={[]} />);
    expect(empty).toContain('preview of how your coin will look');
    const filled = html(<CoinPreviewCard name="Paxe" symbol="PAXE" description="hello" image={null} strategy={2} socialLinks={['https://x.com/paxe']} />);
    expect(filled).toContain('Paxe');
    expect(filled).toContain('$PAXE');
    expect(filled).toContain('Holders');
    expect(filled).toContain('x.com');
    expect(filled).toContain('no market cap yet');
    expect(filled).not.toMatch(BORDER);
  });

  it('shows the protocol fee and a network-derived gas line only after a funding read', () => {
    const before = html(<FeeSummary read={null} plan={null} firstBuyRaw="0" />);
    expect(before).toContain('1.00 USDL');
    expect(before).toContain('shown before you confirm');
    const deployment = { revision: 'r1', chainId: 125, usdl: '0x' + '1'.repeat(40), router: '0x' + '2'.repeat(40), factory: '0x' + '3'.repeat(40) };
    const cost = verifyCost(deployment, { revision: 'r1', decimals: 6, feeRaw: '1000000', gasRaw: '2500000000000000' }, '5000000');
    const read: FundingRead = { cost, balances: { usdl: '12000000', pax: '1000000000000000000', allowance: '0' }, blockHeight: '1', blockHash: '0x' + 'a'.repeat(64), gasStage: 'approval', observedAt: Date.now() };
    const plan = creationFunding(read);
    expect(plan.state).toBe('approval_required');
    const after = html(<FeeSummary read={read} plan={plan} firstBuyRaw="5000000" />);
    expect(after).toContain('0.0025 PAX');
    expect(after).toContain('Network fee (approval)');
    expect(after).toContain('5.00 USDL');
    expect(after).toContain('12.00 USDL');
    expect(after).toContain(fundingCopy.approval_required.replace(/'/g, '&#x27;').replace(/;/g, ';'));
    expect(after).not.toMatch(BORDER);
    const short = creationFunding({ ...read, balances: { ...read.balances, usdl: '1000000' } });
    expect(short.state).toBe('insufficient_usdl');
  });

  it('renders the stepper with aria-current on the active step and bodies only when open', () => {
    const markup = html(
      <LaunchProgress
        busy
        message="Confirm in your wallet"
        steps={[
          { id: 'approve', label: 'Approve 1.00 USDL', status: 'done', detail: 'Allowance in place' },
          { id: 'create', label: 'Create pool', status: 'active', body: <span>recovery controls</span> },
          { id: 'image', label: 'Upload image', status: 'pending', body: <span>hidden body</span> },
          { id: 'metadata', label: 'Publish metadata', status: 'pending' },
          { id: 'first-buy', label: 'First purchase', status: 'skipped', detail: 'Not requested' },
        ]}
      />,
    );
    expect(markup).toContain('aria-current="step"');
    expect(markup).toContain('recovery controls');
    expect(markup).not.toContain('hidden body');
    expect(markup).toContain('Confirm in your wallet');
    expect(markup).not.toMatch(BORDER);
  });
});

describe('create page loading frame', () => {
  it('renders the full form layout, not a lone heading, while the wizard hydrates', () => {
    const markup = html(<CreatePageFallback />);
    const doc = new DOMParser().parseFromString(markup, 'text/html');
    const h1 = doc.querySelector('h1');
    expect(h1?.textContent).toBe('Create new coin');
    expect(h1?.className).toContain('text-md');
    expect(h1?.className).not.toMatch(/text-(2xl|3xl|4xl)|font-extrabold/);
    expect(markup).toContain('sm:grid-cols-2');
    expect(markup).toContain('Coin name');
    expect(markup).toContain('Ticker');
    expect(markup).toContain('Preview');
    expect(markup).toContain('lg:min-h-72');
    expect(doc.querySelector('[data-create-fallback]')?.getAttribute('aria-busy')).toBe('true');
    expect(markup).not.toMatch(BORDER);
  });
});
