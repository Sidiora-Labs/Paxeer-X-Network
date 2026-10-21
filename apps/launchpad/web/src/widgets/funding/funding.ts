import { measureWalletOperation } from '../../core/wallet-sdk/measurement';
import { decodeEventLog, erc20Abi, getAddress, type Address, type Hex } from 'viem';
import { assetKey, assertAssetOnChain, rawAmount, readAssetBalance, type AssetRef, type WalletPublicClient } from '../../core/wallet-sdk/assets';
import { WalletSessionController, type AccountScope } from '../../core/wallet-sdk/session';
import type { CommittedWalletHeadV2 } from '../../core/wallet-sdk/backend';

export interface DepositIntent { wallet: Address; chainId: 125; assetRef: AssetRef; requestedRaw?: string; createdAt: number }
export function depositIntent(wallet: Address, assetRef: AssetRef, requestedRaw?: string): Readonly<DepositIntent> {
  assetKey(assetRef);
  if (requestedRaw !== undefined) rawAmount(requestedRaw);
  return Object.freeze({ wallet: getAddress(wallet), chainId: 125, assetRef: Object.freeze({ ...assetRef }), requestedRaw, createdAt: Date.now() });
}
/** EIP681 payload can be passed to a QR renderer and retains chain/asset identity. */
export function fundingURI(intent: DepositIntent) {
  const amount = intent.requestedRaw === undefined ? '' : rawAmount(intent.requestedRaw).toString();
  return intent.assetRef.kind === 'native'
    ? `ethereum:${getAddress(intent.wallet)}@125${amount ? `?value=${amount}` : ''}`
    : `ethereum:${getAddress(intent.assetRef.address)}@125/transfer?address=${getAddress(intent.wallet)}${amount ? `&uint256=${amount}` : ''}`;
}
export interface FundingSnapshot { state: 'instructions' | 'checking_paused' | 'detected' | 'balance_confirmed' | 'reverted' | 'unknown_external_source'; hash?: Hex; balanceRaw?: string; blockNumber?: bigint; detectedRaw?: string; reason?: string }
/** Notifications may supply a hash to inspect, never a balance or credit.
 * Reads are deduplicated and cancelled results discarded by the account scope. */
export class FundingTracker {
  snapshot: FundingSnapshot = { state: 'instructions' };
  private readonly pending = new Map<string, Promise<FundingSnapshot>>();
  private serial: Promise<unknown> = Promise.resolve();
  constructor(private readonly client: WalletPublicClient, private readonly sessions: WalletSessionController, readonly intent: DepositIntent, private readonly committedHead?: (signal: AbortSignal) => Promise<CommittedWalletHeadV2>, private readonly onDetected?: (snapshot: FundingSnapshot) => void) {}
  refresh(hash?: Hex): Promise<FundingSnapshot> {
    if (hash !== undefined && !/^0x[0-9a-fA-F]{64}$/.test(hash)) return Promise.reject(new Error('Invalid incoming transaction hash'));
    const key = hash?.toLowerCase() ?? 'balance';
    const existing = this.pending.get(key);
    if (existing) return existing;
    if (this.pending.size >= 25) return Promise.reject(new Error('Deposit checking queue is full; refresh verified balances explicitly'));
    const scope = this.sessions.capture();
    const work = this.serial.then(() => measureWalletOperation('balance.refresh', `epoch:${scope.sessionEpoch}:${assetKey(this.intent.assetRef)}`, () => this.check(scope, hash)));
    const result = work.finally(() => { this.pending.delete(key); });
    this.pending.set(key, result);
    this.serial = result.then(() => undefined, () => undefined);
    return result;
  }
  private async check(scope: AccountScope, hash?: Hex): Promise<FundingSnapshot> {
    this.sessions.assertCurrent(scope, true);
    if (scope.account !== getAddress(this.intent.wallet)) throw new Error('Funding destination differs from selected wallet');
    try {
      const committed = await this.committedHead?.(scope.signal);
      const head = committed?.number ?? await this.client.getBlockNumber({ cacheTime: 0 });
      const canonicalHead = await this.client.getBlock({ blockNumber: head });
      if (!canonicalHead.hash || (committed && canonicalHead.hash !== committed.hash)) throw new Error('Committed funding head mismatch');
      await assertAssetOnChain(this.client, this.intent.assetRef, head);
      let detectedRaw: string | undefined;
      if (hash) {
        const receipt = await this.client.getTransactionReceipt({ hash });
        if (receipt.transactionHash.toLowerCase() !== hash.toLowerCase()) throw new Error('Incoming receipt hash mismatch');
        if (receipt.blockNumber > head) throw new Error('Receipt is newer than committed balance head');
        const block = await this.client.getBlock({ blockNumber: receipt.blockNumber });
        if (block.hash !== receipt.blockHash) throw new Error('Receipt block does not match committed chain');
        this.sessions.assertCurrent(scope, true);
        if (receipt.status === 'reverted') return this.snapshot = { state: 'reverted', hash, blockNumber: receipt.blockNumber };
        const asset = this.intent.assetRef;
        if (asset.kind === 'native') {
          const tx = await this.client.getTransaction({ hash });
          if (tx.to && getAddress(tx.to) === this.intent.wallet && tx.value > 0n) detectedRaw = tx.value.toString();
        } else {
          let total = 0n;
          for (const log of receipt.logs) {
            if (getAddress(log.address) !== asset.address) continue;
            try {
              const event = decodeEventLog({ abi: erc20Abi, eventName: 'Transfer', data: log.data, topics: log.topics });
              if (getAddress(event.args.to) === this.intent.wallet) total += event.args.value;
            } catch { /* Other token events cannot prove a deposit. */ }
          }
          if (total > 0n) detectedRaw = total.toString();
        }
        this.sessions.assertCurrent(scope, true);
        if (detectedRaw === undefined) return this.snapshot = { state: 'unknown_external_source', hash, reason: 'No matching incoming transfer to this wallet' };
        this.snapshot = { state: 'detected', hash, detectedRaw, blockNumber: receipt.blockNumber };
        this.onDetected?.(this.snapshot);
      }
      const balance = await readAssetBalance(this.client, this.intent.assetRef, scope.account, head);
      if ((await this.client.getBlock({ blockNumber: head })).hash !== canonicalHead.hash) throw new Error('Committed funding head changed; checking paused');
      this.sessions.assertCurrent(scope, true);
      return this.snapshot = { state: 'balance_confirmed', hash, balanceRaw: balance.toString(), detectedRaw, blockNumber: head };
    } catch (error) {
      this.sessions.assertCurrent(scope); // A stale result must never mutate the current wallet view.
      // `reason` is a diagnostic for tests and logs; the panel shows only the plain state label.
      console.error('[funding balance check]', error);
      return this.snapshot = { state: 'checking_paused', hash, reason: error instanceof Error ? error.message : 'Balance check unavailable' };
    }
  }
}
