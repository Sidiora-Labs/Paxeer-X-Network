import { readFileSync } from 'node:fs';
import { createServer, type IncomingMessage, type Server, type ServerResponse } from 'node:http';
import type { AddressInfo } from 'node:net';
import { secp256k1 } from '@noble/curves/secp256k1';
import { keccak_256 } from '@noble/hashes/sha3';
import {
  BRIDGE_EVENTS,
  EXCHANGE_EVENTS,
  LAUNCHPAD_EVENTS,
  SIDIORA_DECIMALS,
  SIDIORA_TOKEN,
  XWEB_PRECOMPILE,
  abiEventTopic,
  abiSelector,
  bridgeInCall,
  bridgeOutCall,
  buildXWebApiRequest,
  encodeAbiCall,
  exchangeCancelOrderCall,
  exchangeDepositMarginCall,
  exchangeDepositMarginTokenCall,
  exchangePlaceOrderCall,
  exchangeRequestSettlementCall,
  exchangeWithdrawMarginCall,
  gasQuoteDigest,
  launchpadBuyCall,
  launchpadClaimFeesCall,
  launchpadCreateMarketCall,
  launchpadSellCall,
  launchpadSetFeeStrategyCall,
  launchpadTokenCall,
  sponsoredBatchCall,
  sponsoredBatchDigest,
  xwebApiRequestCall,
  type GasQuote,
  type GasResult,
  type LaunchpadTokenWrite,
  type PrecompileCall,
  type SponsoredBatch,
} from '@sidiora/layerx-sdk';
import { afterAll, beforeAll, describe, expect, it } from 'vitest';

import {
  FEE_TOKEN_PRECOMPILE,
  GasStationError,
  ModuleError,
  SPONSORED_SUBMIT_PATH,
  WEB_DATA_EVENTS,
  bridge,
  constructionDigest,
  decodeAbiString,
  exchange,
  feeChoice,
  feeToken,
  feeTokenAmount,
  gasStation,
  launchpad,
  toWireConstruction,
  webData,
  type ModuleProvider,
  type SponsoredBatchConstruction,
  type SponsoredSubmitRequest,
} from '../src/index.js';

type Json = Record<string, unknown>;
type AbiInput = { name: string; type: string; indexed?: boolean };
type AbiEntry = { type: string; name?: string; inputs?: AbiInput[] };
type EventVector = { event: string; fields: Record<string, string | boolean> };

const root = new URL('../../../../', import.meta.url);
const calls = JSON.parse(readFileSync(new URL('./fixtures/modules/calls.json', import.meta.url), 'utf8')) as Json;
const eventVectors = JSON.parse(readFileSync(new URL('./fixtures/modules/events.json', import.meta.url), 'utf8')) as Record<
  string,
  EventVector[]
>;

function abi(name: string): AbiEntry[] {
  return JSON.parse(readFileSync(new URL(`precompiles/${name}/abi.json`, root), 'utf8')) as AbiEntry[];
}

function section(name: string): Json {
  return calls[name] as Json;
}

function big(value: unknown): bigint {
  return BigInt(value as string);
}

function text(value: unknown): string {
  return value as string;
}

function hex(bytes: Uint8Array): string {
  return `0x${Buffer.from(bytes).toString('hex')}`;
}

function unhex(value: string): Uint8Array {
  return new Uint8Array(Buffer.from(value.slice(2), 'hex'));
}

function selectors(entries: AbiEntry[]): Set<string> {
  return new Set(
    entries
      .filter((entry) => entry.type === 'function')
      .map((entry) => abiSelector(`${entry.name}(${(entry.inputs ?? []).map((input) => input.type).join(',')})`)),
  );
}

function word(type: string, value: string | boolean): string {
  if (type === 'address') return text(value).slice(2).toLowerCase().padStart(64, '0');
  if (type === 'bytes32') return text(value).slice(2).toLowerCase();
  if (type === 'bool') return (value ? 1n : 0n).toString(16).padStart(64, '0');
  return BigInt(value as string).toString(16).padStart(64, '0');
}

function tail(bytes: Uint8Array): string {
  const body = Buffer.from(bytes).toString('hex');
  return BigInt(bytes.length).toString(16).padStart(64, '0') + body.padEnd(Math.ceil(body.length / 64) * 64, '0');
}

function encodeLog(address: string, entry: AbiEntry, fields: Record<string, string | boolean>): { address: string; topics: string[]; data: string } {
  const inputs = entry.inputs ?? [];
  const topics = [abiEventTopic(`${entry.name}(${inputs.map((input) => input.type).join(',')})`)];
  const body = inputs.filter((input) => !input.indexed);
  const heads: string[] = [];
  const tails: string[] = [];
  let offset = body.length * 32;
  for (const input of inputs) {
    const value = fields[input.name];
    if (value === undefined) throw new Error(`fixture misses ${input.name}`);
    if (input.indexed) {
      topics.push(`0x${word(input.type, value)}`);
    } else if (input.type === 'string' || input.type === 'bytes') {
      const encoded = tail(input.type === 'string' ? new TextEncoder().encode(text(value)) : unhex(text(value)));
      heads.push(offset.toString(16).padStart(64, '0'));
      tails.push(encoded);
      offset += encoded.length / 2;
    } else {
      heads.push(word(input.type, value));
    }
  }
  return { address, topics, data: `0x${heads.join('')}${tails.join('')}` };
}

function expected(entry: AbiEntry, fields: Record<string, string | boolean>): Record<string, bigint | boolean | string> {
  const out: Record<string, bigint | boolean | string> = {};
  for (const input of entry.inputs ?? []) {
    const value = fields[input.name] as string | boolean;
    if (input.type.startsWith('uint')) out[input.name] = BigInt(value as string);
    else if (typeof value === 'string' && input.type !== 'string') out[input.name] = value.toLowerCase();
    else out[input.name] = value;
  }
  return out;
}

function checkEvents(
  vectors: EventVector[],
  entries: AbiEntry[],
  address: string,
  decode: (log: { address: string; topics: string[]; data: string }) => { event: string; fields: Record<string, unknown> },
): number {
  const events = entries.filter((entry) => entry.type === 'event');
  expect(vectors.map((vector) => vector.event).sort()).toEqual(events.map((entry) => entry.name).sort());
  for (const vector of vectors) {
    const entry = events.find((candidate) => candidate.name === vector.event);
    if (entry === undefined) throw new Error(`no ABI event ${vector.event}`);
    const decoded = decode(encodeLog(address, entry, vector.fields));
    expect(decoded.event).toBe(vector.event);
    expect(decoded.fields).toEqual(expected(entry, vector.fields));
  }
  return vectors.length;
}

function value<T>(result: GasResult<T>): T {
  if (!result.ok) throw new Error(`${result.refusal.code}: ${result.refusal.field}`);
  return result.value;
}

const accountKey = keccak_256(new TextEncoder().encode('modules account signer'));
const relayerKey = keccak_256(new TextEncoder().encode('modules relayer signer'));

function addressOf(key: Uint8Array): string {
  return hex(keccak_256(secp256k1.getPublicKey(key, false).slice(1)).slice(12));
}

function signDigest(digest: string, key: Uint8Array): string {
  const signature = secp256k1.sign(unhex(digest), key);
  return `0x${signature.toCompactHex()}${(27 + signature.recovery).toString(16)}`;
}

const account = addressOf(accountKey);
const relayer = addressOf(relayerKey);
const chainId = 125n;
const paymaster = `0x${'5f'.repeat(20)}`;

function batchFromConstruction(construction: SponsoredBatchConstruction): SponsoredBatch {
  return {
    chainId: BigInt(construction.chainId),
    account: construction.account,
    nonce: BigInt(construction.nonce),
    calls: construction.calls.map((call) => ({ to: call.to, value: BigInt(call.value), data: call.data })),
    quote: {
      sponsor: construction.quote.sponsor,
      token: construction.quote.token,
      maxTokenAmount: BigInt(construction.quote.maxTokenAmount),
      tokenAmount: BigInt(construction.quote.tokenAmount),
      deadline: BigInt(construction.quote.deadline),
      quoteNonce: BigInt(construction.quote.quoteNonce),
      gasCost: BigInt(construction.quote.gasCost),
      decimals: SIDIORA_DECIMALS,
    },
  };
}

const signed: Array<{ method: string; params: readonly unknown[] }> = [];

const signer: ModuleProvider = {
  async request(argument) {
    const params = argument.params ?? [];
    signed.push({ method: argument.method, params });
    if (argument.method === 'eth_gasPrice') return '0x3b9aca00';
    if (argument.method === 'eth_sign') {
      const [from, digest, construction] = params as [string, string, SponsoredBatchConstruction];
      if (from.toLowerCase() !== account || construction.kind !== 'sponsored_batch') throw new Error('refused construction');
      const recomputed = value(sponsoredBatchDigest(batchFromConstruction(construction)));
      if (digest !== recomputed) throw new Error('refused digest');
      return signDigest(recomputed, accountKey);
    }
    throw new Error(`unsupported ${argument.method}`);
  },
};

const submissions: SponsoredSubmitRequest[] = [];
let server: Server;
let gatewayUrl: string;

function handle(req: IncomingMessage, res: ServerResponse): void {
  const chunks: Buffer[] = [];
  req.on('data', (chunk: Buffer) => chunks.push(chunk));
  req.on('end', () => {
    if (req.method !== 'POST' || req.url !== SPONSORED_SUBMIT_PATH || req.headers.authorization !== 'Bearer session-token') {
      res.writeHead(404, { 'content-type': 'application/json' });
      res.end(JSON.stringify({ error: 'not_found' }));
      return;
    }
    const body = JSON.parse(Buffer.concat(chunks).toString('utf8')) as SponsoredSubmitRequest;
    submissions.push(body);
    res.writeHead(200, { 'content-type': 'application/json' });
    res.end(JSON.stringify({ tx_hash: hex(keccak_256(unhex(body.data))) }));
  });
}

beforeAll(async () => {
  server = createServer(handle);
  await new Promise<void>((resolve) => server.listen(0, '127.0.0.1', resolve));
  gatewayUrl = `http://127.0.0.1:${(server.address() as AddressInfo).port}`;
});

afterAll(async () => {
  await new Promise<void>((resolve, reject) => server.close((err) => (err ? reject(err) : resolve())));
});

describe('modules', () => {
  it('modules_exchange calldata equals the agent SDK builders and the ABI selectors', () => {
    const args = section('exchange');
    const module = exchange(signer);
    const order = args.placeOrder as Json;
    const placeOrder = {
      marketId: text(order.marketId),
      side: order.side as number,
      price: big(order.price),
      quantity: big(order.quantity),
      timeInForce: order.timeInForce as number,
    };
    const deposit = args.depositMargin as Json;
    const token = args.depositMarginToken as Json;
    const withdraw = args.withdrawMargin as Json;
    const pairs: Array<[PrecompileCall, PrecompileCall]> = [
      [module.placeOrder(placeOrder), exchangePlaceOrderCall(placeOrder)],
      [module.cancelOrder(text((args.cancelOrder as Json).orderId)), exchangeCancelOrderCall(text((args.cancelOrder as Json).orderId))],
      [
        module.requestSettlement(text((args.requestSettlement as Json).positionId)),
        exchangeRequestSettlementCall(text((args.requestSettlement as Json).positionId)),
      ],
      [
        module.depositMargin(text(deposit.account), big(deposit.amountWei)),
        exchangeDepositMarginCall(text(deposit.account), big(deposit.amountWei)),
      ],
      [
        module.depositMarginToken(text(token.pointer), big(token.amount), text(token.account)),
        exchangeDepositMarginTokenCall(text(token.pointer), big(token.amount), text(token.account)),
      ],
      [
        module.withdrawMargin(text(withdraw.account), text(withdraw.assetId), big(withdraw.amount)),
        exchangeWithdrawMarginCall(text(withdraw.account), text(withdraw.assetId), big(withdraw.amount)),
      ],
    ];
    const known = selectors(abi('layerxexchange'));
    for (const [ours, theirs] of pairs) {
      expect(ours).toEqual(theirs);
      expect(known.has(ours.data.slice(0, 10))).toBe(true);
    }
    expect(module.depositMargin(text(deposit.account), big(deposit.amountWei)).value).toBe(big(deposit.amountWei));
    const moved = exchange(signer, '0x00000000000000000000000000000000000A1015');
    expect(moved.placeOrder(placeOrder)).toEqual({ ...exchangePlaceOrderCall(placeOrder), to: '0x00000000000000000000000000000000000a1015' });
  });

  it('modules_bridge calldata equals the agent SDK builders and the ABI selectors', () => {
    const args = section('bridge');
    const module = bridge(signer);
    const inbound = args.bridgeIn as Json;
    const attestation = {
      chain: big(inbound.chain),
      vault: text(inbound.vault),
      txHash: text(inbound.txHash),
      logIndex: big(inbound.logIndex),
      recipient: text(inbound.recipient),
      asset: text(inbound.asset),
      amount: big(inbound.amount),
      signatures: inbound.signatures as string[],
    };
    const outbound = args.bridgeOut as Json;
    const known = selectors(abi('layerxbridge'));
    const pairs: Array<[PrecompileCall, PrecompileCall]> = [
      [module.bridgeIn(attestation), bridgeInCall(attestation)],
      [
        module.bridgeOut(big(outbound.chain), text(outbound.asset), big(outbound.amount), text(outbound.recipient)),
        bridgeOutCall(big(outbound.chain), text(outbound.asset), big(outbound.amount), text(outbound.recipient)),
      ],
    ];
    for (const [ours, theirs] of pairs) {
      expect(ours).toEqual(theirs);
      expect(known.has(ours.data.slice(0, 10))).toBe(true);
    }
  });

  it('modules_launchpad calldata equals the agent SDK builders and the ABI selectors', () => {
    const args = section('launchpad');
    const module = launchpad(signer);
    const swap = (entry: unknown) => {
      const order = entry as Json;
      return {
        token: text(order.token),
        amountIn: big(order.amountIn),
        minOut: big(order.minOut),
        recipient: text(order.recipient),
        deadline: big(order.deadline),
      };
    };
    const market = args.createMarket as Json;
    const strategy = args.setFeeStrategy as Json;
    const claim = args.claimFees as Json;
    const pairs: Array<[PrecompileCall, PrecompileCall]> = [
      [module.buy(swap(args.buy)), launchpadBuyCall(swap(args.buy))],
      [module.sell(swap(args.sell)), launchpadSellCall(swap(args.sell))],
      [
        module.createMarket(text(market.name), text(market.symbol), market.feeStrategy as number),
        launchpadCreateMarketCall(text(market.name), text(market.symbol), market.feeStrategy as number),
      ],
      [
        module.setFeeStrategy(text(strategy.token), strategy.feeStrategy as number),
        launchpadSetFeeStrategyCall(text(strategy.token), strategy.feeStrategy as number),
      ],
      [module.claimFees(text(claim.token), text(claim.recipient)), launchpadClaimFeesCall(text(claim.token), text(claim.recipient))],
      ...(args.tokenWrite as LaunchpadTokenWrite[]).map(
        (write): [PrecompileCall, PrecompileCall] => [module.tokenWrite(write, text(claim.token)), launchpadTokenCall(write, text(claim.token))],
      ),
    ];
    const known = selectors(abi('launchpad'));
    expect(pairs).toHaveLength(11);
    for (const [ours, theirs] of pairs) {
      expect(ours).toEqual(theirs);
      expect(known.has(ours.data.slice(0, 10))).toBe(true);
    }
  });

  it('modules_fee_token calldata matches the ABI and decodes the fee denom answer', () => {
    const args = section('feeToken');
    const module = feeToken(signer);
    const known = selectors(abi('feetoken'));
    const set = module.setFeeDenom(text(args.denom));
    expect(set).toEqual({ to: FEE_TOKEN_PRECOMPILE, data: encodeAbiCall('setFeeDenom', ['string'], [text(args.denom)]), value: 0n });
    expect(module.clearFeeDenom()).toEqual({ to: FEE_TOKEN_PRECOMPILE, data: abiSelector('clearFeeDenom()'), value: 0n });
    expect(module.getFeeDenomCallData(text(args.account))).toBe(encodeAbiCall('getFeeDenom', ['address'], [text(args.account)]));
    for (const data of [set.data, module.clearFeeDenom().data, module.getFeeDenomCallData(text(args.account))]) {
      expect(known.has(data.slice(0, 10))).toBe(true);
    }
    const answer = `0x${encodeAbiCall('getFeeDenom', ['string'], [text(args.denom)]).slice(10)}`;
    expect(decodeAbiString(answer)).toBe('usid');
    expect(() => module.setFeeDenom('u')).toThrow(ModuleError);
    expect(() => decodeAbiString('0x00')).toThrow(ModuleError);
  });

  it('modules_web_data calldata equals the agent SDK builders and the ABI selectors', () => {
    const args = section('webData');
    const module = webData(signer);
    const callbackGas = big(args.callbackGas);
    const fee = big(args.fee);
    const apiCall = args.api as { method: 'GET'; url: string };
    const attestors = { attestors: [], required: 0 };
    const built = module.buildApiRequest(apiCall, attestors);
    expect(built).toEqual(buildXWebApiRequest(apiCall, attestors));
    expect(module.api(built, callbackGas, fee)).toEqual(xwebApiRequestCall(built.payload, callbackGas, fee));
    expect(module.request(3, built.payload, callbackGas, fee)).toEqual(xwebApiRequestCall(built.payload, callbackGas, fee));
    const known = selectors(abi('xweb'));
    for (const [tx, kind, payload] of [
      [module.fetch(text(args.fetch), callbackGas, fee), 1n, text(args.fetch)],
      [module.search(text(args.search), callbackGas, fee), 2n, text(args.search)],
    ] as const) {
      const reference = xwebApiRequestCall(new TextEncoder().encode(payload), callbackGas, fee);
      expect(tx.to).toBe(XWEB_PRECOMPILE);
      expect(tx.value).toBe(fee);
      expect(tx.data.slice(0, 10)).toBe(abiSelector('request(uint8,bytes,uint64)'));
      expect(BigInt(`0x${tx.data.slice(10, 74)}`)).toBe(kind);
      expect(tx.data.slice(74)).toBe(reference.data.slice(74));
      const body = unhex(`0x${tx.data.slice(10)}`);
      const length = Number(BigInt(hex(body.subarray(96, 128))));
      expect(new TextDecoder().decode(body.subarray(128, 128 + length))).toBe(payload);
      expect(known.has(tx.data.slice(0, 10))).toBe(true);
    }
    const refund = module.refund(big(args.refund));
    expect(refund).toEqual({ to: XWEB_PRECOMPILE, data: encodeAbiCall('refund', ['uint64'], [7n]), value: 0n });
    expect(known.has(refund.data.slice(0, 10))).toBe(true);
    expect(() => module.fetch('', callbackGas, fee)).toThrow(ModuleError);
  });

  it('modules_events decode logs encoded from each precompile ABI', () => {
    const decoded =
      checkEvents(eventVectors.exchange ?? [], abi('layerxexchange'), exchange(signer).address, exchange(signer).decodeEvent) +
      checkEvents(eventVectors.bridge ?? [], abi('layerxbridge'), bridge(signer).address, bridge(signer).decodeEvent) +
      checkEvents(eventVectors.launchpad ?? [], abi('launchpad'), launchpad(signer).address, launchpad(signer).decodeEvent) +
      checkEvents(eventVectors.webData ?? [], abi('xweb'), webData(signer).address, webData(signer).decodeEvent);
    expect(decoded).toBe(EXCHANGE_EVENTS.length + BRIDGE_EVENTS.length + LAUNCHPAD_EVENTS.length + WEB_DATA_EVENTS.length);
    const vector = eventVectors.webData?.[0];
    const entry = abi('xweb').find((candidate) => candidate.name === vector?.event);
    if (vector === undefined || entry === undefined) throw new Error('missing web data vector');
    const log = encodeLog(XWEB_PRECOMPILE, entry, vector.fields);
    expect(() => webData(signer).decodeEvent({ ...log, address: FEE_TOKEN_PRECOMPILE })).toThrow(ModuleError);
    expect(() => webData(signer).decodeEvent({ ...log, topics: log.topics.slice(0, 2) })).toThrow(ModuleError);
    expect(() => exchange(signer).decodeEvent({ ...log, address: exchange(signer).address })).toThrow();
  });

  it('modules_gas_station digest equals the agent SDK and the flow signs, assembles and submits', async () => {
    const station = gasStation(signer, {
      chainId,
      sponsor: relayer,
      paymaster,
      quoteUrl: `${gatewayUrl}/quote`,
      gatewayUrl,
      accessToken: () => 'session-token',
    });
    const budget = await station.quote({ gasLimit: 250_000n, rate: '3114000' });
    const gasCost = 250_000n * 1_000_000_000n;
    expect(budget).toEqual({
      gasLimit: 250_000n,
      gasPrice: 1_000_000_000n,
      gasCost,
      rate: '3114000',
      tokenAmount: (gasCost * 3_114_000n + 10n ** 18n - 1n) / 10n ** 18n,
      token: SIDIORA_TOKEN,
      symbol: 'SID',
      decimals: 6,
    });
    expect(feeTokenAmount(21_000n * 1_000_000_000n, '3114000')).toBe(66n);
    expect(feeTokenAmount(10n ** 18n, '3.114')).toBe(4n);
    expect(feeTokenAmount(10n ** 18n, '3114000')).toBe(3_114_000n);
    expect(feeTokenAmount(10n ** 18n, '0.000000000000000001')).toBe(1n);
    expect(() => feeTokenAmount(1n, '0')).toThrow(ModuleError);
    expect(() => feeTokenAmount(1n, '1.0000000000000000001')).toThrow(ModuleError);

    const quote: GasQuote = {
      sponsor: relayer,
      token: SIDIORA_TOKEN,
      maxTokenAmount: budget.tokenAmount * 2n,
      tokenAmount: budget.tokenAmount,
      deadline: 1_900_000_000n,
      quoteNonce: 9n,
      gasCost,
      decimals: SIDIORA_DECIMALS,
    };
    const orders = exchange(signer);
    const batch: SponsoredBatch = {
      chainId,
      account,
      nonce: 4n,
      calls: [
        orders.cancelOrder(`0x${'22'.repeat(32)}`),
        feeToken(signer).setFeeDenom('usid'),
        { to: `0x${'dd'.repeat(20)}`, value: 5n, data: '0x' },
      ],
      quote,
    };
    expect(station.digest(batch)).toBe(value(sponsoredBatchDigest(batch)));
    const construction = station.construction(batch);
    expect(construction.kind).toBe('sponsored_batch');
    expect(Object.keys(construction).sort()).toEqual(['account', 'calls', 'chainId', 'kind', 'nonce', 'quote']);
    expect(Object.keys(construction.quote).sort()).toEqual(
      ['deadline', 'gasCost', 'maxTokenAmount', 'quoteNonce', 'sponsor', 'token', 'tokenAmount'].sort(),
    );
    expect(value(sponsoredBatchDigest(batchFromConstruction(construction)))).toBe(station.digest(batch));
    const changed = station.construction({ ...batch, nonce: 5n });
    expect(value(sponsoredBatchDigest(batchFromConstruction(changed)))).not.toBe(station.digest(batch));

    const relayerSignature = signDigest(value(gasQuoteDigest(chainId, account, quote)), relayerKey);
    const now = quote.deadline - 60n;
    const hash = await station.submit(batch, relayerSignature, { now });
    const signRequest = signed.find((entry) => entry.method === 'eth_sign');
    expect(signRequest?.params).toEqual([account, station.digest(batch), construction]);
    const accountSignature = signDigest(station.digest(batch), accountKey);
    const config = { quoteUrl: `${gatewayUrl}/quote`, chainId, sponsor: relayer, token: SIDIORA_TOKEN, decimals: SIDIORA_DECIMALS, paymaster };
    const reference = value(sponsoredBatchCall(config, batch, accountSignature, relayerSignature, now));
    expect(station.executeCall(batch, accountSignature, relayerSignature, now)).toEqual(reference);
    expect(submissions).toHaveLength(1);
    expect(submissions[0]).toEqual({
      chain_id: '125',
      account,
      to: account,
      data: reference.data,
      value: '0',
      construction,
      account_signature: accountSignature,
      relayer_signature: relayerSignature,
    });
    expect(hash).toBe(hex(keccak_256(unhex(reference.data))));

    expect(() => station.digest({ ...batch, calls: [] })).toThrow(ModuleError);
    expect(() => station.digest({ ...batch, chainId: 1n })).toThrow(GasStationError);
    expect(() => station.executeCall(batch, accountSignature, relayerSignature, quote.deadline + 1n)).toThrow(GasStationError);
    expect(() => station.executeCall(batch, relayerSignature, relayerSignature, now)).toThrow(GasStationError);
    const offline = gasStation(signer, { chainId, sponsor: relayer, paymaster, quoteUrl: `${gatewayUrl}/quote` });
    await expect(offline.submit(batch, relayerSignature, { now })).rejects.toThrow(ModuleError);
  });

  it('modules_gas_station digest is the provider routine and equals the agent SDK for identical fields', () => {
    const station = gasStation(signer, { chainId, sponsor: relayer, paymaster, quoteUrl: `${gatewayUrl}/quote` });
    const batch: SponsoredBatch = {
      chainId,
      account,
      nonce: 11n,
      calls: [
        { to: SIDIORA_TOKEN, value: 0n, data: `0xa9059cbb${'00'.repeat(12)}${relayer.slice(2)}${'00'.repeat(31)}2a` },
        { to: `0x${'dd'.repeat(20)}`, value: 7n, data: '0x' },
      ],
      quote: {
        sponsor: relayer,
        token: SIDIORA_TOKEN,
        maxTokenAmount: 500n,
        tokenAmount: 311n,
        deadline: 1_900_000_000n,
        quoteNonce: 2n,
        gasCost: 10n ** 14n,
        decimals: SIDIORA_DECIMALS,
      },
    };
    const construction = station.construction(batch);
    const digest = station.digest(batch);
    expect(construction.account).toBe(account);
    expect(construction.calls[0]?.data).toBe(batch.calls[0]?.data);
    expect(digest).toBe(value(sponsoredBatchDigest(batch)));
    expect(digest).toBe(value(sponsoredBatchDigest(batchFromConstruction(construction))));
    expect(constructionDigest(toWireConstruction(construction))).toBe(digest);
    expect(toWireConstruction(construction)).toEqual(construction);
    const altered = station.construction({ ...batch, quote: { ...batch.quote, tokenAmount: 312n } });
    expect(station.digest({ ...batch, quote: { ...batch.quote, tokenAmount: 312n } })).toBe(
      value(sponsoredBatchDigest(batchFromConstruction(altered))),
    );
    expect(constructionDigest(altered)).not.toBe(digest);
  });

  it('modules_fee_choice labels PAX gas, SID sponsored and SID native with their denominations and paths', () => {
    const helper = feeChoice();
    expect(helper.choices.map((choice) => [choice.id, choice.label])).toEqual([
      ['pax_gas', 'PAX gas'],
      ['sid_sponsored', 'SID sponsored'],
      ['sid_native', 'SID native'],
    ]);
    expect(helper.choice('pax_gas').denomination).toEqual({
      symbol: 'PAX',
      name: 'Paxeer',
      decimals: 18,
      native: true,
      token: null,
      denom: null,
    });
    const sid = { symbol: 'SID', name: 'Sidiora', decimals: SIDIORA_DECIMALS, native: false, token: SIDIORA_TOKEN.toLowerCase(), denom: 'usid' };
    expect(helper.choice('sid_sponsored').denomination).toEqual(sid);
    expect(helper.choice('sid_native').denomination).toEqual(sid);
    expect(helper.choice('pax_gas').path).toEqual({ kind: 'native_gas', method: 'eth_sendTransaction' });
    expect(helper.choice('sid_sponsored').path).toEqual({
      kind: 'sponsored_batch',
      method: 'eth_sign',
      construction: 'sponsored_batch',
      submit: 'gateway',
    });
    expect(helper.choice('sid_native').path).toEqual({
      kind: 'fee_token_preference',
      method: 'eth_sendTransaction',
      precompile: FEE_TOKEN_PRECOMPILE,
      setFeeDenom: 'usid',
    });
    expect(helper.denominate('pax_gas', 21_000n * 1_000_000_000n).display).toBe('0.000021 PAX');
    expect(helper.denominate('sid_sponsored', 66n).display).toBe('0.000066 SID');
    expect(helper.denominate('sid_native', 3_114_000n)).toEqual({
      choice: 'sid_native',
      amount: 3_114_000n,
      symbol: 'SID',
      decimals: 6,
      display: '3.114 SID',
    });
    expect(() => helper.denominate('pax_gas', -1n)).toThrow(ModuleError);
  });
});
