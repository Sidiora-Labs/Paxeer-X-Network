import type { EventPort } from '../ports/event-port';
import type { StoragePort } from '../ports/storage-port';
import type { TimerPort } from '../ports/timer-port';
import type { AuthThrottleRecord } from '../types/storage';
import type { SecurityEventKind } from '../types/events';
import { WalletError } from '../types/errors';
import type { VaultManager, VaultUnlockResult } from './vault-manager';
import type { SessionManager } from './session-manager';

const THROTTLE_NAMESPACE = 'paxport-wallet-auth-throttle-v2';
const VAULT_NAMESPACE = 'paxport-wallet-v2';
const INITIAL_BACKOFF_MS = 30_000;
const MAX_BACKOFF_MS = 60 * 60 * 1_000;
const FAILURE_THRESHOLD = 5;
const MAX_CAS_RETRIES = 5;

export interface AuthenticationOptions {
  readonly inactivityMs: number;
}

export interface AuthenticationThrottleStatus {
  readonly failedAttempts: number;
  readonly lockedUntil: number;
  readonly retryAfterMs: number;
}

export class AuthenticationManager {
  constructor(
    private readonly vaults: VaultManager,
    private readonly sessions: SessionManager,
    private readonly storage: StoragePort,
    private readonly events: EventPort | null,
    private readonly timer: TimerPort,
    private readonly options: AuthenticationOptions,
  ) {}

  async unlock(password: string): Promise<VaultUnlockResult> {
    const stored = await this.storage.read(VAULT_NAMESPACE);
    if (!stored) throw WalletError.corruptVault('No v2 vault exists');
    await this.enforceThrottle(stored.vaultId);

    try {
      const result = await this.vaults.unlockVault(password, stored.manifest);
      await this.sessions.unlock(
        result.vaultKey,
        result.manifest,
        this.options.inactivityMs,
        result.authenticatedSlotId,
      );
      await this.clearThrottle(result.manifest.vaultId);
      return result;
    } catch (error) {
      if (error instanceof WalletError && error.code === 'AUTHENTICATION_FAILED') {
        const throttle = await this.recordFailure(stored.vaultId);
        this.emit('auth:failed', stored.vaultId, {
          failedAttempts: throttle.failedAttempts,
        });
      }
      throw error;
    }
  }

  async stepUp(password: string): Promise<void> {
    const manifest = this.sessions.getManifest();
    if (!manifest) throw WalletError.locked();
    await this.enforceThrottle(manifest.vaultId);

    try {
      const result = await this.vaults.unlockVault(password, manifest);
      if (result.manifest.vaultId !== manifest.vaultId) {
        throw WalletError.authenticationFailed();
      }
      await this.clearThrottle(manifest.vaultId);
      this.sessions.recordStepUp();
    } catch (error) {
      if (error instanceof WalletError && error.code === 'AUTHENTICATION_FAILED') {
        const throttle = await this.recordFailure(manifest.vaultId);
        this.emit('auth:failed', manifest.vaultId, {
          failedAttempts: throttle.failedAttempts,
          operation: 'step_up',
        });
      }
      throw error;
    }
  }

  async getThrottleStatus(vaultId: string): Promise<AuthenticationThrottleStatus> {
    const record = await this.storage.readAuthThrottle(THROTTLE_NAMESPACE);
    if (!record || record.vaultId !== vaultId) {
      return { failedAttempts: 0, lockedUntil: 0, retryAfterMs: 0 };
    }
    return {
      failedAttempts: record.failedAttempts,
      lockedUntil: record.lockedUntil,
      retryAfterMs: Math.max(0, record.lockedUntil - this.timer.now()),
    };
  }

  private async enforceThrottle(vaultId: string): Promise<void> {
    const status = await this.getThrottleStatus(vaultId);
    if (status.retryAfterMs > 0) {
      this.emit('auth:throttled', vaultId, {
        retryAfterMs: status.retryAfterMs,
        failedAttempts: status.failedAttempts,
      });
      throw WalletError.authenticationThrottled(status.retryAfterMs);
    }
  }

  private async recordFailure(vaultId: string): Promise<AuthThrottleRecord> {
    for (let attempt = 0; attempt < MAX_CAS_RETRIES; attempt++) {
      const current = await this.storage.readAuthThrottle(THROTTLE_NAMESPACE);
      const baseRevision = current?.revision ?? 0;
      const failedAttempts =
        current?.vaultId === vaultId ? current.failedAttempts + 1 : 1;
      const lockedUntil = failedAttempts >= FAILURE_THRESHOLD
        ? this.timer.now() + Math.min(
          INITIAL_BACKOFF_MS * (2 ** (failedAttempts - FAILURE_THRESHOLD)),
          MAX_BACKOFF_MS,
        )
        : 0;
      const next: AuthThrottleRecord = {
        version: 1,
        vaultId,
        revision: baseRevision + 1,
        failedAttempts,
        lockedUntil,
        updatedAt: this.timer.now(),
      };
      const write = await this.storage.writeAuthThrottle(
        THROTTLE_NAMESPACE,
        next,
        current ? current.revision : null,
      );
      if (write.ok) return write.record;
      if (write.error !== 'WRITE_CONFLICT') throw WalletError.storageUnavailable();
    }
    throw WalletError.writeConflict();
  }

  private async clearThrottle(vaultId: string): Promise<void> {
    for (let attempt = 0; attempt < MAX_CAS_RETRIES; attempt++) {
      const current = await this.storage.readAuthThrottle(THROTTLE_NAMESPACE);
      if (!current || current.vaultId !== vaultId) return;
      const deletion = await this.storage.deleteAuthThrottle(
        THROTTLE_NAMESPACE,
        current.revision,
      );
      if (deletion.ok) return;
      if (deletion.error !== 'WRITE_CONFLICT') {
        throw WalletError.storageUnavailable();
      }
    }
    throw WalletError.writeConflict();
  }

  private emit(
    kind: SecurityEventKind,
    vaultId: string,
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
