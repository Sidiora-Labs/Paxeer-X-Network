import { describe, expect, it } from 'vitest';
import { orderWalletChoices, readRecentConnectorId } from './wallet-choices';

const metamask = { id: 'io.metamask', name: 'MetaMask', type: 'injected' };
const rabby = { id: 'io.rabby', name: 'Rabby Wallet', type: 'injected' };
const generic = { id: 'injected', name: 'Injected', type: 'injected' };
const walletConnect = { id: 'walletConnect', name: 'WalletConnect', type: 'walletConnect' };

describe('orderWalletChoices', () => {
  it('marks EIP-6963 providers as detected and hides the generic injected connector when a named one exists', () => {
    const rows = orderWalletChoices([generic, walletConnect, metamask, rabby], null, true);
    expect(rows.map(row => row.connector.id)).toEqual(['io.metamask', 'io.rabby', 'walletConnect']);
    expect(rows.map(row => row.badge)).toEqual(['detected', 'detected', null]);
  });
  it('puts the last-used connector first with the recent badge', () => {
    const rows = orderWalletChoices([metamask, rabby, walletConnect], 'walletConnect', false);
    expect(rows[0]).toEqual({ connector: walletConnect, badge: 'recent' });
    expect(rows.slice(1).map(row => row.badge)).toEqual(['detected', 'detected']);
  });
  it('keeps the generic injected connector when nothing was discovered and reports detection from window.ethereum presence', () => {
    expect(orderWalletChoices([generic, walletConnect], null, false).map(row => row.badge)).toEqual([null, null]);
    expect(orderWalletChoices([walletConnect, generic], null, true).map(row => [row.connector.id, row.badge])).toEqual([['injected', 'detected'], ['walletConnect', null]]);
  });
  it('never returns an empty list for a non-empty connector set', () => {
    expect(orderWalletChoices([generic], null, false)).toHaveLength(1);
  });
});

describe('readRecentConnectorId', () => {
  it('returns null when browser storage is not available', () => {
    expect(readRecentConnectorId()).toBeNull();
  });
});
