// @vitest-environment jsdom
import { act, useLayoutEffect } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import { ethers } from 'ethers';
import type { TypedDataPayload } from '@paxeer/wallet';
import { custodyChoiceRepository } from '@/platform/storage/repositories';
import { IdentitySession } from './identity';
import { WalletProvider, useWallet, useWalletState, type WalletContextValue, type WalletState } from './WalletProvider';
import { EMAIL, EMAIL_CODE, fixtureBody, loadConstructions, startGateway, type TestGateway } from './test/gateway';
import { INJECTED_UUID, TestInjectedProvider, announceInjected } from './test/injected';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

let gateway: TestGateway;
let root: Root | null = null;
let container: HTMLDivElement;
let current: WalletContextValue | null = null;
let facade: WalletState | null = null;

function Probe() {
    const value = useWallet();
    const state = useWalletState();
    useLayoutEffect(() => {
        current = value;
        facade = state;
    });
    return null;
}

beforeEach(async () => {
    window.localStorage.clear();
    gateway = await startGateway();
    container = document.createElement('div');
    document.body.appendChild(container);
    current = null;
    facade = null;
});

afterEach(async () => {
    await act(async () => {
        root?.unmount();
    });
    root = null;
    container.remove();
    await gateway.close();
});

function identity(): IdentitySession {
    return IdentitySession.create(gateway.config, { persistSession: false, detectSessionInUrl: false });
}

async function mount(props: { target: EventTarget; config?: typeof gateway.config | null; identitySession?: IdentitySession | null }) {
    await act(async () => {
        root = createRoot(container);
        root.render(
            <WalletProvider
                config={props.config === undefined ? gateway.config : props.config}
                identity={props.identitySession === undefined ? identity() : props.identitySession}
                eventTarget={props.target}
            >
                <Probe />
            </WalletProvider>,
        );
    });
}

function wallet(): WalletContextValue {
    if (!current) throw new Error('the wallet context did not render');
    return current;
}

async function until(check: () => boolean): Promise<void> {
    for (let attempt = 0; attempt < 200; attempt += 1) {
        if (check()) return;
        await act(async () => {
            await new Promise((resolve) => setTimeout(resolve, 10));
        });
    }
    throw new Error(`condition not reached; status ${current?.status ?? 'none'}, error ${current?.error ?? 'none'}`);
}

function withoutDomainType(types: Record<string, Array<{ name: string; type: string }>>) {
    const copy = { ...types };
    delete copy.EIP712Domain;
    return copy;
}

describe('WalletProvider', () => {
    it('wallet_provider_signs_in_provisions_sends_and_signs_through_the_gateway', async () => {
        gateway.scenario = 'unprovisioned';
        await mount({ target: new EventTarget() });
        await until(() => wallet().status === 'signed-out');
        expect(wallet().embeddedAvailable).toBe(true);
        expect(facade?.hasWallet).toBe(false);

        const constructions = loadConstructions();
        let address: string | null = null;
        await act(async () => {
            address = await wallet().verifyEmailCode(EMAIL, EMAIL_CODE);
        });
        expect(address).toBe(constructions.address);
        expect(wallet().status).toBe('ready');
        expect(wallet().mode).toBe('embedded');
        expect(wallet().identity?.email).toBe(EMAIL);
        expect(facade?.activeAccount?.kind).toBe('managed');
        expect(facade?.activeAccount?.address).toBe(constructions.address);
        expect(custodyChoiceRepository.read()).toBe('embedded');
        expect(
            gateway.requests.filter((r) => r.path.startsWith('/v1/wallet/')).map((r) => `${r.method} ${r.path}`),
        ).toEqual(['GET /v1/wallet/me', 'POST /v1/wallet/provision']);

        let hash = '';
        await act(async () => {
            hash = await wallet().send({ to: constructions.transaction.to, value: '0.001' });
        });
        expect(hash).toBe(fixtureBody('default', 'POST', '/v1/wallet/send').tx_hash);

        let signature = '';
        await act(async () => {
            signature = await wallet().signMessage(constructions.message);
        });
        expect(ethers.verifyMessage(constructions.message, signature)).toBe(constructions.address);

        const typed = constructions.typedData;
        await act(async () => {
            signature = await wallet().signTypedData(typed as unknown as TypedDataPayload);
        });
        expect(ethers.verifyTypedData(typed.domain, withoutDomainType(typed.types), typed.message, signature)).toBe(
            constructions.address,
        );

        await act(async () => {
            await wallet().signOut();
        });
        expect(wallet().status).toBe('signed-out');
        expect(wallet().address).toBeNull();
        expect(custodyChoiceRepository.read()).toBeNull();
        await expect(wallet().signMessage('after sign-out')).rejects.toThrow(/requires a connected wallet/);
    });

    it('wallet_provider_connects_an_injected_wallet_and_routes_every_request_through_it', async () => {
        const target = new EventTarget();
        const { provider } = announceInjected(target, new TestInjectedProvider(1));
        await mount({ target });
        await until(() => wallet().status === 'signed-out' && wallet().injected.length === 1);
        expect(wallet().injected[0]?.info.uuid).toBe(INJECTED_UUID);

        await act(async () => {
            await wallet().connectInjected(INJECTED_UUID);
        });
        expect(wallet().status).toBe('ready');
        expect(wallet().mode).toBe('injected');
        expect(wallet().address).toBe(provider.wallet.address);
        expect(provider.chainId).toBe(125);
        expect(provider.calls.some((c) => c.method === 'wallet_switchEthereumChain')).toBe(true);
        expect(facade?.activeAccount?.kind).toBe('injected');
        expect(custodyChoiceRepository.read()).toBe('injected');

        const constructions = loadConstructions();
        let hash = '';
        await act(async () => {
            hash = await wallet().send({ to: constructions.transaction.to, value: '0.25' });
        });
        expect(hash).toBe(ethers.keccak256(provider.sent[0] ?? '0x'));
        expect(ethers.Transaction.from(provider.sent[0] ?? '0x').value).toBe(ethers.parseEther('0.25'));

        let signature = '';
        await act(async () => {
            signature = await wallet().signMessage(constructions.message);
        });
        expect(ethers.verifyMessage(constructions.message, signature)).toBe(provider.wallet.address);
        const typed = constructions.typedData;
        await act(async () => {
            signature = await wallet().signTypedData(typed as unknown as TypedDataPayload);
        });
        expect(ethers.verifyTypedData(typed.domain, withoutDomainType(typed.types), typed.message, signature)).toBe(
            provider.wallet.address,
        );
        expect(gateway.requests).toEqual([]);

        await act(async () => {
            provider.emit('accountsChanged', []);
        });
        expect(wallet().status).toBe('signed-out');
    });

    it('wallet_provider_reconnects_an_authorised_injected_wallet_silently', async () => {
        custodyChoiceRepository.write('injected');
        const target = new EventTarget();
        const injected = new TestInjectedProvider();
        injected.authorised = true;
        announceInjected(target, injected);
        await mount({ target });
        await until(() => wallet().status === 'ready');
        expect(wallet().mode).toBe('injected');
        expect(wallet().address).toBe(injected.wallet.address);
        expect(injected.calls.some((c) => c.method === 'eth_requestAccounts')).toBe(false);
    });

    it('wallet_provider_without_configuration_offers_no_embedded_sign_in', async () => {
        await mount({ target: new EventTarget(), config: null, identitySession: null });
        await until(() => wallet().status === 'signed-out');
        expect(wallet().embeddedAvailable).toBe(false);
        expect(wallet().configError).toBe('the embedded wallet is not configured');
        await expect(wallet().sendEmailCode(EMAIL)).rejects.toThrow(/not configured/);
        await expect(wallet().connectEmbedded()).rejects.toThrow(/not configured/);
        expect(gateway.requests).toEqual([]);
    });
});
