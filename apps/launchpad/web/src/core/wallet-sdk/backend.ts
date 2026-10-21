import { decodeEventLog, decodeFunctionData, erc20Abi, getAddress, keccak256, stringToHex, type Address, type Hex } from 'viem';
import type { ComponentType } from 'react';
import { assertAssetOnChain, assetKey, nativePAX, usdlAsset, type AssetRef, type WalletPublicClient } from './assets';
import { PendingAttemptStore, ReceiptRecoveryScheduler, type RecoveryUpdate } from '../../widgets/trading/transaction-controls/recovery';
import type { PendingAttemptRecord } from '../../widgets/trading/transaction-controls/domain';
import type { TransferReview } from '../../widgets/funding/transfer';

export interface PublicNetworkConfigurationV1 {
  schemaVersion: 1; deploymentVersion: string; manifestHash: string; sourceManifestSha256: string;
  chainId: 125; publicRPC: string; explorerBase: string;
  native: { symbol: 'PAX'; decimals: 18 }; settlement: { symbol: 'USDL'; decimals: 6 };
  addresses: Readonly<Record<string, string>>; creationFeeRaw: string;
}
export interface CommittedWalletHeadV2 { number: bigint; hash: Hex }
export interface IncomingTransferHintV2 { chainId: 125; recipient: Address; transactionHash: Hex }
export interface WalletTransferRecoveryUpdateV2 extends RecoveryUpdate {
  confirmedTransfer?: Readonly<{ asset: AssetRef; recipient: Address; amountRaw: string }>;
}
export interface WalletNetworkAuthorityV2 {
  network: Readonly<PublicNetworkConfigurationV1>;
  usdlRuntimeHash: Hex;
  committedHead(signal: AbortSignal): Promise<CommittedWalletHeadV2>;
  subscribeIncomingTransfer?(account: Address, listener: (hint: IncomingTransferHintV2) => void): () => void;
  FundingQRCode?: ComponentType<{ value: string; label: string }>;
}
export function isWalletPrivateQueryV2(query: { queryKey: readonly unknown[]; meta?: Record<string, unknown> }, account?: Address): boolean {
  const family = String(query.queryKey[0]);
  if (query.meta?.private === true || ['wallet','balance','allowance','portfolio','session','wallet-balances','address-counters','explorer-txs','explorer-transfers'].includes(family)) return true;
  if (!['readContract', 'readContracts', 'infiniteReadContracts'].includes(family) || !account) return false;
  const input = query.queryKey[1];
  if (family !== 'readContract') {
    if (typeof input !== 'object' || input === null || !('contracts' in input) || !Array.isArray(input.contracts)) return false;
    return input.contracts.some(contract => isWalletPrivateQueryV2({ queryKey: ['readContract', contract] }, account));
  }
  if (typeof input !== 'object' || input === null || !('functionName' in input) || !['balanceOf','allowance'].includes(String(input.functionName)) || !('args' in input) || !Array.isArray(input.args)) return false;
  return typeof input.args[0] === 'string' && input.args[0].toLowerCase() === account.toLowerCase();
}
export function approvedWalletAssets(authority: WalletNetworkAuthorityV2): readonly AssetRef[] {
  const n = authority.network;
  if (n.schemaVersion !== 1 || n.chainId !== 125 || !n.deploymentVersion || !/^[0-9a-f]{64}$/.test(n.sourceManifestSha256) || !/^[0-9a-f]{64}$/.test(n.manifestHash) || n.native.symbol !== 'PAX' || n.native.decimals !== 18 || n.settlement.symbol !== 'USDL' || n.settlement.decimals !== 6) throw new Error('Approved Paxeer network identity is unavailable');
  for (const endpoint of [n.publicRPC, n.explorerBase]) {
    const url = new URL(endpoint);
    if (url.protocol !== 'https:' || url.username || url.password || url.search || url.hash) throw new Error('Approved public network endpoint required');
  }
  if (!n.addresses.USDL || !/^0x[0-9a-fA-F]{64}$/.test(authority.usdlRuntimeHash)) throw new Error('Attested USDL identity is unavailable');
  return Object.freeze([nativePAX, usdlAsset(n.addresses.USDL, authority.usdlRuntimeHash)]);
}
export async function checkedCommittedHead(client: WalletPublicClient, authority: WalletNetworkAuthorityV2, signal: AbortSignal): Promise<CommittedWalletHeadV2> {
  signal.throwIfAborted();
  if (await client.getChainId() !== 125) throw new Error('Committed balance RPC is not chain 125');
  const head = await authority.committedHead(signal);
  if (head.number < 0n || !/^0x[0-9a-fA-F]{64}$/.test(head.hash)) throw new Error('Committed head authority is unavailable');
  const canonical = await client.getBlock({ blockNumber: head.number });
  signal.throwIfAborted();
  if (canonical.hash?.toLowerCase() !== head.hash.toLowerCase()) throw new Error('Committed balance head is no longer canonical');
  return Object.freeze({ ...head });
}
export function walletTransferFingerprint(wallet: Address, asset: AssetRef, recipient: Address, amountRaw: string): Hex {
  return keccak256(stringToHex(JSON.stringify([125, getAddress(wallet), assetKey(asset), getAddress(recipient), amountRaw])));
}
export class WalletTransferHistoryV2 {
  readonly store: PendingAttemptStore;
  private readonly scheduler: ReceiptRecoveryScheduler;
  private readonly watches = new Map<string, () => void>();
  private readonly confirmed = new Map<string, Readonly<{ asset: AssetRef; recipient: Address; amountRaw: string }>>();
  constructor(storage: Storage, account: Address, private readonly client: WalletPublicClient, private readonly authority: WalletNetworkAuthorityV2) {
    this.store = new PendingAttemptStore(storage, account);
    this.scheduler = new ReceiptRecoveryScheduler(async (attempt, signal) => {
      const head = await checkedCommittedHead(client, authority, signal);
      const hash = attempt.txHash as Hex;
      let receipt;
      try { receipt = await client.getTransactionReceipt({ hash }); }
      catch (error) { if (error instanceof Error && error.name === 'TransactionReceiptNotFoundError') return undefined; throw error; }
      if (receipt.transactionHash.toLowerCase() !== hash.toLowerCase()) throw new Error('Recovery receipt hash mismatch');
      if (receipt.blockNumber > head.number) return undefined;
      const block = await client.getBlock({ blockNumber: receipt.blockNumber });
      const tx = await client.getTransaction({ hash });
      signal.throwIfAborted();
      if (block.hash !== receipt.blockHash || tx.hash.toLowerCase() !== hash.toLowerCase() || !tx.to || getAddress(tx.from) !== getAddress(attempt.from)) throw new Error('Transfer recovery transaction identity mismatch');
      let asset: AssetRef = nativePAX, recipient = getAddress(tx.to), amountRaw = tx.value.toString();
      if (tx.input !== '0x') {
        asset = approvedWalletAssets(authority)[1]!;
        if (asset.kind !== 'erc20' || getAddress(tx.to) !== asset.address || tx.value !== 0n) throw new Error('Unknown recovered asset');
        await assertAssetOnChain(client, asset, head.number);
        const decoded = decodeFunctionData({ abi: erc20Abi, data: tx.input });
        if (decoded.functionName !== 'transfer') throw new Error('Recovered transaction is not an asset transfer');
        const [to, amount] = decoded.args;
        recipient = getAddress(to); amountRaw = amount.toString();
      }
      if (attempt.action !== 'wallet_transfer' || walletTransferFingerprint(getAddress(attempt.from), asset, recipient, amountRaw) !== attempt.actionFingerprint) throw new Error('Recovered transfer differs from its reviewed action');
      if (asset.kind === 'erc20' && receipt.status === 'success') {
        const token = asset.address;
        if (!receipt.logs.some(log => {
          if (getAddress(log.address) !== token) return false;
          try { const event = decodeEventLog({ abi: erc20Abi, eventName: 'Transfer', topics: log.topics, data: log.data }); return getAddress(event.args.from) === getAddress(attempt.from) && getAddress(event.args.to) === recipient && event.args.value.toString() === amountRaw; }
          catch { return false; }
        })) throw new Error('Recovered successful receipt lacks the reviewed token transfer');
      }
      if ((await client.getBlock({ blockNumber: head.number })).hash !== head.hash) throw new Error('Committed recovery head changed');
      signal.throwIfAborted();
      if (receipt.status === 'success') this.confirmed.set(hash.toLowerCase(), Object.freeze({ asset, recipient, amountRaw }));
      return { transactionHash: receipt.transactionHash, status: receipt.status, blockNumber: receipt.blockNumber };
    });
  }
  record(review: Readonly<TransferReview>, txHash: Hex): PendingAttemptRecord {
    const record: PendingAttemptRecord = { version: 1, intentId: review.intentId, chainId: 125, from: review.wallet, txHash, submittedAt: Date.now(), action: 'wallet_transfer', actionFingerprint: walletTransferFingerprint(review.wallet, review.assetRef, review.recipient, review.amountRaw) };
    this.store.save(record);
    return record;
  }
  resume(notify: (update: WalletTransferRecoveryUpdateV2) => void) {
    const loaded = this.store.load();
    if (loaded.corrupt) throw new Error('Transaction history needs attention; existing hashes were preserved');
    for (const attempt of loaded.records.filter(record => record.action === 'wallet_transfer')) {
      const key = attempt.txHash.toLowerCase();
      if (this.watches.has(key)) continue;
      const stop = this.scheduler.watch(attempt, update => { notify({ ...update, confirmedTransfer: update.receipt?.status === 'success' ? this.confirmed.get(attempt.txHash.toLowerCase()) : undefined }); if (update.receipt) this.watches.delete(key); });
      this.watches.set(key, stop);
    }
    return loaded.records;
  }
  wake(hash: Hex) { this.scheduler.wake(hash); }
  close() { for (const stop of this.watches.values()) stop(); this.watches.clear(); this.confirmed.clear(); this.scheduler.close(); }
}
