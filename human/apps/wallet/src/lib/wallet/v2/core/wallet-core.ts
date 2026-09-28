import { HDKey } from '@scure/bip32';
import * as bip39 from '@scure/bip39';
import { ethers } from 'ethers';
import type { CryptoPort } from '../ports/crypto-port';
import type { EventPort } from '../ports/event-port';
import type { TimerPort } from '../ports/timer-port';
import type { WalletAccount } from '../types/account';
import type {
  ApprovedMessageRequest,
  ApprovedTransactionRequest,
  ApprovedTypedDataRequest,
  SigningAuthorization,
} from '../types/signing';
import type { SecurityEventKind } from '../types/events';
import type {
  VaultAccountV2,
  WalletPayloadV2,
} from '../types/vault';
import { WalletError } from '../types/errors';
import { validateAccountName, validatePayload } from './vault-validators';
import type { SessionManager } from './session-manager';
import type { VaultManager } from './vault-manager';

const BASE_PATH = "m/44'/60'/0'/0";

export class WalletCoreV2 {
  private mutationQueue: Promise<void> = Promise.resolve();

  constructor(
    private readonly crypto: CryptoPort,
    private readonly vaults: VaultManager,
    private readonly sessions: SessionManager,
    private readonly events: EventPort | null,
    private readonly timer: TimerPort,
    private readonly chainId: number = 125,
  ) {}

  async getAccounts(): Promise<WalletAccount[]> {
    const payload = await this.loadPayload();
    return payload.accounts.map(account => this.toPublicAccount(account));
  }

  async getActiveAccount(): Promise<WalletAccount | null> {
    const payload = await this.loadPayload();
    if (!payload.activeAccountId) return null;
    const account = payload.accounts.find(item => item.id === payload.activeAccountId);
    if (!account) throw WalletError.corruptVault('Active account is missing');
    return this.toPublicAccount(account);
  }

  async getAccountSnapshot(): Promise<{
    accounts: WalletAccount[];
    activeAccount: WalletAccount | null;
  }> {
    const payload = await this.loadPayload();
    const accounts = payload.accounts.map(account => this.toPublicAccount(account));
    if (!payload.activeAccountId) return { accounts, activeAccount: null };
    const active = payload.accounts.find(
      account => account.id === payload.activeAccountId,
    );
    if (!active) throw WalletError.corruptVault('Active account is missing');
    return {
      accounts,
      activeAccount: this.toPublicAccount(active),
    };
  }

  async deriveNextAccount(name: string): Promise<WalletAccount> {
    const normalizedName = validateAccountName(name, 'account.name');
    return this.mutate(async (payload) => {
      const account = this.deriveAccount(
        payload.mnemonic,
        payload.nextAccountIndex,
        normalizedName,
      );
      const next: WalletPayloadV2 = {
        ...payload,
        accounts: [...payload.accounts, account],
        activeAccountId: payload.activeAccountId ?? account.id,
        nextAccountIndex: payload.nextAccountIndex + 1,
      };
      return { payload: next, result: this.toPublicAccount(account) };
    });
  }

  async importPrivateKey(privateKey: string, name: string): Promise<WalletAccount> {
    const normalizedName = validateAccountName(name, 'account.name');
    let wallet: ethers.Wallet;
    try {
      wallet = new ethers.Wallet(privateKey);
    } catch {
      throw WalletError.invalidInput('privateKey', 'must be a valid secp256k1 key');
    }

    return this.mutate(async (payload) => {
      if (
        payload.accounts.some(
          account => account.address.toLowerCase() === wallet.address.toLowerCase(),
        )
      ) {
        throw WalletError.invalidInput('privateKey', 'account already exists');
      }
      const account: VaultAccountV2 = {
        id: this.generateUuid(),
        kind: 'imported',
        address: wallet.address,
        name: normalizedName,
        privateKey: wallet.privateKey,
      };
      const next: WalletPayloadV2 = {
        ...payload,
        accounts: [...payload.accounts, account],
        activeAccountId: payload.activeAccountId ?? account.id,
      };
      return { payload: next, result: this.toPublicAccount(account) };
    });
  }

  async renameAccount(address: string, name: string): Promise<void> {
    const checksummed = this.normalizeAddress(address);
    const normalizedName = validateAccountName(name, 'account.name');
    await this.mutate(async (payload) => {
      let found = false;
      const accounts = payload.accounts.map((account) => {
        if (account.address !== checksummed) return account;
        found = true;
        return { ...account, name: normalizedName };
      });
      if (!found) throw WalletError.accountNotFound(checksummed);
      return { payload: { ...payload, accounts }, result: undefined };
    });
  }

  async deleteAccount(address: string): Promise<void> {
    const checksummed = this.normalizeAddress(address);
    await this.mutate(async (payload) => {
      const target = payload.accounts.find(account => account.address === checksummed);
      if (!target) throw WalletError.accountNotFound(checksummed);
      const accounts = payload.accounts.filter(account => account.id !== target.id);
      const activeAccountId = payload.activeAccountId === target.id
        ? accounts[0]?.id ?? null
        : payload.activeAccountId;
      return {
        payload: { ...payload, accounts, activeAccountId },
        result: undefined,
      };
    });
  }

  async setActiveAccount(address: string): Promise<void> {
    const checksummed = this.normalizeAddress(address);
    await this.mutate(async (payload) => {
      const account = payload.accounts.find(item => item.address === checksummed);
      if (!account) throw WalletError.accountNotFound(checksummed);
      return {
        payload: { ...payload, activeAccountId: account.id },
        result: undefined,
      };
    });
  }

  async exportMnemonic(): Promise<string> {
    this.requireStepUp();
    const payload = await this.loadPayload();
    this.sessions.touchSensitiveActivity();
    this.emit('export:mnemonic', this.requireManifest().vaultId);
    return payload.mnemonic;
  }

  async exportPrivateKey(address: string): Promise<string> {
    this.requireStepUp();
    const payload = await this.loadPayload();
    const account = this.findAccount(payload, address);
    const privateKey = this.resolvePrivateKey(payload, account);
    this.sessions.touchSensitiveActivity();
    this.emit(
      'export:private_key',
      this.requireManifest().vaultId,
      account.address,
    );
    return privateKey;
  }

  async signMessage(request: ApprovedMessageRequest): Promise<string> {
    return this.withSigningWallet(request, 'sign:message', wallet =>
      wallet.signMessage(request.message),
    );
  }

  async signTypedData(request: ApprovedTypedDataRequest): Promise<string> {
    const domainChainId = request.domain.chainId;
    if (
      domainChainId != null
      && BigInt(domainChainId) !== BigInt(request.chainId)
    ) {
      throw WalletError.invalidInput(
        'typedData.domain.chainId',
        'does not match the approved chain',
      );
    }
    return this.withSigningWallet(request, 'sign:typed_data', wallet =>
      wallet.signTypedData(request.domain, request.types, request.value),
    );
  }

  async signTransaction(request: ApprovedTransactionRequest): Promise<string> {
    const transaction = ethers.copyRequest(request.transaction);
    const transactionChainId = transaction.chainId;
    if (
      transactionChainId != null
      && BigInt(transactionChainId) !== BigInt(request.chainId)
    ) {
      throw WalletError.invalidInput(
        'transaction.chainId',
        'does not match the approved chain',
      );
    }
    if (
      transaction.from != null
      && (
        typeof transaction.from !== 'string'
        || this.normalizeAddress(transaction.from) !==
        this.normalizeAddress(request.accountAddress)
      )
    ) {
      throw WalletError.invalidInput(
        'transaction.from',
        'does not match the approved account',
      );
    }
    return this.withSigningWallet(request, 'sign:transaction', wallet =>
      wallet.signTransaction({
        ...transaction,
        from: undefined,
        chainId: request.chainId,
      }),
    );
  }

  private async loadPayload(): Promise<WalletPayloadV2> {
    const manifest = this.requireManifest();
    return this.vaults.decryptPayload(this.sessions.requireVaultKey(), manifest);
  }

  private resolvePrivateKey(payload: WalletPayloadV2, account: VaultAccountV2): string {
    if (account.kind === 'imported') return account.privateKey;
    const root = HDKey.fromMasterSeed(bip39.mnemonicToSeedSync(payload.mnemonic));
    const derived = root.derive(account.derivationPath);
    if (!derived.privateKey) {
      throw WalletError.corruptVault('Derivation did not produce a private key');
    }
    const privateKey = ethers.hexlify(derived.privateKey);
    if (new ethers.Wallet(privateKey).address !== account.address) {
      throw WalletError.corruptVault('Derived private key address mismatch');
    }
    return privateKey;
  }

  private findAccount(payload: WalletPayloadV2, address: string): VaultAccountV2 {
    const checksummed = this.normalizeAddress(address);
    const account = payload.accounts.find(item => item.address === checksummed);
    if (!account) throw WalletError.accountNotFound(checksummed);
    return account;
  }

  private async withSigningWallet<T>(
    authorization: SigningAuthorization,
    eventKind: 'sign:message' | 'sign:typed_data' | 'sign:transaction',
    operation: (wallet: ethers.Wallet) => Promise<T>,
  ): Promise<T> {
    this.validateSigningAuthorization(authorization);
    const payload = await this.loadPayload();
    const account = this.findAccount(payload, authorization.accountAddress);
    const privateKey = this.resolvePrivateKey(payload, account);
    const signingWallet = new ethers.Wallet(privateKey);
    const result = await operation(signingWallet);
    this.sessions.touchSensitiveActivity();
    this.emit(
      eventKind,
      this.requireManifest().vaultId,
      account.address,
      { approvalId: authorization.approvalId, chainId: authorization.chainId },
    );
    return result;
  }

  private async mutate<T>(
    operation: (
      payload: WalletPayloadV2,
    ) => Promise<{ payload: WalletPayloadV2; result: T }>,
  ): Promise<T> {
    let resolveResult!: (value: T | PromiseLike<T>) => void;
    let rejectResult!: (reason?: unknown) => void;
    const result = new Promise<T>((resolve, reject) => {
      resolveResult = resolve;
      rejectResult = reject;
    });

    this.mutationQueue = this.mutationQueue
      .catch(() => undefined)
      .then(async () => {
        try {
          const currentManifest = this.requireManifest();
          const currentPayload = await this.loadPayload();
          const mutation = await operation(currentPayload);
          const payload = validatePayload(mutation.payload);
          const committed = await this.vaults.commitPayload(
            this.sessions.requireVaultKey(),
            currentManifest,
            payload,
          );
          this.sessions.updateManifest(committed.manifest);
          this.sessions.notifyRevisionChange(committed.manifest.revision);
          this.sessions.touchSensitiveActivity();
          this.emit('account:changed', committed.manifest.vaultId);
          this.emit('vault:mutated', committed.manifest.vaultId, undefined, {
            revision: committed.manifest.revision,
          });
          resolveResult(mutation.result);
        } catch (error) {
          rejectResult(error);
        }
      });

    return result;
  }

  private deriveAccount(
    mnemonic: string,
    accountIndex: number,
    name: string,
  ): VaultAccountV2 {
    const derivationPath = `${BASE_PATH}/${accountIndex}`;
    const root = HDKey.fromMasterSeed(bip39.mnemonicToSeedSync(mnemonic));
    const derived = root.derive(derivationPath);
    if (!derived.privateKey) {
      throw WalletError.corruptVault('Derivation did not produce a private key');
    }
    return {
      id: this.generateUuid(),
      kind: 'derived',
      address: new ethers.Wallet(ethers.hexlify(derived.privateKey)).address,
      name,
      derivationPath,
      accountIndex,
    };
  }

  private toPublicAccount(account: VaultAccountV2): WalletAccount {
    return account.kind === 'derived'
      ? {
        id: account.id,
        kind: 'derived',
        address: account.address,
        name: account.name,
        derivationPath: account.derivationPath,
        accountIndex: account.accountIndex,
      }
      : {
        id: account.id,
        kind: 'imported',
        address: account.address,
        name: account.name,
        derivationPath: 'imported',
        accountIndex: -1,
      };
  }

  private requireManifest() {
    const manifest = this.sessions.getManifest();
    if (!manifest) throw WalletError.locked();
    return manifest;
  }

  private requireStepUp(): void {
    if (!this.sessions.isStepUpValid()) {
      throw WalletError.reauthenticationRequired();
    }
  }

  private normalizeAddress(address: string): string {
    try {
      return ethers.getAddress(address);
    } catch {
      throw WalletError.invalidInput('address', 'must be a valid EVM address');
    }
  }

  private validateSigningAuthorization(
    authorization: SigningAuthorization,
  ): void {
    if (
      !authorization.approvalId
      || authorization.approvalId.length < 16
      || authorization.approvalId.length > 128
      || !/^[A-Za-z0-9._:-]+$/.test(authorization.approvalId)
    ) {
      throw WalletError.invalidInput(
        'approvalId',
        'must be 16-128 safe identifier characters',
      );
    }
    if (
      !Number.isSafeInteger(authorization.chainId)
      || authorization.chainId !== this.chainId
    ) {
      throw WalletError.invalidInput(
        'chainId',
        `must match configured chain ${this.chainId}`,
      );
    }
    this.normalizeAddress(authorization.accountAddress);
  }

  private generateUuid(): string {
    const bytes = this.crypto.generateRandomBytes(16);
    bytes[6] = (bytes[6] & 0x0f) | 0x40;
    bytes[8] = (bytes[8] & 0x3f) | 0x80;
    const hex = Array.from(bytes, byte => byte.toString(16).padStart(2, '0')).join('');
    return [
      hex.slice(0, 8),
      hex.slice(8, 12),
      hex.slice(12, 16),
      hex.slice(16, 20),
      hex.slice(20),
    ].join('-');
  }

  private emit(
    kind: SecurityEventKind,
    vaultId: string,
    accountAddress?: string,
    metadata?: Record<string, string | number | boolean>,
  ): void {
    this.events?.emit({
      kind,
      timestamp: this.timer.now(),
      vaultId,
      accountAddress,
      metadata,
    });
  }
}
