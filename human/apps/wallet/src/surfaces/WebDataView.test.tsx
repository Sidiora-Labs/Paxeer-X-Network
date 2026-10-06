// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import { XWEB_PRECOMPILE, abiSelector } from '@sidiora/layerx-sdk';
import { WEB_DATA_EVENTS, WEB_DATA_KIND_SEARCH, webData, type WalletCapsSnapshot, type WalletCapsState } from '@paxeer/wallet';
import { WebDataView } from './WebDataView';
import {
    SurfaceChain,
    click,
    eventVectors,
    expectedField,
    mountSurface,
    radio,
    text,
    typeInto,
    until,
    vectorLog,
    type Mounted,
} from './test/chain';

let chain: SurfaceChain;
let mounted: Mounted | null = null;

beforeEach(() => {
    chain = new SurfaceChain();
    chain.xwebFee = 1_000_000_000_000_000n;
});

afterEach(async () => {
    await mounted?.unmount();
    mounted = null;
});

const BUDGET_ID = 'b1'.repeat(32);
const GRANT_ID = 'a1'.repeat(32);
const CAPS: WalletCapsSnapshot = {
    state: 'ready',
    did: 'did:layerx:caps-fixture',
    account_id: 'c3'.repeat(32),
    network_id: 229,
    context: { principal: 'caps-fixture', session_id: 'session-1', address: `0x${'11'.repeat(20)}`, chain_id: 229, expires_at: '1900000000' },
    observation: { verification: 'state_proven', state_root: 'e5'.repeat(32), sequence: '42', observed_head: '43', batch: '7' },
    budgets: [{
        id: BUDGET_ID, owner: 'c3'.repeat(32), asset: 'd4'.repeat(32), account: 'c3'.repeat(32), source_account: null,
        limit: '250000', configured_limit: '250000', spent: '1000', remaining: '249000', carry_cap: '0', carried: '0',
        period_start: '1800000000', period_length: '86400', expiry: '1900000000', revocation_sequence: '0', closed: false, revoked: false,
    }],
    grants: [{
        id: GRANT_ID, owner: 'c3'.repeat(32), recipient: 'f6'.repeat(32), asset: 'd4'.repeat(32),
        per_draw_maximum: '1000', allowance: '50000', drawn_total: '0', drawn_this_period: '0',
        recurring: true, window_length: '86400', window_start: '1800000000', expiration: '1900000000',
        revocation_sequence: '0', revoked_at_sequence: '0', revoked: false, invoice_settled: false,
    }],
};
const UNAVAILABLE: WalletCapsState = { state: 'unavailable', reason: 'not_configured' };

const send = (container: HTMLElement) => container.querySelector<HTMLButtonElement>('[data-action="send"]');

describe('WebDataView', () => {
    it('web_data_view_sends_a_search_carrying_the_precompile_fee_and_shows_the_decoded_event', async () => {
        const vector = eventVectors('webData').find((candidate) => candidate.event === 'XWebRequested');
        if (!vector) throw new Error('no XWebRequested vector');
        chain.logsFor = () => [vectorLog(XWEB_PRECOMPILE, WEB_DATA_EVENTS, vector)];
        let refreshed = 0;
        mounted = await mountSurface(
            chain,
            <WebDataView sidRate={null} caps={UNAVAILABLE} refreshCaps={() => { refreshed += 1; }} />,
        );
        const { container } = mounted;

        await until(() => text(container, '[data-role="call-value"]') === 'This call carries 0.001 PAX', 'fee read');
        await click(radio(container, 'Web data action', 'Search'), 'search action');
        await typeInto(container, 'query', 'paxeer network');
        await typeInto(container, 'callbackGas', '150000');
        await until(() => send(container)?.disabled === false, 'send enabled');
        await click(send(container), 'send');
        await until(() => text(container, '[data-role="status"]') === 'confirmed', 'receipt');

        const expected = webData(chain).search('paxeer network', 150_000n, chain.xwebFee);
        expect(chain.sent).toEqual([
            { from: chain.address, to: XWEB_PRECOMPILE, data: expected.data, value: `0x${chain.xwebFee.toString(16)}` },
        ]);
        const data = chain.sent[0]?.data ?? '';
        expect(data.startsWith(abiSelector('request(uint8,bytes,uint64)'))).toBe(true);
        expect(BigInt(`0x${data.slice(10, 74)}`)).toBe(BigInt(WEB_DATA_KIND_SEARCH));
        expect(chain.calls.some((call) => call.to === XWEB_PRECOMPILE && call.data === abiSelector('fee()'))).toBe(true);

        const event = container.querySelector('[data-event="XWebRequested"]');
        expect(event).not.toBeNull();
        const spec = WEB_DATA_EVENTS.find((candidate) => candidate.name === 'XWebRequested');
        if (!spec) throw new Error('no XWebRequested spec');
        for (const [name, value] of Object.entries(vector.fields)) {
            expect(event?.querySelector(`[data-field="${name}"]`)?.textContent).toBe(expectedField(spec, name, value));
        }

        expect(container.querySelector('[data-caps-state="unavailable"] [role="status"]')?.textContent).toBe('not_configured');
        expect(container.querySelector('[data-grant]')).toBeNull();
        expect(refreshed).toBeGreaterThan(0);
    });

    it('web_data_view_requests_a_refund_through_the_module', async () => {
        mounted = await mountSurface(chain, <WebDataView sidRate={null} caps={UNAVAILABLE} refreshCaps={() => undefined} />);
        const { container } = mounted;
        await click(radio(container, 'Web data action', 'Refund'), 'refund action');
        await typeInto(container, 'requestId', '42');
        await until(() => send(container)?.disabled === false, 'send enabled');
        await click(send(container), 'send');
        await until(() => text(container, '[data-role="status"]') === 'confirmed', 'receipt');
        expect(chain.sent).toEqual([{ from: chain.address, to: XWEB_PRECOMPILE, data: webData(chain).refund(42n).data, value: '0x0' }]);
        expect(chain.sent[0]?.data.startsWith(abiSelector('refund(uint64)'))).toBe(true);
    });

    it('web_data_view_shows_402_draws_with_their_budget_and_allowance_caps', async () => {
        mounted = await mountSurface(
            chain,
            <WebDataView sidRate={null} caps={CAPS} refreshCaps={() => undefined} />,
        );
        const { container } = mounted;
        expect(text(container, '[data-role="caps-observation"]')).toBe('state_proven · sequence 42 · batch 7');
        const budget = container.querySelector(`[data-budget="${BUDGET_ID}"]`);
        expect(budget?.querySelector('[data-role="budget-cap"]')?.textContent).toBe('Period cap: 250000 units · spent 1000 · remaining 249000');
        const grant = container.querySelector(`[data-grant="${GRANT_ID}"]`);
        expect(grant?.querySelector('[data-role="draw-cap"]')?.textContent).toBe('Per-draw cap: 1000 units');
        expect(grant?.querySelector('[data-role="allowance-cap"]')?.textContent).toBe('Allowance cap: 50000 units · drawn 0');
        expect(grant?.textContent).toContain('Window 1800000000 + 86400');
    });
});
