export type SecurityEventKind =
  | 'wallet:locked'
  | 'wallet:unlocked'
  | 'wallet:created'
  | 'wallet:reset'
  | 'vault:mutated'
  | 'vault:corruption_detected'
  | 'account:changed'
  | 'sign:transaction'
  | 'sign:message'
  | 'sign:typed_data'
  | 'auth:failed'
  | 'auth:throttled'
  | 'auth:step_up'
  | 'export:mnemonic'
  | 'export:private_key'
  | 'migration:started'
  | 'migration:completed'
  | 'migration:failed'
  | 'session:timeout'
  | 'session:cross_context_lock'
  | 'session:cross_context_revision';

export interface SecurityEvent {
  readonly kind: SecurityEventKind;
  readonly timestamp: number;
  readonly vaultId?: string;
  readonly accountAddress?: string;
  readonly metadata?: Record<string, string | number | boolean>;
}
