import { describe, it, expect } from 'vitest';
import { mergeProfileWallets, parseWalletLink, type WalletLink } from './wallet-links-transport';

const route = '0xABCDEFabcdefABCDEFabcdefABCDEFabcdefABCD';
const link = (wallet: string, state: WalletLink['state'] = 'verified', chainId = '125'): WalletLink => ({ linkId: `link-${wallet.slice(-4)}`, accountId: 'acct', chainId, wallet: wallet.toLowerCase(), visibility: 'private', state, linkedAt: '2026-10-12T00:00:00.000Z' });

describe('wallet links (media/gateway identity_models.go WalletLink)', () => {
  it('parses the gateway row shape and lowercases the wallet', () => {
    const parsed = parseWalletLink({ linkId: 'l1', accountId: 'a1', chainId: '125', wallet: route, visibility: 'public_optin', state: 'verified', linkedAt: null });
    expect(parsed.wallet).toBe(route.toLowerCase());
    expect(parsed.visibility).toBe('public_optin');
    expect(parsed.linkedAt).toBeNull();
  });
  it('rejects rows that disagree with the struct', () => {
    expect(() => parseWalletLink(null)).toThrow(TypeError);
    expect(() => parseWalletLink({ linkId: 'l1', accountId: 'a1', chainId: '125', wallet: route, visibility: 'everyone', state: 'verified', linkedAt: null })).toThrow(TypeError);
    expect(() => parseWalletLink({ linkId: 'l1', accountId: 'a1', chainId: '125', wallet: route, visibility: 'private', state: 'active', linkedAt: null })).toThrow(TypeError);
    expect(() => parseWalletLink({ linkId: 'l1', accountId: 'a1', chainId: '125', wallet: '0x123', visibility: 'private', state: 'verified', linkedAt: null })).toThrow();
  });
  it('lists the route wallet first and dedupes verified chain-125 links', () => {
    const other = '0x1111111111111111111111111111111111111111';
    const third = '0x2222222222222222222222222222222222222222';
    expect(mergeProfileWallets(route, [])).toEqual([route.toLowerCase()]);
    expect(mergeProfileWallets(route, [link(route), link(other), link(other), link(third, 'verified', '1')])).toEqual([route.toLowerCase(), other]);
  });
});
