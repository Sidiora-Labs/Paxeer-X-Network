import { createServer, type IncomingMessage, type ServerResponse } from 'node:http';
import { readFileSync, readdirSync } from 'node:fs';
import type { AddressInfo } from 'node:net';
import path from 'node:path';
import { ethers } from 'ethers';
import type { WalletConfig } from '../config';

export const FIXTURE_DIR = `${path.resolve(__dirname, '../../../../../wallet/sdk/test/fixtures/gateway')}/`;
export const ACCESS_TOKEN = 'synthetic-access-token';
export const EMAIL = 'holder@example.test';
export const EMAIL_CODE = '424242';
export const IDENTITY_KEY = 'synthetic-publishable-key';
export const FIXTURE_KEY = ethers.keccak256(ethers.toUtf8Bytes('paxeer-wallet-sdk-fixture'));

export interface GatewayFixture {
    readonly scenario: string;
    readonly method: string;
    readonly path?: string;
    readonly status: number;
    readonly body: Record<string, unknown>;
}

export interface Constructions {
    readonly address: string;
    readonly message: string;
    readonly typedData: {
        readonly domain: Record<string, unknown>;
        readonly types: Record<string, Array<{ name: string; type: string }>>;
        readonly primaryType: string;
        readonly message: Record<string, unknown>;
    };
    readonly transaction: {
        readonly to: string;
        readonly value: string;
        readonly data: string;
        readonly gas: string;
        readonly maxFeePerGas: string;
        readonly maxPriorityFeePerGas: string;
        readonly nonce: number;
        readonly chainId: number;
    };
}

export function loadFixtures(): GatewayFixture[] {
    return readdirSync(FIXTURE_DIR)
        .filter((name) => name.endsWith('.json') && name !== 'constructions.json')
        .map((name) => JSON.parse(readFileSync(`${FIXTURE_DIR}${name}`, 'utf8')) as GatewayFixture);
}

export function loadConstructions(): Constructions {
    return JSON.parse(readFileSync(`${FIXTURE_DIR}constructions.json`, 'utf8')) as Constructions;
}

export function fixtureBody(scenario: string, method: string, path: string): Record<string, unknown> {
    const found = loadFixtures().find((f) => f.scenario === scenario && f.method === method && f.path === path);
    if (!found) throw new Error(`no fixture for ${scenario} ${method} ${path}`);
    return found.body;
}

export interface RecordedRequest {
    readonly method: string;
    readonly path: string;
    readonly authorization: string | null;
    readonly body: unknown;
}

export interface TestGateway {
    readonly url: string;
    readonly config: WalletConfig;
    readonly requests: RecordedRequest[];
    scenario: 'default' | 'unprovisioned';
    close(): Promise<void>;
}

function readBody(req: IncomingMessage): Promise<unknown> {
    return new Promise((resolve, reject) => {
        const chunks: Buffer[] = [];
        req.on('data', (chunk: Buffer) => chunks.push(chunk));
        req.on('end', () => {
            const text = Buffer.concat(chunks).toString('utf8');
            if (!text) return resolve(null);
            try {
                resolve(JSON.parse(text));
            } catch (error) {
                reject(error);
            }
        });
        req.on('error', reject);
    });
}

function send(res: ServerResponse, status: number, body: unknown): void {
    res.writeHead(status, {
        'content-type': 'application/json',
        'access-control-allow-origin': '*',
        'access-control-allow-headers': '*',
        'access-control-allow-methods': 'GET, POST, OPTIONS',
    });
    res.end(body === null ? '' : JSON.stringify(body));
}

async function minedResponse(hash: string): Promise<Record<string, unknown>> {
    const tx = loadConstructions().transaction;
    const signed = await new ethers.Wallet(FIXTURE_KEY).signTransaction({
        type: 2,
        to: tx.to,
        value: BigInt(tx.value),
        data: tx.data,
        gasLimit: BigInt(tx.gas),
        maxFeePerGas: BigInt(tx.maxFeePerGas),
        maxPriorityFeePerGas: BigInt(tx.maxPriorityFeePerGas),
        nonce: tx.nonce,
        chainId: tx.chainId,
    });
    const parsed = ethers.Transaction.from(signed);
    const signature = parsed.signature;
    if (!signature || !parsed.from) throw new Error('the fixture transaction did not sign');
    return {
        hash,
        type: '0x2',
        from: parsed.from,
        to: parsed.to,
        nonce: ethers.toQuantity(parsed.nonce),
        gas: ethers.toQuantity(parsed.gasLimit),
        maxFeePerGas: ethers.toQuantity(parsed.maxFeePerGas ?? 0n),
        maxPriorityFeePerGas: ethers.toQuantity(parsed.maxPriorityFeePerGas ?? 0n),
        gasPrice: ethers.toQuantity(parsed.maxFeePerGas ?? 0n),
        value: ethers.toQuantity(parsed.value),
        input: parsed.data,
        chainId: ethers.toQuantity(parsed.chainId),
        accessList: [],
        r: signature.r,
        s: signature.s,
        v: ethers.toQuantity(signature.yParity),
        yParity: ethers.toQuantity(signature.yParity),
        blockHash: null,
        blockNumber: null,
        transactionIndex: null,
    };
}

function identitySession() {
    const now = Math.floor(Date.now() / 1000);
    return {
        access_token: ACCESS_TOKEN,
        token_type: 'bearer',
        expires_in: 3600,
        expires_at: now + 3600,
        refresh_token: 'synthetic-refresh-token',
        user: {
            id: '0b7d3c8e-1f2a-4b5c-8d9e-0a1b2c3d4e5f',
            aud: 'authenticated',
            role: 'authenticated',
            email: EMAIL,
            app_metadata: { provider: 'email' },
            user_metadata: {},
            created_at: new Date(0).toISOString(),
        },
    };
}

export async function startGateway(): Promise<TestGateway> {
    const requests: RecordedRequest[] = [];
    const fixtures = loadFixtures();
    const sentHashes = new Set<string>();
    const state = { scenario: 'default' as 'default' | 'unprovisioned', provisioned: false };

    const server = createServer((req, res) => {
        void (async () => {
            const url = new URL(req.url ?? '/', 'http://127.0.0.1');
            if (req.method === 'OPTIONS') return send(res, 204, null);
            const body = await readBody(req);
            const authorization = req.headers.authorization ?? null;
            requests.push({ method: req.method ?? 'GET', path: url.pathname, authorization, body });

            if (url.pathname.startsWith('/auth/v1/')) {
                if (req.headers.apikey !== IDENTITY_KEY) return send(res, 401, { message: 'invalid api key' });
                if (url.pathname === '/auth/v1/otp') return send(res, 200, {});
                if (url.pathname === '/auth/v1/verify') {
                    const payload = body as { email?: string; token?: string; type?: string } | null;
                    if (payload?.email !== EMAIL || payload.token !== EMAIL_CODE || payload.type !== 'email') {
                        return send(res, 403, { code: 403, error_code: 'otp_expired', msg: 'Token has expired or is invalid' });
                    }
                    return send(res, 200, identitySession());
                }
                if (url.pathname === '/auth/v1/logout') return send(res, 204, null);
                return send(res, 404, { message: 'not found' });
            }

            if (url.pathname === '/rpc') {
                const call = body as { id: number; method: string; params?: unknown[] };
                if (call.method === 'eth_chainId') return send(res, 200, { jsonrpc: '2.0', id: call.id, result: '0x7d' });
                if (call.method === 'eth_getTransactionByHash') {
                    const hash = String(call.params?.[0] ?? '').toLowerCase();
                    const result = sentHashes.has(hash) ? await minedResponse(hash) : null;
                    return send(res, 200, { jsonrpc: '2.0', id: call.id, result });
                }
                const rpc = fixtures.find((f) => f.scenario === 'rpc' && f.method === call.method);
                if (rpc) return send(res, rpc.status, { ...rpc.body, id: call.id });
                return send(res, 200, { jsonrpc: '2.0', id: call.id, error: { code: -32601, message: 'method not found' } });
            }

            if (authorization !== `Bearer ${ACCESS_TOKEN}`) {
                return send(res, 401, { error: 'unauthorized', message: 'missing or invalid bearer' });
            }
            let scenario: string = state.scenario;
            if (state.scenario === 'unprovisioned' && state.provisioned && url.pathname === '/v1/wallet/me') {
                scenario = 'default';
            }
            let fixture = fixtures.find((f) => f.scenario === scenario && f.method === req.method && f.path === url.pathname);
            if (!fixture) {
                fixture = fixtures.find((f) => f.scenario === 'default' && f.method === req.method && f.path === url.pathname);
            }
            if (!fixture) return send(res, 404, { error: 'not_found', message: `${req.method} ${url.pathname}` });
            if (url.pathname === '/v1/wallet/provision') state.provisioned = true;
            const hash = fixture.body.tx_hash;
            if (typeof hash === 'string') sentHashes.add(hash.toLowerCase());
            return send(res, fixture.status, fixture.body);
        })().catch((error: unknown) => {
            send(res, 500, { error: 'test_gateway', message: error instanceof Error ? error.message : String(error) });
        });
    });

    await new Promise<void>((resolve) => server.listen(0, '127.0.0.1', resolve));
    const { port } = server.address() as AddressInfo;
    const url = `http://127.0.0.1:${port}`;
    return {
        url,
        config: {
            gatewayUrl: url,
            rpcUrl: `${url}/rpc`,
            identityUrl: url,
            identityKey: IDENTITY_KEY,
            authRedirectUrl: null,
            chainId: 125,
        },
        requests,
        get scenario() {
            return state.scenario;
        },
        set scenario(value) {
            state.scenario = value;
            state.provisioned = false;
        },
        close: () =>
            new Promise<void>((resolve, reject) => {
                server.closeAllConnections();
                server.close((error) => (error ? reject(error) : resolve()));
            }),
    };
}
