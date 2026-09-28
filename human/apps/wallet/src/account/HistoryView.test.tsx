// @vitest-environment jsdom
import { afterAll, afterEach, beforeAll, beforeEach, describe, expect, it } from 'vitest';
import { AccountProvider } from './AccountProvider';
import { HistoryView } from './HistoryView';
import { OWNER, startAccountEndpoint, type AccountEndpoint } from './test/endpoint';
import { mount, type Mounted } from './test/render';

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

function ids(mounted: Mounted): string[] {
    return mounted.all('[data-item]').map((item) => item.getAttribute('data-item') ?? '');
}

describe('HistoryView', () => {
    it('pages the unified history by cursor until the end', async () => {
        const mounted = await mount(
            <AccountProvider config={endpoint.config('available')}>
                <HistoryView account={OWNER} pageSize={2} />
            </AccountProvider>,
        );
        view = mounted;
        await mounted.until(() => ids(mounted).length === 2, 'first page');
        expect(ids(mounted)).toEqual(['5', '4']);
        expect(mounted.all('[data-item]').map((item) => item.getAttribute('data-side'))).toEqual(['paxeer', 'layerx']);
        expect(mounted.text('[data-item="4"]')).toContain('Sent LXP');
        expect(mounted.text('[data-item="4"]')).toContain('-0.004 LXP');
        expect(mounted.text('[data-item="4"]')).toContain('LayerX · lxp transfer');
        expect(mounted.text('[data-item="5"]')).toContain('Paxeer · erc20 transfer');
        expect(mounted.query('[data-item="5"] button')?.textContent).toBe('Status');
        expect(mounted.query('[data-item="4"] button')).toBeNull();

        const more = () => mounted.all('button').find((button) => button.textContent === 'Load more') ?? null;
        await mounted.until(() => more() !== null, 'load more');
        await mounted.click('section > button:last-of-type');
        await mounted.until(() => ids(mounted).length === 4, 'second page');
        await mounted.click('section > button:last-of-type');
        await mounted.until(() => mounted.query('[data-history="end"]') !== null, 'end of history');
        expect(ids(mounted)).toEqual(['5', '4', '3', '2', '1']);
        expect(more()).toBeNull();
        expect(mounted.text('[data-history="end"]')).toBe('End of history');
        expect(endpoint.seen.filter((entry) => entry.scope === 'available').map((entry) => (entry.body as { params: unknown[] }).params)).toEqual([
            [OWNER, null, 2, null],
            [OWNER, '4', 2, null],
            [OWNER, '2', 2, null],
        ]);
        expect(endpoint.unmatched).toEqual([]);
    });

    it('opens the status ladder of a Paxeer item from the explorer', async () => {
        const mounted = await mount(
            <AccountProvider config={endpoint.config('available')}>
                <HistoryView account={OWNER} pageSize={2} />
            </AccountProvider>,
        );
        view = mounted;
        await mounted.until(() => ids(mounted).length === 2, 'first page');
        await mounted.click('[data-item="5"] button');
        expect(mounted.query('[data-item="5"] button')?.getAttribute('aria-expanded')).toBe('true');
        await mounted.until(() => mounted.query('[data-item="5"] [role="alert"]') !== null, 'explorer answer');
        expect(mounted.text('[data-item="5"] [role="alert"]')).toContain('explorer status answered HTTP 404');
        expect(endpoint.unmatched).toEqual([`explorer /api/v2/transactions/0x${'05'.repeat(32)}/status`]);
    });

    it('reports a history the endpoint cannot serve', async () => {
        const mounted = await mount(
            <AccountProvider config={endpoint.config('kernel-unreachable')}>
                <HistoryView account={OWNER} pageSize={2} />
            </AccountProvider>,
        );
        view = mounted;
        await mounted.until(() => mounted.query('[role="alert"]') !== null, 'history error');
        expect(mounted.text('[role="alert"]')).toContain('History could not be read');
        expect(mounted.query('[data-item]')).toBeNull();
    });
});
