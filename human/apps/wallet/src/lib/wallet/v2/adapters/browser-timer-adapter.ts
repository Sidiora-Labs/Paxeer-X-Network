import type { TimerPort } from '../ports/timer-port';

export class BrowserTimerAdapter implements TimerPort {
  setTimeout(callback: () => void, ms: number): unknown {
    return globalThis.setTimeout(callback, ms);
  }

  clearTimeout(handle: unknown): void {
    globalThis.clearTimeout(handle as ReturnType<typeof globalThis.setTimeout>);
  }

  now(): number {
    return Date.now();
  }
}
