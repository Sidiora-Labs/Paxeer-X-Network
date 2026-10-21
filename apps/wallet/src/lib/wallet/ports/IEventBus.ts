/**
 * Event bus interface.
 * Replaces direct window.dispatchEvent / window.addEventListener usage.
 */
export type EventHandler = (data?: unknown) => void;

export interface IEventBus {
  emit(event: string, data?: unknown): void;
  on(event: string, handler: EventHandler): void;
  off(event: string, handler: EventHandler): void;
}
