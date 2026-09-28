// @vitest-environment jsdom
import { afterAll, afterEach, beforeAll, describe, expect, it } from 'vitest';
import { statusLadder } from '@paxeer/wallet';
import { AccountProvider } from './AccountProvider';
import { StatusLadder, TransactionLadder } from './StatusLadder';
import { startAccountEndpoint, type AccountEndpoint } from './test/endpoint';
import { mount, type Mounted } from './test/render';

let endpoint: AccountEndpoint;
let view: Mounted | null = null;

beforeAll(async () => {
    endpoint = await startAccountEndpoint();
});

afterAll(async () => {
    await endpoint.close();
});

afterEach(async () => {
    await view?.unmount();
    view = null;
});

function rungs(mounted: Mounted): Array<[string, string, string | null]> {
    return mounted
        .all('[data-rung]')
        .map((rung) => [rung.getAttribute('data-rung') ?? '', rung.getAttribute('data-reached') ?? '', rung.getAttribute('data-source')]);
}

describe('StatusLadder', () => {
    it.each([
        ['e1', [['instant', 'false', null], ['sealed', 'false', null], ['final', 'false', null]]],
        ['e2', [['instant', 'true', 'explorer'], ['sealed', 'false', null], ['final', 'false', null]]],
        ['e3', [['instant', 'true', 'explorer'], ['sealed', 'true', 'explorer'], ['final', 'false', null]]],
        ['e4', [['instant', 'true', 'explorer'], ['sealed', 'true', 'explorer'], ['final', 'true', 'explorer']]],
    ] as const)('climbs the ladder from the recorded explorer status of 0x%s', async (byte, expected) => {
        const mounted = await mount(
            <AccountProvider config={endpoint.config('available')}>
                <TransactionLadder hash={`0x${byte.repeat(32)}`} />
            </AccountProvider>,
        );
        view = mounted;
        await mounted.until(() => mounted.query('[data-rung]') !== null, 'ladder');
        expect(rungs(mounted)).toEqual(expected);
    });

    it('takes the source of the highest step for every rung it reaches', async () => {
        const mounted = await mount(
            <StatusLadder steps={[statusLadder.fromExplorer({ rung: 'instant', block_number: 1, sealed_batch_number: null, finalized_batch_number: null, checkpoint_id: null }), statusLadder.fromJourney('done')]} />,
        );
        view = mounted;
        expect(rungs(mounted)).toEqual([
            ['instant', 'true', 'journey'],
            ['sealed', 'true', 'journey'],
            ['final', 'false', null],
        ]);
        expect(mounted.query('ol')?.getAttribute('aria-label')).toBe('Status');
    });

    it('reaches nothing for a journey state without a rung', async () => {
        const mounted = await mount(<StatusLadder steps={[statusLadder.fromJourney('refused')]} />);
        view = mounted;
        expect(rungs(mounted).every(([, reached]) => reached === 'false')).toBe(true);
    });
});
