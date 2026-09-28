import { IndexedDBStorageAdapter } from '../../src/lib/wallet/v2/adapters/indexeddb-storage-adapter';
import { WebCryptoAdapter } from '../../src/lib/wallet/v2/adapters/web-crypto-adapter';
import { BrowserTimerAdapter } from '../../src/lib/wallet/v2/adapters/browser-timer-adapter';
import { SecurityEventBus } from '../../src/lib/wallet/v2/adapters/security-event-bus';
import { AuthenticationManager } from '../../src/lib/wallet/v2/core/authentication-manager';
import {
  SESSION_NAMESPACE,
  SessionManager,
} from '../../src/lib/wallet/v2/core/session-manager';
import { VaultManager } from '../../src/lib/wallet/v2/core/vault-manager';
import { LegacyMigrationManager } from '../../src/lib/wallet/v2/core/legacy-migration-manager';
import { PaxeerWallet } from '../../src/lib/wallet/PaxeerWallet';
import CryptoJS from 'crypto-js';
import { HDKey } from '@scure/bip32';
import * as bip39 from '@scure/bip39';
import { wordlist } from '@scure/bip39/wordlists/english';
import { ethers } from 'ethers';
import type {
  AuthThrottleRecord,
  StorageRecord,
} from '../../src/lib/wallet/v2/types/storage';
import type { VaultManifestV2 } from '../../src/lib/wallet/v2/types/vault';

declare global {
  interface Window {
    runStorageAcceptance(): Promise<Record<string, boolean>>;
    runAuthenticationAcceptance(): Promise<Record<string, boolean>>;
    runLegacyMigrationAcceptance(): Promise<Record<string, boolean>>;
    runProductionAcceptance(): Promise<Record<string, boolean>>;
  }
}

const DB_NAME = 'paxport-wallet-v2';
const VAULT_NAMESPACE = 'paxport-wallet-v2';
const THROTTLE_NAMESPACE = 'paxport-wallet-auth-throttle-v2';

function base64Url(length: number): string {
  const bytes = crypto.getRandomValues(new Uint8Array(length));
  let binary = '';
  for (const byte of bytes) binary += String.fromCharCode(byte);
  return btoa(binary).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '');
}

function manifest(vaultId: string, revision: number): VaultManifestV2 {
  const now = Date.now();
  return {
    schema: 2,
    vaultId,
    revision,
    createdAt: now,
    updatedAt: now,
    keySlots: [{
      version: 1,
      id: crypto.randomUUID(),
      type: 'password',
      kdf: {
        algorithm: 'PBKDF2-HMAC-SHA-256',
        salt: base64Url(16),
        iterations: 600_000,
      },
      wrappedVaultKey: {
        version: 1,
        algorithm: 'AES-256-GCM',
        iv: base64Url(12),
        ciphertext: base64Url(48),
      },
      createdAt: now,
    }],
    verifier: {
      version: 1,
      algorithm: 'AES-256-GCM',
      iv: base64Url(12),
      ciphertext: base64Url(32),
    },
    payload: {
      version: 1,
      algorithm: 'AES-256-GCM',
      iv: base64Url(12),
      ciphertext: base64Url(64),
    },
  };
}

function record(vaultId: string, revision: number): StorageRecord {
  return { vaultId, revision, manifest: manifest(vaultId, revision) };
}

async function deleteDatabase(): Promise<void> {
  await new Promise<void>((resolve, reject) => {
    const request = indexedDB.deleteDatabase(DB_NAME);
    request.onsuccess = () => resolve();
    request.onerror = () => reject(request.error);
    request.onblocked = () => reject(new Error('database deletion blocked'));
  });
}

async function readAllBrowserStorage(): Promise<{
  rows: unknown[];
  local: Record<string, string>;
  session: Record<string, string>;
}> {
  const rows = await new Promise<unknown[]>((resolve, reject) => {
    const request = indexedDB.open(DB_NAME, 2);
    request.onsuccess = () => {
      const db = request.result;
      const tx = db.transaction('vaults', 'readonly');
      const all = tx.objectStore('vaults').getAll();
      all.onsuccess = () => resolve(all.result);
      all.onerror = () => reject(all.error);
      tx.oncomplete = () => db.close();
    };
    request.onerror = () => reject(request.error);
  });
  return {
    rows,
    local: { ...localStorage },
    session: { ...sessionStorage },
  };
}

async function replaceRawVaultRow(row: unknown): Promise<void> {
  await new Promise<void>((resolve, reject) => {
    const request = indexedDB.open(DB_NAME, 2);
    request.onsuccess = () => {
      const db = request.result;
      const tx = db.transaction('vaults', 'readwrite');
      tx.objectStore('vaults').put(row);
      tx.oncomplete = () => {
        db.close();
        resolve();
      };
      tx.onabort = () => {
        db.close();
        reject(tx.error);
      };
    };
    request.onerror = () => reject(request.error);
  });
}

window.runStorageAcceptance = async () => {
  await deleteDatabase();
  const first = new IndexedDBStorageAdapter();
  const second = new IndexedDBStorageAdapter();
  await first.initialize();
  await second.initialize();

  const result: Record<string, boolean> = {};
  const vaultId = crypto.randomUUID();

  const created = await first.write(VAULT_NAMESPACE, record(vaultId, 1), null);
  result.createCommitted = created.ok && (await first.read(VAULT_NAMESPACE))?.revision === 1;

  const duplicateCreate = await first.write(
    VAULT_NAMESPACE,
    record(vaultId, 2),
    null,
  );
  result.createOnlyConflict =
    !duplicateCreate.ok
    && duplicateCreate.error === 'WRITE_CONFLICT'
    && (await first.read(VAULT_NAMESPACE))?.revision === 1;

  const updated = await first.write(VAULT_NAMESPACE, record(vaultId, 2), 1);
  result.revisionUpdateCommitted =
    updated.ok && (await first.read(VAULT_NAMESPACE))?.revision === 2;

  const [writerA, writerB] = await Promise.all([
    first.write(VAULT_NAMESPACE, record(vaultId, 3), 2),
    second.write(VAULT_NAMESPACE, record(vaultId, 3), 2),
  ]);
  const concurrent = [writerA, writerB];
  result.concurrentConflict =
    concurrent.filter(item => item.ok).length === 1
    && concurrent.filter(item => !item.ok && item.error === 'WRITE_CONFLICT').length === 1
    && (await first.read(VAULT_NAMESPACE))?.revision === 3;

  const stale = await first.write(VAULT_NAMESPACE, record(vaultId, 4), 1);
  result.abortedConflictPreserved =
    !stale.ok
    && stale.error === 'WRITE_CONFLICT'
    && (await first.read(VAULT_NAMESPACE))?.revision === 3;

  const throttle: AuthThrottleRecord = {
    version: 1,
    vaultId,
    revision: 1,
    failedAttempts: 5,
    lockedUntil: Date.now() + 30_000,
    updatedAt: Date.now(),
  };
  const throttleWrite = await first.writeAuthThrottle(
    THROTTLE_NAMESPACE,
    throttle,
    null,
  );
  result.throttleMetadataSeparated =
    throttleWrite.ok
    && (await first.readAuthThrottle(THROTTLE_NAMESPACE))?.failedAttempts === 5
    && (await first.read(VAULT_NAMESPACE))?.revision === 3;

  const notification = new Promise<boolean>((resolve) => {
    const timeout = setTimeout(() => resolve(false), 2_000);
    const unsubscribe = second.subscribe((event) => {
      if (event.kind !== 'lock' || event.vaultId !== vaultId) return;
      clearTimeout(timeout);
      unsubscribe();
      resolve(true);
    });
  });
  first.notify('lock', vaultId);
  result.crossContextNotification = await notification;

  const wrongDelete = await first.delete(VAULT_NAMESPACE, 2);
  result.deleteConflictPreserved =
    !wrongDelete.ok
    && wrongDelete.error === 'WRITE_CONFLICT'
    && (await first.read(VAULT_NAMESPACE))?.revision === 3;

  const deleted = await first.delete(VAULT_NAMESPACE, 3);
  result.deleteCommitted =
    deleted.ok
    && deleted.deleted
    && (await first.read(VAULT_NAMESPACE)) === null;

  await first.close();
  await second.close();
  await deleteDatabase();

  const indexedDbDescriptor = Object.getOwnPropertyDescriptor(
    globalThis,
    'indexedDB',
  );
  try {
    Object.defineProperty(globalThis, 'indexedDB', {
      configurable: true,
      value: undefined,
    });
    try {
      IndexedDBStorageAdapter.assertReady();
      result.unavailableStorageFailsClosed = false;
    } catch {
      result.unavailableStorageFailsClosed = true;
    }
  } finally {
    if (indexedDbDescriptor) {
      Object.defineProperty(globalThis, 'indexedDB', indexedDbDescriptor);
    }
  }
  return result;
};

window.runAuthenticationAcceptance = async () => {
  await deleteDatabase();
  localStorage.clear();

  const storage = new IndexedDBStorageAdapter();
  await storage.initialize();
  const cryptoAdapter = new WebCryptoAdapter();
  const timer = new BrowserTimerAdapter();
  const events = new SecurityEventBus();
  const vaults = new VaultManager(cryptoAdapter, storage);
  const sessions = new SessionManager(storage, events, timer);
  const auth = new AuthenticationManager(
    vaults,
    sessions,
    storage,
    events,
    timer,
    { inactivityMs: 5_000 },
  );
  const password = '482915';
  const mnemonic = bip39.entropyToMnemonic(new Uint8Array(16), wordlist);
  const created = await vaults.createVault({ password, mnemonic });
  const result: Record<string, boolean> = {};

  for (let i = 0; i < 4; i++) {
    try {
      await auth.unlock('739204');
    } catch {
      // The real failed-attempt record is asserted below.
    }
  }
  const beforeSuccess = await auth.getThrottleStatus(created.manifest.vaultId);
  result.failedAttemptsPersist =
    beforeSuccess.failedAttempts === 4 && beforeSuccess.retryAfterMs === 0;

  await auth.unlock(password);
  const afterSuccess = await auth.getThrottleStatus(created.manifest.vaultId);
  result.successClearsFailures =
    sessions.isUnlocked()
    && afterSuccess.failedAttempts === 0
    && afterSuccess.retryAfterMs === 0;

  const persistedRows = await new Promise<unknown[]>((resolve, reject) => {
    const request = indexedDB.open(DB_NAME, 2);
    request.onsuccess = () => {
      const db = request.result;
      const tx = db.transaction('vaults', 'readonly');
      const rows = tx.objectStore('vaults').getAll();
      rows.onsuccess = () => {
        db.close();
        resolve(rows.result);
      };
      rows.onerror = () => reject(rows.error);
    };
    request.onerror = () => reject(request.error);
  });
  const storageText = JSON.stringify({
    indexedDb: persistedRows,
    localStorage: { ...localStorage },
    sessionStorage: { ...sessionStorage },
  });
  const persistedSession = await storage.readSession(SESSION_NAMESPACE);
  result.noPersistedPlaintextOrExtractableKey =
    !storageText.includes(password)
    && !storageText.includes(mnemonic)
    && !storageText.includes('CryptoKey')
    && persistedSession !== null
    && persistedSession.vaultKey.extractable === false;

  const reloaded = new SessionManager(storage, events, timer);
  if (persistedSession) {
    await vaults.decryptPayload(persistedSession.vaultKey, created.manifest);
    await reloaded.restore(persistedSession, created.manifest);
  }
  result.reloadPreservesValidSession =
    reloaded.isUnlocked() && sessions.isUnlocked();

  await sessions.lock('manual');
  for (let i = 0; i < 5; i++) {
    try {
      await auth.unlock('739204');
    } catch {
      // The sixth attempt must be rejected before another KDF run.
    }
  }
  const lockedStatus = await auth.getThrottleStatus(created.manifest.vaultId);
  let throttled = false;
  try {
    await auth.unlock(password);
  } catch (error) {
    throttled =
      error instanceof Error
      && 'code' in error
      && error.code === 'AUTHENTICATION_THROTTLED';
  }
  result.fifthFailureStartsBackoff =
    lockedStatus.failedAttempts === 5
    && lockedStatus.retryAfterMs > 0
    && throttled;

  const throttle = await storage.readAuthThrottle(THROTTLE_NAMESPACE);
  if (throttle) {
    await storage.deleteAuthThrottle(THROTTLE_NAMESPACE, throttle.revision);
  }
  await auth.unlock(password);

  const peerStorage = new IndexedDBStorageAdapter();
  await peerStorage.initialize();
  const peerVaults = new VaultManager(cryptoAdapter, peerStorage);
  const peerUnlock = await peerVaults.unlockVault(password);
  const peer = new SessionManager(peerStorage, events, timer);
  await peer.unlock(
    peerUnlock.vaultKey,
    peerUnlock.manifest,
    5_000,
    peerUnlock.authenticatedSlotId,
  );
  await sessions.lock('manual');
  await new Promise(resolve => setTimeout(resolve, 100));
  result.crossContextSessionRevoked = !sessions.isUnlocked() && !peer.isUnlocked();

  await auth.unlock(password);
  globalThis.dispatchEvent(new PageTransitionEvent('pagehide'));
  result.pageHidePreservesSession = sessions.isUnlocked();

  await peer.destroy();
  await reloaded.destroy();
  await peerStorage.close();
  await storage.close();
  await deleteDatabase();
  return result;
};

window.runLegacyMigrationAcceptance = async () => {
  await deleteDatabase();
  localStorage.clear();

  const pin = '123456';
  const password = '482915';
  const mnemonic = bip39.entropyToMnemonic(new Uint8Array(16), wordlist);
  const root = HDKey.fromMasterSeed(bip39.mnemonicToSeedSync(mnemonic));
  const derived = root.derive("m/44'/60'/0'/0/0");
  if (!derived.privateKey) throw new Error('fixture derivation failed');
  const derivedWallet = new ethers.Wallet(ethers.hexlify(derived.privateKey));
  const importedWallet = new ethers.Wallet(`0x${'ab'.repeat(32)}`);
  const walletKey = CryptoJS.PBKDF2(pin, 'paxeer_wallet_salt_2024', {
    keySize: 256 / 32,
    iterations: 10_000,
  }).toString();
  const encrypt = (plaintext: string) =>
    CryptoJS.AES.encrypt(plaintext, walletKey).toString();
  const salt = CryptoJS.lib.WordArray.random(16).toString();
  const pinRecord = JSON.stringify({
    salt,
    hash: CryptoJS.PBKDF2(pin, salt, {
      keySize: 512 / 32,
      iterations: 10_000,
    }).toString(),
  });
  const walletRecord = JSON.stringify({
    encryptedMnemonic: encrypt(mnemonic),
    accounts: [
      {
        address: derivedWallet.address,
        privateKey: encrypt(derivedWallet.privateKey),
        name: 'Account 1',
        derivationPath: "m/44'/60'/0'/0/0",
        accountIndex: 0,
      },
      {
        address: importedWallet.address,
        privateKey: encrypt(importedWallet.privateKey),
        name: 'Imported',
        derivationPath: 'imported',
        accountIndex: -1,
      },
    ],
    nextAccountIndex: 1,
    salt: CryptoJS.lib.WordArray.random(32).toString(),
    timestamp: Date.now(),
  });
  const installLegacy = () => {
    localStorage.setItem('paxeer_pin_hash', pinRecord);
    localStorage.setItem('paxeer_wallet_state', walletRecord);
    localStorage.setItem('paxeer_active_account', importedWallet.address);
    localStorage.setItem('paxeer_session', '{"legacy":true}');
    localStorage.setItem('paxeer_session_data', '{"key":"must-disappear"}');
  };
  installLegacy();

  const storage = new IndexedDBStorageAdapter();
  await storage.initialize();
  const cryptoAdapter = new WebCryptoAdapter();
  const timer = new BrowserTimerAdapter();
  const events = new SecurityEventBus();
  const vaults = new VaultManager(cryptoAdapter, storage);
  const migration = new LegacyMigrationManager(
    cryptoAdapter,
    storage,
    vaults,
    events,
    timer,
  );
  const result: Record<string, boolean> = {};

  try {
    await migration.migrate('000000', password);
  } catch {
    // Wrong-PIN preservation is asserted below.
  }
  result.wrongPinPreservesLegacy =
    localStorage.getItem('paxeer_wallet_state') === walletRecord
    && localStorage.getItem('paxeer_pin_hash') === pinRecord
    && (await storage.read(VAULT_NAMESPACE)) === null;

  const corruptWalletRecord = JSON.stringify({
    ...JSON.parse(walletRecord),
    encryptedMnemonic: `${encrypt(mnemonic).slice(0, -4)}AAAA`,
  });
  localStorage.setItem('paxeer_wallet_state', corruptWalletRecord);
  try {
    await migration.migrate(pin, password);
  } catch {
    // Correct-PIN corrupt-ciphertext preservation is asserted below.
  }
  result.corruptCiphertextPreservesLegacy =
    localStorage.getItem('paxeer_wallet_state') === corruptWalletRecord
    && localStorage.getItem('paxeer_pin_hash') === pinRecord
    && (await storage.read(VAULT_NAMESPACE)) === null;
  localStorage.setItem('paxeer_wallet_state', walletRecord);

  const migrated = await migration.migrate(pin, password);
  const unlocked = await vaults.unlockVault(password);
  const imported = unlocked.payload.accounts.find(
    account => account.kind === 'imported',
  );
  const hd = unlocked.payload.accounts.find(account => account.kind === 'derived');
  result.mixedAccountsMigrated =
    migrated.accountCount === 2
    && imported?.address === importedWallet.address
    && imported?.privateKey === importedWallet.privateKey
    && hd?.address === derivedWallet.address
    && hd !== undefined
    && !('privateKey' in hd)
    && unlocked.payload.activeAccountId === imported?.id;
  result.legacyDeletedAfterVerifiedCommit =
    localStorage.getItem('paxeer_wallet_state') === null
    && localStorage.getItem('paxeer_pin_hash') === null
    && localStorage.getItem('paxeer_session') === null
    && localStorage.getItem('paxeer_session_data') === null;

  installLegacy();
  const resumed = await migration.migrate(pin, password);
  result.alreadyMigratedRecovery =
    resumed.vaultId === migrated.vaultId
    && localStorage.getItem('paxeer_wallet_state') === null;

  const mismatchedLegacy = JSON.stringify({
    ...JSON.parse(walletRecord),
    nextAccountIndex: 2,
  });
  localStorage.setItem('paxeer_pin_hash', pinRecord);
  localStorage.setItem('paxeer_wallet_state', mismatchedLegacy);
  localStorage.setItem('paxeer_active_account', importedWallet.address);
  try {
    await migration.migrate(pin, password);
  } catch {
    // A different legacy wallet must not be treated as an interrupted commit.
  }
  result.mismatchedRecoveryPreservesLegacy =
    localStorage.getItem('paxeer_wallet_state') === mismatchedLegacy
    && (await storage.read(VAULT_NAMESPACE))?.vaultId === migrated.vaultId;
  localStorage.clear();

  const persisted = JSON.stringify(await storage.read(VAULT_NAMESPACE));
  result.migratedStorageContainsNoPlaintext =
    !persisted.includes(mnemonic)
    && !persisted.includes(derivedWallet.privateKey)
    && !persisted.includes(importedWallet.privateKey)
    && !persisted.includes(pin)
    && !persisted.includes(password);

  await storage.close();
  await deleteDatabase();
  localStorage.clear();
  return result;
};

window.runProductionAcceptance = async () => {
  await deleteDatabase();
  localStorage.clear();
  sessionStorage.clear();

  const config = {
    rpcUrl: 'http://127.0.0.1:8545',
    chainId: 125,
    sessionTimeoutMs: 10_000,
  };
  const password = '482915';
  const importedPrivateKey = `0x${'cd'.repeat(32)}`;
  const challenge = 'paxport-wallet-production-acceptance-v2';
  const result: Record<string, boolean> = {};
  const snapshots: string[] = [];
  const primary = new PaxeerWallet(config);
  const created = await primary.createNewWallet(password, 'Primary');
  snapshots.push(JSON.stringify(await readAllBrowserStorage()));

  const expectedDerived = ethers.HDNodeWallet.fromPhrase(
    created.mnemonic,
    undefined,
    "m/44'/60'/0'/0/0",
  );
  result.productionCreateAndDerive =
    created.account.address === expectedDerived.address
    && !('privateKey' in created.account);

  const imported = await primary.importPrivateKey(
    importedPrivateKey,
    'Imported',
  );
  result.facadeEncapsulation = [
    'wallet',
    'walletCore',
    'tx',
    'transactions',
    'session',
    'sessions',
    'vaults',
    'storage',
    'crypto',
  ].every(property => !(property in primary));
  let exportWithoutStepUpRejected = false;
  try {
    await primary.exportPrivateKey(imported.address);
  } catch {
    exportWithoutStepUpRejected = true;
  }
  await primary.reauthenticate(password);
  const exportedMnemonic = await primary.exportMnemonic();
  const exportedPrivateKey = await primary.exportPrivateKey(imported.address);
  const snapshot = await primary.getSnapshot();
  result.facadeSnapshotAndStepUp =
    exportWithoutStepUpRejected
    && exportedMnemonic === created.mnemonic
    && exportedPrivateKey === importedPrivateKey
    && !snapshot.isLocked
    && snapshot.accounts.length === 2
    && snapshot.accounts.every(account => !('privateKey' in account))
    && snapshot.activeAccount?.address === created.account.address;
  const signer = primary.getSigner(imported.address);
  const signature = await signer.signMessage(challenge);
  const secondSignature = await signer.signMessage(challenge);
  result.keylessDeterministicSigning =
    signature === secondSignature
    && ethers.verifyMessage(challenge, signature) === imported.address
    && signer.constructor.name === 'VaultSigner'
    && !('privateKey' in signer);
  snapshots.push(JSON.stringify(await readAllBrowserStorage()));

  const reloaded = new PaxeerWallet(config);
  result.reloadPreservesValidSession =
    (await reloaded.isReady())
    && reloaded.isSessionValid();

  await primary.lock();
  await new Promise(resolve => setTimeout(resolve, 100));
  let lockedSignerRejected = false;
  try {
    await signer.signMessage(challenge);
  } catch {
    lockedSignerRejected = true;
  }
  result.lockRevokesExistingSigner = lockedSignerRejected;
  snapshots.push(JSON.stringify(await readAllBrowserStorage()));

  await reloaded.unlock(password);
  snapshots.push(JSON.stringify(await readAllBrowserStorage()));

  const peer = new PaxeerWallet(config);
  await peer.unlock(password);
  await peer.renameAccount(imported.address, 'Imported Peer');
  await new Promise(resolve => setTimeout(resolve, 100));
  result.revisionChangeRevokesStaleContext =
    !reloaded.isSessionValid()
    && peer.isSessionValid();

  await reloaded.unlock(password);
  const beforeTamper = await reloaded.getAccounts();
  await reloaded.lock();
  await new Promise(resolve => setTimeout(resolve, 100));
  result.productionCrossContextLock =
    !reloaded.isSessionValid()
    && !peer.isSessionValid();

  const rawBeforeTamper = await readAllBrowserStorage();
  const original = rawBeforeTamper.rows.find((value) =>
    (value as { namespace?: string }).namespace === VAULT_NAMESPACE,
  ) as {
    manifest: {
      payload: { ciphertext: string };
    };
  };
  const tampered = structuredClone(original);
  const ciphertext = tampered.manifest.payload.ciphertext;
  tampered.manifest.payload.ciphertext =
    `${ciphertext[0] === 'A' ? 'B' : 'A'}${ciphertext.slice(1)}`;
  await replaceRawVaultRow(tampered);

  const tamperedWallet = new PaxeerWallet(config);
  let tamperRejected = false;
  try {
    await tamperedWallet.unlock(password);
  } catch {
    tamperRejected = true;
  }
  result.tamperedVaultRejected = tamperRejected;
  await tamperedWallet.destroy();
  await replaceRawVaultRow(original);

  const finalWallet = new PaxeerWallet(config);
  await finalWallet.unlock(password);
  const reopened = await finalWallet.getAccounts();
  result.reopensUnchangedWallet =
    JSON.stringify(reopened) === JSON.stringify(beforeTamper);
  snapshots.push(JSON.stringify(await readAllBrowserStorage()));

  result.lifecycleStorageForensics = snapshots.every((snapshot) =>
    !snapshot.includes(created.mnemonic)
    && !snapshot.includes(importedPrivateKey)
    && !snapshot.includes(password)
    && !snapshot.includes('CryptoKey'),
  );

  await finalWallet.reset();
  const afterReset = await readAllBrowserStorage();
  result.resetCleanup =
    afterReset.rows.length === 0
    && Object.keys(afterReset.local).length === 0
    && Object.keys(afterReset.session).length === 0;

  await primary.destroy();
  await reloaded.destroy();
  await peer.destroy();
  await finalWallet.destroy();
  await deleteDatabase();
  return result;
};
