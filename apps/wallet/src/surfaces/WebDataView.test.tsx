// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import { ADDR_PRECOMPILE_ADDRESS, XWEB_PRECOMPILE, abiSelector } from '@sidiora/layerx-sdk';
import {
    GasStationError,
    LEDGER_PRECOMPILE,
    PAYER_GRANT_BYTES,
    WEB_DATA_EVENTS,
    WEB_DATA_KIND_SEARCH,
    ledgerLogFloor,
    webData,
    type WalletCapsSnapshot,
    type WalletCapsState,
} from '@paxeer/wallet';
import { encodeAbiParameters, encodePacked, keccak256, parseAbiParameters, size, toHex, type Hex } from 'viem';
import { WebDataView } from './WebDataView';
import { GRANT_LOG_BATCH, GRANT_LOG_WINDOW, GRANT_LOG_WINDOW_CEILING, boundDid, readGrantCaps, useWebDataCaps } from './useSurface';
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

const DID_PUBLIC_KEY: Hex = `0x${'5d'.repeat(32)}`;
const DID = `did:layerx:${'5d'.repeat(32)}`;
const ACCOUNT_ID: Hex = `0x${'c3'.repeat(32)}`;
const FOREIGN_ACCOUNT_ID: Hex = `0x${'c4'.repeat(32)}`;
const FLOOR = ledgerLogFloor(125)!;
const FOREIGN_ADDRESS = `0x${'f1'.repeat(20)}`;
const SPONSOR_ADDRESS = `0x${'5e'.repeat(20)}`;
const HEAD = { number: toHex(FLOOR + 42n), stateRoot: `0x${'e5'.repeat(32)}` };
const GET_LAYERX_DID = abiSelector('getLayerXDid(address)');
const ACCOUNT_ID_SELECTOR = abiSelector('accountId(bytes32,bytes32)');
const GET_PAYER_GRANT = abiSelector('getPayerGrant(bytes32)');
const PAYER_GRANT_ISSUED = keccak256(toHex('PayerGrantIssued(bytes32)'));

interface IssuedGrant {
    readonly grantId: Hex;
    readonly from: Hex;
    readonly drawnThisPeriod: bigint;
    readonly block: bigint;
    readonly expiration?: bigint;
    readonly sponsored?: boolean;
}

const issuingTx = (grant: IssuedGrant): Hex => keccak256(grant.grantId);

const OWNED_GRANT: IssuedGrant = { grantId: `0x${GRANT_ID}`, from: ACCOUNT_ID, drawnThisPeriod: 250n, block: FLOOR + 1n };
const FOREIGN_GRANT: IssuedGrant = { grantId: `0x${'a2'.repeat(32)}`, from: FOREIGN_ACCOUNT_ID, drawnThisPeriod: 0n, block: FLOOR + 1n };

function payerGrantAnswer(grant: IssuedGrant): Hex {
    const wire = encodePacked(
        ['bytes32', 'bytes32', 'bytes32', 'bytes32', 'uint128', 'uint128', 'bool', 'uint64', 'uint64', 'bytes32', 'bool', 'bytes32', 'uint64', 'bytes32', 'bytes'],
        [grant.grantId, grant.from, `0x${'f6'.repeat(32)}`, `0x${'d4'.repeat(32)}`, 1000n, 50000n, true, 86400n, grant.expiration ?? 1_900_000_000n,
            `0x${'77'.repeat(32)}`, false, `0x${'00'.repeat(32)}`, 0n, DID_PUBLIC_KEY, `0x${'99'.repeat(64)}`],
    );
    if (size(wire) !== PAYER_GRANT_BYTES) throw new Error(`payer grant wire is ${size(wire)} bytes`);
    return encodeAbiParameters(parseAbiParameters('bytes, uint128, uint128, uint64, uint64, bool, bool'), [
        wire, 1250n, grant.drawnThisPeriod, 1_800_000_000n, 0n, false, false,
    ]);
}

class GrantChain extends SurfaceChain {
    didPublicKey: Hex = DID_PUBLIC_KEY;
    finalised: typeof HEAD | null = HEAD;
    issued: readonly IssuedGrant[] = [];
    readonly logQueries: { fromBlock: string; toBlock: string }[] = [];
    readonly blockQueries: unknown[] = [];
    failLogsFrom: bigint | null = null;
    capsChainId = 125;
    readonly txQueries: string[] = [];
    inFlight = 0;
    peakInFlight = 0;
    holderRecords: ((params: readonly unknown[]) => unknown) | null = null;
    readonly holderQueries: (readonly unknown[])[] = [];

    override async request(args: { method: string; params?: readonly unknown[] | object }): Promise<unknown> {
        const params = Array.isArray(args.params) ? (args.params as readonly unknown[]) : [];
        if (args.method === 'eth_getBlockByNumber') {
            this.methods.push(args.method);
            this.blockQueries.push(params[0]);
            if (params[0] !== 'finalized') throw new Error('only the finalised block is served');
            return this.finalised === null ? null : { ...this.finalised, hash: `0x${'b7'.repeat(32)}` };
        }
        if (args.method === 'eth_chainId') return toHex(this.capsChainId);
        if (args.method === 'px_listHolderRecords') {
            this.methods.push(args.method);
            this.holderQueries.push(params);
            if (this.holderRecords === null) throw Object.assign(new Error('method not found'), { code: -32601 });
            return this.holderRecords(params);
        }
        if (args.method === 'eth_getTransactionByHash') {
            this.methods.push(args.method);
            this.txQueries.push(params[0] as string);
            const grant = this.issued.find((candidate) => issuingTx(candidate) === params[0]);
            if (!grant) return null;
            const own = grant.from === ACCOUNT_ID;
            if (grant.sponsored) return { hash: params[0], from: SPONSOR_ADDRESS, to: own ? this.address : FOREIGN_ADDRESS };
            return { hash: params[0], from: own ? this.address : FOREIGN_ADDRESS, to: LEDGER_PRECOMPILE };
        }
        if (args.method === 'eth_getLogs') {
            this.methods.push(args.method);
            const query = params[0] as { fromBlock: string; toBlock: string };
            this.logQueries.push(query);
            this.inFlight += 1;
            this.peakInFlight = Math.max(this.peakInFlight, this.inFlight);
            await new Promise((resolve) => setTimeout(resolve, 0));
            this.inFlight -= 1;
            if (this.failLogsFrom !== null && BigInt(query.fromBlock) === this.failLogsFrom) throw new Error('log range unavailable');
            return this.issued
                .filter((grant) => grant.block >= BigInt(query.fromBlock) && grant.block <= BigInt(query.toBlock))
                .map((grant) => ({ address: LEDGER_PRECOMPILE, topics: [PAYER_GRANT_ISSUED, grant.grantId], data: '0x', transactionHash: issuingTx(grant) }));
        }
        if (args.method === 'eth_call') {
            const call = params[0] as { to: string; data: string };
            const to = call.to.toLowerCase();
            const data = call.data.toLowerCase();
            const answer = this.grantView(to, data);
            if (answer !== null) {
                this.methods.push(args.method);
                this.calls.push({ to, data });
                return answer;
            }
        }
        return super.request(args);
    }

    private grantView(to: string, data: string): Hex | null {
        if (to === ADDR_PRECOMPILE_ADDRESS && data === `${GET_LAYERX_DID}${this.address.slice(2).padStart(64, '0')}`) {
            return encodeAbiParameters(parseAbiParameters('bytes32, string'), [this.didPublicKey, DID]);
        }
        if (to === LEDGER_PRECOMPILE && data === `${ACCOUNT_ID_SELECTOR}${this.didPublicKey.slice(2)}${'00'.repeat(32)}`) {
            return encodeAbiParameters(parseAbiParameters('bytes32'), [ACCOUNT_ID]);
        }
        const grant = this.issued.find((candidate) => to === LEDGER_PRECOMPILE && data === `${GET_PAYER_GRANT}${candidate.grantId.slice(2)}`);
        return grant ? payerGrantAnswer(grant) : null;
    }
}

function GrantSurface() {
    const { caps, refreshCaps } = useWebDataCaps();
    return <WebDataView sidRate={null} caps={caps} refreshCaps={refreshCaps} />;
}

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
        expect(text(container, '[data-role="caps-height"]')).toBe('Observed at height 43');
        expect(text(container, '[data-role="caps-principal"]')).toBe('Principal caps-fixture');
        expect(text(container, '[data-role="caps-expiry"]')).toBe('Earliest grant expiry 1900000000');
        const budget = container.querySelector(`[data-budget="${BUDGET_ID}"]`);
        expect(budget?.querySelector('[data-role="budget-cap"]')?.textContent).toBe('Period cap: 250000 units · spent 1000 · remaining 249000');
        const grant = container.querySelector(`[data-grant="${GRANT_ID}"]`);
        expect(grant?.querySelector('[data-role="draw-cap"]')?.textContent).toBe('Per-draw cap: 1000 units');
        expect(grant?.querySelector('[data-role="allowance-cap"]')?.textContent).toBe('Allowance cap: 50000 units · drawn 0');
        expect(grant?.textContent).toContain('Window 1800000000 + 86400');
    });

    it('web_data_view_reads_the_connected_account_caps_by_grant_id', async () => {
        const grants = new GrantChain();
        grants.xwebFee = chain.xwebFee;
        grants.issued = [FOREIGN_GRANT, OWNED_GRANT];
        mounted = await mountSurface(grants, <GrantSurface />);
        const { container } = mounted;

        await until(() => container.querySelector('[data-caps-state="ready"]') !== null, 'caps ready');
        expect(text(container, '[data-role="caps-observation"]')).toBe(`checkpoint_finalised · sequence ${FLOOR + 42n} · batch ${FLOOR + 42n}`);
        expect(text(container, '[data-role="caps-account"]')).toBe(`Account ${'c3'.repeat(32)}`);
        expect(text(container, '[data-role="caps-height"]')).toBe(`Observed at height ${FLOOR + 42n}`);
        expect(text(container, '[data-role="caps-principal"]')).toBe(`Principal ${DID}`);
        expect(text(container, '[data-role="caps-expiry"]')).toBe('Earliest grant expiry 1900000000');
        expect(container.querySelector('[data-budget]')).toBeNull();
        expect(container.querySelectorAll('[data-grant]')).toHaveLength(1);
        expect(container.querySelector(`[data-grant="${'a2'.repeat(32)}"]`)).toBeNull();
        const grant = container.querySelector(`[data-grant="${GRANT_ID}"]`);
        expect(grant?.querySelector('[data-role="draw-cap"]')?.textContent).toBe('Per-draw cap: 1000 units');
        expect(grant?.querySelector('[data-role="allowance-cap"]')?.textContent).toBe('Allowance cap: 50000 units · drawn 250');
        expect(grant?.textContent).toContain(`Asset ${'d4'.repeat(32)}`);
        expect(grant?.textContent).toContain('Window 1800000000 + 86400 · expiry 1900000000');
        expect(grant?.textContent).toContain('Open');

        expect(grants.blockQueries).not.toContain('latest');
        expect(grants.logQueries[0]).toEqual({ address: LEDGER_PRECOMPILE, topics: [PAYER_GRANT_ISSUED], fromBlock: toHex(FLOOR), toBlock: HEAD.number });
        expect(grants.calls).toEqual(expect.arrayContaining([
            { to: ADDR_PRECOMPILE_ADDRESS, data: `${GET_LAYERX_DID}${grants.address.slice(2).padStart(64, '0')}` },
            { to: LEDGER_PRECOMPILE, data: `${ACCOUNT_ID_SELECTOR}${DID_PUBLIC_KEY.slice(2)}${'00'.repeat(32)}` },
            { to: XWEB_PRECOMPILE, data: abiSelector('fee()') },
            { to: LEDGER_PRECOMPILE, data: `${GET_PAYER_GRANT}${OWNED_GRANT.grantId.slice(2)}` },
        ]));
        expect(grants.calls).not.toContainEqual({ to: LEDGER_PRECOMPILE, data: `${GET_PAYER_GRANT}${FOREIGN_GRANT.grantId.slice(2)}` });
        expect(grants.txQueries.sort()).toEqual([issuingTx(FOREIGN_GRANT), issuingTx(OWNED_GRANT)].sort());
        expect(grants.methods).not.toContain('lx_getWalletCaps');
        expect(text(container, '[data-role="caps-history"]')).toBe(`Grant history is limited to the chain scan window, blocks ${FLOOR} to ${FLOOR + 42n}`);
        expect(container.querySelector('[data-history-source="chain_scan"]')).not.toBeNull();
        expect(grants.holderQueries[0]).toEqual([grants.address.toLowerCase(), 'grant', null, 100]);
    });

    it('web_data_view_shows_empty_caps_for_a_bound_account_with_no_grant', async () => {
        const grants = new GrantChain();
        grants.xwebFee = chain.xwebFee;
        grants.issued = [FOREIGN_GRANT];
        mounted = await mountSurface(grants, <GrantSurface />);
        const { container } = mounted;

        await until(() => container.querySelector('[data-caps-state="empty"]') !== null, 'caps empty');
        expect(text(container, '[data-role="caps-empty"]')).toBe('No budgets or grants at this verified observation.');
        expect(text(container, '[data-role="caps-account"]')).toBe(`Account ${'c3'.repeat(32)}`);
        expect(text(container, '[data-role="caps-principal"]')).toBe(`Principal ${DID}`);
        expect(container.querySelector('[data-grant]')).toBeNull();
        expect(grants.calls.some((call) => call.data.startsWith(GET_PAYER_GRANT))).toBe(false);
        expect(grants.methods).not.toContain('lx_getWalletCaps');
    });

    it('web_data_view_refuses_caps_for_an_address_with_no_bound_did', async () => {
        const grants = new GrantChain();
        grants.xwebFee = chain.xwebFee;
        grants.didPublicKey = `0x${'00'.repeat(32)}`;
        grants.issued = [OWNED_GRANT];
        mounted = await mountSurface(grants, <GrantSurface />);
        const { container } = mounted;

        await until(() => container.querySelector('[data-caps-state="refused"]') !== null, 'caps refused');
        expect(container.querySelector('[data-caps-state="refused"] [role="status"]')?.textContent).toBe('The account or its evidence was refused.');
        expect(grants.methods).not.toContain('eth_getLogs');
    });

    it('web_data_view_quote_did_is_refused_for_an_address_with_no_bound_did', async () => {
        const grants = new GrantChain();
        await expect(boundDid(grants, grants.address)).resolves.toBe(DID);
        grants.didPublicKey = `0x${'00'.repeat(32)}`;
        const refusal = await boundDid(grants, grants.address).then(() => null, (cause: unknown) => cause);
        expect(refusal).toBeInstanceOf(GasStationError);
        expect((refusal as GasStationError).refusal).toEqual({ code: 'refused', field: 'did' });
    });

    it('web_data_view_reports_caps_unavailable_without_a_finalised_checkpoint', async () => {
        const grants = new GrantChain();
        grants.xwebFee = chain.xwebFee;
        grants.finalised = null;
        grants.issued = [OWNED_GRANT];
        mounted = await mountSurface(grants, <GrantSurface />);
        const { container } = mounted;

        await until(() => container.querySelector('[data-caps-state="unavailable"]') !== null, 'caps unavailable');
        expect(container.querySelector('[data-caps-state="unavailable"] [role="status"]')?.textContent).toBe('No finalised checkpoint is available yet.');
        expect(container.querySelector('[data-role="caps-observation"]')).toBeNull();
        expect(grants.methods).not.toContain('eth_getLogs');
    });

    it('web_data_view_reads_grant_logs_back_from_the_finalised_head_in_bounded_windows', async () => {
        const grants = new GrantChain();
        grants.xwebFee = chain.xwebFee;
        grants.finalised = { ...HEAD, number: toHex(FLOOR + 2n * GRANT_LOG_WINDOW + 5n) };
        grants.issued = [{ ...OWNED_GRANT, block: FLOOR + 3n }, { ...FOREIGN_GRANT, block: FLOOR + 2n * GRANT_LOG_WINDOW }];
        mounted = await mountSurface(grants, <GrantSurface />);
        const { container } = mounted;

        await until(() => container.querySelector('[data-caps-state="ready"]') !== null, 'caps ready');
        expect(container.querySelectorAll('[data-grant]')).toHaveLength(1);
        expect(text(container, '[data-role="caps-height"]')).toBe(`Observed at height ${FLOOR + 2n * GRANT_LOG_WINDOW + 5n}`);
        expect(grants.logQueries.slice(0, 3).map(({ fromBlock, toBlock }) => [BigInt(fromBlock), BigInt(toBlock)])).toEqual([
            [FLOOR + GRANT_LOG_WINDOW + 6n, FLOOR + 2n * GRANT_LOG_WINDOW + 5n],
            [FLOOR + 6n, FLOOR + GRANT_LOG_WINDOW + 5n],
            [FLOOR, FLOOR + 5n],
        ]);
    });

    it('web_data_view_keeps_every_grant_and_the_earliest_expiry_across_all_windows', async () => {
        const grants = new GrantChain();
        grants.xwebFee = chain.xwebFee;
        const head = FLOOR + 30n * GRANT_LOG_WINDOW;
        const older: IssuedGrant = { ...OWNED_GRANT, grantId: `0x${'a3'.repeat(32)}`, block: FLOOR + 2n, expiration: 1_850_000_000n };
        grants.finalised = { ...HEAD, number: toHex(head) };
        grants.issued = [{ ...OWNED_GRANT, block: head - 5n }, older];
        mounted = await mountSurface(grants, <GrantSurface />);
        const { container } = mounted;

        await until(() => container.querySelector('[data-caps-state="ready"]') !== null, 'caps ready');
        expect(container.querySelectorAll('[data-grant]')).toHaveLength(2);
        expect(container.querySelector(`[data-grant="${'a3'.repeat(32)}"]`)).not.toBeNull();
        expect(text(container, '[data-role="caps-expiry"]')).toBe('Earliest grant expiry 1850000000');
    });

    it('web_data_view_scans_every_window_down_to_the_ledger_floor_and_never_below_it', async () => {
        const grants = new GrantChain();
        grants.xwebFee = chain.xwebFee;
        const head = FLOOR + 25n * GRANT_LOG_WINDOW + 7n;
        grants.finalised = { ...HEAD, number: toHex(head) };
        grants.issued = [{ ...OWNED_GRANT, block: FLOOR - 1n }];
        mounted = await mountSurface(grants, <GrantSurface />);
        const { container } = mounted;

        await until(() => container.querySelector('[data-caps-state="empty"]') !== null, 'caps empty');
        const windows = grants.logQueries.map(({ fromBlock, toBlock }) => [BigInt(fromBlock), BigInt(toBlock)] as const).sort((x, y) => (x[0] < y[0] ? 1 : -1));
        expect(windows).toHaveLength(26);
        expect(windows[0]![1]).toBe(head);
        expect(windows[windows.length - 1]![0]).toBe(FLOOR);
        windows.forEach(([from, to], index) => {
            expect(to - from).toBeLessThan(GRANT_LOG_WINDOW);
            if (index > 0) expect(windows[index - 1]![0]).toBe(to + 1n);
        });
        expect(container.querySelector('[data-grant]')).toBeNull();
    });

    it('web_data_view_reads_grant_windows_in_bounded_parallel_batches', async () => {
        const grants = new GrantChain();
        grants.xwebFee = chain.xwebFee;
        grants.finalised = { ...HEAD, number: toHex(FLOOR + 20n * GRANT_LOG_WINDOW) };
        grants.issued = [OWNED_GRANT];
        mounted = await mountSurface(grants, <GrantSurface />);
        const { container } = mounted;

        await until(() => container.querySelector('[data-caps-state="ready"]') !== null, 'caps ready');
        expect(grants.logQueries).toHaveLength(21);
        expect(grants.peakInFlight).toBe(GRANT_LOG_BATCH);
    });

    it('web_data_view_reports_caps_unavailable_when_any_grant_window_fails', async () => {
        const grants = new GrantChain();
        grants.xwebFee = chain.xwebFee;
        grants.finalised = { ...HEAD, number: toHex(FLOOR + 12n * GRANT_LOG_WINDOW) };
        grants.issued = [{ ...OWNED_GRANT, block: FLOOR + 12n * GRANT_LOG_WINDOW }];
        grants.failLogsFrom = FLOOR;
        mounted = await mountSurface(grants, <GrantSurface />);
        const { container } = mounted;

        await until(() => container.querySelector('[data-caps-state="unavailable"]') !== null, 'caps unavailable');
        expect(container.querySelector('[data-caps-state="unavailable"] [role="status"]')?.textContent).toBe('Verified caps are currently unavailable.');
        expect(container.querySelector('[data-grant]')).toBeNull();
    });
    it('web_data_view_reads_a_sponsored_grant_sent_to_the_account_and_no_foreign_grant', async () => {
        const grants = new GrantChain();
        grants.xwebFee = chain.xwebFee;
        const sponsoredForeign: IssuedGrant = { ...FOREIGN_GRANT, grantId: `0x${'a4'.repeat(32)}`, sponsored: true };
        grants.issued = [FOREIGN_GRANT, sponsoredForeign, { ...OWNED_GRANT, sponsored: true }];
        mounted = await mountSurface(grants, <GrantSurface />);
        const { container } = mounted;

        await until(() => container.querySelector('[data-caps-state="ready"]') !== null, 'caps ready');
        expect(container.querySelectorAll('[data-grant]')).toHaveLength(1);
        expect(container.querySelector(`[data-grant="${GRANT_ID}"]`)).not.toBeNull();
        expect(grants.calls.filter((call) => call.data.startsWith(GET_PAYER_GRANT))).toEqual([
            { to: LEDGER_PRECOMPILE, data: `${GET_PAYER_GRANT}${OWNED_GRANT.grantId.slice(2)}` },
        ]);
    });

    it('web_data_view_reports_caps_unavailable_past_the_window_ceiling_without_scanning', async () => {
        const grants = new GrantChain();
        grants.finalised = { ...HEAD, number: toHex(FLOOR + GRANT_LOG_WINDOW_CEILING * GRANT_LOG_WINDOW) };
        grants.issued = [OWNED_GRANT];
        expect(await readGrantCaps(grants, grants.address)).toEqual({ state: 'unavailable', reason: 'The grant history is longer than this view can scan.' });
        expect(grants.methods).not.toContain('eth_getLogs');

        const atCeiling = new GrantChain();
        atCeiling.xwebFee = chain.xwebFee;
        atCeiling.finalised = { ...HEAD, number: toHex(FLOOR + GRANT_LOG_WINDOW_CEILING * GRANT_LOG_WINDOW - 1n) };
        atCeiling.issued = [OWNED_GRANT];
        expect((await readGrantCaps(atCeiling, atCeiling.address)).state).toBe('ready');
        expect(atCeiling.logQueries).toHaveLength(Number(GRANT_LOG_WINDOW_CEILING));
    });

    it('web_data_view_reports_caps_unavailable_on_a_chain_without_a_configured_floor', async () => {
        const grants = new GrantChain();
        grants.capsChainId = 229;
        grants.issued = [{ ...OWNED_GRANT, block: 1n }];
        grants.finalised = { ...HEAD, number: toHex(40_000_000n) };
        expect(await readGrantCaps(grants, grants.address)).toEqual({ state: 'unavailable', reason: 'No ledger log floor is configured for chain 229.' });
        expect(grants.methods).not.toContain('eth_getLogs');
    });

    it('web_data_view_reads_grants_from_holder_records_when_the_endpoint_serves_them', async () => {
        const grants = new GrantChain();
        grants.xwebFee = chain.xwebFee;
        const second: IssuedGrant = { ...OWNED_GRANT, grantId: `0x${'a3'.repeat(32)}`, expiration: 1_850_000_000n };
        grants.issued = [FOREIGN_GRANT, OWNED_GRANT, second];
        const holder = grants.address.toLowerCase();
        const record = (grant: IssuedGrant) => ({ kind: 'grant', holder, contract: 'ledger', record_id: grant.grantId.slice(2), state: 'active' });
        const cursor = '00000000000000070000000000000000';
        grants.holderRecords = (params) => (params[2] === null
            ? { items: [record(OWNED_GRANT), record(FOREIGN_GRANT)], next_cursor: cursor, indexedHeight: 990, chainHeight: 1000 }
            : { items: [record(second)], next_cursor: null, indexedHeight: 995, chainHeight: 1001 });
        mounted = await mountSurface(grants, <GrantSurface />);
        const { container } = mounted;

        await until(() => container.querySelector('[data-caps-state="ready"]') !== null, 'caps ready');
        expect(container.querySelectorAll('[data-grant]')).toHaveLength(2);
        expect(container.querySelector(`[data-grant="${'a2'.repeat(32)}"]`)).toBeNull();
        expect(text(container, '[data-role="caps-expiry"]')).toBe('Earliest grant expiry 1850000000');
        expect(text(container, '[data-role="caps-history"]')).toBe('Grants from the indexer · indexed height 990 · chain height 1001');
        expect(container.querySelector('[data-history-source="indexer"]')).not.toBeNull();
        expect(grants.holderQueries.slice(0, 2)).toEqual([[holder, 'grant', null, 100], [holder, 'grant', cursor, 100]]);
        expect(grants.methods).not.toContain('eth_getLogs');
        expect(grants.methods).not.toContain('eth_getTransactionByHash');
    });

    it('web_data_view_probes_holder_records_once_and_keeps_the_chain_scan_when_not_served', async () => {
        const grants = new GrantChain();
        grants.xwebFee = chain.xwebFee;
        grants.issued = [OWNED_GRANT];
        const first = await readGrantCaps(grants, grants.address);
        const again = await readGrantCaps(grants, grants.address);
        expect(first.state).toBe('ready');
        expect(again).toEqual(first);
        expect(first.state === 'ready' ? first.history : null).toEqual({ source: 'chain_scan', floor: FLOOR.toString(), head: (FLOOR + 42n).toString() });
        expect(grants.holderQueries).toHaveLength(1);
        expect(grants.logQueries).toHaveLength(2);
    });

    it('web_data_view_reports_caps_unavailable_when_served_holder_records_fail_and_never_falls_back_to_the_scan', async () => {
        const grants = new GrantChain();
        grants.xwebFee = chain.xwebFee;
        grants.issued = [OWNED_GRANT];
        let fail = false;
        grants.holderRecords = () => {
            if (fail) throw Object.assign(new Error('History unavailable'), { code: -32001 });
            return { items: [], next_cursor: null, indexedHeight: 40, chainHeight: 42 };
        };
        expect((await readGrantCaps(grants, grants.address)).state).toBe('empty');
        fail = true;
        await expect(readGrantCaps(grants, grants.address)).rejects.toMatchObject({ code: -32001 });
        grants.holderRecords = null;
        await expect(readGrantCaps(grants, grants.address)).rejects.toMatchObject({ code: -32601 });
        expect(grants.methods).not.toContain('eth_getLogs');
    });
});
