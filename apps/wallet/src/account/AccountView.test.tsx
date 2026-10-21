// @vitest-environment jsdom
import { afterAll, afterEach, beforeAll, beforeEach, describe, expect, it } from 'vitest';
import { AccountProvider } from './AccountProvider';
import { AccountView } from './AccountView';
import { ACCOUNT_ENV, AccountConfigError, readAccountConfig, resolveAccountConfig } from './config';
import { OWNER, OWNER_ACCOUNT, OWNER_DID, startAccountEndpoint, type AccountEndpoint, type Scenario } from './test/endpoint';
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

async function render(scenario: Scenario, account = OWNER): Promise<Mounted> {
    view = await mount(
        <AccountProvider config={endpoint.config(scenario)}>
            <AccountView account={account} />
        </AccountProvider>,
    );
    return view;
}

describe('AccountView', () => {
    it('shows the unified account document the endpoint resolves', async () => {
        const mounted = await render('available');
        await mounted.until(() => mounted.query('[data-field="binding"]') !== null && mounted.query('[data-kernel]') !== null, 'account document');
        expect(mounted.text('[data-field="address"]')).toBe(OWNER);
        expect(mounted.text('[data-field="pax-address"]')).toBe('pax1zyg3zyg3zyg3zyg3zyg3zyg3zyg3zyg3a8h3w5');
        expect(mounted.text('[data-field="did"]')).toBe(OWNER_DID);
        expect(mounted.text('[data-field="main-account"]')).toBe(OWNER_ACCOUNT);
        expect(mounted.text('[data-field="binding"]')).toBe('Bound to its LayerX identity');
        expect(mounted.query('[data-kernel]')?.getAttribute('data-kernel')).toBe('available');
        expect(mounted.text('[data-kernel]')).toBe('The LayerX kernel is available');
        expect(endpoint.methods('available').sort()).toEqual(['px_getNetwork', 'px_resolveAccount']);
        expect(endpoint.unmatched).toEqual([]);
    });

    it.each([
        ['kernel-unreachable', 'The LayerX kernel is unreachable'],
        ['no-finalised-checkpoint', 'The LayerX kernel has no finalised checkpoint on the chain yet'],
        ['kernel-not-configured', 'The LayerX kernel is not configured on the endpoint'],
    ] as const)('names the unavailable kernel state for %s', async (scenario, reason) => {
        const mounted = await render(scenario);
        await mounted.until(() => mounted.query('[data-kernel="unavailable"]') !== null, 'kernel notice');
        expect(mounted.text('[data-kernel="unavailable"]')).toBe(reason);
        expect(mounted.query('[data-kernel="unavailable"]')?.getAttribute('role')).toBe('status');
    });

    it('reports an account the endpoint cannot resolve', async () => {
        const mounted = await render('available', `0x${'12'.repeat(20)}`);
        await mounted.until(() => mounted.query('[role="alert"]') !== null, 'resolution error');
        expect(mounted.text('[role="alert"]')).toContain('The account could not be resolved');
        expect(endpoint.unmatched).toEqual([`available px_resolveAccount ["0x${'12'.repeat(20)}"]`]);
    });
});

describe('account configuration', () => {
    const complete = {
        [ACCOUNT_ENV.endpointUrl]: 'https://endpoint.example.test/',
        [ACCOUNT_ENV.humanUrl]: 'https://human.example.test',
        [ACCOUNT_ENV.explorerUrl]: 'https://explorer.example.test//',
    };

    it('reads the three service origins without trailing slashes', () => {
        expect(readAccountConfig(complete)).toEqual({
            endpointUrl: 'https://endpoint.example.test',
            humanUrl: 'https://human.example.test',
            explorerUrl: 'https://explorer.example.test',
        });
    });

    it('refuses a missing, malformed or credentialed origin by name', () => {
        const missing = resolveAccountConfig({ ...complete, [ACCOUNT_ENV.humanUrl]: '' });
        expect(missing.ok).toBe(false);
        if (!missing.ok) expect(missing.error.variable).toBe(ACCOUNT_ENV.humanUrl);
        expect(() => readAccountConfig({ ...complete, [ACCOUNT_ENV.explorerUrl]: 'not a url' })).toThrow(AccountConfigError);
        expect(() => readAccountConfig({ ...complete, [ACCOUNT_ENV.endpointUrl]: 'ftp://endpoint.example.test' })).toThrow(
            /must be an http or https URL/,
        );
        expect(() => readAccountConfig({ ...complete, [ACCOUNT_ENV.endpointUrl]: 'https://user:pass@endpoint.example.test' })).toThrow(
            /must not carry credentials/,
        );
    });
});
