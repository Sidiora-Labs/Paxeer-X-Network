import { JsonRpcClient, JsonRpcError, JsonRpcTransportError, isRecord, type JsonRpcClientOptions } from './rpc.js';
import type {
  AccountBalances,
  AnchorHead,
  AssetMap,
  Capabilities,
  CompletedBalance,
  CompletionAsset,
  CustodyAssetRecord,
  HistoryItem,
  HistoryPage,
  HistoryQuery,
  JoinedAccount,
  JoinedAsset,
  JoinedBalanceRow,
  JoinedBalances,
  JsonRpcCall,
  JsonRpcOutcome,
  KernelReason,
  KernelStatus,
  NetworkHead,
  UnifiedAccountDocument,
} from './types.js';

export const DEFAULT_ENDPOINT_URL = 'https://api-mainnet-beta.paxeer.network';

export const ERC20_BALANCE_OF_SELECTOR = '0x70a08231';

const KERNEL_REASONS: readonly KernelReason[] = ['available', 'not_configured', 'unreachable', 'no_finalised_checkpoint'];
const HEX_QUANTITY = /^0x[0-9a-fA-F]{1,64}$/;
const HEX_WORD = /^0x[0-9a-fA-F]{64}$/;
const EVM_ADDRESS = /^0x[0-9a-fA-F]{40}$/;
const DECIMAL = /^(0|[1-9][0-9]*)$/;
const HISTORY_CURSOR = /^[0-9A-Za-z]{1,32}$/;
const HISTORY_KIND = /^[a-z0-9_]{1,64}$/;

export class EndpointClient {
  readonly rpc: JsonRpcClient;

  constructor(options: (Omit<JsonRpcClientOptions, 'url'> & { url?: string }) | JsonRpcClient = {}) {
    this.rpc = options instanceof JsonRpcClient
      ? options
      : new JsonRpcClient({ ...options, url: options.url ?? DEFAULT_ENDPOINT_URL });
  }

  batch(calls: readonly JsonRpcCall[]): Promise<JsonRpcOutcome[]> {
    return this.rpc.batch(calls);
  }

  async resolveAccount(account: string): Promise<UnifiedAccountDocument> {
    return decodeAccountDocument(await this.rpc.call('px_resolveAccount', [account]));
  }

  async getAccount(account: string): Promise<JoinedAccount> {
    return decodeJoinedAccount(await this.rpc.call('px_getAccount', [account]));
  }

  async listAssets(): Promise<AssetMap> {
    return decodeAssetMap(await this.rpc.call('px_listAssets', []));
  }

  async getCapabilities(): Promise<Capabilities> {
    return decodeCapabilities(await this.rpc.call('px_getCapabilities', []));
  }

  async getNetwork(): Promise<NetworkHead> {
    return decodeNetwork(await this.rpc.call('px_getNetwork', []));
  }

  async getBalances(account: string, assets: readonly CompletionAsset[] = []): Promise<AccountBalances> {
    const [balancesOutcome, mapOutcome] = await this.rpc.batch([
      { method: 'px_getBalances', params: [account] },
      { method: 'px_listAssets', params: [] },
    ]);
    const joined = decodeJoinedBalances(unwrap(balancesOutcome));
    const assetMap = decodeAssetMap(unwrap(mapOutcome));
    const owner = joined.account.evm_address;
    const unmapped = owner === null ? [] : assets.filter((asset) => !inCustodyMap(asset, assetMap));
    const completed = owner === null ? [] : await this.complete(owner, unmapped);
    return {
      ...joined,
      asset_map: assetMap,
      join_limit_reached: assetMap.assets.length >= assetMap.joined_limit,
      completed,
    };
  }

  async getUnifiedHistory(account: string, cursor: string | null = null, query: HistoryQuery = {}): Promise<HistoryPage> {
    if (cursor !== null && !HISTORY_CURSOR.test(cursor)) throw new RangeError('history cursor is malformed');
    if (query.limit !== undefined && (!Number.isInteger(query.limit) || query.limit < 1 || query.limit > 100)) {
      throw new RangeError('history limit must be an integer from 1 to 100');
    }
    if (query.kind !== undefined && !HISTORY_KIND.test(query.kind)) throw new RangeError('history kind is malformed');
    return decodeHistoryPage(
      await this.rpc.call('px_getUnifiedHistory', [account, cursor, query.limit ?? null, query.kind ?? null]),
    );
  }

  async *historyPages(account: string, query: HistoryQuery = {}): AsyncGenerator<HistoryPage, void, undefined> {
    let cursor: string | null = null;
    const seen = new Set<string>();
    for (;;) {
      const page: HistoryPage = await this.getUnifiedHistory(account, cursor, query);
      yield page;
      if (page.next_cursor === null) return;
      if (seen.has(page.next_cursor)) throw new JsonRpcTransportError('history cursor repeated');
      seen.add(page.next_cursor);
      cursor = page.next_cursor;
    }
  }

  async *history(account: string, query: HistoryQuery = {}): AsyncGenerator<HistoryItem, void, undefined> {
    for await (const page of this.historyPages(account, query)) {
      yield* page.items;
    }
  }

  private async complete(owner: `0x${string}`, assets: readonly CompletionAsset[]): Promise<CompletedBalance[]> {
    const calls: JsonRpcCall[] = assets.map((asset) =>
      asset.kind === 'native'
        ? { method: 'eth_getBalance', params: [owner, 'latest'] }
        : { method: 'eth_call', params: [{ to: asset.address, data: balanceOfCalldata(owner) }, 'latest'] },
    );
    const outcomes = await this.rpc.batch(calls);
    return assets.map((asset, index) => {
      const outcome = outcomes[index] as JsonRpcOutcome;
      const source = asset.kind === 'native' ? 'eth_getBalance' : 'erc20_balanceOf';
      if (!outcome.ok) return { asset, source, amount: null, error: outcome.error };
      const pattern = asset.kind === 'native' ? HEX_QUANTITY : HEX_WORD;
      if (typeof outcome.result !== 'string' || !pattern.test(outcome.result)) {
        return { asset, source, amount: null, error: { code: -32603, message: `undecodable ${source} answer` } };
      }
      return { asset, source, amount: BigInt(outcome.result).toString(), error: null };
    });
  }
}

export function balanceOfCalldata(owner: string): `0x${string}` {
  if (!EVM_ADDRESS.test(owner)) throw new RangeError('owner is not an EVM address');
  return `${ERC20_BALANCE_OF_SELECTOR}${owner.slice(2).toLowerCase().padStart(64, '0')}`;
}

export function inCustodyMap(asset: CompletionAsset, map: AssetMap): boolean {
  if (asset.kind === 'erc20') {
    const address = asset.address.toLowerCase();
    return map.assets.some((entry) => entry.paxeer !== null && entry.paxeer.pointer.toLowerCase() === address);
  }
  const denom = asset.denom;
  if (denom === undefined) return false;
  return map.assets.some((entry) => entry.paxeer !== null && entry.paxeer.denom === denom);
}

function unwrap(outcome: JsonRpcOutcome | undefined): unknown {
  if (outcome === undefined) throw new JsonRpcTransportError('batch answer missing');
  if (!outcome.ok) throw new JsonRpcError(outcome.error);
  return outcome.result;
}

function fail(what: string): never {
  throw new JsonRpcTransportError(`malformed ${what}`);
}

function record(value: unknown, what: string): Record<string, unknown> {
  if (!isRecord(value)) fail(what);
  return value;
}

function nullableString(value: unknown, what: string): string | null {
  if (value === null) return null;
  if (typeof value !== 'string') fail(what);
  return value;
}

function nullableRecord(value: unknown, what: string): Record<string, unknown> | null {
  if (value === null) return null;
  return record(value, what);
}

function decimalString(value: unknown, what: string): string {
  if (typeof value !== 'string' || !DECIMAL.test(value)) fail(what);
  return value;
}

function hexQuantity(value: unknown, what: string): `0x${string}` {
  if (typeof value !== 'string' || !HEX_QUANTITY.test(value)) fail(what);
  return value as `0x${string}`;
}

function evmAddress(value: unknown, what: string): `0x${string}` {
  if (typeof value !== 'string' || !EVM_ADDRESS.test(value)) fail(what);
  return value as `0x${string}`;
}

function side(value: unknown, what: string): 'layerx' | 'paxeer' {
  if (value !== 'layerx' && value !== 'paxeer') fail(what);
  return value;
}

export function decodeAccountDocument(value: unknown): UnifiedAccountDocument {
  const doc = record(value, 'account document');
  if (typeof doc.bound !== 'boolean') fail('account document bound');
  return {
    evm_address: doc.evm_address === null ? null : evmAddress(doc.evm_address, 'account evm_address'),
    pax_address: nullableString(doc.pax_address, 'account pax_address'),
    layerx_did: nullableString(doc.layerx_did, 'account layerx_did'),
    layerx_account: nullableString(doc.layerx_account, 'account layerx_account'),
    bound: doc.bound,
  };
}

export function decodeJoinedAccount(value: unknown): JoinedAccount {
  const doc = record(value, 'joined account');
  let paxeer: JoinedAccount['paxeer'] = null;
  if (doc.paxeer !== null) {
    const half = record(doc.paxeer, 'paxeer half');
    paxeer = {
      address: evmAddress(half.address, 'paxeer address'),
      balance: hexQuantity(half.balance, 'paxeer balance'),
      nonce: hexQuantity(half.nonce, 'paxeer nonce'),
    };
  }
  return {
    account: decodeAccountDocument(doc.account),
    paxeer,
    layerx: nullableRecord(doc.layerx, 'layerx half'),
  };
}

function decodeCustody(value: unknown): CustodyAssetRecord | null {
  if (value === null) return null;
  const doc = record(value, 'custody record');
  if (typeof doc.asset_id !== 'string' || typeof doc.denom !== 'string') fail('custody record');
  if (typeof doc.enabled !== 'boolean' || typeof doc.paused !== 'boolean') fail('custody flags');
  return {
    asset_id: doc.asset_id,
    denom: doc.denom,
    pointer: evmAddress(doc.pointer, 'custody pointer'),
    enabled: doc.enabled,
    paused: doc.paused,
    minimum_deposit: decimalString(doc.minimum_deposit, 'custody minimum_deposit'),
    custody_cap: decimalString(doc.custody_cap, 'custody custody_cap'),
    custodied: decimalString(doc.custodied, 'custody custodied'),
    released: decimalString(doc.released, 'custody released'),
    pending: decimalString(doc.pending, 'custody pending'),
  };
}

function joinedLimit(value: unknown): number {
  if (typeof value !== 'number' || !Number.isInteger(value) || value < 1) fail('joined_limit');
  return value;
}

export function decodeAssetMap(value: unknown): AssetMap {
  const doc = record(value, 'asset map');
  if (!Array.isArray(doc.assets)) fail('asset map assets');
  const assets: JoinedAsset[] = doc.assets.map((entry) => {
    const asset = record(entry, 'joined asset');
    if (typeof asset.asset_id !== 'string') fail('joined asset id');
    return { asset_id: asset.asset_id, layerx: record(asset.layerx, 'joined asset layerx'), paxeer: decodeCustody(asset.paxeer) };
  });
  return { assets, joined_limit: joinedLimit(doc.joined_limit) };
}

export function decodeJoinedBalances(value: unknown): JoinedBalances {
  const doc = record(value, 'joined balances');
  if (!Array.isArray(doc.balances)) fail('joined balances rows');
  const balances: JoinedBalanceRow[] = doc.balances.map((entry) => {
    const row = record(entry, 'balance row');
    if (typeof row.asset_id !== 'string') fail('balance row asset_id');
    let paxeer: JoinedBalanceRow['paxeer'] = null;
    if (row.paxeer !== null) {
      const half = record(row.paxeer, 'balance row paxeer');
      if (typeof half.denom !== 'string') fail('balance row paxeer denom');
      paxeer = { denom: half.denom, amount: decimalString(half.amount, 'balance row paxeer amount') };
    }
    return {
      asset_id: row.asset_id,
      denom: nullableString(row.denom, 'balance row denom'),
      custody: decodeCustody(row.custody),
      paxeer,
      layerx: nullableRecord(row.layerx, 'balance row layerx'),
    };
  });
  return { account: decodeAccountDocument(doc.account), balances, joined_limit: joinedLimit(doc.joined_limit) };
}

export function decodeCapabilities(value: unknown): Capabilities {
  const doc = record(value, 'capabilities');
  for (const key of ['exchange', 'bridge', 'launchpad'] as const) {
    if (typeof doc[key] !== 'boolean') fail(`capabilities ${key}`);
  }
  if (typeof doc.probed_at !== 'number' || !Number.isInteger(doc.probed_at) || doc.probed_at < 0) fail('capabilities probed_at');
  return {
    exchange: doc.exchange as boolean,
    bridge: doc.bridge as boolean,
    launchpad: doc.launchpad as boolean,
    probed_at: doc.probed_at,
    rpc_height: decimalString(doc.rpc_height, 'capabilities rpc_height'),
  };
}

export function decodeKernelStatus(value: unknown): KernelStatus {
  const doc = record(value, 'kernel status');
  if (typeof doc.available !== 'boolean') fail('kernel available');
  if (typeof doc.reason !== 'string' || !(KERNEL_REASONS as readonly string[]).includes(doc.reason)) fail('kernel reason');
  const reason = doc.reason as KernelReason;
  if (doc.available !== (reason === 'available')) fail('kernel availability and reason disagree');
  return { available: doc.available, reason };
}

function decodeAnchor(value: unknown): AnchorHead | null {
  if (value === null) return null;
  const doc = record(value, 'anchor head');
  const batch = doc.latest_finalized_batch;
  if (batch !== null && (typeof batch !== 'number' || !Number.isInteger(batch) || batch < 0)) fail('anchor batch');
  const status = doc.status;
  if (status !== null && (typeof status !== 'number' || !Number.isInteger(status))) fail('anchor status');
  const name = doc.status_name;
  if (name !== null && name !== 'unknown' && name !== 'submitted' && name !== 'final') fail('anchor status_name');
  const ladder = record(doc.status_ladder, 'anchor status_ladder');
  const statusLadder: Record<string, string> = {};
  for (const [key, rung] of Object.entries(ladder)) {
    if (typeof rung !== 'string') fail('anchor status_ladder');
    statusLadder[key] = rung;
  }
  return { latest_finalized_batch: batch, status, status_name: name, status_ladder: statusLadder };
}

export function decodeNetwork(value: unknown): NetworkHead {
  const doc = record(value, 'network head');
  if (typeof doc.network_id !== 'string') fail('network_id');
  const paxeer = record(doc.paxeer, 'network paxeer');
  const layerx = record(doc.layerx, 'network layerx');
  return {
    network_id: doc.network_id,
    paxeer: {
      chain_id: hexQuantity(paxeer.chain_id, 'network chain_id'),
      latest_block: hexQuantity(paxeer.latest_block, 'network latest_block'),
    },
    layerx: { node_info: nullableRecord(layerx.node_info, 'network node_info') },
    anchor: decodeAnchor(doc.anchor),
    kernel: decodeKernelStatus(doc.kernel),
  };
}

function decodeHistoryItem(value: unknown): HistoryItem {
  const item = record(value, 'history item');
  const text = (key: string): string => {
    if (typeof item[key] !== 'string') fail(`history item ${key}`);
    return item[key] as string;
  };
  if (!/^[1-9][0-9]*$/.test(text('id'))) fail('history item id');
  if (item.direction !== 'in' && item.direction !== 'out') fail('history item direction');
  if (typeof item.final !== 'boolean') fail('history item final');
  if (!('decoded' in item)) fail('history item decoded');
  let metadata: HistoryItem['asset_metadata'] = null;
  if (item.asset_metadata !== null) {
    const meta = record(item.asset_metadata, 'history asset_metadata');
    if (typeof meta.asset !== 'string' || typeof meta.kind !== 'string') fail('history asset_metadata');
    const decimals = meta.decimals;
    if (decimals !== null && (typeof decimals !== 'number' || !Number.isInteger(decimals) || decimals < 0)) {
      fail('history asset_metadata decimals');
    }
    metadata = {
      asset: meta.asset,
      chain: side(meta.chain, 'history asset_metadata chain'),
      kind: meta.kind,
      address: nullableString(meta.address, 'history asset_metadata address'),
      denom: nullableString(meta.denom, 'history asset_metadata denom'),
      symbol: nullableString(meta.symbol, 'history asset_metadata symbol'),
      decimals,
      native_id: nullableString(meta.native_id, 'history asset_metadata native_id'),
      pointer: nullableString(meta.pointer, 'history asset_metadata pointer'),
      metadata: meta.metadata,
    };
  }
  return {
    id: text('id'),
    height_or_seq: decimalString(item.height_or_seq, 'history item height_or_seq'),
    chain: side(item.chain, 'history item chain'),
    kind: text('kind'),
    direction: item.direction,
    account: text('account'),
    counterparty: nullableString(item.counterparty, 'history item counterparty'),
    asset: text('asset'),
    amount: decimalString(item.amount, 'history item amount'),
    tx_id: text('tx_id'),
    ordinal: decimalString(item.ordinal, 'history item ordinal'),
    final: item.final,
    decoded: item.decoded,
    asset_metadata: metadata,
    side: side(item.side, 'history item side'),
  };
}

export function decodeHistoryPage(value: unknown): HistoryPage {
  const doc = record(value, 'history page');
  if (!Array.isArray(doc.accounts) || doc.accounts.length > 16) fail('history accounts');
  if (!Array.isArray(doc.items) || doc.items.length > 100) fail('history items');
  const accounts = doc.accounts.map((entry) => {
    const account = record(entry, 'history account');
    if (typeof account.account !== 'string') fail('history account key');
    return { side: side(account.side, 'history account side'), account: account.account };
  });
  if (!('next_cursor' in doc)) fail('history next_cursor');
  const next = nullableString(doc.next_cursor, 'history next_cursor');
  if (next !== null && !HISTORY_CURSOR.test(next)) fail('history next_cursor');
  return {
    account: decodeAccountDocument(doc.account),
    accounts,
    items: doc.items.map(decodeHistoryItem),
    next_cursor: next,
  };
}
