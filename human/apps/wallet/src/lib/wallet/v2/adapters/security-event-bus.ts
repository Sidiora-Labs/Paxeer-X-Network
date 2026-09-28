import type { EventPort } from '../ports/event-port';
import type { SecurityEvent, SecurityEventKind } from '../types/events';

export class SecurityEventBus implements EventPort {
  private readonly listeners = new Map<
    SecurityEventKind,
    Set<(event: SecurityEvent) => void>
  >();

  emit(event: SecurityEvent): void {
    for (const listener of this.listeners.get(event.kind) ?? []) {
      listener(event);
    }
  }

  on(
    kind: SecurityEventKind,
    handler: (event: SecurityEvent) => void,
  ): () => void {
    const listeners = this.listeners.get(kind) ?? new Set();
    listeners.add(handler);
    this.listeners.set(kind, listeners);
    return () => {
      listeners.delete(handler);
      if (listeners.size === 0) this.listeners.delete(kind);
    };
  }
}
