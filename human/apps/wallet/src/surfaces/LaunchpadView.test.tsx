// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import {
    LAUNCHPAD_EVENTS,
    LAUNCHPAD_PRECOMPILE,
    abiSelector,
    launchpadBuyCall,
    launchpadCreateMarketCall,
} from '@sidiora/layerx-sdk';
import { launchpad } from '@paxeer/wallet';
import { LaunchpadView } from './LaunchpadView';
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

const send = (container: HTMLElement) => container.querySelector<HTMLButtonElement>('[data-action="send"]');

describe('LaunchpadView', () => {
    it('launchpad_view_buys_through_the_module_and_shows_the_decoded_swap', async () => {
        const fixture = callFixtures().launchpad.buy as { token: string; amountIn: string; minOut: string; deadline: string };
        const vector = eventVectors('launchpad').find((candidate) => candidate.event === 'Swap');
        if (!vector) throw new Error('no Swap vector');
        chain.logsFor = () => [vectorLog(LAUNCHPAD_PRECOMPILE, LAUNCHPAD_EVENTS, vector)];
        const now = () => (Number(fixture.deadline) - 600) * 1000;
        mounted = await mountSurface(chain, <LaunchpadView sidRate={null} now={now} />);
        const { container } = mounted;

        await typeInto(container, 'token', fixture.token);
        await typeInto(container, 'amountIn', '1');
        await typeInto(container, 'minOut', fixture.minOut);
        await until(() => send(container)?.disabled === false, 'send enabled');
        await click(send(container), 'send');
        await until(() => text(container, '[data-role="status"]') === 'confirmed', 'receipt');

        const order = {
            token: fixture.token,
            amountIn: BigInt(fixture.amountIn),
            minOut: BigInt(fixture.minOut),
            recipient: chain.address,
            deadline: BigInt(fixture.deadline),
        };
        const expected = launchpadBuyCall(order);
        expect(chain.sent).toEqual([{ from: chain.address, to: LAUNCHPAD_PRECOMPILE, data: expected.data, value: '0x0' }]);
        expect(chain.sent[0]?.data.startsWith(abiSelector('buy(address,uint256,uint256,address,uint256)'))).toBe(true);
        expect(chain.sent[0]?.data).toBe(launchpad(chain).buy(order).data);

        const spec = LAUNCHPAD_EVENTS.find((candidate) => candidate.name === 'Swap');
        if (!spec) throw new Error('no Swap spec');
        const event = container.querySelector('[data-event="Swap"]');
        expect(event).not.toBeNull();
        for (const [name, value] of Object.entries(vector.fields)) {
            expect(event?.querySelector(`[data-field="${name}"]`)?.textContent).toBe(expectedField(spec, name, value));
        }
    });

    it('launchpad_view_creates_a_market_through_the_module', async () => {
        const fixture = callFixtures().launchpad.createMarket as { name: string; symbol: string; feeStrategy: number };
        const vector = eventVectors('launchpad').find((candidate) => candidate.event === 'MarketCreated');
        if (!vector) throw new Error('no MarketCreated vector');
        chain.logsFor = () => [vectorLog(LAUNCHPAD_PRECOMPILE, LAUNCHPAD_EVENTS, vector)];
        mounted = await mountSurface(chain, <LaunchpadView sidRate={null} />);
        const { container } = mounted;

        await click(radio(container, 'Launchpad action', 'Create'), 'create action');
        await typeInto(container, 'name', fixture.name);
        await typeInto(container, 'symbol', fixture.symbol);
        await typeInto(container, 'feeStrategy', String(fixture.feeStrategy));
        await until(() => send(container)?.disabled === false, 'send enabled');
        await click(send(container), 'send');
        await until(() => text(container, '[data-role="status"]') === 'confirmed', 'receipt');

        const expected = launchpadCreateMarketCall(fixture.name, fixture.symbol, fixture.feeStrategy);
        expect(chain.sent).toEqual([{ from: chain.address, to: LAUNCHPAD_PRECOMPILE, data: expected.data, value: '0x0' }]);
        expect(chain.sent[0]?.data.startsWith(abiSelector('createMarket(string,string,uint8)'))).toBe(true);
        expect(container.querySelector('[data-event="MarketCreated"] [data-field="symbol"]')?.textContent).toBe(String(vector.fields.symbol));
    });
});
