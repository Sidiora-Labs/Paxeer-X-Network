// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import { PERPS_EVENTS, PERPS_PRECOMPILE, abiSelector, encodeAbiCall, perpsDepositCall, perpsOpenPositionCall } from '@sidiora/layerx-sdk';
import { exchange } from '@paxeer/wallet';
import { ExchangeView, USDL } from './ExchangeView';
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
});

afterEach(async () => {
    await mounted?.unmount();
    mounted = null;
});

function sendButton(container: HTMLElement): HTMLButtonElement {
    const button = container.querySelector<HTMLButtonElement>('[data-action="send"]');
    if (!button) throw new Error('no send button');
    return button;
}

const usdl = USDL.address;
const approve = (amount: bigint) => encodeAbiCall('approve', ['address', 'uint256'], [PERPS_PRECOMPILE, amount]);

describe('ExchangeView', () => {
    it('exchange_view_opens_a_perps_position_after_the_exact_usdl_approve_and_shows_the_decoded_event', async () => {
        const vector = eventVectors('exchange').find((candidate) => candidate.event === 'OrderPlaced');
        if (!vector) throw new Error('no OrderPlaced vector');
        chain.logsFor = () => [vectorLog(PERPS_PRECOMPILE, PERPS_EVENTS, vector)];
        mounted = await mountSurface(chain, <ExchangeView sidRate={null} />);
        const { container } = mounted;

        await typeInto(container, 'marketId', '3');
        await click(radio(container, 'Side', 'Short'), 'short side');
        await typeInto(container, 'collateral', '50');
        await typeInto(container, 'leverage', '5');
        await until(() => !sendButton(container).disabled, 'send enabled');

        const order = { marketId: 3n, collateralToken: usdl, collateralAmount: 50_000_000n, leverage: 5n * 10n ** 18n, isLong: false };
        const expected = perpsOpenPositionCall(order);
        const signature = 'openPosition(uint256,address,uint256,uint256,bool)';
        expect(text(container, '[data-role="preview"]')).toContain(abiSelector(signature));

        await click(sendButton(container), 'send');
        await until(() => text(container, '[data-role="status"]') === 'confirmed', 'receipt');

        expect(chain.sent).toEqual([
            { from: chain.address, to: usdl.toLowerCase(), data: approve(50_000_000n), value: '0x0' },
            { from: chain.address, to: PERPS_PRECOMPILE, data: expected.data, value: '0x0' },
        ]);
        expect(chain.sent[1]?.data).toBe(exchange(chain).openPosition(order).data);

        const spec = PERPS_EVENTS.find((candidate) => candidate.name === 'OrderPlaced');
        if (!spec) throw new Error('no OrderPlaced spec');
        const event = container.querySelector('[data-event="OrderPlaced"]');
        expect(event).not.toBeNull();
        for (const [name, value] of Object.entries(vector.fields)) {
            expect(event?.querySelector(`[data-field="${name}"]`)?.textContent).toBe(expectedField(spec, name, value));
        }
    });

    it('exchange_view_deposits_usdl_without_native_value', async () => {
        mounted = await mountSurface(chain, <ExchangeView sidRate={null} />);
        const { container } = mounted;

        await click(radio(container, 'Exchange action', 'Deposit'), 'deposit action');
        await typeInto(container, 'amount', '1');
        await until(() => !sendButton(container).disabled, 'send enabled');
        await click(sendButton(container), 'send');
        await until(() => text(container, '[data-role="status"]') === 'confirmed', 'receipt');

        expect(chain.sent).toEqual([
            { from: chain.address, to: usdl.toLowerCase(), data: approve(1_000_000n), value: '0x0' },
            { from: chain.address, to: PERPS_PRECOMPILE, data: perpsDepositCall(usdl, 1_000_000n).data, value: '0x0' },
        ]);
        expect(chain.sent[1]?.data.startsWith(abiSelector('deposit(address,uint256)'))).toBe(true);
    });
});
