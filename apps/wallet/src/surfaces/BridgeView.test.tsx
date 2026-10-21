// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import { BRIDGE_EVENTS, LAYERX_BRIDGE_PRECOMPILE, abiSelector, bridgeOutCall } from '@sidiora/layerx-sdk';
import { bridge } from '@paxeer/wallet';
import { BridgeView } from './BridgeView';
import {
    SurfaceChain,
    callFixtures,
    click,
    eventVectors,
    expectedField,
    mountSurface,
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

describe('BridgeView', () => {
    it('bridge_view_bridges_out_through_the_module_and_shows_the_decoded_event', async () => {
        const fixture = callFixtures().bridge.bridgeOut as { chain: string; asset: string; amount: string; recipient: string };
        const vector = eventVectors('bridge').find((candidate) => candidate.event === 'BridgeOut');
        if (!vector) throw new Error('no BridgeOut vector');
        chain.logsFor = () => [vectorLog(LAYERX_BRIDGE_PRECOMPILE, BRIDGE_EVENTS, vector)];
        mounted = await mountSurface(chain, <BridgeView sidRate={null} />);
        const { container } = mounted;

        await typeInto(container, 'chain', fixture.chain);
        await typeInto(container, 'asset', fixture.asset);
        await typeInto(container, 'decimals', '6');
        await typeInto(container, 'amount', '2.5');
        await typeInto(container, 'recipient', fixture.recipient);
        const send = () => container.querySelector<HTMLButtonElement>('[data-action="send"]');
        await until(() => send()?.disabled === false, 'send enabled');
        await click(send(), 'send');
        await until(() => text(container, '[data-role="status"]') === 'confirmed', 'receipt');

        const expected = bridgeOutCall(BigInt(fixture.chain), fixture.asset, BigInt(fixture.amount), fixture.recipient);
        expect(chain.sent).toEqual([{ from: chain.address, to: LAYERX_BRIDGE_PRECOMPILE, data: expected.data, value: '0x0' }]);
        expect(chain.sent[0]?.data.startsWith(abiSelector('bridgeOut(uint64,address,uint256,address)'))).toBe(true);
        expect(chain.sent[0]?.data).toBe(bridge(chain).bridgeOut(BigInt(fixture.chain), fixture.asset, BigInt(fixture.amount), fixture.recipient).data);

        const spec = BRIDGE_EVENTS.find((candidate) => candidate.name === 'BridgeOut');
        if (!spec) throw new Error('no BridgeOut spec');
        const event = container.querySelector('[data-event="BridgeOut"]');
        expect(event).not.toBeNull();
        for (const [name, value] of Object.entries(vector.fields)) {
            expect(event?.querySelector(`[data-field="${name}"]`)?.textContent).toBe(expectedField(spec, name, value));
        }
    });
});
