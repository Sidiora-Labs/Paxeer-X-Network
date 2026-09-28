// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import { SIDIORA_TOKEN, abiSelector, exchangeCancelOrderCall } from '@sidiora/layerx-sdk';
import { FEE_TOKEN_PRECOMPILE, SIDIORA_FEE_DENOM, feeToken, type IntentLeg, type ModuleTransaction } from '@paxeer/wallet';
import { FeeChoice } from './FeeChoice';
import { SurfaceFrame } from './Surface';
import { SPONSORED_UNAVAILABLE, useFeeSelection, useSurfaceWallet } from './useSurface';
import { SurfaceChain, click, mountSurface, text, until, type Mounted } from './test/chain';

let chain: SurfaceChain;
let mounted: Mounted | null = null;

beforeEach(() => {
    chain = new SurfaceChain();
});

afterEach(async () => {
    await mounted?.unmount();
    mounted = null;
});

const TRANSACTION: ModuleTransaction = exchangeCancelOrderCall(`0x${'22'.repeat(32)}`);
const RATE = '3114000';

const LEGS: IntentLeg[] = [
    {
        index: 0,
        mechanism: 'paxeer_transfer',
        domain: 'paxeer',
        source: { kind: 'paxeer-wallet' },
        destination: { kind: 'human' },
        money: { amount: '1000000', currency: 'SID' },
        fee: { amount: '7', currency: 'PAX' },
    },
    {
        index: 1,
        mechanism: 'kernel_send',
        domain: 'layerx',
        source: { kind: 'human' },
        destination: { kind: 'agent', account: 'agent:did:layerx:main' },
        money: { amount: '1000000', currency: 'SID' },
        fee: { amount: '1200', currency: 'SID' },
    },
    {
        index: 2,
        mechanism: 'budget_grant',
        domain: 'layerx',
        source: { kind: 'agent' },
        destination: { kind: 'agent-budget' },
        money: { amount: '500', currency: 'SID' },
        fee: { amount: '300', currency: 'SID' },
    },
];

function Harness({ legs }: { legs: readonly IntentLeg[] }) {
    const { provider, address } = useSurfaceWallet();
    const selection = useFeeSelection(provider, address);
    return (
        <SurfaceFrame title="Fees" connected={provider !== null}>
            <FeeChoice provider={provider} address={address} transaction={TRANSACTION} selection={selection} sidRate={RATE} legs={legs} />
        </SurfaceFrame>
    );
}

function choice(container: HTMLElement, id: string): HTMLElement {
    const node = container.querySelector<HTMLElement>(`[data-fee-choice="${id}"]`);
    if (!node) throw new Error(`no fee choice ${id}`);
    return node;
}

function part(container: HTMLElement, id: string, role: string): string {
    return choice(container, id).querySelector(`[data-role="${role}"]`)?.textContent ?? '';
}

describe('FeeChoice', () => {
    it('fee_choice_labels_every_fee_path_with_its_denomination_and_chain_gas', async () => {
        mounted = await mountSurface(chain, <Harness legs={LEGS} />);
        const { container } = mounted;
        await until(() => part(container, 'pax_gas', 'gas') === '0.000021 PAX', 'gas estimate');

        expect(part(container, 'pax_gas', 'label')).toBe('PAX gas');
        expect(part(container, 'pax_gas', 'denomination')).toBe('PAX · Paxeer · 18 decimals · native coin');
        expect(part(container, 'pax_gas', 'path')).toBe('Signed with eth_sendTransaction; gas is paid in PAX');
        expect(part(container, 'pax_gas', 'gas')).toBe('0.000021 PAX');

        expect(part(container, 'sid_sponsored', 'label')).toBe('SID sponsored');
        expect(part(container, 'sid_sponsored', 'denomination')).toBe(`SID · Sidiora · 6 decimals · token ${SIDIORA_TOKEN.toLowerCase()}`);
        expect(part(container, 'sid_sponsored', 'path')).toBe(
            'Signed with eth_sign as a sponsored_batch construction and submitted through the gateway; the sponsor is repaid in SID',
        );
        expect(part(container, 'sid_sponsored', 'gas')).toBe('0.000066 SID');

        expect(part(container, 'sid_native', 'label')).toBe('SID native');
        expect(part(container, 'sid_native', 'denomination')).toBe(`SID · Sidiora · 6 decimals · fee denom ${SIDIORA_FEE_DENOM}`);
        expect(part(container, 'sid_native', 'path')).toBe(
            `Signed with eth_sendTransaction; gas is charged in ${SIDIORA_FEE_DENOM} through the fee token precompile ${FEE_TOKEN_PRECOMPILE}`,
        );
        expect(part(container, 'sid_native', 'gas')).toBe('0.000066 SID');

        const legs = Array.from(container.querySelectorAll('[aria-label="LayerX fee per leg"] [data-layerx-leg]'));
        expect(legs.map((leg) => leg.getAttribute('data-layerx-leg'))).toEqual(['1', '2']);
        expect(legs.map((leg) => leg.textContent)).toEqual(['Leg 1 · kernel_send1200 SID', 'Leg 2 · budget_grant300 SID']);
        expect(text(container, '[aria-label="Fee path"]')).not.toContain('1200 SID');
        expect(chain.methods).toContain('eth_estimateGas');
        expect(chain.methods).toContain('eth_gasPrice');
    });

    it('fee_choice_switches_the_fee_token_preference_through_the_precompile', async () => {
        mounted = await mountSurface(chain, <Harness legs={[]} />);
        const { container } = mounted;
        await until(() => text(container, '[data-role="fee-denom"]') === 'Fee token: none, gas is paid in PAX', 'fee denom read');
        expect(container.querySelector('[data-role="fee-blocked"]')).toBeNull();
        expect(text(container, '[data-role="layerx-none"]')).toBe('This action has no LayerX leg');
        const getFeeDenom = feeToken(chain).getFeeDenomCallData(chain.address).toLowerCase();
        expect(chain.calls).toContainEqual({ to: FEE_TOKEN_PRECOMPILE, data: getFeeDenom });

        await click(choice(container, 'sid_native'), 'sid native');
        expect(text(container, '[data-role="fee-blocked"]')).toBe(`set ${SIDIORA_FEE_DENOM} as the fee token first`);
        const action = () => container.querySelector('[data-action="fee-preference"]');
        expect(action()?.textContent).toBe('Use SID for gas');
        await click(action(), 'use SID');
        await until(() => text(container, '[data-role="fee-denom"]') === `Fee token: ${SIDIORA_FEE_DENOM}`, 'fee denom set');
        expect(chain.sent).toEqual([
            { from: chain.address, to: FEE_TOKEN_PRECOMPILE, data: feeToken(chain).setFeeDenom(SIDIORA_FEE_DENOM).data, value: '0x0' },
        ]);
        expect(chain.sent[0]?.data.startsWith(abiSelector('setFeeDenom(string)'))).toBe(true);
        expect(container.querySelector('[data-role="fee-blocked"]')).toBeNull();

        await click(choice(container, 'pax_gas'), 'pax gas');
        expect(text(container, '[data-role="fee-blocked"]')).toBe('clear the fee token preference first');
        expect(action()?.textContent).toBe('Use PAX for gas');
        await click(action(), 'use PAX');
        await until(() => text(container, '[data-role="fee-denom"]') === 'Fee token: none, gas is paid in PAX', 'fee denom cleared');
        expect(chain.sent[1]).toEqual({ from: chain.address, to: FEE_TOKEN_PRECOMPILE, data: feeToken(chain).clearFeeDenom().data, value: '0x0' });
        expect(chain.sent[1]?.data).toBe(abiSelector('clearFeeDenom()'));

        await click(choice(container, 'sid_sponsored'), 'sid sponsored');
        expect(text(container, '[data-role="fee-blocked"]')).toBe(SPONSORED_UNAVAILABLE);
        expect(action()).toBeNull();
    });
});
