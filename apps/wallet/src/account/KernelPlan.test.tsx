// @vitest-environment jsdom
import { afterAll, afterEach, beforeAll, beforeEach, describe, expect, it } from 'vitest';
import type { PlanIntentRequest } from '@paxeer/wallet';
import { AccountProvider } from './AccountProvider';
import { KernelPlan, legBindings } from './KernelPlan';
import { OWNER, OWNER_ACCOUNT, OWNER_DID, expectedSequence, golden, startAccountEndpoint, type AccountEndpoint, type Scenario } from './test/endpoint';
import { mount, type Mounted } from './test/render';

const planRequest = golden('intent.plan.request').body as PlanIntentRequest;
const submitRequest = golden('intent.submit.request');
const IDEMPOTENCY_KEY = submitRequest.headers?.['Idempotency-Key'] ?? '';
const MINUTES = 15;
const DEADLINE_MS = Date.parse(planRequest.constraints.deadline);
const NOW_MS = DEADLINE_MS - MINUTES * 60 * 1000;
const TOKEN = 'account-session-token';

let endpoint: AccountEndpoint;
let view: Mounted | null = null;

beforeAll(async () => {
    endpoint = await startAccountEndpoint();
    endpoint.now = Math.floor(NOW_MS / 1000);
});

afterAll(async () => {
    await endpoint.close();
});

beforeEach(() => {
    endpoint.seen.length = 0;
    endpoint.unmatched.length = 0;
    endpoint.refusals.length = 0;
});

afterEach(async () => {
    await view?.unmount();
    view = null;
});

async function render(scenario: Scenario): Promise<Mounted> {
    view = await mount(
        <AccountProvider config={endpoint.config(scenario)} authorization={() => TOKEN}>
            <KernelPlan account={OWNER} now={() => NOW_MS} idempotencyKey={() => IDEMPOTENCY_KEY} />
        </AccountProvider>,
    );
    return view;
}

async function fill(mounted: Mounted, amount = planRequest.money.amount): Promise<void> {
    await mounted.type('input[name="destination"]', planRequest.destination.account ?? '');
    await mounted.type('input[name="asset"]', planRequest.asset_id);
    await mounted.type('input[name="amount"]', amount);
    await mounted.type('input[name="currency"]', planRequest.money.currency);
    await mounted.type('input[name="max-fee"]', planRequest.constraints.max_fee.amount);
    await mounted.type('input[name="minutes"]', String(MINUTES));
}

function button(mounted: Mounted, name: string): HTMLButtonElement {
    const found = mounted.query<HTMLButtonElement>(`button[name="${name}"]`);
    if (!found) throw new Error(`no ${name} button`);
    return found;
}

describe('KernelPlan', () => {
    it('plans, presents, submits with the plan digest and bindings, and tracks the journey', async () => {
        const mounted = await render('available');
        await mounted.until(() => mounted.query('[data-kernel="available"]') !== null, 'kernel state');
        expect(button(mounted, 'plan').disabled).toBe(true);
        await fill(mounted);
        expect(button(mounted, 'plan').disabled).toBe(false);
        await mounted.click('button[name="plan"]');
        await mounted.until(() => mounted.query('[data-plan]') !== null, 'plan');

        const planBody = endpoint.seen.find((entry) => entry.scope === 'human' && entry.path === '/v1/intents/plan');
        expect(planBody?.body).toEqual(planRequest);
        expect(planBody?.authorization).toBe(`Bearer ${TOKEN}`);
        expect(mounted.query('[data-plan]')?.getAttribute('data-plan')).toBe('4b'.repeat(32));
        expect(mounted.all('[data-leg]').map((leg) => leg.getAttribute('data-domain'))).toEqual(['paxeer', 'layerx']);
        expect(mounted.text('[data-leg="0"]')).toContain('paxeer custody deposit on Paxeer');
        expect(mounted.text('[data-leg="0"]')).toContain('Paxeer wallet to agent:did:layerx:alice:main');
        expect(mounted.text('[data-leg="1"]')).toContain('transfer on LayerX');
        expect(mounted.text('[data-leg="1"]')).toContain('agent:did:layerx:alice:main to agent:did:layerx:bob:main');
        expect(mounted.text('[data-fee="0"]')).toBe('Fee 1 LXP');
        expect(mounted.text('[data-fee="1"]')).toBe('Fee 8 LXP');
        expect(mounted.text('[data-signer="0"]')).toBe('Signer custody-key');
        expect(mounted.text('[data-signer="1"]')).toBe('Signer agent-authority');
        expect(mounted.text('[data-total-fee]')).toBe('Total fee 9 LXP');

        await mounted.until(() => !button(mounted, 'submit').disabled, 'submit enabled');
        await mounted.click('button[name="submit"]');
        await mounted.until(() => mounted.query('[data-journey]') !== null || mounted.query('[data-plan-refused]') !== null, 'journey');
        expect(endpoint.refusals).toEqual([]);
        expect(mounted.query('[data-plan-refused]')).toBeNull();

        const submit = endpoint.seen.find((entry) => entry.scope === 'human' && entry.path === '/v1/intents/submit');
        expect(submit?.idempotencyKey).toBe(IDEMPOTENCY_KEY);
        const body = submit?.body as { plan_digest: string; signed_digest: string; bindings: { account_sequence: number; actor: string; not_after: number }[] };
        expect(body.plan_digest).toBe('4b'.repeat(32));
        expect(body.signed_digest).toBe(body.plan_digest);
        expect(body.bindings.map((binding) => binding.account_sequence)).toEqual([expectedSequence(), expectedSequence()]);
        expect(body.bindings.every((binding) => binding.actor === OWNER_DID)).toBe(true);
        expect(body.bindings.every((binding) => binding.not_after === Math.floor(DEADLINE_MS / 1000))).toBe(true);
        expect(endpoint.methods('available')).toContain('lx_getSequence');
        expect(
            endpoint.seen.find((entry) => entry.scope === 'available' && entry.methods.includes('lx_getSequence'))?.body,
        ).toMatchObject({ params: [OWNER_ACCOUNT] });

        const journey = mounted.query('[data-journey]');
        expect(journey?.getAttribute('data-journey')).toBe('jrn_01j2gx3fam9kq4vte8n5w6y7z8');
        expect(journey?.getAttribute('data-state')).toBe('processing');
        expect(mounted.all('[data-rung]').map((rung) => [rung.getAttribute('data-rung'), rung.getAttribute('data-reached')])).toEqual([
            ['instant', 'true'],
            ['sealed', 'false'],
            ['final', 'false'],
        ]);
        expect(mounted.all('[data-stage]').map((stage) => stage.getAttribute('data-state'))).toEqual(['done', 'done', 'processing']);
        expect(endpoint.unmatched).toEqual([]);
    });

    it('shows the service refusal when the plan does not match', async () => {
        const mounted = await render('available');
        await mounted.until(() => mounted.query('[data-kernel="available"]') !== null, 'kernel state');
        await fill(mounted, '400000');
        await mounted.click('button[name="plan"]');
        await mounted.until(() => mounted.query('[data-plan-refused]') !== null, 'refusal');
        expect(mounted.query('[data-plan]')).toBeNull();
        expect(mounted.query('button[name="submit"]')).toBeNull();
    });

    it.each([
        ['kernel-unreachable', 'The LayerX kernel is unreachable'],
        ['no-finalised-checkpoint', 'The LayerX kernel has no finalised checkpoint on the chain yet'],
        ['kernel-not-configured', 'The LayerX kernel is not configured on the endpoint'],
    ] as const)('disables planning with the reason when the kernel is %s', async (scenario, reason) => {
        const mounted = await render(scenario);
        await mounted.until(() => mounted.query('[data-kernel="unavailable"]') !== null, 'kernel notice');
        await fill(mounted);
        expect(mounted.text('[data-kernel="unavailable"]')).toBe(reason);
        expect(button(mounted, 'plan').disabled).toBe(true);
        await mounted.click('button[name="plan"]');
        expect(endpoint.seen.filter((entry) => entry.scope === 'human')).toEqual([]);
    });
});

describe('legBindings', () => {
    const plan = (golden('intent.plan.response').body as { result: Parameters<typeof legBindings>[0] }).result;
    const document = { evm_address: OWNER, pax_address: null, layerx_did: OWNER_DID, layerx_account: OWNER_ACCOUNT, bound: true } as Parameters<typeof legBindings>[1];
    const window = { notBefore: 10, notAfter: 20 };

    it('consumes an account sequence only after a LayerX leg', () => {
        const reordered = { ...plan, legs: [...plan.legs].reverse().map((leg, index) => ({ ...leg, index })) };
        const reorderedPlan = {
            ...reordered,
            signing_requirements: plan.signing_requirements.map((requirement, index) => ({ ...requirement, leg_index: 1 - index })),
        };
        expect(legBindings(plan, document, 8, window, { amount: '250', currency: 'LXP' }).map((binding) => binding.account_sequence)).toEqual([8, 8]);
        expect(legBindings(reorderedPlan, document, 8, window, { amount: '250', currency: 'LXP' }).map((binding) => binding.account_sequence)).toEqual([8, 9]);
    });

    it('refuses an unbound account and a leg fee above the maximum fee', () => {
        expect(() => legBindings(plan, { ...document, bound: false }, 8, window, { amount: '250', currency: 'LXP' })).toThrow(/not bound/);
        expect(() => legBindings(plan, document, 8, window, { amount: '7', currency: 'LXP' })).toThrow(/leg 1 exceeds the maximum fee/);
        expect(() => legBindings(plan, document, 8, window, { amount: '250', currency: 'SID' })).toThrow(/leg 0 exceeds the maximum fee/);
    });
});
