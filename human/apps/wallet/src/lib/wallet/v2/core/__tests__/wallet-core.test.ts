import { beforeEach, describe, expect, it } from 'vitest';
import { ethers } from 'ethers';
import * as bip39 from '@scure/bip39';
import { wordlist } from '@scure/bip39/wordlists/english';
import { WebCryptoAdapter } from '../../adapters/web-crypto-adapter';
import { BrowserTimerAdapter } from '../../adapters/browser-timer-adapter';
import { SessionManager } from '../session-manager';
import { VaultManager } from '../vault-manager';
import { WalletCoreV2 } from '../wallet-core';
import { VaultSigner } from '../vault-signer';

const PASSWORD = '482915';
const MNEMONIC = bip39.entropyToMnemonic(new Uint8Array(16), wordlist);

describe('WalletCoreV2', () => {
  const crypto = new WebCryptoAdapter();
  const timer = new BrowserTimerAdapter();
  let vaults: VaultManager;
  let sessions: SessionManager;
  let wallet: WalletCoreV2;

  beforeEach(async () => {
    vaults = new VaultManager(crypto, null);
    const created = await vaults.createVault({
      password: PASSWORD,
      mnemonic: MNEMONIC,
    });
    sessions = new SessionManager(null, null, timer);
    await sessions.unlock(
      created.vaultKey,
      created.manifest,
      60_000,
      created.manifest.keySlots[0].id,
    );
    wallet = new WalletCoreV2(crypto, vaults, sessions, null, timer);
  });

  it('derives metadata-only accounts without persisting derived private keys', async () => {
    const account = await wallet.deriveNextAccount('Account 1');
    const payload = await vaults.decryptPayload(
      sessions.requireVaultKey(),
      sessions.getManifest()!,
    );

    expect(account.address).toBe('0x9858EfFD232B4033E47d90003D41EC34EcaEda94');
    expect(account).not.toHaveProperty('privateKey');
    expect(payload.accounts[0]).not.toHaveProperty('privateKey');
    expect(JSON.stringify(sessions.getManifest())).not.toContain(MNEMONIC);
  });

  it('imports a private key only inside the authenticated payload', async () => {
    const importedWallet = ethers.Wallet.createRandom();
    const account = await wallet.importPrivateKey(
      importedWallet.privateKey,
      'Imported',
    );
    const publicAccounts = await wallet.getAccounts();
    const payload = await vaults.decryptPayload(
      sessions.requireVaultKey(),
      sessions.getManifest()!,
    );

    expect(account.address).toBe(importedWallet.address);
    expect(publicAccounts[0]).not.toHaveProperty('privateKey');
    expect(payload.accounts[0]).toMatchObject({
      kind: 'imported',
      address: importedWallet.address,
      privateKey: importedWallet.privateKey,
    });
    expect(JSON.stringify(sessions.getManifest())).not.toContain(
      importedWallet.privateKey,
    );
  });

  it('rejects duplicate imported accounts', async () => {
    const importedWallet = ethers.Wallet.createRandom();
    await wallet.importPrivateKey(importedWallet.privateKey, 'Imported');
    await expect(
      wallet.importPrivateKey(importedWallet.privateKey, 'Duplicate'),
    ).rejects.toMatchObject({ code: 'INVALID_INPUT' });
  });

  it('validates active account selection', async () => {
    const first = await wallet.deriveNextAccount('First');
    const second = await wallet.deriveNextAccount('Second');

    expect((await wallet.getActiveAccount())?.address).toBe(first.address);
    await wallet.setActiveAccount(second.address);
    expect((await wallet.getActiveAccount())?.address).toBe(second.address);
    await expect(
      wallet.setActiveAccount(ethers.Wallet.createRandom().address),
    ).rejects.toMatchObject({ code: 'ACCOUNT_NOT_FOUND' });
  });

  it('renames and deletes accounts without corrupting active selection', async () => {
    const first = await wallet.deriveNextAccount('First');
    const second = await wallet.deriveNextAccount('Second');
    await wallet.setActiveAccount(second.address);
    await wallet.renameAccount(second.address, 'Treasury');
    expect((await wallet.getActiveAccount())?.name).toBe('Treasury');

    await wallet.deleteAccount(second.address);
    expect((await wallet.getActiveAccount())?.address).toBe(first.address);
    expect(await wallet.getAccounts()).toHaveLength(1);
  });

  it('serializes simultaneous mutations without losing an account', async () => {
    const [first, second] = await Promise.all([
      wallet.deriveNextAccount('First'),
      wallet.deriveNextAccount('Second'),
    ]);
    const accounts = await wallet.getAccounts();

    expect(accounts.map(account => account.address)).toEqual([
      first.address,
      second.address,
    ]);
    expect(new Set(accounts.map(account => account.accountIndex))).toEqual(
      new Set([0, 1]),
    );
  });

  it('signs messages inside wallet-core without exposing a private key', async () => {
    const account = await wallet.deriveNextAccount('Signer');
    const signature = await wallet.signMessage({
      approvalId: 'approval-message-0001',
      accountAddress: account.address,
      chainId: 125,
      message: 'PaxPort signing boundary',
    });

    expect(ethers.verifyMessage('PaxPort signing boundary', signature)).toBe(
      account.address,
    );
    expect(account).not.toHaveProperty('privateKey');
  });

  it('binds transactions to the approved account and chain', async () => {
    const account = await wallet.deriveNextAccount('Signer');
    const signed = await wallet.signTransaction({
      approvalId: 'approval-transaction-0001',
      accountAddress: account.address,
      chainId: 125,
      transaction: {
        to: ethers.Wallet.createRandom().address,
        value: 1n,
        nonce: 0,
        gasLimit: 21_000n,
        gasPrice: 1n,
        chainId: 125,
      },
    });
    expect(ethers.Transaction.from(signed).from).toBe(account.address);

    await expect(
      wallet.signTransaction({
        approvalId: 'approval-transaction-0002',
        accountAddress: account.address,
        chainId: 1,
        transaction: { to: account.address, chainId: 1 },
      }),
    ).rejects.toMatchObject({ code: 'INVALID_INPUT' });
  });

  it('requires fresh step-up for explicit mnemonic and key export', async () => {
    const account = await wallet.deriveNextAccount('Exporter');
    await expect(wallet.exportMnemonic()).rejects.toMatchObject({
      code: 'REAUTHENTICATION_REQUIRED',
    });
    await expect(wallet.exportPrivateKey(account.address)).rejects.toMatchObject({
      code: 'REAUTHENTICATION_REQUIRED',
    });

    sessions.recordStepUp();
    expect(await wallet.exportMnemonic()).toBe(MNEMONIC);
    const privateKey = await wallet.exportPrivateKey(account.address);
    expect(new ethers.Wallet(privateKey).address).toBe(account.address);
  });

  it('provides a keyless ethers signer bridge', async () => {
    const account = await wallet.deriveNextAccount('Bridge');
    const signer = new VaultSigner(wallet, account.address, null, 125);
    const signature = await signer.signMessage('bridge message');

    expect(signer).not.toBeInstanceOf(ethers.Wallet);
    expect(ethers.verifyMessage('bridge message', signature)).toBe(account.address);
  });

  it('preserves populated ethers Transaction fields across the signer bridge', async () => {
    const account = await wallet.deriveNextAccount('Transaction bridge');
    const signer = new VaultSigner(wallet, account.address, null, 125);
    const recipient = ethers.Wallet.createRandom().address;
    const populated = ethers.Transaction.from({
      to: recipient,
      value: 123n,
      nonce: 7,
      gasLimit: 21_000n,
      maxFeePerGas: 9n,
      maxPriorityFeePerGas: 2n,
      chainId: 125,
      type: 2,
    });

    const signed = await signer.signTransaction(populated);
    const decoded = ethers.Transaction.from(signed);

    expect(decoded.from).toBe(account.address);
    expect(decoded.to).toBe(recipient);
    expect(decoded.value).toBe(123n);
    expect(decoded.nonce).toBe(7);
    expect(decoded.gasLimit).toBe(21_000n);
    expect(decoded.maxFeePerGas).toBe(9n);
    expect(decoded.maxPriorityFeePerGas).toBe(2n);
    expect(decoded.chainId).toBe(125n);
  });
});
