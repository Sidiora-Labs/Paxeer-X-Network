import { recordWalletEvent } from './measurement';
import { getAddress, zeroAddress, type Address, type EIP1193Provider } from 'viem';

export interface WalletSession {
  connectorId: string;
  providerKind: 'injected' | 'walletConnect';
  chainId: number;
  account: Address;
  sessionEpoch: number;
  capabilities: readonly ['signMessage', 'sendTransaction'];
  connectedAt: number;
}
export interface AccountScope { account: Address; chainId: number; sessionEpoch: number; signal: AbortSignal }
export class StaleWalletSessionError extends Error {
  constructor() { super('Wallet changed; review and authorize again'); this.name = 'StaleWalletSessionError'; }
}

/** Own one controller per mounted wallet adapter. Invalidations are synchronous,
 * refresh work is batched; public market queries must not use this private scope. */
export class WalletSessionController {
  private epoch = 0;
  private disconnectEpoch = 0;
  private connectionObservation?: { account?: Address; chainId?: number; sessionEpoch: number };
  private session: Readonly<WalletSession> | null = null;
  private abort = new AbortController();
  private listeners = new Set<() => void>();
  private refreshTimer?: ReturnType<typeof setTimeout>;
  private cleanup?: () => void;
  private providerBinding = 0;
  constructor(private readonly clearPrivateCache: (previous?: Readonly<WalletSession>) => void, private readonly refreshPrivateCache: () => void = () => {}) {}
  get current() { return this.session; }
  get sessionEpoch() { return this.epoch; }
  get cancellationEpoch() { return this.disconnectEpoch; }
  subscribe(listener: () => void) { this.listeners.add(listener); return () => { this.listeners.delete(listener); }; }
  private invalidate() {
    const previous = this.session;
    this.epoch++;
    this.connectionObservation = undefined;
    this.abort.abort();
    this.abort = new AbortController();
    this.session = null;
    recordWalletEvent('wallet.cache_clear', `epoch:${this.epoch}`);
    this.clearPrivateCache(previous ?? undefined);
  }
  private notify() { for (const listener of this.listeners) listener(); }
  private scheduleRefresh() {
    clearTimeout(this.refreshTimer);
    this.refreshTimer = setTimeout(() => { if (this.session) { recordWalletEvent('wallet.refresh_batch', `epoch:${this.epoch}`); this.refreshPrivateCache(); } }, 50);
  }
  select(connectorId: string, providerKind: WalletSession['providerKind'], account: string, chainId: number, registeredIds: readonly string[]) {
    if (!registeredIds.includes(connectorId) || !connectorId || !Number.isSafeInteger(chainId) || chainId < 1) throw new Error('Unregistered connector or invalid chain');
    const checked = getAddress(account);
    if (checked === zeroAddress) throw new Error('Selected wallet account must not be zero');
    this.invalidate();
    this.session = Object.freeze({ connectorId, providerKind, account: checked, chainId, sessionEpoch: this.epoch, capabilities: ['signMessage', 'sendTransaction'] as const, connectedAt: Date.now() });
    this.notify();
    this.scheduleRefresh();
    return this.session;
  }
  /** Provider authorization can emit accountsChanged before connect resolves.
   * Accept only its latest matching observation, never another active session or
   * a disconnect that occurred while the request was in flight. */
  assertConnecting(epoch: number, account: Address, chainId: number, cancellationEpoch: number) {
    if (cancellationEpoch !== this.disconnectEpoch) throw new StaleWalletSessionError();
    if (this.session) throw new StaleWalletSessionError();
    if (epoch === this.epoch) return;
    const observed = this.connectionObservation;
    if (!observed || observed.sessionEpoch !== this.epoch || (observed.account !== undefined && observed.account !== account) || (observed.chainId !== undefined && observed.chainId !== chainId)) throw new StaleWalletSessionError();
  }
  capture(): AccountScope {
    if (!this.session) throw new Error('Connect an external wallet first');
    return Object.freeze({ account: this.session.account, chainId: this.session.chainId, sessionEpoch: this.epoch, signal: this.abort.signal });
  }
  assertCurrent(scope: AccountScope, writes = false) {
    if (scope.signal.aborted || !this.session || scope.sessionEpoch !== this.epoch || scope.account !== this.session.account || scope.chainId !== this.session.chainId) { recordWalletEvent('wallet.epoch_discard', `epoch:${scope.sessionEpoch}`); throw new StaleWalletSessionError(); }
    if (writes && scope.chainId !== 125) throw new Error('Switch to chain 125 before signing');
  }
  async run<T>(work: (scope: AccountScope) => Promise<T>): Promise<T> {
    const scope = this.capture();
    const result = await work(scope);
    this.assertCurrent(scope);
    return result;
  }
  disconnect() { this.disconnectEpoch++; this.invalidate(); clearTimeout(this.refreshTimer); this.notify(); }
  bind(provider: EIP1193Provider) {
    this.cleanup?.();
    const binding = ++this.providerBinding;
    const accountsChanged = (accounts: string[]) => {
      if (binding !== this.providerBinding) return;
      recordWalletEvent('wallet.event', `epoch:${this.epoch}`);
      if (!Array.isArray(accounts) || accounts.some(account => typeof account !== 'string')) { this.disconnect(); return; }
      const previous = this.session;
      if (!accounts.length) { this.disconnect(); return; }
      if (!previous) {
        try {
          const account = getAddress(accounts[0]!);
          if (account === zeroAddress) throw new Error('Invalid wallet account');
          const chainId = this.connectionObservation?.chainId;
          this.invalidate();
          this.connectionObservation = { account, chainId, sessionEpoch: this.epoch };
          this.notify();
        } catch { this.disconnect(); }
        return;
      }
      try { this.select(previous.connectorId, previous.providerKind, accounts[0]!, previous.chainId, [previous.connectorId]); } catch { this.disconnect(); }
    };
    const chainChanged = (chain: string) => {
      if (binding !== this.providerBinding) return;
      recordWalletEvent('wallet.event', `epoch:${this.epoch}`);
      if (typeof chain !== 'string' || !/^0x[0-9a-fA-F]+$/.test(chain)) { this.disconnect(); return; }
      const previous = this.session;
      if (!previous) {
        const chainId = Number(chain);
        if (!Number.isSafeInteger(chainId) || chainId < 1) { this.disconnect(); return; }
        const account = this.connectionObservation?.account;
        this.invalidate();
        this.connectionObservation = { account, chainId, sessionEpoch: this.epoch };
        this.notify();
        return;
      }
      try { this.select(previous.connectorId, previous.providerKind, previous.account, Number(chain), [previous.connectorId]); } catch { this.disconnect(); }
    };
    const disconnected = () => { if (binding === this.providerBinding) this.disconnect(); };
    provider.on('accountsChanged', accountsChanged);
    provider.on('chainChanged', chainChanged);
    provider.on('disconnect', disconnected);
    const detach = () => {
      if (binding === this.providerBinding) this.providerBinding++;
      provider.removeListener('accountsChanged', accountsChanged);
      provider.removeListener('chainChanged', chainChanged);
      provider.removeListener('disconnect', disconnected);
    };
    this.cleanup = detach;
    return () => { detach(); if (this.cleanup === detach) { this.cleanup = undefined; this.disconnect(); } };
  }
  dispose() { this.cleanup?.(); this.cleanup = undefined; this.disconnect(); this.listeners.clear(); }
}
