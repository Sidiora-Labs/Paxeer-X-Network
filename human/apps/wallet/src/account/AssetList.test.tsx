// @vitest-environment jsdom
import { afterAll, afterEach, beforeAll, beforeEach, describe, expect, it } from 'vitest';
import type { CompletionAsset } from '@paxeer/wallet';
import { AccountProvider } from './AccountProvider';
import { AssetList } from './AssetList';
import { OWNER, startAccountEndpoint, type AccountEndpoint, type Scenario } from './test/endpoint';
import { mount, type Mounted } from './test/render';

const COMPLETION: readonly CompletionAsset[] = [
    { kind: 'native', symbol: 'PAX', decimals: 18 },
    { kind: 'erc20', address: `0x${'33'.repeat(20)}`, symbol: 'USDL', decimals: 6 },
    { kind: 'erc20', address: `0x${'22'.repeat(20)}`, symbol: 'LXP', decimals: 6 },
    { kind: 'erc20', address: `0x${'44'.repeat(20)}` },
];

let endpoint: AccountEndpoint;
let view: Mounted | null = null;

beforeAll(async () => {
    endpoint = await startAccountEndpoint();
});

afterAll(async () => {
    await endpoint.close();
});

beforeEach(() => {
    endpoint.seen.length = 0;
    endpoint.unmatched.length = 0;
});

afterEach(async () => {
    await view?.unmount();
    view = null;
});

async function render(scenario: Scenario): Promise<Mounted> {
    view = await mount(
        <AccountProvider config={endpoint.config(scenario)}>
            <AssetList account={OWNER} assets={COMPLETION} />
        </AccountProvider>,
    );
    return view;
}

describe('AssetList', () => {
    it('shows joined balances on both sides and completes the rest from the chain', async () => {
        const mounted = await render('available');
        await mounted.until(() => mounted.query('[data-join-limit]') !== null, 'balances');
        const lxp = mounted.query(`[data-asset="${'aa'.repeat(32)}"]`);
        expect(lxp?.textContent).toContain('LXP');
        expect(lxp?.querySelector('[data-side="paxeer"]')?.textContent).toBe('Paxeer 0.125LXP');
        expect(lxp?.querySelector('[data-side="layerx"]')?.textContent).toBe('LayerX 0.75LXP');
        const sid = mounted.query(`[data-asset="${'bb'.repeat(32)}"]`);
        expect(sid?.querySelector('[data-side="paxeer"]')?.textContent).toBe('Paxeer no balance');
        expect(sid?.querySelector('[data-side="layerx"]')?.textContent).toBe('LayerX no balance');

        expect(mounted.text('[data-completed="native:PAX"]')).toContain('Chain balance');
        expect(mounted.text('[data-completed="native:PAX"]')).toContain('1.0PAX');
        expect(mounted.text(`[data-completed="0x${'33'.repeat(20)}"]`)).toContain('Token contract balance');
        expect(mounted.text(`[data-completed="0x${'33'.repeat(20)}"]`)).toContain('1.0USDL');
        expect(mounted.query(`[data-completed="0x${'22'.repeat(20)}"]`)).toBeNull();
        expect(mounted.text(`[data-completed="0x${'44'.repeat(20)}"]`)).toContain('execution reverted');
        expect(mounted.query('[data-join-limit]')?.getAttribute('data-join-limit')).toBe('16');
        expect(mounted.text('[data-join-limit]')).toBe('2 of at most 16 joined assets.');
        expect(endpoint.unmatched).toEqual([]);
    });

    it('shows the unavailable kernel state instead of balances when the kernel is unreachable', async () => {
        const mounted = await render('kernel-unreachable');
        await mounted.until(() => mounted.query('[data-kernel="unavailable"]') !== null, 'kernel notice');
        expect(mounted.text('[data-kernel="unavailable"]')).toBe('The LayerX kernel is unreachable (public core)');
        expect(mounted.query('[data-asset]')).toBeNull();
        expect(mounted.query('[role="alert"]')).toBeNull();
    });
});
