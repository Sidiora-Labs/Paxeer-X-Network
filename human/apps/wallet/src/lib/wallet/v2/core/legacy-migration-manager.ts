import { HDKey } from '@scure/bip32';
import * as bip39 from '@scure/bip39';
import { wordlist } from '@scure/bip39/wordlists/english';
import { ethers } from 'ethers';
import type { CryptoPort } from '../ports/crypto-port';
import type { EventPort } from '../ports/event-port';
import type { StoragePort } from '../ports/storage-port';
import type { TimerPort } from '../ports/timer-port';
import type { VaultAccountV2, WalletPayloadV2 } from '../types/vault';
import type { SecurityEventKind } from '../types/events';
import { WalletError } from '../types/errors';
import { LegacyCryptoJsReader } from '../adapters/legacy-cryptojs-reader';
import { VaultManager } from './vault-manager';
import { validatePayload } from './vault-validators';

const LEGACY_WALLET_KEY = 'paxeer_wallet_state';
const LEGACY_PIN_KEY = 'paxeer_pin_hash';
const LEGACY_ACTIVE_ACCOUNT_KEY = 'paxeer_active_account';
const LEGACY_SESSION_KEYS = ['paxeer_session', 'paxeer_session_data'] as const;
const LEGACY_BIOMETRIC_KEYS = [
  'paxeer_biometric',
  'paxeer_biometric_native',
] as const;
const V2_VAULT_NAMESPACE = 'paxport-wallet-v2';
const BASE_PATH = "m/44'/60'/0'/0";
const KDF_TARGET_MS = 250;

interface LegacyAccount {
  address: string;
  privateKey: string;
  name: string;
  derivationPath: string;
  accountIndex: number;
}

interface LegacyWalletState {
  encryptedMnemonic: string;
  accounts: LegacyAccount[];
  nextAccountIndex: number;
}

export interface LegacyMigrationResult {
  readonly vaultId: string;
  readonly revision: number;
  readonly accountCount: number;
}

export class LegacyMigrationManager {
  private readonly reader = new LegacyCryptoJsReader();

  constructor(
    private readonly crypto: CryptoPort,
    private readonly storage: StoragePort,
    private readonly vaults: VaultManager,
    private readonly events: EventPort | null,
    private readonly timer: TimerPort,
  ) {}

  hasLegacyWallet(): boolean {
    const browserStorage = this.getLegacyStorage();
    return Boolean(
      browserStorage.getItem(LEGACY_WALLET_KEY)
      && browserStorage.getItem(LEGACY_PIN_KEY),
    );
  }

  hasLegacyRecords(): boolean {
    const browserStorage = this.getLegacyStorage();
    return Boolean(
      browserStorage.getItem(LEGACY_WALLET_KEY)
      || browserStorage.getItem(LEGACY_PIN_KEY),
    );
  }

  async migrate(
    legacyPin: string,
    newPassword: string,
  ): Promise<LegacyMigrationResult> {
    const browserStorage = this.getLegacyStorage();
    const rawWallet = browserStorage.getItem(LEGACY_WALLET_KEY);
    const rawPin = browserStorage.getItem(LEGACY_PIN_KEY);
    if (!rawWallet || !rawPin) throw WalletError.migrationRequired();

    this.emit('migration:started');
    try {
      if (!this.reader.verifyPin(legacyPin, rawPin)) {
        throw WalletError.authenticationFailed();
      }

      const legacy = this.parseLegacyWallet(rawWallet);
      const mnemonic = this.reader
        .decryptWalletSecret(legacyPin, legacy.encryptedMnemonic)
        .trim()
        .toLowerCase()
        .replace(/\s+/g, ' ');
      if (!bip39.validateMnemonic(mnemonic, wordlist)) {
        throw WalletError.migrationFailed('Legacy mnemonic is invalid');
      }
      const activeAddress = browserStorage.getItem(LEGACY_ACTIVE_ACCOUNT_KEY);
      const accounts = this.convertAccounts(legacyPin, mnemonic, legacy.accounts);
      const activeAccount = activeAddress
        ? accounts.find(
          account => account.address.toLowerCase() === activeAddress.toLowerCase(),
        )
        : accounts[0];
      if (accounts.length > 0 && !activeAccount) {
        throw WalletError.migrationFailed('Legacy active account is invalid');
      }

      const existing = await this.storage.read(V2_VAULT_NAMESPACE);
      if (existing) {
        const unlocked = await this.vaults.unlockVault(
          newPassword,
          existing.manifest,
        );
        this.assertEquivalentMigration(
          mnemonic,
          accounts,
          activeAccount?.address ?? null,
          legacy.nextAccountIndex,
          unlocked.payload,
        );
        await this.finishLegacyDeletion(browserStorage);
        this.emit('migration:completed', existing.vaultId, {
          resumed: true,
          accountCount: unlocked.payload.accounts.length,
        });
        return {
          vaultId: existing.vaultId,
          revision: existing.revision,
          accountCount: unlocked.payload.accounts.length,
        };
      }

      const transientVaults = new VaultManager(this.crypto, null);
      const iterations = await this.crypto.calibrateKdf(KDF_TARGET_MS, 600_000);
      const created = await transientVaults.createVault({
        password: newPassword,
        mnemonic,
        iterations,
      });
      const payload: WalletPayloadV2 = validatePayload({
        schema: 2,
        mnemonic,
        derivation: {
          curve: 'secp256k1',
          standard: 'bip44',
          basePath: BASE_PATH,
        },
        accounts,
        activeAccountId: activeAccount?.id ?? null,
        nextAccountIndex: legacy.nextAccountIndex,
        createdAt: created.manifest.createdAt,
      });
      const prepared = await transientVaults.commitPayload(
        created.vaultKey,
        created.manifest,
        payload,
      );
      const persisted = await this.storage.write(
        V2_VAULT_NAMESPACE,
        {
          vaultId: prepared.manifest.vaultId,
          revision: prepared.manifest.revision,
          manifest: prepared.manifest,
        },
        null,
      );
      if (!persisted.ok) {
        throw persisted.error === 'WRITE_CONFLICT'
          ? WalletError.writeConflict()
          : WalletError.storageUnavailable();
      }

      try {
        const verified = await this.vaults.unlockVault(newPassword);
        if (verified.payload.accounts.length !== accounts.length) {
          throw WalletError.migrationFailed('Migrated account count mismatch');
        }
      } catch (error) {
        await this.storage.delete(
          V2_VAULT_NAMESPACE,
          prepared.manifest.revision,
        );
        throw error;
      }

      await this.finishLegacyDeletion(browserStorage);
      this.emit('migration:completed', prepared.manifest.vaultId, {
        resumed: false,
        accountCount: accounts.length,
      });
      return {
        vaultId: prepared.manifest.vaultId,
        revision: prepared.manifest.revision,
        accountCount: accounts.length,
      };
    } catch (error) {
      this.emit('migration:failed');
      if (error instanceof WalletError) throw error;
      throw WalletError.migrationFailed('Legacy migration failed');
    }
  }

  private parseLegacyWallet(raw: string): LegacyWalletState {
    let parsed: unknown;
    try {
      parsed = JSON.parse(raw);
    } catch {
      throw WalletError.migrationFailed('Legacy wallet JSON is malformed');
    }
    if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) {
      throw WalletError.migrationFailed('Legacy wallet record is malformed');
    }
    const record = parsed as Record<string, unknown>;
    if (
      typeof record.encryptedMnemonic !== 'string'
      || !Array.isArray(record.accounts)
      || !Number.isInteger(record.nextAccountIndex)
      || (record.nextAccountIndex as number) < 0
      || record.accounts.length > 256
    ) {
      throw WalletError.migrationFailed('Legacy wallet fields are malformed');
    }

    const accounts = record.accounts.map((value, index) => {
      if (!value || typeof value !== 'object' || Array.isArray(value)) {
        throw WalletError.migrationFailed(`Legacy account ${index} is malformed`);
      }
      const account = value as Record<string, unknown>;
      if (
        typeof account.address !== 'string'
        || typeof account.privateKey !== 'string'
        || typeof account.name !== 'string'
        || typeof account.derivationPath !== 'string'
        || !Number.isInteger(account.accountIndex)
      ) {
        throw WalletError.migrationFailed(`Legacy account ${index} is malformed`);
      }
      return account as unknown as LegacyAccount;
    });

    return {
      encryptedMnemonic: record.encryptedMnemonic,
      accounts,
      nextAccountIndex: record.nextAccountIndex as number,
    };
  }

  private convertAccounts(
    pin: string,
    mnemonic: string,
    accounts: LegacyAccount[],
  ): VaultAccountV2[] {
    const root = HDKey.fromMasterSeed(bip39.mnemonicToSeedSync(mnemonic));
    const ids = new Set<string>();
    const addresses = new Set<string>();

    return accounts.map((legacy, index) => {
      let address: string;
      try {
        address = ethers.getAddress(legacy.address);
      } catch {
        throw WalletError.migrationFailed(`Legacy account ${index} address is invalid`);
      }
      const privateKey = this.reader.decryptWalletSecret(pin, legacy.privateKey);
      let privateKeyWallet: ethers.Wallet;
      try {
        privateKeyWallet = new ethers.Wallet(privateKey);
      } catch {
        throw WalletError.migrationFailed(`Legacy account ${index} key is invalid`);
      }
      if (privateKeyWallet.address !== address) {
        throw WalletError.migrationFailed(
          `Legacy account ${index} key does not match address`,
        );
      }
      const addressKey = address.toLowerCase();
      if (addresses.has(addressKey)) {
        throw WalletError.migrationFailed('Legacy wallet has duplicate addresses');
      }
      addresses.add(addressKey);

      const id = this.generateUuid();
      if (ids.has(id)) throw WalletError.migrationFailed('Account id collision');
      ids.add(id);

      const name = legacy.name.trim();
      if (!name || name.length > 128 || /[\u0000-\u001f\u007f]/.test(name)) {
        throw WalletError.migrationFailed(`Legacy account ${index} name is invalid`);
      }

      if (legacy.derivationPath === 'imported' || legacy.accountIndex === -1) {
        return {
          id,
          kind: 'imported',
          address,
          name,
          privateKey: privateKeyWallet.privateKey,
        };
      }

      const expectedPath = `${BASE_PATH}/${legacy.accountIndex}`;
      if (
        legacy.accountIndex < 0
        || legacy.derivationPath !== expectedPath
      ) {
        throw WalletError.migrationFailed(
          `Legacy account ${index} derivation metadata is invalid`,
        );
      }
      const derived = root.derive(expectedPath);
      if (
        !derived.privateKey
        || new ethers.Wallet(ethers.hexlify(derived.privateKey)).address !== address
        || ethers.hexlify(derived.privateKey).toLowerCase()
          !== privateKeyWallet.privateKey.toLowerCase()
      ) {
        throw WalletError.migrationFailed(
          `Legacy account ${index} does not match mnemonic derivation`,
        );
      }
      return {
        id,
        kind: 'derived',
        address,
        name,
        derivationPath: expectedPath,
        accountIndex: legacy.accountIndex,
      };
    });
  }

  private async finishLegacyDeletion(storage: Storage): Promise<void> {
    for (const key of LEGACY_SESSION_KEYS) storage.removeItem(key);
    for (const key of LEGACY_BIOMETRIC_KEYS) storage.removeItem(key);
    storage.removeItem(LEGACY_PIN_KEY);
    storage.removeItem(LEGACY_ACTIVE_ACCOUNT_KEY);
    storage.removeItem(LEGACY_WALLET_KEY);
  }

  private assertEquivalentMigration(
    mnemonic: string,
    legacyAccounts: readonly VaultAccountV2[],
    activeAddress: string | null,
    nextAccountIndex: number,
    payload: WalletPayloadV2,
  ): void {
    if (
      payload.mnemonic !== mnemonic
      || payload.nextAccountIndex !== nextAccountIndex
      || payload.accounts.length !== legacyAccounts.length
    ) {
      throw WalletError.migrationFailed(
        'Existing v2 vault does not match the legacy wallet',
      );
    }
    const existingByAddress = new Map(
      payload.accounts.map(account => [account.address, account]),
    );
    for (const legacy of legacyAccounts) {
      const existing = existingByAddress.get(legacy.address);
      if (
        !existing
        || existing.kind !== legacy.kind
        || existing.name !== legacy.name
      ) {
        throw WalletError.migrationFailed(
          'Existing v2 accounts do not match the legacy wallet',
        );
      }
      if (legacy.kind === 'derived') {
        if (
          existing.kind !== 'derived'
          || existing.derivationPath !== legacy.derivationPath
          || existing.accountIndex !== legacy.accountIndex
        ) {
          throw WalletError.migrationFailed(
            'Existing v2 derivation does not match the legacy wallet',
          );
        }
      } else if (
        existing.kind !== 'imported'
        || existing.privateKey.toLowerCase() !== legacy.privateKey.toLowerCase()
      ) {
        throw WalletError.migrationFailed(
          'Existing v2 imported account does not match the legacy wallet',
        );
      }
    }
    const active = payload.accounts.find(
      account => account.id === payload.activeAccountId,
    );
    if ((active?.address ?? null) !== activeAddress) {
      throw WalletError.migrationFailed(
        'Existing v2 active account does not match the legacy wallet',
      );
    }
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

  private getLegacyStorage(): Storage {
    if (typeof globalThis.localStorage === 'undefined') {
      throw WalletError.storageUnavailable(
        new Error('Legacy localStorage is unavailable'),
      );
    }
    return globalThis.localStorage;
  }

  private emit(
    kind: SecurityEventKind,
    vaultId?: string,
    metadata?: Record<string, string | number | boolean>,
  ): void {
    this.events?.emit({
      kind,
      timestamp: this.timer.now(),
      vaultId,
      metadata,
    });
  }
}
