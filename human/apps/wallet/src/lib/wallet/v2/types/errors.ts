export type WalletErrorCode =
  | 'LOCKED'
  | 'AUTHENTICATION_FAILED'
  | 'AUTHENTICATION_THROTTLED'
  | 'REAUTHENTICATION_REQUIRED'
  | 'CORRUPT_VAULT'
  | 'UNSUPPORTED_VERSION'
  | 'STORAGE_UNAVAILABLE'
  | 'WRITE_CONFLICT'
  | 'INVALID_INPUT'
  | 'ACCOUNT_NOT_FOUND'
  | 'MIGRATION_REQUIRED'
  | 'MIGRATION_FAILED';

export class WalletError extends Error {
  readonly code: WalletErrorCode;
  readonly cause?: Error;

  constructor(code: WalletErrorCode, message?: string, cause?: Error) {
    super(message ?? code);
    this.name = 'WalletError';
    this.code = code;
    this.cause = cause;
  }

  static locked(): WalletError {
    return new WalletError('LOCKED', 'Wallet is locked.');
  }

  static authenticationFailed(): WalletError {
    return new WalletError('AUTHENTICATION_FAILED', 'Authentication failed.');
  }

  static authenticationThrottled(retryAfterMs: number): WalletError {
    return new WalletError(
      'AUTHENTICATION_THROTTLED',
      `Too many failed attempts. Retry after ${retryAfterMs}ms.`,
    );
  }

  static reauthenticationRequired(): WalletError {
    return new WalletError('REAUTHENTICATION_REQUIRED', 'Fresh authentication required.');
  }

  static corruptVault(detail?: string): WalletError {
    return new WalletError('CORRUPT_VAULT', detail ?? 'Vault data is corrupt or tampered.');
  }

  static unsupportedVersion(version: number): WalletError {
    return new WalletError('UNSUPPORTED_VERSION', `Unsupported schema version: ${version}`);
  }

  static storageUnavailable(cause?: Error): WalletError {
    return new WalletError('STORAGE_UNAVAILABLE', 'Storage is unavailable.', cause);
  }

  static writeConflict(): WalletError {
    return new WalletError('WRITE_CONFLICT', 'Concurrent write conflict.');
  }

  static invalidInput(field: string, reason: string): WalletError {
    return new WalletError('INVALID_INPUT', `Invalid ${field}: ${reason}`);
  }

  static accountNotFound(identifier: string): WalletError {
    return new WalletError('ACCOUNT_NOT_FOUND', `Account not found: ${identifier}`);
  }

  static migrationFailed(detail?: string): WalletError {
    return new WalletError('MIGRATION_FAILED', detail ?? 'Legacy migration failed.');
  }

  static migrationRequired(): WalletError {
    return new WalletError(
      'MIGRATION_REQUIRED',
      'Legacy wallet requires migration to the v2 vault.',
    );
  }
}
