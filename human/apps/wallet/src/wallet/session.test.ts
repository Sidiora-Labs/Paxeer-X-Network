import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import { ethers } from 'ethers';
import { announceProvider, type TypedDataPayload } from '@paxeer/wallet';
import { IdentitySession } from './identity';
import {
    EMBEDDED_WALLET_INFO,
    TransferError,
    authorisedAccount,
    discoverInjectedWallets,
    embeddedWallet,
    injectedWallet,
    isEmbeddedDetail,
    transferTransaction,
} from './session';
import {
    ACCESS_TOKEN,
    EMAIL,
    EMAIL_CODE,
    fixtureBody,
    loadConstructions,
    startGateway,
    type TestGateway,
} from './test/gateway';
import { INJECTED_UUID, announceInjected } from './test/injected';

let gateway: TestGateway;

beforeEach(async () => {
    gateway = await startGateway();
});

afterEach(async () => {
    await gateway.close();
});

async function signedIn(): Promise<IdentitySession> {
    const identity = IdentitySession.create(gateway.config, { persistSession: false, detectSessionInUrl: false });
    await identity.verifyEmailCode(EMAIL, EMAIL_CODE);
    return identity;
}

function withoutDomainType(types: Record<string, Array<{ name: string; type: string }>>) {
    const copy = { ...types };
    delete copy.EIP712Domain;
    return copy;
}

describe('embedded session', () => {
    it('wallet_session_provisions_through_the_gateway_when_no_wallet_exists', async () => {
        gateway.scenario = 'unprovisioned';
        const wallet = embeddedWallet(gateway.config, await signedIn());
        expect(wallet.mode).toBe('embedded');
        const constructions = loadConstructions();
        expect(await wallet.accounts()).toEqual([constructions.address]);
        const walletCalls = gateway.requests.filter((r) => r.path.startsWith('/v1/wallet/'));
        expect(walletCalls.map((r) => `${r.method} ${r.path}`)).toEqual(['GET /v1/wallet/me', 'POST /v1/wallet/provision']);
        expect(walletCalls.every((r) => r.authorization === `Bearer ${ACCESS_TOKEN}`)).toBe(true);
    });

    it('wallet_session_sends_signs_a_message_and_typed_data_through_the_gateway', async () => {
        const wallet = embeddedWallet(gateway.config, await signedIn());
        const constructions = loadConstructions();
        await wallet.accounts();

        const hash = await wallet.sendTransaction(transferTransaction({ to: constructions.transaction.to, value: '0.001' }));
        expect(hash).toBe(fixtureBody('default', 'POST', '/v1/wallet/send').tx_hash);
        const sent = gateway.requests.find((r) => r.path === '/v1/wallet/send');
        expect(sent?.body).toEqual({ tx: { to: ethers.getAddress(constructions.transaction.to), value: '1000000000000000' } });

        const messageSignature = await wallet.signMessage(constructions.message);
        expect(ethers.verifyMessage(constructions.message, messageSignature)).toBe(constructions.address);
        expect(gateway.requests.find((r) => r.path === '/v1/wallet/sign-message')?.body).toEqual({ message: constructions.message });

        const typed = constructions.typedData;
        const typedSignature = await wallet.signTypedData(typed as unknown as TypedDataPayload);
        expect(ethers.verifyTypedData(typed.domain, withoutDomainType(typed.types), typed.message, typedSignature)).toBe(
            constructions.address,
        );
    });

    it('wallet_session_without_a_session_is_refused_before_any_wallet_call', async () => {
        const identity = IdentitySession.create(gateway.config, { persistSession: false, detectSessionInUrl: false });
        const wallet = embeddedWallet(gateway.config, identity);
        await expect(wallet.accounts()).rejects.toThrow();
        expect(gateway.requests.filter((r) => r.path.startsWith('/v1/wallet/') && r.authorization !== null)).toEqual([]);
    });
});

describe('injected session', () => {
    it('wallet_session_discovers_injected_wallets_and_skips_the_embedded_provider', async () => {
        const target = new EventTarget();
        const identity = IdentitySession.create(gateway.config, { persistSession: false, detectSessionInUrl: false });
        const embedded = embeddedWallet(gateway.config, identity);
        announceProvider({ info: EMBEDDED_WALLET_INFO, provider: embedded.provider }, target);
        const { provider } = announceInjected(target);
        const found: string[] = [];
        const discovery = discoverInjectedWallets((detail) => found.push(detail.info.uuid), target);
        expect(found).toEqual([INJECTED_UUID]);
        const detail = discovery.providers.find((d) => d.info.uuid === INJECTED_UUID);
        expect(detail && isEmbeddedDetail(detail)).toBe(false);
        discovery.stop();
        if (!detail) throw new Error('the injected wallet was not discovered');

        expect(await authorisedAccount(detail)).toBeNull();
        const wallet = injectedWallet(detail);
        expect(wallet.mode).toBe('injected');
        expect(await wallet.accounts()).toEqual([provider.wallet.address]);
        expect(await authorisedAccount(detail)).toBe(provider.wallet.address);

        const constructions = loadConstructions();
        const hash = await wallet.sendTransaction(transferTransaction({ to: constructions.transaction.to, value: '0.5' }));
        expect(hash).toBe(ethers.keccak256(provider.sent[0] ?? '0x'));
        const parsed = ethers.Transaction.from(provider.sent[0] ?? '0x');
        expect(parsed.from).toBe(provider.wallet.address);
        expect(parsed.value).toBe(ethers.parseEther('0.5'));

        const signature = await wallet.signMessage(constructions.message);
        expect(ethers.verifyMessage(constructions.message, signature)).toBe(provider.wallet.address);
        const typed = constructions.typedData;
        const typedSignature = await wallet.signTypedData(typed as unknown as TypedDataPayload);
        expect(ethers.verifyTypedData(typed.domain, withoutDomainType(typed.types), typed.message, typedSignature)).toBe(
            provider.wallet.address,
        );
        expect(gateway.requests.filter((r) => r.path.startsWith('/v1/wallet/'))).toEqual([]);
    });
});

describe('transfer construction', () => {
    it('wallet_session_builds_native_and_token_transfers_and_refuses_bad_input', () => {
        const to = '0x3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c';
        const token = '0x9b9b9b9b9b9b9b9b9b9b9b9b9b9b9b9b9b9b9b9b';
        expect(transferTransaction({ to, value: '1.5' })).toEqual({ to: ethers.getAddress(to), value: 1_500_000_000_000_000_000n });
        const erc20 = transferTransaction({ to, value: '2', tokenAddress: token, decimals: 6 });
        expect(erc20.to).toBe(ethers.getAddress(token));
        expect(erc20.value).toBe(0n);
        const decoded = new ethers.Interface(['function transfer(address,uint256)']).decodeFunctionData('transfer', erc20.data ?? '0x');
        expect(decoded[0]).toBe(ethers.getAddress(to));
        expect(decoded[1]).toBe(2_000_000n);
        expect(() => transferTransaction({ to: 'nope', value: '1' })).toThrow(TransferError);
        expect(() => transferTransaction({ to, value: '0' })).toThrow(TransferError);
        expect(() => transferTransaction({ to, value: 'abc' })).toThrow(TransferError);
        expect(() => transferTransaction({ to, value: '1', decimals: 40 })).toThrow(TransferError);
        expect(() => transferTransaction({ to, value: '1', tokenAddress: 'nope' })).toThrow(TransferError);
    });
});
