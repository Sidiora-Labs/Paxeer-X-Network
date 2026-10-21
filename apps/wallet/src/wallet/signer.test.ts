import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import { ethers } from 'ethers';
import { IdentitySession } from './identity';
import { embeddedWallet } from './session';
import { WalletSigner, WalletSignerError } from './signer';
import { EMAIL, EMAIL_CODE, fixtureBody, loadConstructions, startGateway, type TestGateway } from './test/gateway';

let gateway: TestGateway;

beforeEach(async () => {
    gateway = await startGateway();
});

afterEach(async () => {
    await gateway.close();
});

async function signer(): Promise<WalletSigner> {
    const identity = IdentitySession.create(gateway.config, { persistSession: false, detectSessionInUrl: false });
    await identity.verifyEmailCode(EMAIL, EMAIL_CODE);
    const read = new ethers.JsonRpcProvider(gateway.config.rpcUrl, 125, { staticNetwork: true, batchMaxCount: 1 });
    return new WalletSigner(embeddedWallet(gateway.config, identity), read);
}

describe('wallet signer', () => {
    it('wallet_signer_sends_through_the_wallet_interface_and_returns_the_chain_response', async () => {
        const walletSigner = await signer();
        const constructions = loadConstructions();
        expect(await walletSigner.getAddress()).toBe(constructions.address);
        const response = await walletSigner.sendTransaction({
            from: constructions.address,
            to: constructions.transaction.to,
            value: 1_000_000_000_000_000n,
            gasLimit: 21_000n,
        });
        expect(response.hash).toBe(fixtureBody('default', 'POST', '/v1/wallet/send').tx_hash);
        expect(response.from).toBe(constructions.address);
        expect(gateway.requests.find((r) => r.path === '/v1/wallet/send')?.body).toEqual({
            tx: { to: ethers.getAddress(constructions.transaction.to), value: '1000000000000000', gas: '21000' },
        });
    });

    it('wallet_signer_signs_messages_and_typed_data_and_refuses_raw_signing', async () => {
        const walletSigner = await signer();
        const constructions = loadConstructions();
        const signature = await walletSigner.signMessage(ethers.toUtf8Bytes(constructions.message));
        expect(ethers.verifyMessage(constructions.message, signature)).toBe(constructions.address);

        const typed = constructions.typedData;
        const types = { ...typed.types };
        delete types.EIP712Domain;
        const typedSignature = await walletSigner.signTypedData(typed.domain, types, typed.message);
        expect(ethers.verifyTypedData(typed.domain, types, typed.message, typedSignature)).toBe(constructions.address);

        await expect(walletSigner.signMessage(new Uint8Array([0xff, 0xfe]))).rejects.toMatchObject({ code: 'message_not_text' });
        await expect(walletSigner.signTransaction()).rejects.toMatchObject({ code: 'raw_signing_unsupported' });
        await expect(
            walletSigner.sendTransaction({ from: ethers.ZeroAddress, to: constructions.transaction.to, value: 1n }),
        ).rejects.toBeInstanceOf(WalletSignerError);
        expect(gateway.requests.filter((r) => r.path === '/v1/wallet/send')).toEqual([]);
    });

    it('wallet_signer_without_a_read_provider_refuses_before_sending', async () => {
        const walletSigner = (await signer()).connect(null);
        const constructions = loadConstructions();
        await expect(walletSigner.sendTransaction({ to: constructions.transaction.to, value: 1n })).rejects.toMatchObject({
            code: 'no_provider',
        });
        expect(gateway.requests.filter((r) => r.path === '/v1/wallet/send')).toEqual([]);
    });
});
