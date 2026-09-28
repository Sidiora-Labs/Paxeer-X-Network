// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import { EXCHANGE_EVENTS, LAYERX_EXCHANGE_PRECOMPILE, abiSelector, exchangeDepositMarginCall, exchangePlaceOrderCall } from '@sidiora/layerx-sdk';
import { exchange } from '@paxeer/wallet';
import { ExchangeView } from './ExchangeView';
import {
    SurfaceChain,
    callFixtures,
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

describe('ExchangeView', () => {
    it('exchange_view_places_an_order_through_the_module_and_shows_the_decoded_event', async () => {
        const fixture = callFixtures().exchange.placeOrder as { marketId: string; side: number; price: string; quantity: string; timeInForce: number };
        const vector = eventVectors('exchange').find((candidate) => candidate.event === 'OrderPlaced');
        if (!vector) throw new Error('no OrderPlaced vector');
        chain.logsFor = () => [vectorLog(LAYERX_EXCHANGE_PRECOMPILE, EXCHANGE_EVENTS, vector)];
        mounted = await mountSurface(chain, <ExchangeView sidRate={null} />);
        const { container } = mounted;

        await typeInto(container, 'marketId', fixture.marketId);
        await click(radio(container, 'Side', 'Sell'), 'sell side');
        await typeInto(container, 'price', '2.5');
        await typeInto(container, 'quantity', fixture.quantity);
        await typeInto(container, 'timeInForce', String(fixture.timeInForce));
        await until(() => !sendButton(container).disabled, 'send enabled');

        const order = {
            marketId: fixture.marketId,
            side: fixture.side,
            price: BigInt(fixture.price),
            quantity: BigInt(fixture.quantity),
            timeInForce: fixture.timeInForce,
        };
        const expected = exchangePlaceOrderCall(order);
        expect(text(container, '[data-role="preview"]')).toContain(abiSelector('placeOrder(bytes32,uint8,uint256,uint256,uint8)'));

        await click(sendButton(container), 'send');
        await until(() => text(container, '[data-role="status"]') === 'confirmed', 'receipt');

        expect(chain.sent).toEqual([{ from: chain.address, to: LAYERX_EXCHANGE_PRECOMPILE, data: expected.data, value: '0x0' }]);
        expect(chain.sent[0]?.data.startsWith(abiSelector('placeOrder(bytes32,uint8,uint256,uint256,uint8)'))).toBe(true);
        expect(chain.sent[0]?.data).toBe(exchange(chain).placeOrder(order).data);

        const spec = EXCHANGE_EVENTS.find((candidate) => candidate.name === 'OrderPlaced');
        if (!spec) throw new Error('no OrderPlaced spec');
        const event = container.querySelector('[data-event="OrderPlaced"]');
        expect(event).not.toBeNull();
        for (const [name, value] of Object.entries(vector.fields)) {
            expect(event?.querySelector(`[data-field="${name}"]`)?.textContent).toBe(expectedField(spec, name, value));
        }
    });

    it('exchange_view_deposits_margin_carrying_the_pax_value', async () => {
        const fixture = callFixtures().exchange.depositMargin as { account: string; amountWei: string };
        mounted = await mountSurface(chain, <ExchangeView sidRate={null} />);
        const { container } = mounted;

        await click(radio(container, 'Exchange action', 'Deposit'), 'deposit action');
        await typeInto(container, 'account', fixture.account);
        await typeInto(container, 'amount', '1');
        await until(() => !sendButton(container).disabled, 'send enabled');
        await click(sendButton(container), 'send');
        await until(() => text(container, '[data-role="status"]') === 'confirmed', 'receipt');

        const expected = exchangeDepositMarginCall(fixture.account, BigInt(fixture.amountWei));
        expect(chain.sent).toEqual([
            { from: chain.address, to: LAYERX_EXCHANGE_PRECOMPILE, data: expected.data, value: `0x${BigInt(fixture.amountWei).toString(16)}` },
        ]);
        expect(chain.sent[0]?.data.startsWith(abiSelector('depositMargin(bytes32)'))).toBe(true);
        expect(container.querySelectorAll('[data-event]')).toHaveLength(0);
    });
});
