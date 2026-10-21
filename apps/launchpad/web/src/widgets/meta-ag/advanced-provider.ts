import { decodeAbiParameters, parseAbiParameters, parseAbiItem, decodeEventLog, erc20Abi, keccak256, parseAbi, type Hex, type PublicClient, type WalletClient } from 'viem';
import { PendingAttemptStore } from '../trading/transaction-controls/recovery';
import { abiFields, abiUint, abiBoolean, abiHex, validateAdapterData, decodeDeployedOrder, address, uint, validateOrder, validateCustomRoute, selectBestRoute, routeNetOutput, orderPageSize, encodeCancelAttempt, decodeCancelAttempt, cancellationOutcome, type Address, type Adapter, type OrderDraft, type OrderIntent, type OrderRules, type OrderView, type RouteQuote, type Hop, type CancelAttempt } from './domain';

const cancellationDispatches = new Set<string>();
const cancellationBroadcasts = new Map<string, CancelAttempt>();
const implementationSlot = '0x360894a13ba1a3210667c828492db98dca3e2076cc3735a920a3ca505d382bbc' as const;
export const advancedOrdersAbi = parseAbi([
  'function placeOrder((uint8 orderType,address stablecoin,address token,uint256 amount,uint256 triggerPrice,uint256 limitPrice,uint256 expiresAt,uint256 minimumOutput) request) returns (uint256)',
  'function getLimitOrder(uint256 id) view returns ((uint256 id,address user,address stablecoin,address token,uint256 amount,uint256 targetPrice,uint8 orderType,uint8 status,uint256 createdAt,uint256 expiresAt))',
  'function getStopLimitOrder(uint256 id) view returns ((uint256 id,address user,address stablecoin,address token,uint256 amount,uint256 stopPrice,uint256 limitPrice,uint8 orderType,uint8 status,uint256 createdAt,uint256 expiresAt))',
  'function cancelLimitOrder(uint256 id)', 'function cancelStopLimitOrder(uint256 id)',
  'function canExecuteLimitOrder(uint256 id) view returns (bool,string)',
  'function orderExecutionPaused() view returns (bool)',
  'function getPricingConfig() view returns ((uint256 fee,uint256 tier1,uint256 tier2,uint256 scalar,bool impact))',
  'function orderMinOutput(uint256 id) view returns (uint256)',
  'event LimitOrderCreated(uint256 indexed orderId,address indexed user,uint8 orderType,uint256 amount,uint256 targetPrice)',
  'event StopLimitOrderCreated(uint256 indexed orderId,address indexed user,uint8 orderType,uint256 amount,uint256 stopPrice,uint256 limitPrice)',
  'event StopLimitActivated(uint256 indexed orderId,uint256 activationPrice)',
  'event LimitOrderExecuted(uint256 indexed orderId,uint256 executionPrice)',
  'event StopLimitExecuted(uint256 indexed orderId,uint256 executionPrice)',
  'event LimitOrderCancelled(uint256 indexed orderId)', 'event StopLimitCancelled(uint256 indexed orderId)',
  'event OrderExpiredAndRefunded(uint256 indexed orderId,address indexed user,uint256 amount)',
]);
const routerAbi = parseAbi([
  'event BestRouteSwap(address indexed user,address indexed tokenIn,address indexed tokenOut,uint256 amountIn,uint256 amountOut,bytes32 adapterId)',
  'event MultiHopSwap(address indexed user,address indexed tokenIn,address indexed tokenOut,uint256 amountIn,uint256 amountOut,uint256 hops)',
  'function getAdapters() view returns ((bytes32 adapterId,address adapter,bool active,string name)[])',
  'function getAdapter(bytes32 id) view returns ((bytes32 adapterId,address adapter,bool active,string name))',
  'function getBestQuote(address tokenIn,address tokenOut,uint256 amountIn) view returns ((uint256 amountOut,uint256 priceImpactBps,uint256 feeBps,uint256 feeAmount,bytes32 adapterId,address adapter,bytes adapterData,bool found))',
  'function swapViaAdapter(bytes32 adapterId,address tokenIn,address tokenOut,uint256 amountIn,uint256 amountOutMin,uint256 deadline) returns (uint256)',
  'function swapMultiHop((address tokenIn,address tokenOut,bytes32 adapterId,uint256 minAmountOut)[] hops,uint256 amountIn,uint256 amountOutMin,uint256 deadline) returns (uint256)',
]);
const quoterAbi = parseAbi(['function getTokenPrice(address token) view returns (uint256 price,uint256 timestamp,bool isStale)']);
const kinds = ['limit-buy', 'limit-sell', 'stop-loss', 'stop-limit-buy', 'stop-limit-sell'] as const;
export interface AdvancedBinding {
  address: Address; runtimeCodeHash: Hex; implementation?: { address: Address; runtimeCodeHash: Hex };
}
export interface AdvancedManifest {
  version: 2; chainId: 125; revision: string; deploymentBlock: string; confirmations: number;
  usdl: Address; orders: AdvancedBinding | null; router: AdvancedBinding | null; quoter: AdvancedBinding | null;
  vault: AdvancedBinding | null; adapters: ReadonlyArray<AdvancedBinding & { id: Hex; adapterData: 'vault-v2' | 'sidiora-v1' }>;
  priceScale: number; priceDenomination: Address; supportedKinds: ReadonlyArray<typeof kinds[number]>;
  maximumOrderLifetimeSeconds: string; gtcSentinel?: string; keeperHealthUrl: string | null;
}
export interface ConnectedOrder extends OrderView {
  family: 'limit' | 'stop'; token: Address; settlementAsset: Address; amountRaw: bigint;
  type: typeof kinds[number]; expiresAt: bigint; eligible: boolean | null; checkedAt: number;
  keeperHealthy: boolean; keeperCheckedAt: number; creationBlock: bigint; feeRaw?: bigint;
}
export interface PreparedOrder { intent: OrderIntent; minimumOutput: bigint; spender: Address; escrowAsset: Address; fees: string; cancellation: string }
export interface OwnedOrderPage { items: ConnectedOrder[]; nextCursor: string | null }
export interface AdvancedProviderOptions {
  publicClient: PublicClient; walletClient: WalletClient; manifestJson: string; manifestSha256: string;
  attempts: Pick<Storage, 'getItem' | 'setItem' | 'removeItem'>;
}
async function sha256(value: string) {
  const digest = await globalThis.crypto.subtle.digest('SHA-256', new TextEncoder().encode(value));
  return Array.from(new Uint8Array(digest), byte => byte.toString(16).padStart(2, '0')).join('');
}
export async function createAdvancedProvider(options: AdvancedProviderOptions) {
  if (!/^[a-f0-9]{64}$/.test(options.manifestSha256) || await sha256(options.manifestJson) !== options.manifestSha256) throw new Error('Advanced manifest digest mismatch');
  if (options.manifestJson.length > 262144) throw new Error('Advanced manifest exceeds admission bound');
  const manifest = JSON.parse(options.manifestJson) as AdvancedManifest;
  const freeze = (value: unknown): void => { if (value && typeof value === 'object') { Object.values(value).forEach(freeze); Object.freeze(value); } };
  freeze(manifest);
  if (!manifest || typeof manifest !== 'object' || manifest.version !== 2 || manifest.chainId !== 125 || typeof manifest.revision !== 'string' || manifest.revision.length < 1 || manifest.revision.length > 128 || typeof manifest.deploymentBlock !== 'string' || !/^(0|[1-9]\d{0,19})$/.test(manifest.deploymentBlock) || !Number.isInteger(manifest.confirmations) || manifest.confirmations < 1 || manifest.confirmations > 100) throw new Error('Unsupported advanced manifest');
  address(manifest.usdl); address(manifest.priceDenomination);
  if (manifest.priceScale !== 18 || address(manifest.usdl) !== address(manifest.priceDenomination) || !/^[1-9]\d{0,8}$/.test(manifest.maximumOrderLifetimeSeconds) || BigInt(manifest.maximumOrderLifetimeSeconds) > 2592000n || manifest.gtcSentinel !== undefined && manifest.gtcSentinel !== '0') throw new Error('Unsupported deployed order price or expiry semantics');
  if (!Array.isArray(manifest.adapters) || !Array.isArray(manifest.supportedKinds)) throw new Error('Malformed capability registry');
  const adapterIds = new Set<string>(), adapterAddresses = new Set<string>();
  const validateBinding = (binding: AdvancedBinding | null) => {
    if (!binding) return;
    address(binding.address); abiHex(binding.runtimeCodeHash, 32);
    if (binding.implementation) { address(binding.implementation.address); abiHex(binding.implementation.runtimeCodeHash, 32); }
  };
  for (const binding of [manifest.orders, manifest.router, manifest.quoter, manifest.vault, ...manifest.adapters]) validateBinding(binding);
  for (const binding of manifest.adapters) {
    const id = abiHex(binding.id, 32).toLowerCase(), target = address(binding.address);
    if (adapterIds.has(id) || adapterAddresses.has(target) || !['vault-v2', 'sidiora-v1'].includes(binding.adapterData)) throw new Error('Duplicate or unsupported admitted adapter');
    adapterIds.add(id); adapterAddresses.add(target);
  }
  if (manifest.adapters.length > 8 || (manifest.orders && !manifest.supportedKinds.length) || manifest.supportedKinds.some(kind => !kinds.includes(kind))) throw new Error('Unsupported capability bounds');
  const client = options.publicClient, wallet = options.walletClient;
  let issuedQuotes = new WeakSet<object>();
  const issuedOrders = new WeakSet<object>();
  const issuedRoutes = new WeakSet<object>();
  const volatileCancels = cancellationBroadcasts;
  const blockers = new Map<string, string>();
  const chain = async () => { if (await client.getChainId() !== 125 || await wallet.getChainId() !== 125) throw new Error('Switch wallet and RPC to chain125'); };
  const owner = async () => { await chain(); const accounts = await wallet.getAddresses(); if (!accounts[0]) throw new Error('Connect wallet'); return address(accounts[0]); };
  const checkedBinding = async (binding: AdvancedBinding | null, name: string) => {
    if (!binding) throw new Error(`${name}: no verified active deployment for chain125`);
    const target = address(binding.address);
    const code = await client.getCode({ address: target });
    if (!code || code === '0x') throw new Error(`${name}: missing contract bytecode (EOA)`);
    if (keccak256(code).toLowerCase() !== binding.runtimeCodeHash.toLowerCase()) throw new Error(`${name}: deployed ABI/code identity changed`);
    if (binding.implementation) {
      const slot = await client.getStorageAt({ address: target, slot: implementationSlot });
      if (!slot || address(`0x${slot.slice(-40)}`) !== address(binding.implementation.address)) throw new Error(`${name}: proxy implementation changed`);
      const implementationCode = await client.getCode({ address: binding.implementation.address });
      if (!implementationCode || keccak256(implementationCode).toLowerCase() !== binding.implementation.runtimeCodeHash.toLowerCase()) throw new Error(`${name}: incompatible implementation bytecode`);
    }
    return target;
  };
  await chain();
  for (const [name, binding] of [['orders', manifest.orders], ['routes', manifest.router], ['oracle', manifest.quoter]] as const) {
    try { await checkedBinding(binding, name); } catch (error) { blockers.set(name, String(error instanceof Error ? error.message : error)); }
  }
  const required = async (name: 'orders' | 'routes' | 'oracle') => {
    const binding = name === 'orders' ? manifest.orders : name === 'routes' ? manifest.router : manifest.quoter;
    try { const target = await checkedBinding(binding, name); blockers.delete(name); return target; }
    catch (error) { blockers.set(name, error instanceof Error ? error.message : String(error)); throw error; }
  };
  const block = async () => {
    const latest = await client.getBlockNumber();
    if (latest < BigInt(manifest.confirmations)) throw new Error('Finalized block unavailable');
    return client.getBlock({ blockNumber: latest - BigInt(manifest.confirmations) + 1n });
  };
  const receipt = async (hash: Hex) => {
    const result = await client.waitForTransactionReceipt({ hash, confirmations: manifest.confirmations, timeout: 60_000 });
    const canonical = await client.getBlock({ blockNumber: result.blockNumber });
    if (result.transactionHash.toLowerCase() !== hash.toLowerCase() || !['success', 'reverted'].includes(result.status)) throw new Error('Receipt identity or status mismatch');
    if (canonical.hash !== result.blockHash) throw new Error('Receipt no longer canonical');
    return result;
  };
  const health = async () => {
    if (!manifest.keeperHealthUrl) return { healthy: false, checkedAt: 0 };
    // Keeper health is an external execution-service authority. The web origin
    // serves no keeper route, so a relative path is not resolved against it:
    // it reads as "no keeper authority" (unhealthy) instead of a same-origin 404.
    if (!URL.canParse(manifest.keeperHealthUrl)) return { healthy: false, checkedAt: 0 };
    const url = new URL(manifest.keeperHealthUrl);
    if (url.protocol !== 'https:' && url.hostname !== '127.0.0.1' && url.hostname !== 'localhost') throw new Error('Insecure keeper health authority');
    const response = await fetch(url, { signal: AbortSignal.timeout(750), cache: 'no-store', credentials: 'same-origin' });
    if (!response.ok) return { healthy: false, checkedAt: 0 };
    const value: unknown = await response.json();
    if (!value || typeof value !== 'object') throw new Error('Malformed keeper health');
    const record = value as Record<string, unknown>;
    if (record.chainId !== 125 || record.manifestRevision !== manifest.revision || typeof record.healthy !== 'boolean' || typeof record.checkedAt !== 'number' || !Number.isFinite(record.checkedAt)) throw new Error('Keeper health deployment mismatch');
    return { healthy: record.healthy && Date.now() >= record.checkedAt && Date.now() - record.checkedAt <= 30_000, checkedAt: record.checkedAt };
  };
  const decimals = async (token: Address) => {
    const value = await client.readContract({ address: address(token), abi: erc20Abi, functionName: 'decimals' });
    if (!Number.isInteger(value) || value < 0 || value > 36) throw new Error('Verified token decimals required');
    return value;
  };
  const adapterDiagnostics = new Map<string, string>();
  const adapters = async (): Promise<Array<Adapter & { name: string }>> => {
    const target = await required('routes');
    const records = await client.readContract({ address: target, abi: routerAbi, functionName: 'getAdapters' });
    if (records.length > 64) throw new Error('Adapter registry capacity exceeded');
    const result: Array<Adapter & { name: string }> = [];
    const seen = new Set<string>();
    adapterDiagnostics.clear();
    for (const record of records) {
      const [id, targetAddress, enabled, name] = abiFields(record, ['adapterId','adapter','active','name']);
      const adapterId = abiHex(id, 32), target = address(targetAddress as string), active = abiBoolean(enabled);
      if (seen.has(adapterId.toLowerCase()) || typeof name !== 'string' || name.length > 256) throw new Error('Malformed adapter registry');
      seen.add(adapterId.toLowerCase());
      if (!active) continue;
      const binding = manifest.adapters.find(item => item.id.toLowerCase() === adapterId.toLowerCase() && address(item.address) === target);
      if (!binding) { adapterDiagnostics.set(adapterId, 'Adapter outside admitted deployment registry'); continue; }
      try {
        await checkedBinding(binding, 'adapter');
        result.push({ id: adapterId, address: target, active: true, name });
      } catch (error) { adapterDiagnostics.set(adapterId, error instanceof Error ? error.message : String(error)); }
    }
    return result;
  };
  const approve = async (token: Address, spender: Address, amount: bigint) => {
    const account = await owner(); uint(amount, true);
    const allowed = [manifest.orders, manifest.router].find(binding => binding && address(binding.address) === address(spender));
    await checkedBinding(allowed ?? null, 'approval spender');
    const balance = await client.readContract({ address: token, abi: erc20Abi, functionName: 'balanceOf', args: [account] });
    if (balance < amount) throw new Error('Insufficient escrow balance');
    const allowance = await client.readContract({ address: token, abi: erc20Abi, functionName: 'allowance', args: [account, spender] });
    if (allowance >= amount) return null;
    const simulation = await client.simulateContract({ account, address: token, abi: erc20Abi, functionName: 'approve', args: [spender, amount] });
    if (await owner() !== account) throw new Error('Wallet account changed');
    const hash = await wallet.writeContract({ ...simulation.request, chain: wallet.chain, account });
    if ((await receipt(hash)).status !== 'success') throw new Error('Approval reverted');
    const confirmed = await client.readContract({ address: token, abi: erc20Abi, functionName: 'allowance', args: [account, spender] });
    if (confirmed < amount) throw new Error('Approval allowance not confirmed');
    return hash;
  };
  const prepareOrder = async (draft: OrderDraft, minimumOutput: bigint): Promise<PreparedOrder> => {
    const account = await owner(), spender = await required('orders'), oracle = await required('oracle');
    if (account !== address(draft.owner)) throw new Error('Order owner changed');
    if (address(draft.settlementAsset) !== address(manifest.usdl)) throw new Error('Order settlement asset differs from reviewed deployment');
    const buy = draft.kind === 'limit-buy' || draft.kind === 'stop-limit-buy';
    const verifiedDecimals = await decimals(buy ? manifest.usdl : draft.token);
    const priceOutput = await client.readContract({ address: oracle, abi: quoterAbi, functionName: 'getTokenPrice', args: [draft.token] });
    const priceFields = abiFields(priceOutput, ['price','timestamp','isStale']);
    const price = abiUint(priceFields[0]), timestamp = abiUint(priceFields[1]), stale = abiBoolean(priceFields[2]);
    const current = await block();
    const execution = await health();
    if (!execution.healthy) throw new Error('Execution service unavailable or degraded');
    const rules: OrderRules = { supportedKinds: manifest.supportedKinds, priceScale: manifest.priceScale, denomination: manifest.priceDenomination, priceFresh: price > 0n && !stale && timestamp <= current.timestamp && current.timestamp - timestamp <= 30n, maxLifetimeSeconds: BigInt(manifest.maximumOrderLifetimeSeconds), gtcSentinel: manifest.gtcSentinel === undefined ? undefined : BigInt(manifest.gtcSentinel), stopLimitRelation: 'buy-limit-gte-stop-sell-limit-lte-stop' };
    const intent = validateOrder({ ...draft, settlementAsset: manifest.usdl, amountDecimals: verifiedDecimals }, rules, current.timestamp);
    uint(minimumOutput, true);
    const pricing = await client.readContract({ address: spender, abi: advancedOrdersAbi, functionName: 'getPricingConfig' });
    for (const field of [pricing.fee, pricing.tier1, pricing.tier2, pricing.scalar]) abiUint(field);
    abiBoolean(pricing.impact);
    if (pricing.fee > 10000n || pricing.tier1 > 10000n || pricing.tier2 > 10000n) throw new Error('Unsupported deployed fee basis');
    const fees = `Fee ${Number(pricing.fee) / 100}%, can change with volume. Your minimum received protects what you get`;
    const prepared: PreparedOrder = { intent, minimumOutput, spender, escrowAsset: buy ? manifest.usdl : draft.token, fees, cancellation: 'Your order may trade before a cancel goes through. Refunds show once confirmed' };
    Object.freeze(intent); Object.freeze(prepared); issuedOrders.add(prepared); return prepared;
  };
  const placeOrder = async (prepared: PreparedOrder) => {
    if (!issuedOrders.has(prepared)) throw new Error('Order review must come from verified provider');
    issuedOrders.delete(prepared);
    const account = await owner();
    if (account !== prepared.intent.owner) throw new Error('Order account changed');
    const revalidate = async () => prepareOrder({ kind: prepared.intent.type, owner: account, token: prepared.intent.token, settlementAsset: prepared.intent.settlementAsset, amount: (await import('viem')).formatUnits(prepared.intent.amountRaw, await decimals(prepared.escrowAsset)), triggerPrice: prepared.intent.triggerPriceRaw === undefined ? undefined : (await import('viem')).formatUnits(prepared.intent.triggerPriceRaw, prepared.intent.priceScale), limitPrice: prepared.intent.limitPriceRaw === undefined ? undefined : (await import('viem')).formatUnits(prepared.intent.limitPriceRaw, prepared.intent.priceScale), expiresAt: prepared.intent.expiresAt }, prepared.minimumOutput);
    if ((await revalidate()).fees !== prepared.fees) throw new Error('Order pricing configuration changed; review again');
    const target = await required('orders');
    await approve(prepared.escrowAsset, target, prepared.intent.amountRaw);
    if ((await revalidate()).fees !== prepared.fees) throw new Error('Order pricing configuration changed; review again');
    const request = { orderType: kinds.indexOf(prepared.intent.type), stablecoin: prepared.intent.settlementAsset, token: prepared.intent.token, amount: prepared.intent.amountRaw, triggerPrice: prepared.intent.triggerPriceRaw ?? prepared.intent.limitPriceRaw!, limitPrice: prepared.intent.limitPriceRaw ?? 0n, expiresAt: prepared.intent.expiresAt, minimumOutput: prepared.minimumOutput };
    const simulation = await client.simulateContract({ address: target, account, abi: advancedOrdersAbi, functionName: 'placeOrder', args: [request] });
    if (await owner() !== account) throw new Error('Wallet account changed before placement');
    const hash = await wallet.writeContract({ ...simulation.request, account, chain: wallet.chain });
    issuedOrders.delete(prepared);
    const result = await receipt(hash); if (result.status !== 'success') throw new Error('Order placement reverted');
    const created = result.logs.filter(log => address(log.address) === target).map(log => { try { return decodeEventLog({ abi: advancedOrdersAbi, data: log.data, topics: log.topics }); } catch { return null; } }).filter(log => log?.eventName === 'LimitOrderCreated' || log?.eventName === 'StopLimitOrderCreated');
    if (created.length !== 1 || !created[0] || !('user' in created[0].args) || !('orderId' in created[0].args) || address(created[0].args.user) !== account) throw new Error('Order placement receipt evidence missing');
    return { hash, orderId: created[0].args.orderId, creationBlock: result.blockNumber, family: created[0].eventName === 'LimitOrderCreated' ? 'limit' as const : 'stop' as const };
  };
  const order = async (id: bigint, family: 'limit' | 'stop', creationBlock: bigint): Promise<ConnectedOrder> => {
    const account = await owner(), target = await required('orders'), finalized = await block();
    uint(id, true); uint(creationBlock);
    if (creationBlock < BigInt(manifest.deploymentBlock) || creationBlock > finalized.number) throw new Error('Order creation block outside admitted history');
    const output = family === 'limit'
      ? await client.readContract({ address: target, abi: advancedOrdersAbi, functionName: 'getLimitOrder', args: [id], blockNumber: finalized.number })
      : await client.readContract({ address: target, abi: advancedOrdersAbi, functionName: 'getStopLimitOrder', args: [id], blockNumber: finalized.number });
    const raw = decodeDeployedOrder(output, family);
    if (raw.id !== id || address(raw.user) !== account || !kinds[raw.orderType] || raw.status < 0 || raw.status > 4) throw new Error('Order ownership or deployed enum mismatch');
    const state = (['active', 'triggered', 'filled', 'cancelled', 'expired'] as const)[raw.status];
    let filledRaw = 0n, refundRaw = 0n, executionTxHash: string | undefined;
    let eventRevision = creationBlock << 32n;
    if (state === 'triggered') {
      if (finalized.number - creationBlock > 2000n) throw new Error('Activation history requires admitted indexed provider');
      const activations = await client.getLogs({ address: target, event: parseAbiItem('event StopLimitActivated(uint256 indexed orderId,uint256 activationPrice)'), args: { orderId: id }, fromBlock: creationBlock, toBlock: finalized.number, strict: true });
      if (activations.length !== 1 || activations[0].blockHash !== (await client.getBlock({ blockNumber: activations[0].blockNumber })).hash) throw new Error('Canonical activation evidence unavailable');
      eventRevision = activations[0].blockNumber << 32n | BigInt(activations[0].logIndex);
    }
    if (raw.status >= 2) {
      if (finalized.number - creationBlock > 2000n) throw new Error('Canonical terminal evidence requires admitted indexed history provider');
      const logs = await client.getLogs({ address: target, fromBlock: creationBlock, toBlock: finalized.number });
      const terminalLogs = logs.flatMap(log => {
        try { const decoded = decodeEventLog({ abi: advancedOrdersAbi, data: log.data, topics: log.topics });
          const expected = raw.status === 2 ? family === 'limit' ? 'LimitOrderExecuted' : 'StopLimitExecuted' : raw.status === 3 ? family === 'limit' ? 'LimitOrderCancelled' : 'StopLimitCancelled' : 'OrderExpiredAndRefunded';
          return decoded.eventName === expected && 'orderId' in decoded.args && decoded.args.orderId === id ? [log] : [];
        } catch { return []; }
      });
      if (terminalLogs.length !== 1) throw new Error('Unique committed terminal order evidence unavailable');
      const committed = await client.getTransactionReceipt({ hash: terminalLogs[0].transactionHash });
      if (committed.status !== 'success' || committed.blockHash !== (await client.getBlock({ blockNumber: committed.blockNumber })).hash || committed.blockNumber > finalized.number) throw new Error('Uncommitted terminal order receipt');
      const receiptTerminalCount = committed.logs.filter(log => log.address.toLowerCase() === target).flatMap(log => { try { const event = decodeEventLog({ abi: advancedOrdersAbi, data: log.data, topics: log.topics }); return ['LimitOrderExecuted','StopLimitExecuted','LimitOrderCancelled','StopLimitCancelled','OrderExpiredAndRefunded'].includes(event.eventName) ? [event] : []; } catch { return []; } }).length;
      if (receiptTerminalCount !== 1) throw new Error('Batch settlement requires admitted per-order transfer attribution');
      const buy = raw.orderType === 0 || raw.orderType === 3;
      const asset = raw.status === 2 ? buy ? raw.token : raw.stablecoin : buy ? raw.stablecoin : raw.token;
      const vault = await checkedBinding(manifest.vault, 'escrow vault');
      let delivered = 0n;
      for (const log of committed.logs) {
        if (address(log.address) !== address(asset)) continue;
        try {
          const transfer = decodeEventLog({ abi: erc20Abi, data: log.data, topics: log.topics });
          if (transfer.eventName === 'Transfer' && address(transfer.args.from) === vault && address(transfer.args.to) === account) delivered += transfer.args.value;
        } catch { /* Other ERC20 events cannot establish a transfer. */ }
      }
      if (delivered <= 0n || raw.status !== 2 && delivered !== raw.amount) throw new Error('Exact escrow settlement transfer unavailable');
      if (raw.status === 2) { filledRaw = delivered; executionTxHash = committed.transactionHash; }
      else refundRaw = delivered;
      eventRevision = committed.blockNumber << 32n | BigInt(terminalLogs[0].logIndex);
    }
    let eligible: boolean | null = null;
    if (state === 'active' || state === 'triggered') {
      try {
        const oracle = await required('oracle');
        const quote = await client.readContract({ address: oracle, abi: quoterAbi, functionName: 'getTokenPrice', args: [raw.token], blockNumber: finalized.number });
        const fields = abiFields(quote, ['price','timestamp','isStale']), price = abiUint(fields[0]), timestamp = abiUint(fields[1]), stale = abiBoolean(fields[2]);
        if (price > 0n && !stale && timestamp <= finalized.timestamp && finalized.timestamp - timestamp <= 30n) {
          if (raw.family === 'limit') eligible = raw.orderType === 0 || raw.orderType === 2 ? price <= raw.targetPrice : price >= raw.targetPrice;
          else if (state === 'active') eligible = raw.orderType === 3 ? price >= raw.stopPrice : price <= raw.stopPrice;
          else eligible = raw.orderType === 3 ? price <= raw.limitPrice : price >= raw.limitPrice;
        }
      } catch { eligible = null; }
    }
    let execution = { healthy: false, checkedAt: 0 };
    try { execution = await health(); } catch { /* Keep actual contract state when execution health is unavailable. */ }
    if (await owner() !== account) throw new Error('Account changed during order reconciliation');
    return Object.freeze({ orderId: id.toString(), owner: account, deploymentRevision: manifest.revision, eventRevision, state, filledRaw, refundRaw, executionTxHash, family, token: address(raw.token), settlementAsset: address(raw.stablecoin), amountRaw: raw.amount, type: kinds[raw.orderType], expiresAt: raw.expiresAt, eligible, checkedAt: Date.now(), keeperHealthy: execution.healthy, keeperCheckedAt: execution.checkedAt, creationBlock });
  };
  const orderPage = async (cursor: string | null = null, size = 25): Promise<OwnedOrderPage> => {
    orderPageSize(size);
    const account = await owner(), target = await required('orders'), finalized = await block();
    let from = BigInt(manifest.deploymentBlock), after = -1;
    if (cursor) {
      if (cursor.length > 1024) throw new Error('Invalid order cursor');
      const value = JSON.parse(cursor);
      if (!value || typeof value !== 'object' || value.revision !== manifest.revision || value.owner !== account || !/^\d+$/.test(value.block) || !Number.isSafeInteger(value.logIndex) || value.logIndex < -1 || BigInt(value.block) < BigInt(manifest.deploymentBlock)) throw new Error('Order cursor account/deployment mismatch');
      from = BigInt(value.block); after = value.logIndex;
    }
    if (from > finalized.number) return { items: [], nextCursor: null };
    const end = from + 1999n < finalized.number ? from + 1999n : finalized.number;
    const [limits, stops] = await Promise.all([
      client.getLogs({ address: target, event: parseAbiItem('event LimitOrderCreated(uint256 indexed orderId,address indexed user,uint8 orderType,uint256 amount,uint256 targetPrice)'), args: { user: account }, fromBlock: from, toBlock: end, strict: true }),
      client.getLogs({ address: target, event: parseAbiItem('event StopLimitOrderCreated(uint256 indexed orderId,address indexed user,uint8 orderType,uint256 amount,uint256 stopPrice,uint256 limitPrice)'), args: { user: account }, fromBlock: from, toBlock: end, strict: true }),
    ]);
    const records = [...limits.map(log => ({ log, family: 'limit' as const })), ...stops.map(log => ({ log, family: 'stop' as const }))].filter(({ log }) => log.blockNumber > from || log.logIndex > after).sort((a, b) => a.log.blockNumber === b.log.blockNumber ? a.log.logIndex - b.log.logIndex : a.log.blockNumber < b.log.blockNumber ? -1 : 1);
    const selected = records.slice(0, size);
    const items = await Promise.all(selected.map(({ log, family }) => {
      const decoded = decodeEventLog({ abi: advancedOrdersAbi, data: log.data, topics: log.topics });
      if (!('orderId' in decoded.args)) throw new Error('Invalid creation evidence');
      return order(decoded.args.orderId, family, log.blockNumber);
    }));
    const last = selected.at(-1);
    const nextCursor = records.length > size && last ? JSON.stringify({ revision: manifest.revision, owner: account, block: last.log.blockNumber.toString(), logIndex: last.log.logIndex }) : end < finalized.number ? JSON.stringify({ revision: manifest.revision, owner: account, block: (end + 1n).toString(), logIndex: -1 }) : null;
    if (await owner() !== account) throw new Error('Account changed during order page');
    return { items, nextCursor };
  };
  const attemptKey = (account: Address, family: string, id: string) => `sidiora:advanced-cancel:2:125:${manifest.revision}:${account}:${family}:${id}`;
  const pendingCancel = async (item: ConnectedOrder): Promise<CancelAttempt | null> => {
    const account = await owner();
    if (item.owner !== account || item.deploymentRevision !== manifest.revision) throw new Error('Cancellation recovery account mismatch');
    const key = attemptKey(account, item.family, item.orderId);
    if (volatileCancels.has(key)) return volatileCancels.get(key)!;
    const text = options.attempts.getItem(key);
    if (text !== null) return decodeCancelAttempt(text, account, item.orderId);
    const stored = new PendingAttemptStore(options.attempts, account).load();
    if (stored.corrupt) throw new Error('Corrupt cancellation recovery storage');
    const candidates = stored.records.filter(value => value.action === 'cancel-order' && value.intentId === `${manifest.revision}:${item.family}:${item.orderId}`.slice(0,128));
    if (candidates.length > 1) throw new Error('Conflicting cancellation attempts require reconciliation');
    if (!candidates[0]) return null;
    const fingerprint = JSON.parse(candidates[0].actionFingerprint);
    if (fingerprint.target !== address(manifest.orders!.address) || fingerprint.family !== item.family || fingerprint.id !== item.orderId || !/^\d+$/.test(fingerprint.revision)) throw new Error('Cancellation recovery identity mismatch');
    const recovered: CancelAttempt = { orderId: item.orderId, owner: account, preStateRevision: BigInt(fingerprint.revision), txHash: candidates[0].txHash, receiptStatus: 'unknown' };
    options.attempts.setItem(attemptKey(account,item.family,item.orderId),encodeCancelAttempt(recovered)); return recovered;
  };
  const cancel = async (item: ConnectedOrder) => {
    const key = attemptKey(address(item.owner), item.family, item.orderId);
    if (cancellationDispatches.has(key)) throw new Error('Cancellation already signing; resume existing attempt');
    cancellationDispatches.add(key);
    try {
    const account = await owner(), target = await required('orders');
    if (await pendingCancel(item)) throw new Error('Cancellation receipt unresolved; resume existing hash');
    const fresh = await order(BigInt(item.orderId), item.family, item.creationBlock);
    if (fresh.owner !== account || !['active', 'triggered'].includes(fresh.state)) throw new Error('Order is not cancellable by connected owner');
    const simulation = await client.simulateContract({ account, address: target, abi: advancedOrdersAbi, functionName: item.family === 'limit' ? 'cancelLimitOrder' : 'cancelStopLimitOrder', args: [BigInt(item.orderId)] });
    if (await owner() !== account) throw new Error('Cancellation account changed');
    const beforeSignature = await order(BigInt(item.orderId), item.family, item.creationBlock);
    if (beforeSignature.owner !== account || !['active', 'triggered'].includes(beforeSignature.state)) throw new Error('Order filled or ownership changed before cancellation signature');
    const hash = await wallet.writeContract({ ...simulation.request, account, chain: wallet.chain });
    const attempt: CancelAttempt = { orderId: item.orderId, owner: account, preStateRevision: fresh.eventRevision, txHash: hash, receiptStatus: 'unknown' };
    volatileCancels.set(key, attempt);
    options.attempts.setItem(key, encodeCancelAttempt(attempt));
    new PendingAttemptStore(options.attempts, account).save({ version: 1, chainId: 125, from: account, txHash: hash, submittedAt: Date.now(), intentId: `${manifest.revision}:${item.family}:${item.orderId}`.slice(0,128), action: 'cancel-order', actionFingerprint: JSON.stringify({ target, family: item.family, id: item.orderId, revision: fresh.eventRevision.toString() }) });
    return attempt;
    } finally { cancellationDispatches.delete(key); }
  };
  const recoverCancel = async (item: ConnectedOrder) => {
    const attempt = await pendingCancel(item); if (!attempt) throw new Error('No submitted cancellation');
    let receiptStatus: CancelAttempt['receiptStatus'] = 'unknown';
    try { receiptStatus = (await receipt(attempt.txHash as Hex)).status; }
    catch { receiptStatus = 'unknown'; }
    const updated: CancelAttempt = { ...attempt, receiptStatus };
    const latest = await order(BigInt(item.orderId), item.family, item.creationBlock);
    volatileCancels.set(attemptKey(attempt.owner, item.family, item.orderId), updated);
    const outcome = cancellationOutcome(updated, latest);
    options.attempts.setItem(attemptKey(attempt.owner, item.family, item.orderId), encodeCancelAttempt(updated));
    if (outcome.state !== 'checking') { volatileCancels.delete(attemptKey(attempt.owner, item.family, item.orderId)); options.attempts.removeItem(attemptKey(attempt.owner, item.family, item.orderId)); new PendingAttemptStore(options.attempts, attempt.owner).remove(attempt.txHash); }
    return { attempt: updated, order: latest, outcome };
  };
  const adapterAbi = parseAbi(['function getQuote(address tokenIn,address tokenOut,uint256 amountIn) view returns ((uint256 amountOut,uint256 priceImpactBps,uint256 feeBps,uint256 feeAmount,bool available,bytes adapterData))']);
  const quoteLatenciesMs: number[] = [];
  let quoteGeneration = 0;
  const quoteInner = async (tokenIn: Address, tokenOut: Address, amount: string, overallDeadline: number, generation: number): Promise<RouteQuote | null> => {
    const account = await owner(); await required('routes');
    if (address(tokenIn) === address(tokenOut)) throw new Error('Route assets must differ');
    const amountInRaw = (await import('./domain')).exactUnits(amount, await decimals(tokenIn));
    await decimals(tokenOut);
    const registered = await adapters(), quoteBlock = await block();
    const expiresAt = Date.now() + 5000;
    if (registered.length > 8) throw new Error('Quote fanout exceeds eight adapters');
    const context = { revision: manifest.revision, tokenIn: address(tokenIn), tokenOut: address(tokenOut), amountInRaw, recipient: account };
    const boundedAdapter = async <T>(work: Promise<T>): Promise<T> => {
      let timer: ReturnType<typeof setTimeout> | undefined;
      try { return await Promise.race([work, new Promise<never>((_, reject) => { timer = setTimeout(() => reject(new Error('Adapter quote deadline exceeded')), Math.max(0,overallDeadline - performance.now() - 80)); })]); }
      finally { clearTimeout(timer); }
    };
    const diagnostics = new Map(adapterDiagnostics);
    const results = await Promise.allSettled(registered.map(adapter => boundedAdapter((async () => {
        const binding = manifest.adapters.find(item => item.id.toLowerCase() === adapter.id.toLowerCase() && address(item.address) === adapter.address)!;
        const result = await client.readContract({ address: adapter.address, abi: adapterAbi, functionName: 'getQuote', args: [tokenIn, tokenOut, amountInRaw], blockNumber: quoteBlock.number });
        for (const field of [result.amountOut, result.priceImpactBps, result.feeBps, result.feeAmount]) abiUint(field);
        abiBoolean(result.available); abiHex(result.adapterData);
        if (!result.available || result.amountOut <= 0n) { diagnostics.set(adapter.id, 'Adapter quote unavailable for exact inputs'); return null; }
        if (result.feeBps > 10000n) throw new Error('Adapter fee exceeds basis point bound');
        const fields = binding.adapterData === 'vault-v2'
          ? decodeAbiParameters(parseAbiParameters('address,address'), result.adapterData)
          : decodeAbiParameters(parseAbiParameters('uint8,address,address'), result.adapterData);
        const decoded = validateAdapterData(binding.adapterData, result.adapterData, fields, tokenIn, tokenOut, manifest.usdl);
        if (binding.adapterData === 'sidiora-v1') {
          const registry = await client.readContract({ address: adapter.address, abi: parseAbi(['function poolRegistry() view returns (address)']), functionName: 'poolRegistry', blockNumber: quoteBlock.number });
          const tokenAssets = address(tokenIn) === address(manifest.usdl) ? [tokenOut] : address(tokenOut) === address(manifest.usdl) ? [tokenIn] : [tokenIn,tokenOut];
          for (let index = 0; index < tokenAssets.length; index++) {
            const pool = await client.readContract({ address: address(registry), abi: parseAbi(['function getPoolByToken(address token) view returns (address)']), functionName: 'getPoolByToken', args: [tokenAssets[index]], blockNumber: quoteBlock.number });
            if (address(pool) !== decoded.pools[index]) throw new Error('Adapter data pool does not match exact asset registry');
          }
        }
        const feeAsset = decoded.feeAsset;
        return Object.freeze({ ...context, adapterId: adapter.id, adapterAddress: adapter.address, outputRaw: result.amountOut, outputBasis: 'net' as const, feeRaw: result.feeAmount, feeAsset, expiresAt, blockHeight: quoteBlock.number });
      })())));
      results.forEach((result, index) => { if (result.status === 'rejected') diagnostics.set(registered[index].id, result.reason instanceof Error ? result.reason.message : String(result.reason)); });
      if (generation !== quoteGeneration) throw new Error('Quote inputs superseded; refresh exact route');
      adapterDiagnostics.clear(); diagnostics.forEach((reason, id) => adapterDiagnostics.set(id, reason));
      const valid = results.flatMap(result => result.status === 'fulfilled' && result.value ? [result.value] : []);
      const best = selectBestRoute(valid, context, registered, Date.now());
      if (await owner() !== account) throw new Error('Quote account changed');
      if (generation !== quoteGeneration || performance.now() >= overallDeadline) throw new Error('Route quote deadline exceeded');
      if (best) issuedQuotes.add(best); return best;
  };
  const quote = async (tokenIn: Address, tokenOut: Address, amount: string): Promise<RouteQuote | null> => {
    const started = performance.now(), generation = ++quoteGeneration;
    issuedQuotes = new WeakSet<object>();
    let timer: ReturnType<typeof setTimeout> | undefined;
    try { return await Promise.race([quoteInner(tokenIn, tokenOut, amount, started + 750, generation), new Promise<never>((_, reject) => { timer = setTimeout(() => reject(new Error('Route quote deadline exceeded')), 750); })]); }
    finally { clearTimeout(timer); quoteLatenciesMs.push(performance.now() - started); if (quoteLatenciesMs.length > 1000) quoteLatenciesMs.shift(); }
  };
  const executeRoute = async (value: RouteQuote, minimum: bigint, deadline: bigint) => {
    if (!issuedQuotes.has(value) || value.expiresAt <= Date.now()) throw new Error('Reviewed route expired or is not provider-issued');
    issuedQuotes.delete(value);
    const account = await owner(), target = await required('routes');
    if (account !== value.recipient || deadline <= BigInt(Math.floor(Date.now() / 1000)) || deadline > BigInt(Math.floor(Date.now() / 1000)) + 1800n) throw new Error('Route context changed');
    uint(minimum, true); if (minimum > routeNetOutput(value)) throw new Error('Minimum exceeds reviewed output');
    const active = await adapters();
    if (!active.some(item => item.id === value.adapterId && item.address === value.adapterAddress)) throw new Error('Quoted adapter deactivated; refresh route');
    await approve(value.tokenIn, target, value.amountInRaw);
    const rechecked = await adapters();
    if (!rechecked.some(item => item.id === value.adapterId && item.address === value.adapterAddress) || await owner() !== account) throw new Error('Adapter or account changed after approval');
    const simulation = await client.simulateContract({ account, address: target, abi: routerAbi, functionName: 'swapViaAdapter', args: [value.adapterId as Hex, value.tokenIn, value.tokenOut, value.amountInRaw, minimum, deadline] });
    if (value.expiresAt <= Date.now() || await owner() !== account) throw new Error('Route review expired or account changed before signature');
    if (!(await adapters()).some(item => item.id === value.adapterId && item.address === value.adapterAddress)) throw new Error('Adapter deactivated before signature');
    const hash = await wallet.writeContract({ ...simulation.request, account, chain: wallet.chain }); issuedQuotes.delete(value);
    const result = await receipt(hash); if (result.status !== 'success') throw new Error('Route execution reverted');
    const settlements = result.logs.filter(log => log.address.toLowerCase() === target).flatMap(log => { try { const event = decodeEventLog({ abi: routerAbi, data: log.data, topics: log.topics }); return event.eventName === 'BestRouteSwap' && address(event.args.user) === account && address(event.args.tokenIn) === address(value.tokenIn) && address(event.args.tokenOut) === address(value.tokenOut) && event.args.amountIn === value.amountInRaw && event.args.adapterId.toLowerCase() === value.adapterId.toLowerCase() && event.args.amountOut >= minimum ? [event] : []; } catch { return []; } });
    if (settlements.length !== 1) throw new Error('Exact reviewed route settlement event unavailable'); return result;
  };
  const prepareCustomRoute = async (hops: readonly Hop[], inputRaw: bigint, finalMinimumRaw: bigint, deadline: bigint) => {
    const account = await owner(), target = await required('routes'), current = await block(), active = await adapters();
    const verified: Hop[] = [];
    for (const hop of hops) verified.push({ ...hop, inputDecimals: await decimals(hop.tokenIn), outputDecimals: await decimals(hop.tokenOut) });
    const contextHash = validateCustomRoute(verified, active, inputRaw, finalMinimumRaw, deadline, current.timestamp);
    for (const hop of verified) {
      const supports = await client.readContract({ address: hop.adapterAddress, abi: parseAbi(['function supportsSwap(address tokenIn,address tokenOut) view returns (bool)']), functionName: 'supportsSwap', args: [hop.tokenIn,hop.tokenOut] });
      if (!abiBoolean(supports)) throw new Error('Unsupported hop asset pair');
    }
    const args = [verified.map(hop => ({ tokenIn: hop.tokenIn, tokenOut: hop.tokenOut, adapterId: hop.adapterId as Hex, minAmountOut: hop.minAmountOut })), inputRaw, finalMinimumRaw, deadline] as const;
    const allowance = await client.readContract({ address: verified[0].tokenIn, abi: erc20Abi, functionName: 'allowance', args: [account,target] });
    const simulation = allowance >= inputRaw ? await client.simulateContract({ account, address: target, abi: routerAbi, functionName: 'swapMultiHop', args }) : null;
    const prepared = Object.freeze({ account, revision: manifest.revision, hops: Object.freeze(verified.map(hop => Object.freeze(hop))), inputRaw, finalMinimumRaw, deadline, contextHash, simulation });
    freeze(prepared); issuedRoutes.add(prepared); return prepared;
  };
  type PreparedCustom = Awaited<ReturnType<typeof prepareCustomRoute>>;
  const approveCustomRoute = async (prepared: PreparedCustom) => {
    if (!issuedRoutes.has(prepared) || prepared.account !== await owner()) throw new Error('Custom route approval review changed');
    issuedRoutes.delete(prepared);
    const target = await required('routes');
    validateCustomRoute(prepared.hops, await adapters(), prepared.inputRaw, prepared.finalMinimumRaw, prepared.deadline, (await block()).timestamp);
    await approve(prepared.hops[0].tokenIn, target, prepared.inputRaw);
    const fresh = await prepareCustomRoute(prepared.hops, prepared.inputRaw, prepared.finalMinimumRaw, prepared.deadline);
    issuedRoutes.delete(prepared); if (!fresh.simulation) throw new Error('Custom route allowance unavailable'); return fresh;
  };
  const executeCustomRoute = async (prepared: PreparedCustom) => {
    if (!issuedRoutes.has(prepared) || prepared.account !== await owner() || prepared.revision !== manifest.revision) throw new Error('Custom route review changed');
    if (!prepared.simulation) throw new Error('Custom route requires successful full simulation');
    issuedRoutes.delete(prepared);
    const fresh = await prepareCustomRoute(prepared.hops, prepared.inputRaw, prepared.finalMinimumRaw, prepared.deadline);
    if (fresh.contextHash !== prepared.contextHash || await owner() !== prepared.account) throw new Error('Custom route context changed');
    if (!fresh.simulation) throw new Error('Allowance changed; simulate again');
    const hash = await wallet.writeContract({ ...fresh.simulation.request, account: prepared.account, chain: wallet.chain }); issuedRoutes.delete(prepared); issuedRoutes.delete(fresh);
    const result = await receipt(hash); if (result.status !== 'success') throw new Error('Custom route reverted');
    const settlements = result.logs.filter(log => log.address.toLowerCase() === address(manifest.router!.address)).flatMap(log => { try { const event = decodeEventLog({ abi: routerAbi, data: log.data, topics: log.topics }); return event.eventName === 'MultiHopSwap' && address(event.args.user) === prepared.account && address(event.args.tokenIn) === address(prepared.hops[0].tokenIn) && address(event.args.tokenOut) === address(prepared.hops.at(-1)!.tokenOut) && event.args.amountIn === prepared.inputRaw && event.args.amountOut >= prepared.finalMinimumRaw && event.args.hops === BigInt(prepared.hops.length) ? [event] : []; } catch { return []; } });
    if (settlements.length !== 1) throw new Error('Exact custom route settlement event unavailable'); return result;
  };
  return Object.freeze({ revision: manifest.revision, manifestSha256: options.manifestSha256, manifest, blockers, adapterDiagnostics, quoteLatenciesMs, owner, health, decimals, adapters, prepareOrder, placeOrder, order, orderPage, pendingCancel, cancel, recoverCancel, quote, executeRoute, prepareCustomRoute, approveCustomRoute, executeCustomRoute, receipt });
}
export type AdvancedProvider = Awaited<ReturnType<typeof createAdvancedProvider>>;
