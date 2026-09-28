import type { IEventBus, EventHandler } from '../ports/IEventBus';

/**
 * Simple in-memory event bus. Works in any JS environment.
 */
export class SimpleEventBus implements IEventBus {
  private listeners = new Map<string, Set<EventHandler>>();

  emit(event: string, data?: unknown): void {
    const handlers = this.listeners.get(event);
    if (handlers) {
      for (const handler of handlers) {
        try {
          handler(data);
        } catch (err) {
          console.error(`Event handler error [${event}]:`, err);
        }
      }
    }
  }

  on(event: string, handler: EventHandler): void {
    if (!this.listeners.has(event)) {
      this.listeners.set(event, new Set());
    }
    this.listeners.get(event)!.add(handler);
  }

  off(event: string, handler: EventHandler): void {
    this.listeners.get(event)?.delete(handler);
  }
}
