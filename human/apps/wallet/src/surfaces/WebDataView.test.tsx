// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import { XWEB_PRECOMPILE, abiSelector, type PayerGrant } from '@sidiora/layerx-sdk';
import { WEB_DATA_EVENTS, WEB_DATA_KIND_SEARCH, webData } from '@paxeer/wallet';
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

const GRANT: PayerGrant = {
    grant_id: `0x${'a1'.repeat(32)}`,
    from: `0x${'b2'.repeat(32)}`,
    recipient: `0x${'c3'.repeat(32)}`,
    asset: `0x${'d4'.repeat(32)}`,
    per_draw_maximum: '1000',
    allowance: '50000',
    recurring: true,
    window_length: '86400',
    expiration: '1900000000',
    purpose_hash: `0x${'e5'.repeat(32)}`,
    has_reference: false,
    reference_hash: `0x${'00'.repeat(32)}`,
    revocation_sequence: '0',
    public_key: `0x${'f6'.repeat(32)}`,
    signature: `0x${'07'.repeat(64)}`,
};

const send = (container: HTMLElement) => container.querySelector<HTMLButtonElement>('[data-action="send"]');

describe('WebDataView', () => {
    it('web_data_view_sends_a_search_carrying_the_precompile_fee_and_shows_the_decoded_event', async () => {
        const vector = eventVectors('webData').find((candidate) => candidate.event === 'XWebRequested');
        if (!vector) throw new Error('no XWebRequested vector');
        chain.logsFor = () => [vectorLog(XWEB_PRECOMPILE, WEB_DATA_EVENTS, vector)];
        mounted = await mountSurface(
            chain,
            <WebDataView sidRate={null} kernel={{ available: false, reason: 'not_configured', backend: 'public_core' }} grants={[GRANT]} budget={null} />,
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

        expect(text(container, '[data-role="kernel-state"]')).toBe('402 draws are unavailable: not_configured (public_core)');
        expect(container.querySelector('[data-grant]')).toBeNull();
    });

    it('web_data_view_requests_a_refund_through_the_module', async () => {
        mounted = await mountSurface(chain, <WebDataView sidRate={null} kernel={null} grants={[]} budget={null} />);
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
            <WebDataView sidRate={null} kernel={{ available: true, reason: 'available' }} grants={[GRANT]} budget={{ amount: '250000', currency: 'SID' }} />,
        );
        const { container } = mounted;
        expect(text(container, '[data-role="budget"]')).toBe('Kernel budget: 250000 SID');
        const grant = container.querySelector(`[data-grant="${GRANT.grant_id}"]`);
        expect(grant?.querySelector('[data-role="draw-cap"]')?.textContent).toBe('Per-draw cap: 1000');
        expect(grant?.querySelector('[data-role="allowance-cap"]')?.textContent).toBe('Allowance cap: 50000');
        expect(grant?.textContent).toContain('Renews every 86400 s');
    });
});
