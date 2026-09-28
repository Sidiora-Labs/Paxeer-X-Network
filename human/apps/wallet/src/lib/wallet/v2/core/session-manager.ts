import type { StoragePort } from '../ports/storage-port';
import type { EventPort } from '../ports/event-port';
import type { TimerPort } from '../ports/timer-port';
import type { SecurityEventKind } from '../types/events';
import type { PersistedSessionRecord } from '../types/storage';
import type { VaultManifestV2 } from '../types/vault';
import { WalletError } from '../types/errors';

export const SESSION_NAMESPACE = 'paxport-wallet-session-v1';

export interface SessionCreateParams {
  vaultKey: CryptoKey;
  manifest: VaultManifestV2;
  inactivityMs: number;
}

export interface SessionStatus {
  unlocked: boolean;
  remainingMs: number | null;
}

interface SessionState {
  vaultKey: CryptoKey;
  manifest: VaultManifestV2;
  inactivityDeadline: number;
  inactivityMs: number;
  lastStepUpAt: number | null;
  authenticatedSlotId: string | null;
}

export class SessionManager {
  private session: SessionState | null = null;
  private timeoutHandle: unknown = null;
  private storageUnsubscribe: (() => void) | null = null;
  private persistenceQueue: Promise<void> = Promise.resolve();

  constructor(
    private readonly storage: StoragePort | null,
    private readonly events: EventPort | null,
    private readonly timer: TimerPort,
  ) {}

  isUnlocked(): boolean {
    this.expireIfNeeded();
    return this.session !== null;
  }

  requireVaultKey(): CryptoKey {
    this.expireIfNeeded();
    if (!this.session) throw WalletError.locked();
    return this.session.vaultKey;
  }

  getManifest(): VaultManifestV2 | null {
    this.expireIfNeeded();
    return this.session?.manifest ?? null;
  }

  updateManifest(manifest: VaultManifestV2): void {
    this.expireIfNeeded();
    if (!this.session) throw WalletError.locked();
    if (manifest.vaultId !== this.session.manifest.vaultId) {
      throw WalletError.corruptVault('Session manifest changed vault identity');
    }
    if (manifest.revision < this.session.manifest.revision) {
      throw WalletError.writeConflict();
    }
    this.session.manifest = manifest;
  }

  getAuthenticatedSlotId(): string | null {
    this.expireIfNeeded();
    return this.session?.authenticatedSlotId ?? null;
  }

  getStatus(): SessionStatus {
    this.expireIfNeeded();
    if (!this.session) return { unlocked: false, remainingMs: null };
    return {
      unlocked: true,
      remainingMs: Math.max(0, this.session.inactivityDeadline - this.now()),
    };
  }

  touchSensitiveActivity(): void {
    this.expireIfNeeded();
    if (!this.session) throw WalletError.locked();
    this.session.inactivityDeadline = this.now() + this.session.inactivityMs;
    this.scheduleTimeout();
    void this.persistCurrentSession().catch(() => {
      void this.lockInternal('session_persistence_failed', true, true);
    });
  }

  recordStepUp(): void {
    this.expireIfNeeded();
    if (!this.session) throw WalletError.locked();
    this.session.lastStepUpAt = this.now();
    this.emit('auth:step_up', this.session.manifest.vaultId);
  }

  isStepUpValid(maxAgeMs: number = 60_000): boolean {
    this.expireIfNeeded();
    if (!this.session || this.session.lastStepUpAt === null) return false;
    return this.now() - this.session.lastStepUpAt < maxAgeMs;
  }

  async unlock(
    vaultKey: CryptoKey,
    manifest: VaultManifestV2,
    inactivityMs: number,
    authenticatedSlotId: string | null = null,
  ): Promise<void> {
    if (!Number.isFinite(inactivityMs) || inactivityMs < 1_000) {
      throw WalletError.invalidInput('inactivityMs', 'must be at least 1000ms');
    }
    if (vaultKey.extractable) {
      throw WalletError.corruptVault('Unlocked vault key must be non-extractable');
    }

    await this.lockInternal('replaced', false, false);
    this.session = {
      vaultKey,
      manifest,
      inactivityDeadline: this.now() + inactivityMs,
      inactivityMs,
      lastStepUpAt: null,
      authenticatedSlotId,
    };

    try {
      this.subscribeToStorage();
      this.scheduleTimeout();
      await this.persistCurrentSession();
    } catch (error) {
      await this.lockInternal('initialization_failed', false, true);
      throw WalletError.storageUnavailable(
        error instanceof Error ? error : new Error('Session initialization failed'),
      );
    }

    this.emit('wallet:unlocked', manifest.vaultId);
  }

  async restore(
    record: PersistedSessionRecord,
    manifest: VaultManifestV2,
  ): Promise<boolean> {
    if (
      record.vaultId !== manifest.vaultId
      || record.inactivityDeadline <= this.now()
    ) {
      await this.deletePersistedSession();
      return false;
    }
    if (record.vaultKey.extractable) {
      await this.deletePersistedSession();
      return false;
    }

    await this.lockInternal('replaced', false, false);
    this.session = {
      vaultKey: record.vaultKey,
      manifest,
      inactivityDeadline: record.inactivityDeadline,
      inactivityMs: record.inactivityMs,
      lastStepUpAt: null,
      authenticatedSlotId: null,
    };

    try {
      this.subscribeToStorage();
      this.scheduleTimeout();
    } catch (error) {
      await this.lockInternal('initialization_failed', false, true);
      throw WalletError.storageUnavailable(
        error instanceof Error ? error : new Error('Session restoration failed'),
      );
    }

    this.emit('wallet:unlocked', manifest.vaultId, { restored: true });
    return true;
  }

  async createSession(
    params: SessionCreateParams,
    authenticatedSlotId: string | null = null,
  ): Promise<void> {
    await this.unlock(
      params.vaultKey,
      params.manifest,
      params.inactivityMs,
      authenticatedSlotId,
    );
  }

  lock(reason: string = 'manual'): Promise<void> {
    return this.lockInternal(
      reason,
      reason !== 'cross_context' && reason !== 'replaced',
      reason !== 'replaced',
    );
  }

  requireUnlocked(): void {
    this.expireIfNeeded();
    if (!this.session) throw WalletError.locked();
  }

  onCrossContextLock(): void {
    void this.lockInternal('cross_context', false, true);
  }

  notifyRevisionChange(revision: number): void {
    this.expireIfNeeded();
    if (!this.session) throw WalletError.locked();
    try {
      this.storage?.notify(
        'revision_change',
        this.session.manifest.vaultId,
        revision,
      );
    } catch (error) {
      void this.lockInternal('coordination_failure', false, true);
      throw WalletError.storageUnavailable(
        error instanceof Error
          ? error
          : new Error('Revision notification failed'),
      );
    }
  }

  destroy(): Promise<void> {
    return this.lockInternal('destroy', false, false);
  }

  private now(): number {
    return this.timer.now();
  }

  private expireIfNeeded(): void {
    if (this.session && this.now() >= this.session.inactivityDeadline) {
      void this.lockInternal('timeout', true, true);
    }
  }

  private scheduleTimeout(): void {
    this.clearTimeout();
    if (!this.session) return;
    const delay = Math.max(0, this.session.inactivityDeadline - this.now());
    this.timeoutHandle = this.timer.setTimeout(() => {
      void this.lockInternal('timeout', true, true);
    }, delay);
  }

  private clearTimeout(): void {
    if (this.timeoutHandle === null) return;
    this.timer.clearTimeout(this.timeoutHandle);
    this.timeoutHandle = null;
  }

  private subscribeToStorage(): void {
    if (!this.storage || !this.session) return;
    const vaultId = this.session.manifest.vaultId;
    this.storageUnsubscribe = this.storage.subscribe((notification) => {
      if (notification.kind === 'lock' && notification.vaultId === vaultId) {
        void this.lockInternal('cross_context', false, true);
        return;
      }
      if (
        notification.kind === 'revision_change'
        && notification.vaultId === vaultId
        && notification.revision !== undefined
        && this.session
        && notification.revision > this.session.manifest.revision
      ) {
        void this.lockInternal('cross_context_revision', false, true);
      }
    });
  }

  private unsubscribeFromStorage(): void {
    this.storageUnsubscribe?.();
    this.storageUnsubscribe = null;
  }

  private lockInternal(
    reason: string,
    notifyPeers: boolean,
    deletePersisted: boolean,
  ): Promise<void> {
    if (!this.session) {
      return deletePersisted
        ? this.deletePersistedSession()
        : Promise.resolve();
    }
    const vaultId = this.session.manifest.vaultId;
    this.session = null;
    this.clearTimeout();
    this.unsubscribeFromStorage();

    if (notifyPeers && this.storage) {
      try {
        this.storage.notify('lock', vaultId);
      } catch {
        // Local revocation has already succeeded. Notification failure is emitted below.
        this.emit('vault:corruption_detected', vaultId, {
          operation: 'cross_context_lock_notification',
        });
      }
    }
    if (reason === 'timeout') {
      this.emit('session:timeout', vaultId);
    } else if (reason === 'cross_context') {
      this.emit('session:cross_context_lock', vaultId);
    } else if (reason === 'cross_context_revision') {
      this.emit('session:cross_context_revision', vaultId);
    }
    this.emit('wallet:locked', vaultId, { reason });
    return deletePersisted
      ? this.deletePersistedSession()
      : Promise.resolve();
  }

  private persistCurrentSession(): Promise<void> {
    if (!this.storage || !this.session) return Promise.resolve();
    const record: PersistedSessionRecord = {
      version: 1,
      vaultId: this.session.manifest.vaultId,
      vaultKey: this.session.vaultKey,
      inactivityDeadline: this.session.inactivityDeadline,
      inactivityMs: this.session.inactivityMs,
    };
    return this.enqueuePersistence(() =>
      this.storage!.writeSession(SESSION_NAMESPACE, record));
  }

  private deletePersistedSession(): Promise<void> {
    if (!this.storage) return Promise.resolve();
    return this.enqueuePersistence(() =>
      this.storage!.deleteSession(SESSION_NAMESPACE));
  }

  private enqueuePersistence(operation: () => Promise<void>): Promise<void> {
    const next = this.persistenceQueue
      .catch(() => undefined)
      .then(operation);
    this.persistenceQueue = next;
    return next;
  }

  private emit(
    kind: SecurityEventKind,
    vaultId: string,
    metadata?: Record<string, string | number | boolean>,
  ): void {
    this.events?.emit({
      kind,
      timestamp: this.now(),
      vaultId,
      metadata,
    });
  }
}
