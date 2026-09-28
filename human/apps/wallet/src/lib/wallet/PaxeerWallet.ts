import * as bip39 from '@scure/bip39';
import { wordlist } from '@scure/bip39/wordlists/english';
import type { IEventBus } from './ports/IEventBus';
import type { ISelfCustodyWallet } from './ports/IWallet';
import type {
  PaxeerWalletConfig,
  SelfCustodyWalletSnapshot,
  TransactionData,
  WalletAccount,
} from './types';
import { SimpleEventBus } from './adapters/SimpleEventBus';
import { WalletEvents } from './types';
import { WebCryptoAdapter } from './v2/adapters/web-crypto-adapter';
import { IndexedDBStorageAdapter } from './v2/adapters/indexeddb-storage-adapter';
import { BrowserTimerAdapter } from './v2/adapters/browser-timer-adapter';
import { SecurityEventBus } from './v2/adapters/security-event-bus';
import { AuthenticationManager } from './v2/core/authentication-manager';
import { LegacyMigrationManager } from './v2/core/legacy-migration-manager';
import {
  SESSION_NAMESPACE,
  SessionManager,
} from './v2/core/session-manager';
import { TransactionServiceV2 } from './v2/core/transaction-service';
import { VaultManager } from './v2/core/vault-manager';
import { VaultSigner } from './v2/core/vault-signer';
import { WalletCoreV2 } from './v2/core/wallet-core';
import type { CryptoPort } from './v2/ports/crypto-port';
import type { EventPort } from './v2/ports/event-port';
import type { StoragePort } from './v2/ports/storage-port';
import type { TimerPort } from './v2/ports/timer-port';
import { WalletError } from './v2/types/errors';

const VAULT_NAMESPACE = 'paxport-wallet-v2';
const STAGED_VAULT_NAMESPACE = 'paxport-wallet-v2-staged';
const THROTTLE_NAMESPACE = 'paxport-wallet-auth-throttle-v2';
const LEGACY_LOCAL_KEYS = [
  'paxeer_wallet_state',
  'paxeer_pin_hash',
  'paxeer_active_account',
  'paxeer_session',
  'paxeer_session_data',
  'paxeer_biometric',
  'paxeer_biometric_native',
] as const;

export interface PaxeerWalletDeps {
  storage?: StoragePort;
  crypto?: CryptoPort;
  timer?: TimerPort;
  securityEvents?: EventPort;
  events?: IEventBus;
}

/**
 * Browser self-custody facade. New wallets use only Web Crypto, an
 * authenticated v2 vault, IndexedDB CAS transactions, and a bounded
 * non-extractable session key handle that survives document reloads.
 */
export class PaxeerWallet implements ISelfCustodyWallet {
  readonly kind = 'self-custody' as const;
  readonly events: IEventBus;

  readonly #crypto: CryptoPort;
  readonly #storage: StoragePort;
  readonly #timer: TimerPort;
  readonly #securityEvents: EventPort;
  readonly #vaults: VaultManager;
  readonly #sessions: SessionManager;
  readonly #authentication: AuthenticationManager;
  readonly #migration: LegacyMigrationManager;
  readonly #walletCore: WalletCoreV2;
  readonly #transactions: TransactionServiceV2;
  readonly #inactivityMs: number;
  readonly #sessionRestore: Promise<void>;
  #sessionRestoreError: unknown = null;

  constructor(config: PaxeerWalletConfig, deps: PaxeerWalletDeps = {}) {
    if (!config.rpcUrl) {
      throw WalletError.invalidInput('rpcUrl', 'is required');
    }

    this.#crypto = deps.crypto ?? new WebCryptoAdapter();
    this.#storage = deps.storage ?? new IndexedDBStorageAdapter();
    this.#timer = deps.timer ?? new BrowserTimerAdapter();
    this.#securityEvents = deps.securityEvents ?? new SecurityEventBus();
    this.events = deps.events ?? new SimpleEventBus();
    this.#inactivityMs = config.sessionTimeoutMs ?? 15 * 60 * 1_000;

    this.assertCapabilities();
    this.#vaults = new VaultManager(this.#crypto, this.#storage);
    this.#sessions = new SessionManager(
      this.#storage,
      this.#securityEvents,
      this.#timer,
    );
    this.#authentication = new AuthenticationManager(
      this.#vaults,
      this.#sessions,
      this.#storage,
      this.#securityEvents,
      this.#timer,
      { inactivityMs: this.#inactivityMs },
    );
    this.#walletCore = new WalletCoreV2(
      this.#crypto,
      this.#vaults,
      this.#sessions,
      this.#securityEvents,
      this.#timer,
      config.chainId ?? 125,
    );
    this.#transactions = new TransactionServiceV2(
      this.#walletCore,
      config.rpcUrl,
      config.chainId ?? 125,
      config.transferGasPrice,
    );
    this.#migration = new LegacyMigrationManager(
      this.#crypto,
      this.#storage,
      this.#vaults,
      this.#securityEvents,
      this.#timer,
    );

    this.bridgeEvents();
    this.#sessionRestore = this.restorePersistedSession().catch((error) => {
      this.#sessionRestoreError = error;
    });
  }

  async createNewWallet(
    password: string,
    accountName: string = 'Account 1',
  ): Promise<{ mnemonic: string; account: WalletAccount }> {
    await this.waitForSessionRestore();
    if (await this.#storage.read(VAULT_NAMESPACE)) {
      throw WalletError.invalidInput('wallet', 'a v2 vault already exists');
    }
    if (this.#migration.hasLegacyRecords()) throw WalletError.migrationRequired();
    this.clearOrphanedLegacyBiometrics();

    const mnemonic = bip39.generateMnemonic(wordlist);
    const created = await this.#vaults.createVault({ password, mnemonic });
    await this.#sessions.unlock(
      created.vaultKey,
      created.manifest,
      this.#inactivityMs,
      created.manifest.keySlots[0]?.id ?? null,
    );
    const account = await this.#walletCore.deriveNextAccount(accountName);
    this.#securityEvents.emit({
      kind: 'wallet:created',
      timestamp: this.#timer.now(),
      vaultId: created.manifest.vaultId,
    });
    return { mnemonic, account };
  }

  async restoreFromMnemonic(
    password: string,
    mnemonic: string,
  ): Promise<WalletAccount[]> {
    await this.waitForSessionRestore();
    if (await this.#storage.read(VAULT_NAMESPACE)) {
      throw WalletError.invalidInput('wallet', 'a v2 vault already exists');
    }
    if (this.#migration.hasLegacyRecords()) throw WalletError.migrationRequired();
    this.clearOrphanedLegacyBiometrics();

    const created = await this.#vaults.createVault({ password, mnemonic });
    await this.#sessions.unlock(
      created.vaultKey,
      created.manifest,
      this.#inactivityMs,
      created.manifest.keySlots[0]?.id ?? null,
    );
    const account = await this.#walletCore.deriveNextAccount('Account 1');
    this.#securityEvents.emit({
      kind: 'wallet:created',
      timestamp: this.#timer.now(),
      vaultId: created.manifest.vaultId,
      metadata: { restored: true },
    });
    return [account];
  }

  async unlock(password: string): Promise<boolean> {
    await this.waitForSessionRestore();
    if (this.#sessions.isUnlocked()) {
      await this.#authentication.stepUp(password);
      return true;
    }
    if (!(await this.#storage.read(VAULT_NAMESPACE))) {
      if (this.#migration.hasLegacyRecords()) throw WalletError.migrationRequired();
      throw WalletError.corruptVault('No wallet exists');
    }
    await this.#authentication.unlock(password);
    return true;
  }

  async reauthenticate(password: string): Promise<void> {
    await this.waitForSessionRestore();
    this.#sessions.requireUnlocked();
    await this.#authentication.stepUp(password);
  }

  needsMigration(): boolean {
    return this.#migration.hasLegacyRecords();
  }

  async migrateLegacy(
    legacyPin: string,
    newPassword: string,
  ): Promise<void> {
    await this.waitForSessionRestore();
    await this.#migration.migrate(legacyPin, newPassword);
    await this.#authentication.unlock(newPassword);
  }

  async migratePassphraseToPin(
    currentPassphrase: string,
    newPin: string,
  ): Promise<void> {
    await this.waitForSessionRestore();
    await this.#vaults.replacePasswordSlot({
      currentPassword: currentPassphrase,
      newPassword: newPin,
    });
    await this.#sessions.lock('credential_replaced');
    await this.#authentication.unlock(newPin);
  }

  async lock(): Promise<void> {
    await this.waitForSessionRestore();
    await this.#sessions.lock('manual');
  }

  getSigner(address: string): VaultSigner {
    this.#sessions.requireUnlocked();
    return this.#transactions.getSigner(address);
  }

  async getAccounts(): Promise<WalletAccount[]> {
    await this.waitForSessionRestore();
    return this.#walletCore.getAccounts();
  }

  async getActiveAccount(): Promise<WalletAccount | null> {
    await this.waitForSessionRestore();
    return this.#walletCore.getActiveAccount();
  }

  async setActiveAccount(address: string): Promise<void> {
    await this.waitForSessionRestore();
    await this.#walletCore.setActiveAccount(address);
  }

  async deriveNextAccount(name: string): Promise<WalletAccount> {
    await this.waitForSessionRestore();
    return this.#walletCore.deriveNextAccount(name);
  }

  async renameAccount(address: string, name: string): Promise<void> {
    await this.waitForSessionRestore();
    await this.#walletCore.renameAccount(address, name);
  }

  async deleteAccount(address: string): Promise<void> {
    await this.waitForSessionRestore();
    await this.#walletCore.deleteAccount(address);
  }

  async importPrivateKey(
    privateKey: string,
    name: string,
  ): Promise<WalletAccount> {
    await this.waitForSessionRestore();
    return this.#walletCore.importPrivateKey(privateKey, name);
  }

  async exportMnemonic(): Promise<string> {
    await this.waitForSessionRestore();
    return this.#walletCore.exportMnemonic();
  }

  async exportPrivateKey(address: string): Promise<string> {
    await this.waitForSessionRestore();
    return this.#walletCore.exportPrivateKey(address);
  }

  async send(transaction: TransactionData): Promise<string> {
    await this.waitForSessionRestore();
    const active = await this.#walletCore.getActiveAccount();
    if (!active) throw WalletError.accountNotFound('active');
    return this.#transactions.sendTransaction(active.address, transaction);
  }

  async getReceiveAddress(): Promise<string> {
    await this.waitForSessionRestore();
    const active = await this.#walletCore.getActiveAccount();
    if (!active) throw WalletError.accountNotFound('active');
    return active.address;
  }

  isSessionValid(): boolean {
    return this.#sessions.isUnlocked();
  }

  getSessionTimeRemaining(): number {
    return this.#sessions.getStatus().remainingMs ?? 0;
  }

  async getSnapshot(): Promise<SelfCustodyWalletSnapshot> {
    await this.waitForSessionRestore();
    const hasV2Vault = Boolean(await this.#storage.read(VAULT_NAMESPACE));
    const migrationRequired = this.#migration.hasLegacyRecords();
    const hasWallet = hasV2Vault || migrationRequired;
    const locked = (): SelfCustodyWalletSnapshot => ({
      hasWallet,
      migrationRequired,
      isLocked: true,
      sessionRemaining: 0,
      accounts: [],
      activeAccount: null,
    });

    if (!hasV2Vault || !this.#sessions.isUnlocked()) return locked();

    try {
      const accountState = await this.#walletCore.getAccountSnapshot();
      const session = this.#sessions.getStatus();
      if (!session.unlocked) return locked();
      return {
        hasWallet,
        migrationRequired,
        isLocked: false,
        sessionRemaining: session.remainingMs ?? 0,
        accounts: accountState.accounts,
        activeAccount: accountState.activeAccount,
      };
    } catch (error) {
      if (error instanceof WalletError && error.code === 'LOCKED') {
        return locked();
      }
      throw error;
    }
  }

  async hasWallet(): Promise<boolean> {
    await this.waitForSessionRestore();
    return Boolean(await this.#storage.read(VAULT_NAMESPACE))
      || this.#migration.hasLegacyRecords();
  }

  async isReady(): Promise<boolean> {
    const snapshot = await this.getSnapshot();
    return snapshot.hasWallet && !snapshot.isLocked;
  }

  async reset(): Promise<void> {
    await this.waitForSessionRestore();
    const stored = await this.#storage.read(VAULT_NAMESPACE);
    await this.#sessions.lock('reset');
    await this.requireDelete(
      this.#storage.delete(VAULT_NAMESPACE, null),
      'vault',
    );
    await this.requireDelete(
      this.#storage.delete(STAGED_VAULT_NAMESPACE, null),
      'staged vault',
    );
    await this.requireDelete(
      this.#storage.deleteAuthThrottle(THROTTLE_NAMESPACE, null),
      'authentication metadata',
    );
    if (typeof globalThis.localStorage !== 'undefined') {
      for (const key of LEGACY_LOCAL_KEYS) globalThis.localStorage.removeItem(key);
    }
    if (stored) {
      this.#securityEvents.emit({
        kind: 'wallet:reset',
        timestamp: this.#timer.now(),
        vaultId: stored.vaultId,
      });
      this.#storage.notify('lock', stored.vaultId);
    }
  }

  async destroy(): Promise<void> {
    await this.#sessions.destroy();
    const close = (this.#storage as StoragePort & {
      close?: () => Promise<void>;
    }).close;
    if (close) await close.call(this.#storage);
  }

  private assertCapabilities(): void {
    const crypto = this.#crypto.capabilities();
    if (!crypto.subtle || !crypto.getRandomValues || !crypto.secureContext) {
      throw WalletError.storageUnavailable(
        new Error('Web Crypto capabilities are required'),
      );
    }
    const storage = this.#storage.capabilities();
    if (
      !storage.indexedDb
      || !storage.atomicTransactions
      || !storage.revisionChecks
      || !storage.crossContextNotifications
    ) {
      throw WalletError.storageUnavailable(
        new Error('Transactional IndexedDB and cross-context locking are required'),
      );
    }
  }

  private async restorePersistedSession(): Promise<void> {
    let persisted;
    try {
      persisted = await this.#storage.readSession(SESSION_NAMESPACE);
    } catch (error) {
      try {
        await this.#storage.deleteSession(SESSION_NAMESPACE);
      } catch {
        throw error;
      }
      return;
    }
    if (!persisted) return;

    const stored = await this.#storage.read(VAULT_NAMESPACE);
    if (
      !stored
      || stored.vaultId !== persisted.vaultId
      || persisted.inactivityDeadline <= this.#timer.now()
    ) {
      await this.#storage.deleteSession(SESSION_NAMESPACE);
      return;
    }

    try {
      await this.#vaults.decryptPayload(persisted.vaultKey, stored.manifest);
    } catch {
      await this.#storage.deleteSession(SESSION_NAMESPACE);
      return;
    }

    await this.#sessions.restore(persisted, stored.manifest);
  }

  private async waitForSessionRestore(): Promise<void> {
    await this.#sessionRestore;
    if (this.#sessionRestoreError) throw this.#sessionRestoreError;
  }

  private bridgeEvents(): void {
    this.#securityEvents.on('wallet:unlocked', event =>
      this.events.emit(WalletEvents.SESSION_CREATED, event));
    this.#securityEvents.on('wallet:locked', event => {
      this.events.emit(WalletEvents.MANUAL_LOCK, event);
      this.events.emit(WalletEvents.SESSION_EXPIRED, event);
    });
    this.#securityEvents.on('account:changed', event =>
      this.events.emit(WalletEvents.ACCOUNT_CHANGED, event));
    this.#securityEvents.on('wallet:reset', event =>
      this.events.emit(WalletEvents.WALLET_CLEARED, event));
  }

  private clearOrphanedLegacyBiometrics(): void {
    if (typeof globalThis.localStorage === 'undefined') return;
    globalThis.localStorage.removeItem('paxeer_biometric');
    globalThis.localStorage.removeItem('paxeer_biometric_native');
  }

  private async requireDelete(
    operation: Promise<
      | { readonly ok: true; readonly deleted: boolean }
      | { readonly ok: false; readonly error: string }
    >,
    target: string,
  ): Promise<void> {
    const result = await operation;
    if (!result.ok) {
      throw WalletError.storageUnavailable(
        new Error(`Failed to delete ${target}: ${result.error}`),
      );
    }
  }
}
