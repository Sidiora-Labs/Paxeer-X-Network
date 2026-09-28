// @vitest-environment jsdom
import { readFileSync } from 'node:fs';
import path from 'node:path';
import { act } from 'react';
import { ethers } from 'ethers';
import { afterEach, beforeAll, afterAll, beforeEach, describe, expect, it } from 'vitest';
import { PAXEER_CHAIN_ID, type Hex } from '@paxeer/wallet';
import { IdentitySession } from '@/wallet/identity';
import { WalletProvider, useWallet, type WalletContextValue } from '@/wallet/WalletProvider';
import { EMAIL, EMAIL_CODE, fixtureBody, loadConstructions, startGateway, type TestGateway } from '@/wallet/test/gateway';
import { AccountProvider } from './AccountProvider';
import { CUSTODY_ABI, CUSTODY_HANDOFF_DOMAIN, CUSTODY_PRECOMPILE, CustodyDepositError, beneficiaryOf, custodyHandoff, depositTokenCalldata } from './custody';
import { DepositFlow } from './DepositFlow';
import { startAccountEndpoint, type AccountEndpoint, type Scenario } from './test/endpoint';
import { mount, type Mounted } from './test/render';

const CUSTODY_ABI_FILE = path.resolve(__dirname, '../../../../../precompiles/layerxcustody/abi.json');
const POINTER = `0x${'22'.repeat(20)}`;
const MAIN_ACCOUNT = 'e1'.repeat(32);

let endpoint: AccountEndpoint;
let gateway: TestGateway;
let view: Mounted | null = null;
let wallet: WalletContextValue | null = null;

function Probe() {
    wallet = useWallet();
    return null;
}

beforeAll(async () => {
    endpoint = await startAccountEndpoint();
});

afterAll(async () => {
    await endpoint.close();
});

beforeEach(async () => {
    window.localStorage.clear();
    endpoint.seen.length = 0;
    endpoint.unmatched.length = 0;
    gateway = await startGateway();
    wallet = null;
});

afterEach(async () => {
    await view?.unmount();
    view = null;
    await gateway.close();
});

async function render(scenario: Scenario, signIn: boolean): Promise<Mounted> {
    const identity = IdentitySession.create(gateway.config, { persistSession: false, detectSessionInUrl: false });
    const mounted = await mount(
        <WalletProvider config={gateway.config} identity={identity} eventTarget={new EventTarget()}>
            <Probe />
            <AccountProvider config={endpoint.config(scenario)}>
                <DepositFlow />
            </AccountProvider>
        </WalletProvider>,
    );
    view = mounted;
    await mounted.until(() => wallet?.status === 'signed-out', 'wallet signed out');
    if (signIn) {
        await act(async () => {
            await wallet?.verifyEmailCode(EMAIL, EMAIL_CODE);
        });
        await mounted.until(() => wallet?.status === 'ready', 'wallet ready');
    }
    return mounted;
}

function depositButton(mounted: Mounted): HTMLButtonElement {
    const found = mounted.query<HTMLButtonElement>('button[name="deposit"]');
    if (!found) throw new Error('no deposit button');
    return found;
}

function reasons(mounted: Mounted): string[] {
    return mounted.all('[data-reason]').map((reason) => reason.textContent ?? '');
}

describe('DepositFlow', () => {
    it('asks for a wallet before a deposit', async () => {
        const mounted = await render('deposit', false);
        expect(mounted.container.textContent).toContain('Connect a wallet to deposit.');
        expect(endpoint.seen).toEqual([]);
    });

    it('signs the custody hand-off with the embedded wallet and sends the deposit to the custody precompile', async () => {
        const mounted = await render('deposit', true);
        const constructions = loadConstructions();
        expect(wallet?.address).toBe(constructions.address);
        await mounted.until(() => mounted.query('select[name="asset"]') !== null && mounted.query('[data-kernel]') !== null, 'deposit form');
        expect(mounted.all('option').map((option) => option.textContent)).toEqual(['LXP']);
        expect(depositButton(mounted).disabled).toBe(true);

        await mounted.type('input[name="amount"]', '0.0001');
        expect(reasons(mounted)).toEqual(['The minimum deposit is 0.001 LXP']);
        await mounted.type('input[name="amount"]', '1000000');
        expect(reasons(mounted)).toEqual(['The deposit would exceed the custody cap']);
        await mounted.type('input[name="amount"]', 'abc');
        expect(reasons(mounted)).toEqual(['Enter a positive amount']);
        await mounted.type('input[name="amount"]', '1.5');
        expect(reasons(mounted)).toEqual([]);
        expect(depositButton(mounted).disabled).toBe(false);

        await mounted.click('button[name="deposit"]');
        await mounted.until(() => mounted.query('[data-rung]') !== null || mounted.query('[data-deposit="refused"]') !== null, 'deposit sent');
        expect(mounted.query('[data-deposit="refused"]')).toBeNull();

        const data = depositTokenCalldata(POINTER, 1_500_000n, beneficiaryOf(MAIN_ACCOUNT));
        const custodyRequest = gateway.requests.find((request) => request.path === '/v1/wallet/sign-custody');
        expect(custodyRequest?.body).toEqual({
            custody: custodyHandoff({ chainId: PAXEER_CHAIN_ID, to: CUSTODY_PRECOMPILE, value: 0n, data }).toLowerCase(),
        });
        const sendRequest = gateway.requests.find((request) => request.path === '/v1/wallet/send');
        expect(sendRequest?.body).toEqual({ tx: { to: CUSTODY_PRECOMPILE, data, value: '0', chainId: PAXEER_CHAIN_ID } });

        const signature = fixtureBody('default', 'POST', '/v1/wallet/sign-custody').signature as string;
        const hash = fixtureBody('default', 'POST', '/v1/wallet/send').tx_hash as string;
        expect(mounted.text('[data-deposit="signature"]')).toContain(signature);
        expect(mounted.text('[data-deposit="hash"]')).toBe(hash);
        expect(mounted.all('[data-rung]').map((rung) => rung.getAttribute('data-reached'))).toEqual(['true', 'false', 'false']);
        expect(endpoint.unmatched).toEqual([]);
    });

    it.each([
        ['kernel-unreachable', 'The LayerX kernel is unreachable'],
        ['no-finalised-checkpoint', 'The LayerX kernel has no finalised checkpoint on the chain yet'],
        ['kernel-not-configured', 'The LayerX kernel is not configured on the endpoint'],
    ] as const)('disables the deposit with the reason when the kernel is %s', async (scenario, reason) => {
        const mounted = await render(scenario, true);
        await mounted.until(() => mounted.query('[data-kernel="unavailable"]') !== null, 'kernel notice');
        expect(mounted.text('[data-kernel="unavailable"]')).toBe(reason);
        expect(reasons(mounted)).toContain(reason);
        expect(depositButton(mounted).disabled).toBe(true);
        await mounted.click('button[name="deposit"]');
        expect(gateway.requests.filter((request) => request.path === '/v1/wallet/sign-custody' || request.path === '/v1/wallet/send')).toEqual([]);
    });
});

describe('custody encoding', () => {
    it('matches the custody precompile ABI', () => {
        const recorded = new ethers.Interface(JSON.parse(readFileSync(CUSTODY_ABI_FILE, 'utf8')) as ethers.InterfaceAbi);
        const ours = new ethers.Interface(CUSTODY_ABI);
        for (const name of ['deposit', 'depositToken']) {
            expect(ours.getFunction(name)?.selector).toBe(recorded.getFunction(name)?.selector);
        }
        const data = depositTokenCalldata(POINTER, 5n, beneficiaryOf(MAIN_ACCOUNT));
        const decoded = recorded.decodeFunctionData('depositToken', data);
        expect([decoded[0], decoded[1], decoded[2]]).toEqual([ethers.getAddress(POINTER), 5n, `0x${MAIN_ACCOUNT}`]);
    });

    it('prefixes the hand-off with its domain and binds every call field', () => {
        const data = depositTokenCalldata(POINTER, 5n, beneficiaryOf(MAIN_ACCOUNT));
        const handoff = custodyHandoff({ chainId: PAXEER_CHAIN_ID, to: CUSTODY_PRECOMPILE, value: 0n, data });
        const bytes = ethers.getBytes(handoff);
        expect(ethers.toUtf8String(bytes.slice(0, CUSTODY_HANDOFF_DOMAIN.length))).toBe(CUSTODY_HANDOFF_DOMAIN);
        expect(bytes.length).toBe(CUSTODY_HANDOFF_DOMAIN.length + 32);
        expect(custodyHandoff({ chainId: PAXEER_CHAIN_ID + 1, to: CUSTODY_PRECOMPILE, value: 0n, data })).not.toBe(handoff);
        expect(custodyHandoff({ chainId: PAXEER_CHAIN_ID, to: CUSTODY_PRECOMPILE, value: 1n, data })).not.toBe(handoff);
        expect(custodyHandoff({ chainId: PAXEER_CHAIN_ID, to: CUSTODY_PRECOMPILE, value: 0n, data: `${data}00` as Hex })).not.toBe(handoff);
    });

    it('refuses a malformed beneficiary, pointer or amount', () => {
        expect(() => beneficiaryOf('7c'.repeat(31))).toThrow(CustodyDepositError);
        expect(() => beneficiaryOf('00'.repeat(32))).toThrow(CustodyDepositError);
        expect(() => depositTokenCalldata('0x1234', 1n, beneficiaryOf(MAIN_ACCOUNT))).toThrow(/pointer/);
        expect(() => depositTokenCalldata(POINTER, 0n, beneficiaryOf(MAIN_ACCOUNT))).toThrow(/positive/);
    });
});
