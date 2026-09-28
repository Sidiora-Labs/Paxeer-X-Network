import type { SecurityEvent, SecurityEventKind } from '../types/events';

export interface EventPort {
  emit(event: SecurityEvent): void;
  on(kind: SecurityEventKind, handler: (event: SecurityEvent) => void): () => void;
}
