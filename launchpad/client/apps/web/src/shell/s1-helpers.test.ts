import { describe, expect, it } from 'vitest';
import { addressFromText, coinPath } from '@/shell/paste-address';
import { platformFooterItems } from '@/shell/platform-metrics';
import { upcomingPlatformFields, type PlatformData } from '@/core/market/platform-metrics';
import { percentChip, usdlChip, isTickerMode, tickerModes } from '@/shell/use-ticker';
import { navItems, createItem } from '@/shell/nav-config';
import { routeForPath } from '@/core/app-foundation/navigation';
import { v2Reads } from '@/core/test-fixtures/market-production/v2-reads';

describe('paste-address helpers', () => {
  it('extracts and lowercases a 0x address from bare text, explorer and coin URLs', () => {
    const addr = '0xAbCdEf0123456789abcdef0123456789ABCDEF01';
    expect(addressFromText(addr)).toBe(addr.toLowerCase());
    expect(addressFromText(`https://paxscan.paxeer.app/address/${addr}`)).toBe(addr.toLowerCase());
    expect(addressFromText(`see /coin/${addr} now`)).toBe(addr.toLowerCase());
    expect(addressFromText('nothing here 0x1234')).toBeNull();
    expect(addressFromText('')).toBeNull();
  });
  it('builds a coin path the route allowlist accepts', () => {
    const path = coinPath('0xAbCdEf0123456789abcdef0123456789ABCDEF01');
    expect(path).toBe('/coin/0xabcdef0123456789abcdef0123456789abcdef01');
    expect(routeForPath(path)?.id).toBe('coin');
    expect(() => coinPath('0x1234')).toThrow();
  });
});

describe('platform footer figures (IF_PLATFORM)', () => {
  const today = { window: '24h' as const, volumeUsdlRaw: '147278912', pools: '2', activeWallets: '1', generatedAt: '2026-10-13T07:12:36.000Z' };
  it('maps the served volume, traders and pools, and leaves market cap, trades and new today unavailable', () => {
    const items = platformFooterItems(today);
    expect(items.map(item => item.label)).toEqual(['24h volume', 'Market cap', '24h trades', '24h traders', 'Coins launched', 'New today']);
    expect(Object.fromEntries(items.map(item => [item.label, item.value]))).toEqual({ '24h volume': '147.28 USDL', 'Market cap': null, '24h trades': null, '24h traders': '1', 'Coins launched': '2', 'New today': null });
    expect(items.filter(item => item.value === null).map(item => item.operation)).toEqual(['IF_PLATFORM (totalMarketCapUsdlRaw)', 'IF_PLATFORM (trades)', 'IF_PLATFORM (newPools)']);
  });
  it('renders the upcoming fields once the envelope serves them, and a served null as unavailable', () => {
    const items = platformFooterItems({ ...today, totalMarketCapUsdlRaw: '10098226981', trades: '42', newPools: null });
    expect(Object.fromEntries(items.map(item => [item.label, item.value]))).toMatchObject({ 'Market cap': '10.1K USDL', '24h trades': '42', 'New today': null });
    expect(items.every(item => item.value !== null || item.operation !== null)).toBe(true);
  });
  it('maps the recorded 2.2.0 IF_PLATFORM bytes onto all six figures', () => {
    const body = v2Reads.responses.platform24h.body as { data: PlatformData; generatedAt: string };
    const items = platformFooterItems({ window: '24h', volumeUsdlRaw: body.data.volumeUsdlRaw, pools: body.data.pools, activeWallets: body.data.activeWallets, ...upcomingPlatformFields(body.data), generatedAt: body.generatedAt });
    expect(Object.fromEntries(items.map(item => [item.label, item.value]))).toEqual({ '24h volume': '147.28 USDL', 'Market cap': '10.1K USDL', '24h trades': '2', '24h traders': '1', 'Coins launched': '2', 'New today': '1' });
  });
  it('reads only well-formed upcoming fields from the envelope and keeps absent ones absent', () => {
    const data = { volumeUsdlRaw: '1', pools: '2', activeWallets: '3' } as PlatformData;
    expect(upcomingPlatformFields(data)).toEqual({});
    expect(upcomingPlatformFields({ ...data, trades: '9', newPools: null } as unknown as PlatformData)).toEqual({ trades: '9', newPools: null });
    expect(() => upcomingPlatformFields({ ...data, totalMarketCapUsdlRaw: '1e6' } as PlatformData)).toThrow(TypeError);
    expect(() => upcomingPlatformFields({ ...data, trades: 12 } as unknown as PlatformData)).toThrow(TypeError);
  });
});

describe('ticker chips', () => {
  it('maps percent changes to tones', () => {
    expect(percentChip(null)).toEqual({ value: null, tone: 'neutral' });
    expect(percentChip(12.345)).toEqual({ value: '+12.35%', tone: 'up' });
    expect(percentChip(-3)).toEqual({ value: '-3.00%', tone: 'down' });
    expect(percentChip(0)).toEqual({ value: '+0.00%', tone: 'flat' });
  });
  it('formats raw signed USDL and refuses non-integers', () => {
    expect(usdlChip('1234567890')).toEqual({ value: '+1.23K USDL', tone: 'up' });
    expect(usdlChip('-5000000')).toEqual({ value: '-5.00 USDL', tone: 'down' });
    expect(usdlChip('0')).toEqual({ value: '0.00 USDL', tone: 'flat' });
    expect(usdlChip(null)).toEqual({ value: null, tone: 'neutral' });
    expect(usdlChip('12.5')).toEqual({ value: null, tone: 'neutral' });
  });
  it('declares five distinct modes whose pages are routable', () => {
    expect(tickerModes.map(mode => mode.id)).toEqual(['watchlist', 'earners', 'holdings', 'following', 'recent']);
    for (const mode of tickerModes) expect(routeForPath(mode.href)?.id).toBeDefined();
    expect(isTickerMode('watchlist')).toBe(true);
    expect(isTickerMode('bogus')).toBe(false);
  });
});

describe('nav config feeding the rail and bottom nav', () => {
  it('has every destination registered in the route allowlist', () => {
    for (const item of [...navItems, createItem]) expect(routeForPath(item.href), item.href).toBeDefined();
    for (const id of ['home', 'explore', 'live', 'profile']) expect(navItems.some(item => item.id === id), id).toBe(true);
  });
});
