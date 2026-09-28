import { readdirSync, readFileSync } from 'node:fs';
import { createServer, type IncomingMessage, type Server, type ServerResponse } from 'node:http';
import type { AddressInfo } from 'node:net';
import { fileURLToPath } from 'node:url';
import { afterAll, beforeAll, describe, expect, it } from 'vitest';
import { keccak256, recoverAddress, recoverMessageAddress, recoverTypedDataAddress, toHex } from 'viem';
import { privateKeyToAccount } from 'viem/accounts';
import { SIDIORA_DECIMALS, sponsoredBatchDigest as agentBatchDigest, type SponsoredBatch } from '@sidiora/layerx-sdk';
import {
  ChainDisconnectedError,
  DisconnectedError,
  GatewayRefusalError,
  InvalidParamsError,
  PaxeerProvider,
  ProviderRpcError,
  RpcResponseError,
  UnauthorizedError,
  UnsupportedMethodError,
  UserRejectedRequestError,
  WalletInterface,
  constructionDigest,
  gasStation,
  toWireConstruction,
  type Eip1193Provider,
  type Hex,
  type ProviderEvent,
  type ProviderListener,
  type RequestArguments,
  type SponsoredBatchConstruction,
  type Eip7702AuthorizationConstruction,
  type TypedDataPayload,
} from '../src/index.js';

type Fixture = {
  scenario: string;
  method: string;
  path?: string;
  kind?: string;
  status: number;
  body: unknown;
};

type Seen = { method: string; path: string; scenario: string; authorization?: string; body: unknown };

type Constructions = {
  address: Hex;
  sponsoredBatch: SponsoredBatchConstruction;
  sponsoredBatchDigest: Hex;
  eip7702Authorization: Eip7702AuthorizationConstruction;
  eip7702AuthorizationDigest: Hex;
  typedData: TypedDataPayload;
  message: string;
  transaction: {
    to: Hex;
    value: string;
    data: Hex;
    gas: string;
    maxFeePerGas: string;
    maxPriorityFeePerGas: string;
    nonce: number;
    chainId: number;
  };
  custody: Hex;
};

const TOKEN = 'synthetic-access-token';
const fixtureDir = fileURLToPath(new URL('./fixtures/gateway/', import.meta.url));
const fixtures: Fixture[] = readdirSync(fixtureDir)
  .filter((name) => name.endsWith('.json') && name !== 'constructions.json')
  .map((name) => JSON.parse(readFileSync(`${fixtureDir}${name}`, 'utf8')) as Fixture);
const vectors = JSON.parse(readFileSync(`${fixtureDir}constructions.json`, 'utf8')) as Constructions;
const fixtureKey = keccak256(toHex('paxeer-wallet-sdk-fixture'));

const seen: Seen[] = [];
let server: Server;
let base: string;
let closedBase: string;

function bodyOf(name: string): Record<string, unknown> {
  const fixture = JSON.parse(readFileSync(`${fixtureDir}${name}.json`, 'utf8')) as Fixture;
  return fixture.body as Record<string, unknown>;
}

async function readBody(req: IncomingMessage): Promise<unknown> {
  const chunks: Buffer[] = [];
  for await (const chunk of req) chunks.push(chunk as Buffer);
  const text = Buffer.concat(chunks).toString('utf8');
  return text.length > 0 ? JSON.parse(text) : undefined;
}

function send(res: ServerResponse, status: number, body: unknown): void {
  res.writeHead(status, { 'content-type': 'application/json' });
  res.end(JSON.stringify(body));
}

async function handle(req: IncomingMessage, res: ServerResponse): Promise<void> {
  const url = req.url ?? '/';
  const [, scenario = '', ...rest] = url.split('/');
  const path = `/${rest.join('/')}`;
  const body = await readBody(req);
  seen.push({ method: req.method ?? '', path, scenario, authorization: req.headers.authorization, body });
  if (scenario === 'rpc') {
    const rpc = body as { method: string; id: number };
    const fixture = fixtures.find((f) => f.scenario === 'rpc' && f.method === rpc.method);
    if (!fixture) return send(res, 200, { jsonrpc: '2.0', id: rpc.id, error: { code: -32601, message: 'no fixture' } });
    return send(res, fixture.status, { ...(fixture.body as object), id: rpc.id });
  }
  if (req.headers.authorization !== `Bearer ${TOKEN}`) {
    return send(res, 401, { error: 'unauthorized', detail: 'invalid token' });
  }
  const kind = (body as { construction?: { kind?: string } } | undefined)?.construction?.kind;
  const matches = (s: string) =>
    fixtures.find(
      (f) => f.scenario === s && f.method === req.method && f.path === path && (f.kind === undefined || f.kind === kind),
    );
  const fixture = matches(scenario) ?? matches('default');
  if (!fixture) return send(res, 404, { error: 'not_found' });
  send(res, fixture.status, fixture.body);
}

beforeAll(async () => {
  server = createServer((req, res) => {
    void handle(req, res);
  });
  await new Promise<void>((resolve) => server.listen(0, '127.0.0.1', resolve));
  base = `http://127.0.0.1:${(server.address() as AddressInfo).port}`;
  const closed = createServer();
  await new Promise<void>((resolve) => closed.listen(0, '127.0.0.1', resolve));
  closedBase = `http://127.0.0.1:${(closed.address() as AddressInfo).port}`;
  await new Promise<void>((resolve) => closed.close(() => resolve()));
});

afterAll(async () => {
  await new Promise<void>((resolve, reject) => server.close((err) => (err ? reject(err) : resolve())));
});

function provider(
  scenario = 'default',
  options: { token?: string | null; confirm?: (r: { method: string }) => boolean; rpc?: string } = {},
): PaxeerProvider {
  const token = options.token === undefined ? TOKEN : options.token;
  return new PaxeerProvider({
    gatewayUrl: `${base}/${scenario}/`,
    rpcUrl: options.rpc ?? `${base}/rpc`,
    token: async () => token,
    confirm: options.confirm,
  });
}

async function connected(scenario = 'default'): Promise<PaxeerProvider> {
  const p = provider(scenario);
  await p.request({ method: 'eth_requestAccounts' });
  return p;
}

function record(p: PaxeerProvider): { event: ProviderEvent; payload: unknown }[] {
  const events: { event: ProviderEvent; payload: unknown }[] = [];
  for (const event of ['connect', 'disconnect', 'accountsChanged', 'chainChanged'] as ProviderEvent[]) {
    p.on(event, (payload) => events.push({ event, payload }));
  }
  return events;
}

async function failure(promise: Promise<unknown>): Promise<unknown> {
  return promise.then(
    () => {
      throw new Error('expected the request to fail');
    },
    (error: unknown) => error,
  );
}

class LocalKeyProvider implements Eip1193Provider {
  private readonly account = privateKeyToAccount(fixtureKey);
  private readonly listeners = new Map<ProviderEvent, Set<ProviderListener>>();

  on(event: ProviderEvent, listener: ProviderListener): this {
    const set = this.listeners.get(event) ?? new Set<ProviderListener>();
    set.add(listener);
    this.listeners.set(event, set);
    return this;
  }

  removeListener(event: ProviderEvent, listener: ProviderListener): this {
    this.listeners.get(event)?.delete(listener);
    return this;
  }

  emit(event: ProviderEvent, payload: unknown): void {
    for (const listener of this.listeners.get(event) ?? []) listener(payload);
  }

  async request(args: RequestArguments): Promise<unknown> {
    const params = (args.params ?? []) as unknown[];
    switch (args.method) {
      case 'eth_requestAccounts':
        return [this.account.address];
      case 'eth_chainId':
        return '0x7d';
      case 'personal_sign':
        return this.account.signMessage({ message: { raw: params[0] as Hex } });
      case 'eth_signTypedData_v4':
        return this.account.signTypedData(JSON.parse(params[1] as string) as Parameters<typeof this.account.signTypedData>[0]);
      case 'eth_sendTransaction': {
        const tx = params[0] as Record<string, Hex>;
        const signed = await this.account.signTransaction({
          type: 'eip1559',
          to: tx.to,
          value: BigInt(tx.value ?? '0x0'),
          data: tx.data ?? '0x',
          gas: BigInt(tx.gas!),
          maxFeePerGas: BigInt(tx.maxFeePerGas!),
          maxPriorityFeePerGas: BigInt(tx.maxPriorityFeePerGas!),
          nonce: Number(BigInt(tx.nonce!)),
          chainId: Number(BigInt(tx.chainId!)),
        });
        return keccak256(signed);
      }
      default:
        throw new UnsupportedMethodError(args.method);
    }
  }
}

describe('PaxeerProvider', () => {
  it('provider_fixtures_are_signed_by_the_recorded_wallet', async () => {
    expect(privateKeyToAccount(fixtureKey).address).toBe(vectors.address);
    expect((bodyOf('wallet-me').wallet as { address: string }).address).toBe(vectors.address);
  });

  it('provider_rejects_a_configuration_missing_a_field', () => {
    const cfg = { gatewayUrl: base, rpcUrl: `${base}/rpc`, token: () => TOKEN };
    expect(() => new PaxeerProvider({ ...cfg, gatewayUrl: '' })).toThrow('gatewayUrl required');
    expect(() => new PaxeerProvider({ ...cfg, rpcUrl: '' })).toThrow('rpcUrl required');
    expect(() => new PaxeerProvider({ ...cfg, token: undefined as unknown as () => string })).toThrow('token supplier required');
  });

  it('provider_eth_chainId_answers_0x7d_without_a_session', async () => {
    const before = seen.length;
    const p = provider('default', { token: null });
    expect(await p.request({ method: 'eth_chainId' })).toBe('0x7d');
    expect(await p.request({ method: 'net_version' })).toBe('125');
    expect(await p.request({ method: 'eth_accounts' })).toEqual([]);
    expect(seen.length).toBe(before);
  });

  it('provider_eth_requestAccounts_reads_the_wallet_and_emits_connect_and_accountsChanged', async () => {
    const p = provider();
    const events = record(p);
    const accounts = await p.request({ method: 'eth_requestAccounts' });
    expect(accounts).toEqual([vectors.address]);
    expect(await p.request({ method: 'eth_accounts' })).toEqual([vectors.address]);
    expect(p.isConnected()).toBe(true);
    expect(events).toEqual([
      { event: 'connect', payload: { chainId: '0x7d' } },
      { event: 'accountsChanged', payload: [vectors.address] },
    ]);
    const last = seen[seen.length - 1]!;
    expect(last).toMatchObject({ method: 'GET', path: '/v1/wallet/me', authorization: `Bearer ${TOKEN}` });
    await p.request({ method: 'eth_requestAccounts' });
    expect(events.length).toBe(2);
  });

  it('provider_eth_requestAccounts_provisions_a_wallet_when_none_exists', async () => {
    const before = seen.length;
    const p = provider('unprovisioned');
    expect(await p.request({ method: 'eth_requestAccounts' })).toEqual([vectors.address]);
    expect(seen.slice(before).map((s) => `${s.method} ${s.path}`)).toEqual([
      'GET /v1/wallet/me',
      'POST /v1/wallet/provision',
    ]);
  });

  it('provider_refuses_a_gateway_call_without_a_session_token', async () => {
    const before = seen.length;
    const err = await failure(provider('default', { token: null }).request({ method: 'eth_requestAccounts' }));
    expect(err).toBeInstanceOf(UnauthorizedError);
    expect(err).toMatchObject({ code: 4100, reason: 'no_token' });
    expect(seen.length).toBe(before);
  });

  it('provider_maps_an_expired_token_to_a_typed_gateway_refusal', async () => {
    const err = await failure(provider('expired').request({ method: 'eth_requestAccounts' }));
    expect(err).toBeInstanceOf(GatewayRefusalError);
    expect(err).toMatchObject({
      code: 4100,
      reason: 'unauthorized',
      status: 401,
      message: '"exp" claim timestamp check failed',
    });
  });

  it('provider_drops_accounts_when_the_gateway_rejects_the_session', async () => {
    let token: string | null = TOKEN;
    const p = new PaxeerProvider({ gatewayUrl: `${base}/default`, rpcUrl: `${base}/rpc`, token: () => token });
    await p.request({ method: 'eth_requestAccounts' });
    const events = record(p);
    token = 'revoked-token';
    const err = await failure(p.request({ method: 'personal_sign', params: [vectors.message, vectors.address] }));
    expect(err).toMatchObject({ code: 4100, reason: 'unauthorized', status: 401 });
    expect(events).toEqual([{ event: 'accountsChanged', payload: [] }]);
    expect(await p.request({ method: 'eth_accounts' })).toEqual([]);
  });

  it('provider_signing_before_connect_is_unauthorized', async () => {
    const err = await failure(provider().request({ method: 'personal_sign', params: [vectors.message, vectors.address] }));
    expect(err).toBeInstanceOf(UnauthorizedError);
    expect(err).toMatchObject({ code: 4100, reason: 'not_connected' });
  });

  it('provider_eth_sendTransaction_forwards_decimal_fields_and_returns_the_hash', async () => {
    const p = await connected();
    const t = vectors.transaction;
    const hash = await p.request({
      method: 'eth_sendTransaction',
      params: [
        {
          from: vectors.address,
          to: t.to,
          value: toHex(BigInt(t.value)),
          data: t.data,
          gas: toHex(BigInt(t.gas)),
          maxFeePerGas: toHex(BigInt(t.maxFeePerGas)),
          maxPriorityFeePerGas: toHex(BigInt(t.maxPriorityFeePerGas)),
          nonce: toHex(t.nonce),
          chainId: '0x7d',
        },
      ],
    });
    expect(hash).toBe(bodyOf('wallet-send').tx_hash);
    const last = seen[seen.length - 1]!;
    expect(last.path).toBe('/v1/wallet/send');
    expect(last.body).toEqual({ tx: t });
  });

  it('provider_eth_sendTransaction_refuses_a_foreign_sender_and_a_foreign_chain', async () => {
    const p = await connected();
    const before = seen.length;
    const foreign = await failure(
      p.request({ method: 'eth_sendTransaction', params: [{ from: '0x3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c', to: vectors.transaction.to }] }),
    );
    expect(foreign).toMatchObject({ code: 4100, reason: 'account_mismatch' });
    const chain = await failure(
      p.request({ method: 'eth_sendTransaction', params: [{ to: vectors.transaction.to, chainId: '0x1' }] }),
    );
    expect(chain).toBeInstanceOf(ChainDisconnectedError);
    expect(chain).toMatchObject({ code: 4901, requestedChainId: 1, connectedChainId: 125 });
    const bad = await failure(p.request({ method: 'eth_sendTransaction', params: [{ to: '0x12' }] }));
    expect(bad).toBeInstanceOf(InvalidParamsError);
    expect(bad).toMatchObject({ code: -32602, field: 'to' });
    expect(seen.length).toBe(before);
  });

  it('provider_surfaces_a_policy_refusal_with_its_gateway_reason', async () => {
    const p = await connected('capped');
    const err = await failure(
      p.request({ method: 'eth_sendTransaction', params: [{ to: vectors.transaction.to, value: '0x38d7ea4c68000' }] }),
    );
    expect(err).toBeInstanceOf(GatewayRefusalError);
    expect(err).toMatchObject({ code: 4100, reason: 'TX_VALUE_CAP', status: 403 });
    expect((err as GatewayRefusalError).body).toEqual(bodyOf('refusal-tx-value-cap'));
  });

  it('provider_eth_signTypedData_v4_forwards_the_typed_data_and_returns_its_signature', async () => {
    const p = await connected();
    const signature = (await p.request({
      method: 'eth_signTypedData_v4',
      params: [vectors.address, JSON.stringify(vectors.typedData)],
    })) as Hex;
    expect(signature).toBe(bodyOf('wallet-sign-typed-data').signature);
    expect(seen[seen.length - 1]!.body).toEqual({ typedData: vectors.typedData });
    const recovered = await recoverTypedDataAddress({
      ...(vectors.typedData as Parameters<typeof recoverTypedDataAddress>[0]),
      signature,
    });
    expect(recovered).toBe(vectors.address);
    const wrongChain = { ...vectors.typedData, domain: { ...vectors.typedData.domain, chainId: 1 } };
    const err = await failure(p.request({ method: 'eth_signTypedData_v4', params: [vectors.address, wrongChain] }));
    expect(err).toMatchObject({ code: 4901 });
    const malformed = await failure(p.request({ method: 'eth_signTypedData_v4', params: [vectors.address, '{not json'] }));
    expect(malformed).toMatchObject({ code: -32602, field: 'typedData' });
  });

  it('provider_personal_sign_accepts_text_and_utf8_hex', async () => {
    const p = await connected();
    const hex = toHex(vectors.message);
    const fromHex = (await p.request({ method: 'personal_sign', params: [hex, vectors.address] })) as Hex;
    expect(seen[seen.length - 1]!.body).toEqual({ message: vectors.message });
    const fromText = await p.request({ method: 'personal_sign', params: [vectors.message, vectors.address] });
    expect(fromText).toBe(fromHex);
    expect(fromHex).toBe(bodyOf('wallet-sign-message').signature);
    expect(await recoverMessageAddress({ message: vectors.message, signature: fromHex })).toBe(vectors.address);
    const binary = await failure(p.request({ method: 'personal_sign', params: ['0xff', vectors.address] }));
    expect(binary).toMatchObject({ code: -32602, field: 'message' });
  });

  it('provider_eth_sign_refuses_an_arbitrary_digest', async () => {
    const p = await connected();
    const before = seen.length;
    const digest = keccak256(toHex('arbitrary bytes'));
    const bare = await failure(p.request({ method: 'eth_sign', params: [vectors.address, digest] }));
    expect(bare).toBeInstanceOf(UnauthorizedError);
    expect(bare).toMatchObject({ code: 4100, reason: 'missing_construction' });
    const mismatched = await failure(
      p.request({ method: 'eth_sign', params: [vectors.address, digest, vectors.sponsoredBatch] }),
    );
    expect(mismatched).toMatchObject({ code: 4100, reason: 'digest_mismatch' });
    const unknown = await failure(
      p.request({ method: 'eth_sign', params: [vectors.address, digest, { kind: 'raw_digest', digest }] }),
    );
    expect(unknown).toMatchObject({ code: 4100, reason: 'unknown_construction' });
    const tampered = { ...vectors.sponsoredBatch, quote: { ...vectors.sponsoredBatch.quote, tokenAmount: '400000' } };
    const tamperedErr = await failure(
      p.request({ method: 'eth_sign', params: [vectors.address, vectors.sponsoredBatchDigest, tampered] }),
    );
    expect(tamperedErr).toMatchObject({ code: 4100, reason: 'digest_mismatch' });
    const otherAccount = { ...vectors.sponsoredBatch, account: '0x3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c' };
    const otherErr = await failure(
      p.request({ method: 'eth_sign', params: [vectors.address, vectors.sponsoredBatchDigest, otherAccount] }),
    );
    expect(otherErr).toMatchObject({ code: 4100, reason: 'account_mismatch' });
    expect(seen.length).toBe(before);
  });

  it('provider_eth_sign_accepts_a_correct_sponsored_batch_and_forwards_its_fields', async () => {
    const p = await connected();
    expect(constructionDigest(toWireConstruction(vectors.sponsoredBatch))).toBe(vectors.sponsoredBatchDigest);
    const signature = (await p.request({
      method: 'eth_sign',
      params: [vectors.address, vectors.sponsoredBatchDigest, vectors.sponsoredBatch],
    })) as Hex;
    expect(signature).toBe(bodyOf('wallet-sign-digest-sponsored-batch').signature);
    const forwarded = seen[seen.length - 1]!;
    expect(forwarded.path).toBe('/v1/wallet/sign-digest');
    expect(forwarded.body).toEqual({ construction: vectors.sponsoredBatch });
    expect(JSON.stringify(forwarded.body)).not.toContain(vectors.sponsoredBatchDigest.slice(2));
    expect(await recoverAddress({ hash: vectors.sponsoredBatchDigest, signature })).toBe(vectors.address);
  });

  it('provider_eth_sign_accepts_a_correct_eip7702_authorisation', async () => {
    const p = await connected();
    const auth = vectors.eip7702Authorization;
    const signature = (await p.request({
      method: 'eth_sign',
      params: [vectors.address, vectors.eip7702AuthorizationDigest, { ...auth, nonce: '0x4', chainId: 125 }],
    })) as Hex;
    expect(signature).toBe(bodyOf('wallet-sign-digest-eip7702').signature);
    expect(seen[seen.length - 1]!.body).toEqual({ construction: auth });
    expect(await recoverAddress({ hash: vectors.eip7702AuthorizationDigest, signature })).toBe(vectors.address);
    const zero = toWireConstruction({ ...auth, nonce: 0 });
    expect(zero.nonce).toBe('0');
    expect(constructionDigest(zero)).not.toBe(vectors.eip7702AuthorizationDigest);
    const foreignChain = await failure(
      p.request({ method: 'eth_sign', params: [vectors.address, vectors.eip7702AuthorizationDigest, { ...auth, chainId: '1' }] }),
    );
    expect(foreignChain).toMatchObject({ code: 4901 });
  });

  it('provider_paxeer_signCustody_passes_the_custody_bytes_through', async () => {
    const p = await connected();
    const signature = (await p.request({ method: 'paxeer_signCustody', params: [{ custody: vectors.custody }] })) as Hex;
    expect(signature).toBe(bodyOf('wallet-sign-custody').signature);
    expect(seen[seen.length - 1]).toMatchObject({ path: '/v1/wallet/sign-custody', body: { custody: vectors.custody } });
    expect(await recoverAddress({ hash: keccak256(vectors.custody), signature })).toBe(vectors.address);
    const bare = await p.request({ method: 'paxeer_signCustody', params: [vectors.custody] });
    expect(bare).toBe(signature);
    const empty = await failure(p.request({ method: 'paxeer_signCustody', params: ['0x'] }));
    expect(empty).toMatchObject({ code: -32602, field: 'custody' });
  });

  it('provider_refuses_unsupported_signing_methods_with_4200', async () => {
    const p = await connected();
    for (const method of ['eth_signTypedData', 'eth_signTypedData_v3', 'eth_signTransaction', 'wallet_addEthereumChain']) {
      const err = await failure(p.request({ method, params: [] }));
      expect(err).toBeInstanceOf(UnsupportedMethodError);
      expect(err).toMatchObject({ code: 4200, method });
    }
  });

  it('provider_wallet_switchEthereumChain_accepts_only_the_connected_chain', async () => {
    const p = await connected();
    expect(await p.request({ method: 'wallet_switchEthereumChain', params: [{ chainId: '0x7d' }] })).toBeNull();
    const err = await failure(p.request({ method: 'wallet_switchEthereumChain', params: [{ chainId: '0x1' }] }));
    expect(err).toMatchObject({ code: 4901, requestedChainId: 1 });
  });

  it('provider_proxies_every_other_method_as_a_read_to_the_rpc_base', async () => {
    const p = provider('default', { token: null });
    expect(await p.request({ method: 'eth_blockNumber' })).toBe('0x1a2b3c4');
    const last = seen[seen.length - 1]!;
    expect(last).toMatchObject({ scenario: 'rpc', authorization: undefined });
    expect(last.body).toMatchObject({ jsonrpc: '2.0', method: 'eth_blockNumber', params: [] });
    const err = await failure(p.request({ method: 'eth_unknownMethod', params: [] }));
    expect(err).toBeInstanceOf(RpcResponseError);
    expect(err).toMatchObject({ code: -32601, message: 'the method eth_unknownMethod does not exist/is not available' });
  });

  it('provider_reports_4900_when_the_rpc_base_is_unreachable', async () => {
    const p = provider('default', { rpc: `${closedBase}/rpc` });
    const err = await failure(p.request({ method: 'eth_blockNumber' }));
    expect(err).toBeInstanceOf(DisconnectedError);
    expect(err).toMatchObject({ code: 4900 });
  });

  it('provider_confirm_hook_rejection_is_4001_and_sends_nothing', async () => {
    const asked: string[] = [];
    const p = provider('default', {
      confirm: (r) => {
        asked.push(r.method);
        return false;
      },
    });
    await p.request({ method: 'eth_requestAccounts' });
    const before = seen.length;
    const err = await failure(p.request({ method: 'personal_sign', params: [vectors.message, vectors.address] }));
    expect(err).toBeInstanceOf(UserRejectedRequestError);
    expect(err).toMatchObject({ code: 4001 });
    expect(asked).toEqual(['personal_sign']);
    expect(seen.length).toBe(before);
  });

  it('provider_disconnect_clears_accounts_and_emits_events', async () => {
    const p = await connected();
    const events = record(p);
    let removed = 0;
    const counter = () => {
      removed += 1;
    };
    p.on('disconnect', counter);
    p.removeListener('disconnect', counter);
    p.disconnect();
    expect(p.isConnected()).toBe(false);
    expect(events.map((e) => e.event)).toEqual(['accountsChanged', 'disconnect']);
    expect(events[0]!.payload).toEqual([]);
    expect(events[1]!.payload).toBeInstanceOf(DisconnectedError);
    expect((events[1]!.payload as ProviderRpcError).code).toBe(4900);
    expect(removed).toBe(0);
  });

  it('provider_emits_chainChanged_when_the_gateway_reports_another_chain', async () => {
    const p = new PaxeerProvider({ gatewayUrl: `${base}/default`, rpcUrl: `${base}/rpc`, token: () => TOKEN, chainId: 1 });
    const events = record(p);
    await p.request({ method: 'eth_requestAccounts' });
    expect(events.map((e) => e.event)).toEqual(['connect', 'chainChanged', 'accountsChanged']);
    expect(events[1]!.payload).toBe('0x7d');
    expect(await p.request({ method: 'eth_chainId' })).toBe('0x7d');
  });

  it('provider_rejects_a_request_without_a_method', async () => {
    const err = await failure(provider().request({ method: '' }));
    expect(err).toMatchObject({ code: -32602, field: 'method' });
  });
});

describe('gas station through PaxeerProvider', () => {
  const paymaster = `0x${'5f'.repeat(20)}` as Hex;

  function vectorBatch(): SponsoredBatch {
    const v = vectors.sponsoredBatch;
    return {
      chainId: BigInt(v.chainId),
      account: v.account,
      nonce: BigInt(v.nonce),
      calls: v.calls.map((call) => ({ to: call.to, value: BigInt(call.value), data: call.data })),
      quote: {
        sponsor: v.quote.sponsor,
        token: v.quote.token,
        maxTokenAmount: BigInt(v.quote.maxTokenAmount),
        tokenAmount: BigInt(v.quote.tokenAmount),
        deadline: BigInt(v.quote.deadline),
        quoteNonce: BigInt(v.quote.quoteNonce),
        gasCost: BigInt(v.quote.gasCost),
        decimals: SIDIORA_DECIMALS,
      },
    };
  }

  async function station(asked: { method: string; params: readonly unknown[] }[]) {
    const p = new PaxeerProvider({
      gatewayUrl: `${base}/default`,
      rpcUrl: `${base}/rpc`,
      token: () => TOKEN,
      confirm: (request) => {
        asked.push(request);
        return true;
      },
    });
    await p.request({ method: 'eth_requestAccounts' });
    const module = gasStation(p, {
      chainId: 125n,
      sponsor: vectors.sponsoredBatch.quote.sponsor,
      paymaster,
      quoteUrl: `${base}/quote`,
    });
    return { p, module };
  }

  it('provider_gas_station_digest_equals_the_agent_sdk_and_the_recorded_vector', async () => {
    const { module } = await station([]);
    const batch = vectorBatch();
    const digest = module.digest(batch);
    expect(digest).toBe(vectors.sponsoredBatchDigest);
    const agent = agentBatchDigest(batch);
    expect(agent).toEqual({ ok: true, value: digest });
    expect(constructionDigest(toWireConstruction(module.construction(batch)))).toBe(digest);
  });

  it('provider_gas_station_sign_is_accepted_and_forwards_only_the_fields', async () => {
    const asked: { method: string; params: readonly unknown[] }[] = [];
    const { module } = await station(asked);
    const batch = vectorBatch();
    const before = seen.length;
    const signature = (await module.sign(batch)) as Hex;
    expect(signature).toBe(String(bodyOf('wallet-sign-digest-sponsored-batch').signature).toLowerCase());
    const construction = module.construction(batch);
    expect(asked.filter((request) => request.method === 'eth_sign').map((request) => request.params)).toEqual([
      [construction.account, vectors.sponsoredBatchDigest, construction],
    ]);
    const forwarded = seen.slice(before);
    expect(forwarded).toHaveLength(1);
    expect(forwarded[0]).toMatchObject({ method: 'POST', path: '/v1/wallet/sign-digest', body: { construction } });
    expect(JSON.stringify(forwarded[0]!.body)).not.toContain(vectors.sponsoredBatchDigest.slice(2));
    expect(await recoverAddress({ hash: module.digest(batch), signature })).toBe(vectors.address);
  });

  it('provider_gas_station_sign_parameters_with_an_altered_digest_or_field_are_refused', async () => {
    const asked: { method: string; params: readonly unknown[] }[] = [];
    const { p, module } = await station(asked);
    const batch = vectorBatch();
    await module.sign(batch);
    const [address, digest, construction] = asked.find((request) => request.method === 'eth_sign')!.params as [
      Hex,
      Hex,
      ReturnType<typeof module.construction>,
    ];
    const before = seen.length;
    const alteredDigest = `${digest.slice(0, -1)}${digest.endsWith('0') ? '1' : '0'}` as Hex;
    const wrongDigest = await failure(p.request({ method: 'eth_sign', params: [address, alteredDigest, construction] }));
    expect(wrongDigest).toBeInstanceOf(UnauthorizedError);
    expect(wrongDigest).toMatchObject({ code: 4100, reason: 'digest_mismatch' });
    const alteredField = { ...construction, quote: { ...construction.quote, tokenAmount: '311401' } };
    const wrongField = await failure(p.request({ method: 'eth_sign', params: [address, digest, alteredField] }));
    expect(wrongField).toBeInstanceOf(UnauthorizedError);
    expect(wrongField).toMatchObject({ code: 4100, reason: 'digest_mismatch' });
    const alteredCall = { ...construction, calls: [...construction.calls].reverse() };
    const wrongCall = await failure(p.request({ method: 'eth_sign', params: [address, digest, alteredCall] }));
    expect(wrongCall).toMatchObject({ code: 4100, reason: 'digest_mismatch' });
    const bare = await failure(p.request({ method: 'eth_sign', params: [address, digest] }));
    expect(bare).toMatchObject({ code: 4100, reason: 'missing_construction' });
    const reordered = await failure(p.request({ method: 'eth_sign', params: [address, construction] }));
    expect(reordered).toBeInstanceOf(InvalidParamsError);
    expect(reordered).toMatchObject({ code: -32602, field: 'digest' });
    expect(seen.length).toBe(before);
    const foreign = await failure(module.sign({ ...batch, account: '0x3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c' }));
    expect(foreign).toMatchObject({ code: 4100, reason: 'account_mismatch' });
    expect(seen.length).toBe(before);
  });
});

describe('WalletInterface', () => {
  async function script(wallet: WalletInterface): Promise<unknown[]> {
    const t = vectors.transaction;
    return [
      await wallet.accounts(),
      await wallet.chainId(),
      await wallet.signMessage(vectors.message),
      await wallet.signTypedData(vectors.typedData),
      await wallet.sendTransaction({
        to: t.to,
        value: BigInt(t.value),
        data: t.data,
        gas: BigInt(t.gas),
        maxFeePerGas: BigInt(t.maxFeePerGas),
        maxPriorityFeePerGas: BigInt(t.maxPriorityFeePerGas),
        nonce: t.nonce,
        chainId: t.chainId,
      }),
    ];
  }

  it('provider_parity_between_the_embedded_and_an_injected_provider', async () => {
    const embedded = new WalletInterface(provider());
    const injected = new WalletInterface(new LocalKeyProvider());
    expect(embedded.mode).toBe('embedded');
    expect(injected.mode).toBe('injected');
    const a = await script(embedded);
    const b = await script(injected);
    expect(a).toEqual(b);
    expect(a[0]).toEqual([vectors.address]);
    expect(a[1]).toBe(125);
    expect(a[4]).toBe(bodyOf('wallet-send').tx_hash);
  });

  it('provider_wallet_signCustody_is_refused_on_injected_providers', async () => {
    const injected = new WalletInterface(new LocalKeyProvider());
    const err = await failure(injected.signCustody(vectors.custody));
    expect(err).toBeInstanceOf(UnauthorizedError);
    expect(err).toMatchObject({ code: 4100, reason: 'custody_not_supported' });
    const embedded = new WalletInterface(provider());
    expect(await embedded.signCustody(vectors.custody)).toBe(bodyOf('wallet-sign-custody').signature);
  });

  it('provider_wallet_on_and_off_reach_the_underlying_provider', async () => {
    const local = new LocalKeyProvider();
    const wallet = new WalletInterface(local);
    const got: unknown[] = [];
    const listener = (payload: unknown) => got.push(payload);
    wallet.on('accountsChanged', listener);
    local.emit('accountsChanged', [vectors.address]);
    wallet.off('accountsChanged', listener);
    local.emit('accountsChanged', []);
    expect(got).toEqual([[vectors.address]]);
  });
});
