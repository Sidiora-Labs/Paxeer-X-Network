export class PublicSnapshotRequests {
  private readonly pending = new Map<string, { controller: AbortController; promise: Promise<unknown>; references: number }>();
  request<T>(key: string, load: (signal: AbortSignal) => Promise<T>, signal?: AbortSignal): Promise<T> {
    signal?.throwIfAborted();
    let entry = this.pending.get(key);
    if (!entry) {
      const controller = new AbortController();
      entry = { controller, references: 0, promise: Promise.resolve().then(() => load(controller.signal)) };
      this.pending.set(key, entry);
      const current = entry;
      void entry.promise.then(() => { if (this.pending.get(key) === current) this.pending.delete(key); }, () => { if (this.pending.get(key) === current) this.pending.delete(key); });
    }
    entry.references++;
    const owned = entry;
    return new Promise<T>((resolve, reject) => {
      let released = false;
      const release = () => {
        if (released) return false;
        released = true; signal?.removeEventListener('abort', abort);
        if (--owned.references === 0 && this.pending.get(key) === owned) { this.pending.delete(key); owned.controller.abort(); }
        return true;
      };
      const abort = () => { if (release()) reject(signal?.reason ?? new DOMException('Aborted', 'AbortError')); };
      signal?.addEventListener('abort', abort, { once: true });
      if (signal?.aborted) { abort(); return; }
      void owned.promise.then(value => { let copied: T; try { copied = structuredClone(value) as T; } catch (error) { if (release()) reject(error); return; } if (release()) resolve(copied); }, error => { if (release()) reject(error); });
    });
  }
  clear(): void { for (const entry of this.pending.values()) entry.controller.abort(); this.pending.clear(); }
  get activeRequests(): number { return this.pending.size; }
}
export function snapshotRequestKey(origin: string, operation: string, query: Readonly<Record<string, unknown>>): string {
  const canonical = (value: unknown): unknown => Array.isArray(value) ? value.map(canonical) : value && typeof value === 'object' ? Object.fromEntries(Object.entries(value).sort(([a], [b]) => a.localeCompare(b)).map(([key, child]) => [key, canonical(child)])) : value;
  return JSON.stringify([new URL(origin).origin, operation, canonical(query)]);
}
