import { readFileSync, readdirSync } from 'node:fs';
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
  eip7702AuthorizationDigest,
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
  sponsoredConsent,
  xwebApiRequestCall,
  type GasQuote,
  type GasResult,
  type LaunchpadTokenWrite,
  type PrecompileCall,
  type SponsoredBatch,
  type SponsoredConsent,
} from '@sidiora/layerx-sdk';
import { afterAll, beforeAll, describe, expect, it } from 'vitest';

import * as walletSdk from '../src/index.js';
import {
  FEE_TOKEN_PRECOMPILE,
  GasStationError,
  LEDGER_PRECOMPILE,
  PAYER_GRANT_BYTES,
  ModuleError,
  PaxeerProvider,
  RpcResponseError,
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
  type SponsoredAdapterRequest,
  type SponsoredBatchConstruction,
  type SponsoredSubmissionEvidence,
  type WebPayerGrant,
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

const XWEB_FEE = 1_000_000_000_000n;
const FEE_SELECTOR = abiSelector('fee()');
const GET_PAYER_GRANT_SELECTOR = abiSelector('getPayerGrant(bytes32)');

function fill(byte: number, length: number): string {
  return `0x${byte.toString(16).padStart(2, '0').repeat(length)}`;
}

function grantFixture(seed: number, recurring: boolean, hasReference: boolean, revoked: boolean): WebPayerGrant {
  return {
    grantId: fill(seed, 32),
    from: fill(seed + 1, 32),
    recipient: fill(seed + 2, 32),
    asset: fill(seed + 3, 32),
    perDrawMaximum: 1_000n + BigInt(seed),
    allowance: (1n << 127n) + BigInt(seed),
    recurring,
    windowLength: recurring ? 3_600n : 0n,
    expiration: (1n << 63n) + BigInt(seed),
    purposeHash: fill(seed + 4, 32),
    hasReference,
    referenceHash: hasReference ? fill(seed + 5, 32) : fill(0, 32),
    revocationSequence: 9n + BigInt(seed),
    publicKey: fill(seed + 6, 32),
    signature: fill(seed + 7, 64),
    drawnTotal: 2_000n + BigInt(seed),
    drawnThisPeriod: 500n,
    windowStart: recurring ? 7_200n : 0n,
    revokedAt: revoked ? 42n : 0n,
    revoked,
    invoiceSettled: hasReference,
  };
}

function grantWire(grant: WebPayerGrant): Uint8Array {
  const be = (value: bigint, length: number): string => value.toString(16).padStart(length * 2, '0');
  const flag = (value: boolean): string => (value ? '01' : '00');
  const wire = unhex(
    `0x${[
      grant.grantId.slice(2),
      grant.from.slice(2),
      grant.recipient.slice(2),
      grant.asset.slice(2),
      be(grant.perDrawMaximum, 16),
      be(grant.allowance, 16),
      flag(grant.recurring),
      be(grant.windowLength, 8),
      be(grant.expiration, 8),
      grant.purposeHash.slice(2),
      flag(grant.hasReference),
      grant.referenceHash.slice(2),
      be(grant.revocationSequence, 8),
      grant.publicKey.slice(2),
      grant.signature.slice(2),
    ].join('')}`,
  );
  expect(wire.length).toBe(PAYER_GRANT_BYTES);
  return wire;
}

function getPayerGrantAnswer(grant: WebPayerGrant): string {
  const head = [
    (7 * 32).toString(16).padStart(64, '0'),
    word('uint128', grant.drawnTotal.toString()),
    word('uint128', grant.drawnThisPeriod.toString()),
    word('uint64', grant.windowStart.toString()),
    word('uint64', grant.revokedAt.toString()),
    word('bool', grant.revoked),
    word('bool', grant.invoiceSettled),
  ];
  return `0x${head.join('')}${tail(grantWire(grant))}`;
}

class ViewChain implements ModuleProvider {
  readonly requests: Array<{ method: string; params: readonly unknown[] }> = [];
  readonly answers = new Map<string, string>();

  async request(argument: { method: string; params?: readonly unknown[] }): Promise<unknown> {
    const params = argument.params ?? [];
    this.requests.push({ method: argument.method, params });
    if (argument.method !== 'eth_call') throw new Error(`method not found: ${argument.method}`);
    const [call, block] = params as [{ to: string; data: string }, string];
    const answer = this.answers.get(`${call.to}:${call.data}`);
    if (block !== 'latest' || answer === undefined) throw new Error('execution reverted: LXP_NO_PAYER_GRANT');
    return answer;
  }
}

function viewChain(grants: readonly WebPayerGrant[]): ViewChain {
  const chain = new ViewChain();
  chain.answers.set(`${XWEB_PRECOMPILE}:${FEE_SELECTOR}`, `0x${word('uint256', XWEB_FEE.toString())}`);
  for (const grant of grants) {
    chain.answers.set(`${LEDGER_PRECOMPILE}:${GET_PAYER_GRANT_SELECTOR}${grant.grantId.slice(2)}`, getPayerGrantAnswer(grant));
  }
  return chain;
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

const BATCH_NONCE = 4n;
const PENDING_NONCE = 7n;
const NONCE_SELECTOR = abiSelector('nonce()');
const SPONSOR_NONCE = '3';

const signed: Array<{ method: string; params: readonly unknown[] }> = [];

const signer: ModuleProvider & { readonly isPaxeer: true } = {
  isPaxeer: true,
  async request(argument) {
    const params = argument.params ?? [];
    signed.push({ method: argument.method, params });
    if (argument.method === 'eth_gasPrice') return '0x3b9aca00';
    if (argument.method === 'eth_accounts') return [account];
    if (argument.method === 'eth_chainId') return `0x${chainId.toString(16)}`;
    if (argument.method === 'eth_getCode') {
      const [target, block] = params as [string, string];
      if (target !== account || block !== 'pending') throw new Error('refused eth_getCode');
      return `0xef0100${paymaster.slice(2)}`;
    }
    if (argument.method === 'eth_call') {
      const [call, block] = params as [{ to: string; data: string }, string];
      if (params.length !== 2 || call.to !== account || call.data !== NONCE_SELECTOR || block !== 'pending') throw new Error('refused eth_call');
      return `0x${word('uint64', BATCH_NONCE.toString())}`;
    }
    if (argument.method === 'eth_getTransactionCount') {
      const [target, block] = params as [string, string];
      if (target !== account || block !== 'pending') throw new Error('refused eth_getTransactionCount');
      return `0x${PENDING_NONCE.toString(16)}`;
    }
    if (argument.method === 'eth_sign') {
      const [from, digest, fields] = params as [string, string, { kind: string }];
      if (from.toLowerCase() !== account) throw new Error('refused account');
      if (fields.kind === 'eip7702_authorization') {
        const authorization = fields as { kind: string; chainId: string; address: string; nonce: string };
        if (Object.keys(authorization).length !== 4) throw new Error('refused authorization');
        const recomputed = value(
          eip7702AuthorizationDigest({ chainId: BigInt(authorization.chainId), address: authorization.address, nonce: BigInt(authorization.nonce) }),
        );
        if (digest !== recomputed) throw new Error('refused digest');
        return signDigest(recomputed, accountKey);
      }
      const construction = fields as SponsoredBatchConstruction;
      if (construction.kind !== 'sponsored_batch') throw new Error('refused construction');
      const recomputed = value(sponsoredBatchDigest(batchFromConstruction(construction)));
      if (digest !== recomputed) throw new Error('refused digest');
      return signDigest(recomputed, accountKey);
    }
    throw new Error(`unsupported ${argument.method}`);
  },
};

const submissions: SponsoredAdapterRequest[] = [];
const answers: SponsoredSubmissionEvidence[] = [];
const rpcCalls: { authorization: string | undefined; body: Json }[] = [];
let server: Server;
let gatewayUrl: string;

function handle(req: IncomingMessage, res: ServerResponse): void {
  const chunks: Buffer[] = [];
  req.on('data', (chunk: Buffer) => chunks.push(chunk));
  req.on('end', () => {
    if (req.method === 'POST' && req.url === '/rpc') {
      const body = JSON.parse(Buffer.concat(chunks).toString('utf8')) as Json;
      rpcCalls.push({ authorization: req.headers.authorization, body });
      res.writeHead(200, { 'content-type': 'application/json' });
      res.end(JSON.stringify({ jsonrpc: '2.0', id: body.id, error: { code: -32601, message: 'method not found' } }));
      return;
    }
    if (req.method !== 'POST' || req.url !== SPONSORED_SUBMIT_PATH || req.headers.authorization !== 'Bearer session-token') {
      res.writeHead(404, { 'content-type': 'application/json' });
      res.end(JSON.stringify({ error: 'not_found' }));
      return;
    }
    const body = JSON.parse(Buffer.concat(chunks).toString('utf8')) as SponsoredAdapterRequest;
    submissions.push(body);
    const transactionHash = hex(keccak_256(unhex(body.data))) as `0x${string}`;
    const answer: SponsoredSubmissionEvidence = {
      tx_hash: transactionHash,
      status: 'pending',
      station: {
        state: 'pending',
        deadline: body.construction.quote.deadline,
        submission: { sponsorNonce: SPONSOR_NONCE, transactionHash },
        replacement: null,
        completion: null,
      },
    };
    answers.push(answer);
    res.writeHead(200, { 'content-type': 'application/json' });
    res.end(JSON.stringify(answer));
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

  it('acceptance 5.40.1: fee() eth_calls fee() on 0x1019 and decodes the uint256 answer', async () => {
    const chain = viewChain([]);
    expect(await webData(chain).fee()).toBe(XWEB_FEE);
    expect(chain.requests).toEqual([{ method: 'eth_call', params: [{ to: XWEB_PRECOMPILE, data: '0xddca3f43' }, 'latest'] }]);
    expect(FEE_SELECTOR).toBe('0xddca3f43');
    expect(selectors(abi('xweb')).has(FEE_SELECTOR)).toBe(true);
    const max = (1n << 256n) - 1n;
    chain.answers.set(`${XWEB_PRECOMPILE}:${FEE_SELECTOR}`, `0x${word('uint256', max.toString())}`);
    expect(await webData(chain).fee()).toBe(max);
    chain.answers.set(`${XWEB_PRECOMPILE}:${FEE_SELECTOR}`, `0x${word('uint256', '1')}00`);
    await expect(webData(chain).fee()).rejects.toMatchObject({ code: 'invalid_answer', field: 'uint' });
  });

  it('acceptance 5.40.2: caps() reads xweb fee() and layerxledger getPayerGrant through eth_call only', async () => {
    const recurring = grantFixture(0x11, true, true, false);
    const single = grantFixture(0xa1, false, false, true);
    const chain = viewChain([recurring, single]);
    const caps = await webData(chain).caps([recurring.grantId, single.grantId.toUpperCase().replace('0X', '0x')]);
    expect(caps).toEqual({ fee: XWEB_FEE, grants: [recurring, single] });
    expect(Object.isFrozen(caps) && Object.isFrozen(caps.grants) && caps.grants.every((grant) => Object.isFrozen(grant))).toBe(true);
    expect(chain.requests.map((request) => request.method)).toEqual(['eth_call', 'eth_call', 'eth_call']);
    expect(chain.requests.some((request) => request.method === 'lx_getWalletCaps')).toBe(false);
    expect(chain.requests.map((request) => request.params)).toEqual([
      [{ to: XWEB_PRECOMPILE, data: FEE_SELECTOR }, 'latest'],
      [{ to: LEDGER_PRECOMPILE, data: `0x423fe4b2${recurring.grantId.slice(2)}` }, 'latest'],
      [{ to: LEDGER_PRECOMPILE, data: `0x423fe4b2${single.grantId.slice(2)}` }, 'latest'],
    ]);
    expect(GET_PAYER_GRANT_SELECTOR).toBe('0x423fe4b2');
    expect(LEDGER_PRECOMPILE).toBe('0x000000000000000000000000000000000000101a');

    const empty = viewChain([]);
    expect(await webData(empty).caps([])).toEqual({ fee: XWEB_FEE, grants: [] });
    expect(empty.requests).toEqual([{ method: 'eth_call', params: [{ to: XWEB_PRECOMPILE, data: FEE_SELECTOR }, 'latest'] }]);

    await expect(webData(chain).caps(['0x1234'])).rejects.toMatchObject({ code: 'invalid_value', field: 'grantId' });
    await expect(webData(chain).caps([fill(0x99, 32)])).rejects.toThrow('LXP_NO_PAYER_GRANT');

    const key = `${LEDGER_PRECOMPILE}:${GET_PAYER_GRANT_SELECTOR}${recurring.grantId.slice(2)}`;
    const good = getPayerGrantAnswer(recurring);
    const patch = (offset: number, value: string): string => `${good.slice(0, 2 + offset * 2)}${value}${good.slice(2 + offset * 2 + value.length)}`;
    const wire = 8 * 32;
    for (const bad of [
      good.slice(0, -2),
      `${good}00`,
      patch(31, 'c0'),
      patch(7 * 32 + 31, '5b'),
      patch(1 * 32 + 15, '01'),
      patch(3 * 32 + 23, '01'),
      patch(5 * 32 + 31, '02'),
      patch(6 * 32 + 31, '02'),
      patch(wire + 160, '02'),
      patch(wire + 209, '02'),
      patch(wire + PAYER_GRANT_BYTES, '01'),
      patch(wire, '12'),
    ]) {
      chain.answers.set(key, bad);
      await expect(webData(chain).caps([recurring.grantId])).rejects.toMatchObject({ code: 'invalid_answer', field: 'getPayerGrant' });
    }
  });

  it('acceptance 5.40.3: the web-data module source imports nothing from kernel.js', () => {
    const source = readFileSync(new URL('../src/modules/web-data.ts', import.meta.url), 'utf8');
    const imports = [...source.matchAll(/^import[\s\S]*?from\s+'([^']+)';$/gmu)].map((match) => match[1]);
    expect(imports).toEqual(['@sidiora/layerx-sdk/browser', './index.js']);
    expect(source).not.toMatch(/kernel\.js|readWalletCaps|WalletCapsSnapshot|lx_getWalletCaps/u);
  });

  it('acceptance 5.50.2: the wallet SDK carries no lx_getWalletCaps path and forwards the retired method to the RPC base', async () => {
    const root = new URL('../src/', import.meta.url);
    const sources = readdirSync(root, { recursive: true, encoding: 'utf8' }).filter((file) => file.endsWith('.ts'));
    expect(sources).toContain('kernel.ts');
    expect(sources).toContain('provider.ts');
    for (const file of sources) expect(readFileSync(new URL(file, root), 'utf8')).not.toMatch(/lx_getWalletCaps|readWalletCaps|decodeWalletCaps/u);
    expect('readWalletCaps' in walletSdk).toBe(false);
    expect('decodeWalletCaps' in walletSdk).toBe(false);

    const provider = new PaxeerProvider({ gatewayUrl, rpcUrl: `${gatewayUrl}/rpc`, token: () => 'session-token' });
    rpcCalls.length = 0;
    const refusal = await provider.request({ method: 'lx_getWalletCaps', params: [] }).then(
      () => null,
      (error: unknown) => error,
    );
    expect(refusal).toBeInstanceOf(RpcResponseError);
    expect(refusal).toMatchObject({ code: -32601, message: 'method not found' });
    expect(rpcCalls).toEqual([{ authorization: undefined, body: { jsonrpc: '2.0', id: 1, method: 'lx_getWalletCaps', params: [] } }]);
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
    const retainedStore = (entries: Map<string, string>) => ({
      getItem: (key: string) => entries.get(key) ?? null,
      setItem: (key: string, item: string) => {
        entries.set(key, item);
      },
    });
    const retained = new Map<string, string>();
    const stationOptions = { chainId, sponsor: relayer, paymaster, quoteUrl: `${gatewayUrl}/quote`, gatewayUrl, accessToken: () => 'session-token' };
    const station = gasStation(signer, { ...stationOptions, submissionStore: retainedStore(retained) });
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
    const config = { quoteUrl: `${gatewayUrl}/quote`, chainId, sponsor: relayer, token: SIDIORA_TOKEN, decimals: SIDIORA_DECIMALS, paymaster };
    const expectedConsent = value(sponsoredConsent(config, batch, now));
    expect(expectedConsent.batchDigest).toBe(station.digest(batch));
    const session = [
      { method: 'eth_accounts', params: [] },
      { method: 'eth_chainId', params: [] },
    ];

    const declinedStore = new Map<string, string>();
    const declined = gasStation(signer, { ...stationOptions, submissionStore: retainedStore(declinedStore) });
    const declinedConsents: SponsoredConsent[] = [];
    signed.length = 0;
    await expect(
      declined.submit(batch, relayerSignature, {
        now,
        confirm: (consent) => {
          declinedConsents.push(consent);
          return false;
        },
      }),
    ).rejects.toMatchObject({ name: 'GasStationError', refusal: { code: 'refused', field: 'consent' } });
    expect(declinedConsents).toEqual([expectedConsent]);
    expect(signed).toEqual(session);
    expect(declinedStore.size).toBe(0);
    expect(submissions).toHaveLength(0);

    const consents: SponsoredConsent[] = [];
    signed.length = 0;
    const hash = await station.submit(batch, relayerSignature, {
      now,
      confirm: (consent) => {
        consents.push(consent);
        return true;
      },
    });
    expect(consents).toHaveLength(1);
    expect(consents[0]).toEqual(expectedConsent);

    const signRequest = signed.find((entry) => entry.method === 'eth_sign');
    expect(signRequest?.params).toEqual([account, station.digest(batch), construction]);
    const accountSignature = signDigest(station.digest(batch), accountKey);
    const authorizationFields = { kind: 'eip7702_authorization', chainId: '125', address: paymaster, nonce: PENDING_NONCE.toString() };
    const authorizationDigest = value(eip7702AuthorizationDigest({ chainId, address: paymaster, nonce: PENDING_NONCE }));
    const authorizationSignature = signDigest(authorizationDigest, accountKey);
    const authorization = {
      chainId: '125',
      address: paymaster,
      nonce: '7',
      yParity: Number.parseInt(authorizationSignature.slice(130), 16) - 27,
      r: `0x${authorizationSignature.slice(2, 66)}`,
      s: `0x${authorizationSignature.slice(66, 130)}`,
    };
    expect([0, 1]).toContain(authorization.yParity);
    expect(signed).toEqual([
      ...session,
      ...session,
      { method: 'eth_chainId', params: [] },
      { method: 'eth_getCode', params: [account, 'pending'] },
      { method: 'eth_call', params: [{ to: account, data: NONCE_SELECTOR }, 'pending'] },
      { method: 'eth_getTransactionCount', params: [account, 'pending'] },
      ...session,
      { method: 'eth_sign', params: [account, station.digest(batch), construction] },
      ...session,
      { method: 'eth_sign', params: [account, authorizationDigest, authorizationFields] },
      ...session,
      ...session,
      ...session,
      { method: 'eth_accounts', params: [] },
    ]);
    expect(await station.batchNonce(account)).toBe(BATCH_NONCE);

    const reference = value(sponsoredBatchCall(config, batch, accountSignature, relayerSignature, now));
    expect(station.executeCall(batch, accountSignature, relayerSignature, now)).toEqual(reference);
    expect(reference.to).toBe(account);
    expect(reference.value).toBe(0n);
    expect(submissions).toHaveLength(1);
    const submitted = submissions[0];
    expect(Object.keys(submitted ?? {}).sort()).toEqual(
      ['account', 'account_signature', 'authorization', 'chain_id', 'construction', 'data', 'quote_decimals', 'relayer_signature', 'to', 'value'],
    );
    expect(submitted?.chain_id).toBe('125');
    expect(submitted?.account).toBe(account);
    expect(submitted?.to).toBe(account);
    expect(submitted?.data).toBe(reference.data);
    expect(submitted?.value).toBe('0');
    expect(submitted?.construction).toEqual(construction);
    expect(submitted?.account_signature).toBe(accountSignature);
    expect(submitted?.relayer_signature).toBe(relayerSignature);
    expect(submitted?.quote_decimals).toBe(6);
    expect(submitted?.authorization).toEqual(authorization);
    expect(submitted).toEqual(station.adapterRequest(batch, accountSignature, relayerSignature, submitted!.authorization, now));

    const transactionHash = hex(keccak_256(unhex(reference.data)));
    expect(answers).toEqual([
      {
        tx_hash: transactionHash,
        status: 'pending',
        station: {
          state: 'pending',
          deadline: quote.deadline.toString(),
          submission: { sponsorNonce: SPONSOR_NONCE, transactionHash },
          replacement: null,
          completion: null,
        },
      },
    ]);
    expect(hash).toBe(transactionHash);

    const identity = `paxeer:sponsored:v1:125:${account}:${relayer}:${quote.quoteNonce}`;
    expect([...retained.keys()].sort()).toEqual([`paxeer:sponsored:pending:v1:125:${account}`, identity].sort());
    expect(retained.get(`paxeer:sponsored:pending:v1:125:${account}`)).toBe(identity);
    expect(JSON.parse(retained.get(identity) ?? 'null')).toEqual(submitted);
    const resumable = await station.pending(account);
    expect(resumable?.relayerSignature).toBe(relayerSignature);
    expect(resumable === null ? null : station.digest(resumable.batch)).toBe(station.digest(batch));

    expect(() => station.digest({ ...batch, calls: [] })).toThrow(ModuleError);
    expect(() => station.digest({ ...batch, chainId: 1n })).toThrow(GasStationError);
    expect(() => station.executeCall(batch, accountSignature, relayerSignature, quote.deadline + 1n)).toThrow(GasStationError);
    expect(() => station.executeCall(batch, relayerSignature, relayerSignature, now)).toThrow(GasStationError);
    const offline = gasStation(signer, { chainId, sponsor: relayer, paymaster, quoteUrl: `${gatewayUrl}/quote` });
    await expect(offline.submit(batch, relayerSignature, { now })).rejects.toThrow(ModuleError);
    await expect(offline.submit(batch, relayerSignature, { now })).rejects.toMatchObject({ code: 'unavailable', field: 'submissionStore' });
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
