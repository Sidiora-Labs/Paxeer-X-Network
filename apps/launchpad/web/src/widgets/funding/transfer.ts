import { measureWalletOperation } from '../../core/wallet-sdk/measurement';
import { decodeEventLog, encodeFunctionData, erc20Abi, getAddress, zeroAddress, type Address, type Hex } from 'viem';
import { assetKey, assertAssetOnChain, nativePAX, rawAmount, readAssetBalance, type AssetRef, type WalletPublicClient } from '../../core/wallet-sdk/assets';
import { PaxeerWallet } from '../../core/wallet-sdk/client';
import type { CommittedWalletHeadV2 } from '../../core/wallet-sdk/backend';
import { WalletSessionController, type AccountScope } from '../../core/wallet-sdk/session';

export function maximumNativeTransfer(balanceRaw: bigint, gasEstimateRaw: bigint) {
  if (balanceRaw < 0n || gasEstimateRaw < 0n) throw new Error('Negative balance or fee');
  if (gasEstimateRaw > balanceRaw) throw new Error('Insufficient PAX for gas');
  return balanceRaw - gasEstimateRaw;
}
export interface TransferIntent { intentId: string; chainId: 125; wallet: Address; assetRef: AssetRef; recipient: Address; amountRaw: string; gasEstimateRaw: string }
export interface TransferReview extends TransferIntent { gas: bigint; gasPrice: bigint; expiresAt: number; scope: AccountScope }
/** Each instance holds one transfer draft. Quote/edits never cause a submission. */
export class WalletTransfer {
  state: 'draft' | 'validated' | 'awaiting_signature' | 'submitted' | 'confirmed' | 'reverted' | 'unknown' = 'draft';
  hash?: Hex;
  review?: Readonly<TransferReview>;
  private version = 0;
  private submitting = false;
  private confirming?: ReturnType<WalletTransfer['readConfirmation']>;
  constructor(private readonly client: WalletPublicClient, private readonly wallet: PaxeerWallet, private readonly sessions: WalletSessionController, private readonly committedHead?: (signal: AbortSignal) => Promise<CommittedWalletHeadV2>) {}
  resetDraft() {
    if (this.submitting || this.hash) throw new Error('Keep submitted transfer for confirmation; create a new draft separately');
    this.version++; this.review = undefined; this.state = 'draft';
  }
  private transaction(asset: AssetRef, recipient: Address, amount: bigint) {
    return asset.kind === 'native' ? { to: recipient, value: amount } : { to: asset.address, data: encodeFunctionData({ abi: erc20Abi, functionName: 'transfer', args: [recipient, amount] }) };
  }
  async maximum(recipient: string): Promise<bigint> {
    if (this.submitting || this.hash) throw new Error('A transfer is already awaiting confirmation');
    const scope = this.sessions.capture();
    this.sessions.assertCurrent(scope, true);
    const to = getAddress(recipient.trim());
    if (to === zeroAddress) throw new Error('Recipient must not be zero');
    const committed = await this.committedHead?.(scope.signal);
    const head = committed?.number ?? await this.client.getBlockNumber({ cacheTime: 0 });
    const block = await this.client.getBlock({ blockNumber: head });
    if (!block.hash || (committed && block.hash !== committed.hash)) throw new Error('Committed maximum balance head mismatch');
    const balance = await readAssetBalance(this.client, nativePAX, scope.account, head);
    const price = await this.client.getGasPrice();
    let gas = await this.client.estimateGas({ account: scope.account, to, value: 0n });
    let amount = maximumNativeTransfer(balance, gas * price);
    for (let attempt = 0; amount > 0n; attempt++) {
      const actual = await this.client.estimateGas({ account: scope.account, to, value: amount });
      if (actual <= gas) break;
      if (attempt >= 2) throw new Error('Maximum transfer gas did not stabilize; enter an amount and review it');
      gas = actual;
      amount = maximumNativeTransfer(balance, gas * price);
    }
    if (await this.client.getGasPrice() > price || (await this.client.getBlock({ blockNumber: head })).hash !== block.hash) throw new Error('Maximum transfer estimate changed; refresh it');
    this.sessions.assertCurrent(scope, true);
    return amount;
  }
  async preview(intentId: string, asset: AssetRef, recipient: string, amountRaw: string): Promise<Readonly<TransferReview>> {
    return measureWalletOperation('transfer.preview', `epoch:${this.sessions.sessionEpoch}:${assetKey(asset)}`, () => this.buildPreview(intentId, asset, recipient, amountRaw));
  }
  private async buildPreview(intentId: string, asset: AssetRef, recipient: string, amountRaw: string): Promise<Readonly<TransferReview>> {
    this.resetDraft();
    const version = this.version;
    const scope = this.sessions.capture();
    this.sessions.assertCurrent(scope, true);
    assetKey(asset);
    if (!intentId || intentId.length > 128) throw new Error('Invalid transfer intent identity');
    const to = getAddress(recipient.trim());
    if (to === zeroAddress) throw new Error('Recipient must not be zero');
    const amount = rawAmount(amountRaw);
    if (!amount) throw new Error('Transfer amount must be positive');
    const committed = await this.committedHead?.(scope.signal);
    const head = committed?.number ?? await this.client.getBlockNumber({ cacheTime: 0 });
    const balance = await readAssetBalance(this.client, asset, scope.account, head);
    if (balance < amount) throw new Error(`Insufficient ${asset.symbol} balance`);
    const transaction = this.transaction(asset, to, amount);
    const gas = await this.client.estimateGas({ ...transaction, account: scope.account });
    const gasPrice = await this.client.getGasPrice();
    const fee = gas * gasPrice;
    const pax = await this.client.getBalance({ address: scope.account, blockNumber: head });
    if (pax < fee + (asset.kind === 'native' ? amount : 0n)) throw new Error('Insufficient PAX for amount and gas');
    if (committed && (await this.client.getBlock({ blockNumber: head })).hash !== committed.hash) throw new Error('Committed transfer balance head changed');
    this.sessions.assertCurrent(scope, true);
    if (version !== this.version) throw new Error('Transfer draft changed during estimate');
    this.review = Object.freeze({ intentId, chainId: 125, wallet: scope.account, assetRef: Object.freeze({ ...asset }), recipient: to, amountRaw, gasEstimateRaw: fee.toString(), gas, gasPrice, expiresAt: Date.now() + 30_000, scope });
    this.state = 'validated';
    return this.review;
  }
  async submit(review: Readonly<TransferReview>, consent: (review: Readonly<TransferReview>) => Promise<boolean>): Promise<Hex> {
    if (this.submitting || this.hash) throw new Error('Transfer already submitted or awaiting signature');
    if (this.review !== review) throw new Error('Transfer review is stale');
    this.submitting = true;
    try {
      this.sessions.assertCurrent(review.scope, true);
      if (Date.now() >= review.expiresAt) throw new Error('Transfer fee estimate expired');
      this.state = 'awaiting_signature';
      if (!await consent(review)) throw new Error('Transfer consent declined');
      this.sessions.assertCurrent(review.scope, true);
      if (Date.now() >= review.expiresAt) throw new Error('Transfer fee estimate expired');
      const currentPrice = await this.client.getGasPrice();
      if (currentPrice > review.gasPrice) throw new Error('Gas price increased; request a fresh preview');
      const committed = await this.committedHead?.(review.scope.signal);
      const head = committed?.number ?? await this.client.getBlockNumber({ cacheTime: 0 });
      const amount = rawAmount(review.amountRaw);
      const balance = await readAssetBalance(this.client, review.assetRef, review.wallet, head);
      const pax = await this.client.getBalance({ address: review.wallet, blockNumber: head });
      if (balance < amount || pax < rawAmount(review.gasEstimateRaw) + (review.assetRef.kind === 'native' ? amount : 0n)) throw new Error('Balance changed; review transfer again');
      if (committed && (await this.client.getBlock({ blockNumber: head })).hash !== committed.hash) throw new Error('Committed transfer balance head changed');
      this.sessions.assertCurrent(review.scope, true);
      this.hash = await this.wallet.sendTransaction(review.wallet, { ...this.transaction(review.assetRef, review.recipient, amount), gas: review.gas, gasPrice: review.gasPrice });
      // A wallet switch after dispatch must not discard its authentic recovery hash.
      this.state = 'submitted';
      return this.hash;
    } catch (error) { this.state = this.hash ? 'unknown' : 'draft'; this.review = undefined; throw error; }
    finally { this.submitting = false; }
  }
  /** L26 may call this read adapter; no timer, indexer event or resubmission. */
  confirm() {
    if (this.confirming) return this.confirming;
    const work = measureWalletOperation('transfer.confirm', `epoch:${this.review?.scope.sessionEpoch ?? 0}`, () => this.readConfirmation());
    this.confirming = work;
    void work.then(() => { if (this.confirming === work) this.confirming = undefined; }, () => { if (this.confirming === work) this.confirming = undefined; });
    return work;
  }
  private async readConfirmation() {
    if (!this.hash || !this.review) throw new Error('No submitted transfer');
    try {
      if (await this.client.getChainId() !== 125) throw new Error('Wrong receipt chain');
      const receipt = await this.client.getTransactionReceipt({ hash: this.hash });
      if (receipt.transactionHash.toLowerCase() !== this.hash.toLowerCase()) throw new Error('Transfer receipt hash mismatch');
      const committed = await this.committedHead?.(new AbortController().signal);
      if (committed && receipt.blockNumber > committed.number) throw new Error('Transfer is awaiting the committed chain head');
      if (committed && (await this.client.getBlock({ blockNumber: committed.number })).hash !== committed.hash) throw new Error('Committed confirmation head mismatch');
      const block = await this.client.getBlock({ blockNumber: receipt.blockNumber });
      if (block.hash !== receipt.blockHash) throw new Error('Receipt does not match committed chain');
      const tx = await this.client.getTransaction({ hash: this.hash });
      const expectedTx = this.transaction(this.review.assetRef, this.review.recipient, rawAmount(this.review.amountRaw));
      if (tx.hash.toLowerCase() !== this.hash.toLowerCase() || getAddress(tx.from) !== this.review.wallet || !tx.to || getAddress(tx.to) !== expectedTx.to || tx.value !== ('value' in expectedTx ? expectedTx.value : 0n) || (tx.input !== ('data' in expectedTx ? expectedTx.data : '0x'))) throw new Error('Receipt transaction differs from reviewed transfer');
      await assertAssetOnChain(this.client, this.review.assetRef, receipt.blockNumber);
      if (receipt.status === 'success' && this.review.assetRef.kind === 'erc20') {
        const review = this.review;
        const matching = receipt.logs.some(log => {
          if (getAddress(log.address) !== (review.assetRef as { address: Address }).address) return false;
          try {
            const event = decodeEventLog({ abi: erc20Abi, eventName: 'Transfer', data: log.data, topics: log.topics });
            return getAddress(event.args.from) === review.wallet && getAddress(event.args.to) === review.recipient && event.args.value === rawAmount(review.amountRaw);
          } catch { return false; }
        });
        if (!matching) throw new Error('Successful receipt lacks the reviewed ERC20 transfer');
      }
      if ((await this.client.getBlock({ blockNumber: receipt.blockNumber })).hash !== receipt.blockHash || (committed && (await this.client.getBlock({ blockNumber: committed.number })).hash !== committed.hash)) throw new Error('Confirmation chain changed; hash preserved');
      this.state = receipt.status === 'success' ? 'confirmed' : 'reverted';
      return { state: this.state, hash: this.hash, amountRaw: this.state === 'confirmed' ? this.review.amountRaw : '0', explorerUrl: `https://paxscan.paxeer.app/tx/${this.hash}`, receipt };
    } catch (error) { this.state = 'unknown'; throw error; }
  }
}
