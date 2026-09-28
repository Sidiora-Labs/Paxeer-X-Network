import { readFileSync } from 'node:fs';
import { createServer, type IncomingMessage, type ServerResponse } from 'node:http';
import type { AddressInfo } from 'node:net';
import path from 'node:path';
import type { AccountConfig } from '../config';

type Exchange = { request: { method: string; params: unknown[] }; response: Record<string, unknown> };
type Golden = { method?: string; path?: string; headers?: Record<string, string>; body?: unknown; status?: number };
type Binding = {
    leg_index: number;
    action_key: string;
    actor: string;
    authority: string;
    relationship: string;
    account_sequence: number;
    not_before: number;
    not_after: number;
    fee_limit: { amount: string; currency: string };
};
type Leg = { index: number; domain: string; fee: { amount: string; currency: string } };
type Plan = {
    plan_digest: string;
    legs: Leg[];
    signing_requirements: { leg_index: number; action_key: string; authority: string }[];
};

const SDK_ENDPOINT_DIR = path.resolve(__dirname, '../../../../../wallet/sdk/test/fixtures/endpoint');
const GOLDEN_DIR = path.resolve(__dirname, '../../../../../wallet/schema/human-api/golden');
const LOCAL_DIR = path.resolve(__dirname, 'fixtures');

export const SCENARIOS = ['available', 'kernel-unreachable', 'no-finalised-checkpoint', 'kernel-not-configured', 'deposit'] as const;
export type Scenario = (typeof SCENARIOS)[number];

export const OWNER = `0x${'11'.repeat(20)}`;
export const OWNER_DID = `did:layerx:${'61'.repeat(32)}`;
export const OWNER_ACCOUNT = '7c'.repeat(32);

function readJson<T>(file: string): T {
    return JSON.parse(readFileSync(file, 'utf8')) as T;
}

export function golden(name: string): Golden {
    return readJson<Golden>(path.join(GOLDEN_DIR, `${name}.json`));
}

export function recordings(scenario: Scenario): Exchange[] {
    const file = scenario === 'deposit' ? path.join(LOCAL_DIR, 'deposit.json') : path.join(SDK_ENDPOINT_DIR, `${scenario}.json`);
    return readJson<Exchange[]>(file);
}

export function explorerStatuses(): Record<string, unknown> {
    return {
        ...readJson<Record<string, unknown>>(path.join(SDK_ENDPOINT_DIR, 'explorer-status.json')),
        ...readJson<Record<string, unknown>>(path.join(LOCAL_DIR, 'deposit-explorer-status.json')),
    };
}

export function expectedSequence(): number {
    const read = recordings('available').find((entry) => entry.request.method === 'lx_getSequence');
    const result = read?.response.result as { next_sequence: string } | undefined;
    if (!result) throw new Error('the available recording carries no account sequence');
    return Number(result.next_sequence);
}

export interface SeenRequest {
    readonly scope: string;
    readonly path: string;
    readonly methods: readonly string[];
    readonly authorization: string | null;
    readonly idempotencyKey: string | null;
    readonly body: unknown;
}

export interface AccountEndpoint {
    readonly base: string;
    readonly seen: SeenRequest[];
    readonly unmatched: string[];
    readonly refusals: string[];
    now: number;
    config(scenario: Scenario): AccountConfig;
    methods(scenario: Scenario): string[];
    close(): Promise<void>;
}

function reply(res: ServerResponse, status: number, body: unknown): void {
    res.writeHead(status, { 'content-type': 'application/json', 'access-control-allow-origin': '*' });
    res.end(JSON.stringify(body));
}

function normalised(value: unknown): string {
    return JSON.stringify(value).toLowerCase();
}

function submitRefusal(body: unknown, now: number): string | null {
    const plan = (golden('intent.plan.response').body as { result: Plan }).result;
    const constraints = (golden('intent.plan.request').body as { constraints: { deadline: string; max_fee: { amount: string; currency: string } } })
        .constraints;
    const deadline = Math.floor(Date.parse(constraints.deadline) / 1000);
    const request = body as { plan_digest?: string; signed_digest?: string; bindings?: Binding[] };
    if (request.plan_digest !== plan.plan_digest) return 'plan_digest differs from the plan';
    if (request.signed_digest !== plan.plan_digest) return 'signed_digest differs from the plan digest';
    if (!Array.isArray(request.bindings) || request.bindings.length !== plan.legs.length) return 'one binding per leg is required';
    let sequence = expectedSequence();
    for (const leg of plan.legs) {
        const binding = request.bindings.find((entry) => entry.leg_index === leg.index);
        const requirement = plan.signing_requirements.find((entry) => entry.leg_index === leg.index);
        if (!binding || !requirement) return `leg ${leg.index} is not bound`;
        if (binding.action_key !== requirement.action_key) return `leg ${leg.index} action key differs`;
        if (binding.authority !== requirement.authority) return `leg ${leg.index} authority differs`;
        if (binding.actor !== OWNER_DID) return `leg ${leg.index} actor differs`;
        if (binding.relationship !== 'self') return `leg ${leg.index} relationship differs`;
        if (binding.fee_limit.currency !== constraints.max_fee.currency) return `leg ${leg.index} fee currency differs`;
        const limit = BigInt(binding.fee_limit.amount);
        if (limit < BigInt(leg.fee.amount) || limit > BigInt(constraints.max_fee.amount)) return `leg ${leg.index} fee limit is out of range`;
        if (binding.account_sequence !== sequence) return `leg ${leg.index} account sequence is stale`;
        if (binding.not_before > binding.not_after || binding.not_after > deadline) return `leg ${leg.index} window exceeds the deadline`;
        if (now < binding.not_before || now > binding.not_after) return `leg ${leg.index} window does not contain the submission time`;
        if (leg.domain === 'layerx') sequence += 1;
    }
    return null;
}

export async function startAccountEndpoint(): Promise<AccountEndpoint> {
    const seen: SeenRequest[] = [];
    const unmatched: string[] = [];
    const refusals: string[] = [];
    const recorded = Object.fromEntries(SCENARIOS.map((scenario) => [scenario, recordings(scenario)])) as Record<Scenario, Exchange[]>;
    const statuses = explorerStatuses();
    const state = { now: 0 };

    function answer(scenario: Scenario, call: { id: unknown; method: string; params: unknown[] }): unknown {
        const exchange = recorded[scenario].find(
            (entry) => entry.request.method === call.method && normalised(entry.request.params) === normalised(call.params),
        );
        if (!exchange) {
            unmatched.push(`${scenario} ${call.method} ${JSON.stringify(call.params)}`);
            return { jsonrpc: '2.0', id: call.id, error: { code: -32601, message: 'Method not found' } };
        }
        return { jsonrpc: '2.0', id: call.id, ...exchange.response };
    }

    function human(req: IncomingMessage, route: string, body: unknown, res: ServerResponse): void {
        const plan = golden('intent.plan.request');
        const submit = golden('intent.submit.request');
        const journey = golden('journey.get.request');
        if (req.method === 'POST' && route === plan.path) {
            const recordedAnswer = golden(JSON.stringify(body) === JSON.stringify(plan.body) ? 'intent.plan.response' : 'intent.plan.failure');
            reply(res, recordedAnswer.status ?? 500, recordedAnswer.body);
            return;
        }
        if (req.method === 'POST' && route === submit.path) {
            const refusal = submitRefusal(body, state.now);
            if (refusal) refusals.push(refusal);
            const recordedAnswer = golden(refusal ? 'intent.submit.failure' : 'intent.submit.response');
            reply(res, recordedAnswer.status ?? 500, recordedAnswer.body);
            return;
        }
        if (req.method === 'GET' && route.startsWith('/v1/journeys/')) {
            const recordedAnswer = golden(route === journey.path ? 'journey.get.response' : 'journey.get.failure');
            reply(res, recordedAnswer.status ?? 500, recordedAnswer.body);
            return;
        }
        reply(res, 404, { ok: false, error: { code: 'not-found', copy_key: 'error.request.not-found', retry: 'final' }, trace: 'trc_route' });
    }

    const server = createServer((req, res) => {
        const chunks: Buffer[] = [];
        req.on('data', (chunk: Buffer) => chunks.push(chunk));
        req.on('end', () => {
            if (req.method === 'OPTIONS') {
                res.writeHead(204, {
                    'access-control-allow-origin': '*',
                    'access-control-allow-headers': '*',
                    'access-control-allow-methods': 'GET, POST',
                });
                res.end();
                return;
            }
            const raw = Buffer.concat(chunks).toString('utf8');
            const body: unknown = raw.length > 0 ? JSON.parse(raw) : undefined;
            const url = new URL(req.url ?? '/', 'http://127.0.0.1');
            const [, scope = '', ...rest] = url.pathname.split('/');
            const route = `/${rest.join('/')}`;
            const header = (name: string) => {
                const value = req.headers[name];
                return typeof value === 'string' ? value : null;
            };
            const calls = Array.isArray(body) ? (body as { method: string }[]) : [];
            seen.push({
                scope,
                path: route,
                methods: scope === 'human' || scope === 'explorer' ? [req.method ?? ''] : calls.length > 0 ? calls.map((call) => call.method) : [(body as { method?: string } | undefined)?.method ?? ''],
                authorization: header('authorization'),
                idempotencyKey: header('idempotency-key'),
                body,
            });
            if (scope === 'human') {
                human(req, route, body, res);
                return;
            }
            if (scope === 'explorer') {
                const match = /^\/api\/v2\/transactions\/(0x[0-9a-fA-F]{64})\/status$/.exec(route);
                const status = match ? statuses[match[1].toLowerCase()] : undefined;
                if (status === undefined) {
                    unmatched.push(`explorer ${route}`);
                    reply(res, 404, { message: 'Not found' });
                    return;
                }
                reply(res, 200, status);
                return;
            }
            const scenario = scope as Scenario;
            if (!SCENARIOS.includes(scenario) || route !== '/rpc' || req.method !== 'POST') {
                unmatched.push(`${req.method ?? ''} ${url.pathname}`);
                reply(res, 404, { error: 'unknown route' });
                return;
            }
            const batch = Array.isArray(body);
            const answers = (batch ? (body as unknown[]) : [body]).map((call) =>
                answer(scenario, call as { id: unknown; method: string; params: unknown[] }),
            );
            reply(res, 200, batch ? answers : answers[0]);
        });
    });
    await new Promise<void>((resolve) => server.listen(0, '127.0.0.1', resolve));
    const base = `http://127.0.0.1:${(server.address() as AddressInfo).port}`;

    return {
        base,
        seen,
        unmatched,
        refusals,
        get now() {
            return state.now;
        },
        set now(value: number) {
            state.now = value;
        },
        config(scenario: Scenario): AccountConfig {
            return { endpointUrl: `${base}/${scenario}/rpc`, humanUrl: `${base}/human`, explorerUrl: `${base}/explorer` };
        },
        methods(scenario: Scenario): string[] {
            return seen.filter((entry) => entry.scope === scenario).flatMap((entry) => entry.methods);
        },
        close(): Promise<void> {
            return new Promise((resolve, reject) => server.close((error) => (error ? reject(error) : resolve())));
        },
    };
}
