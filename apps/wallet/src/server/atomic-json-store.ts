import { randomUUID } from 'node:crypto';
import {
  copyFile,
  mkdir,
  open,
  readFile,
  rename,
  stat,
  unlink,
  writeFile,
} from 'node:fs/promises';
import path from 'node:path';

const queues = new Map<string, Promise<unknown>>();

export class DurableStoreCorruptError extends Error {
  constructor(readonly storePath: string) {
    super('Durable store is corrupt');
    this.name = 'DurableStoreCorruptError';
  }
}

export interface AtomicJsonStoreOptions<T> {
  readonly filePath: string;
  readonly empty: () => T;
  readonly parse: (input: unknown) => T;
  readonly migrate?: (input: unknown) => T;
  readonly maxBytes?: number;
}

function enqueue<T>(key: string, operation: () => Promise<T>): Promise<T> {
  const previous = queues.get(key) ?? Promise.resolve();
  const current = previous.then(operation, operation);
  queues.set(key, current);
  const cleanup = () => {
    if (queues.get(key) === current) queues.delete(key);
  };
  void current.then(cleanup, cleanup);
  return current;
}

async function wait(milliseconds: number): Promise<void> {
  await new Promise<void>((resolve) => setTimeout(resolve, milliseconds));
}

export class AtomicJsonStore<T> {
  readonly #filePath: string;
  readonly #lockPath: string;
  readonly #empty: () => T;
  readonly #parse: (input: unknown) => T;
  readonly #migrate?: (input: unknown) => T;
  readonly #maxBytes: number;

  constructor(options: AtomicJsonStoreOptions<T>) {
    this.#filePath = options.filePath;
    this.#lockPath = `${options.filePath}.lock`;
    this.#empty = options.empty;
    this.#parse = options.parse;
    this.#migrate = options.migrate;
    this.#maxBytes = options.maxBytes ?? 5_242_880;
  }

  async read(): Promise<T> {
    return enqueue(this.#filePath, () => this.#readUnlocked());
  }

  async update(mutator: (current: T) => T | Promise<T>): Promise<T> {
    return enqueue(this.#filePath, async () => {
      await mkdir(path.dirname(this.#filePath), { recursive: true });
      const release = await this.#acquireFileLock();
      try {
        const current = await this.#readUnlocked();
        const next = await mutator(current);
        this.#parse(next);
        await this.#writeUnlocked(next);
        return next;
      } finally {
        await release();
      }
    });
  }

  async #readUnlocked(): Promise<T> {
    try {
      const metadata = await stat(this.#filePath);
      if (metadata.size > this.#maxBytes) {
        await this.#quarantine();
        throw new DurableStoreCorruptError(this.#filePath);
      }
      const raw = await readFile(this.#filePath, 'utf8');
      const input: unknown = JSON.parse(raw);
      try {
        return this.#parse(input);
      } catch {
        if (!this.#migrate) throw new Error('No migration');
        const migrated = this.#migrate(input);
        this.#parse(migrated);
        await this.#writeUnlocked(migrated);
        return migrated;
      }
    } catch (error) {
      if (
        error instanceof Error &&
        'code' in error &&
        error.code === 'ENOENT'
      ) {
        return this.#empty();
      }
      if (error instanceof DurableStoreCorruptError) throw error;
      await this.#quarantine();
      throw new DurableStoreCorruptError(this.#filePath);
    }
  }

  async #writeUnlocked(value: T): Promise<void> {
    const serialized = JSON.stringify(value);
    if (Buffer.byteLength(serialized) > this.#maxBytes) {
      throw new RangeError('Durable store exceeds its size limit');
    }
    await mkdir(path.dirname(this.#filePath), { recursive: true });
    const temporary = `${this.#filePath}.${process.pid}.${randomUUID()}.tmp`;
    try {
      await writeFile(temporary, serialized, { encoding: 'utf8', mode: 0o600 });
      try {
        await copyFile(this.#filePath, `${this.#filePath}.bak`);
      } catch (error) {
        if (
          !(error instanceof Error && 'code' in error && error.code === 'ENOENT')
        ) {
          throw error;
        }
      }
      await rename(temporary, this.#filePath);
    } finally {
      await unlink(temporary).catch(() => undefined);
    }
  }

  async #quarantine(): Promise<void> {
    const target = `${this.#filePath}.corrupt-${Date.now()}`;
    await rename(this.#filePath, target).catch(() => undefined);
  }

  async #acquireFileLock(): Promise<() => Promise<void>> {
    for (let attempt = 0; attempt < 100; attempt += 1) {
      try {
        const handle = await open(this.#lockPath, 'wx', 0o600);
        return async () => {
          await handle.close();
          await unlink(this.#lockPath).catch(() => undefined);
        };
      } catch (error) {
        if (
          !(error instanceof Error && 'code' in error && error.code === 'EEXIST')
        ) {
          throw error;
        }
        const lockStat = await stat(this.#lockPath).catch(() => null);
        if (lockStat && Date.now() - lockStat.mtimeMs > 30_000) {
          await unlink(this.#lockPath).catch(() => undefined);
        }
        await wait(10 + attempt * 2);
      }
    }
    throw new Error('Timed out acquiring durable store lock');
  }
}
