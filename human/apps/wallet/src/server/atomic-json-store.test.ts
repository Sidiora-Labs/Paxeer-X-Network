import { mkdtemp, readFile, readdir, writeFile } from 'node:fs/promises';
import os from 'node:os';
import path from 'node:path';
import { describe, expect, it } from 'vitest';
import {
  AtomicJsonStore,
  DurableStoreCorruptError,
} from './atomic-json-store';

interface CounterState {
  version: 1;
  value: number;
}

function parseCounter(input: unknown): CounterState {
  if (
    typeof input !== 'object' ||
    input === null ||
    Array.isArray(input) ||
    (input as CounterState).version !== 1 ||
    !Number.isSafeInteger((input as CounterState).value)
  ) {
    throw new TypeError('Counter is invalid');
  }
  return { version: 1, value: (input as CounterState).value };
}

describe('AtomicJsonStore', () => {
  it('serializes concurrent real filesystem updates without losing writes', async () => {
    const directory = await mkdtemp(path.join(os.tmpdir(), 'paxport-store-'));
    const filePath = path.join(directory, 'counter.json');
    const store = new AtomicJsonStore<CounterState>({
      filePath,
      empty: () => ({ version: 1, value: 0 }),
      parse: parseCounter,
    });

    await Promise.all(
      Array.from({ length: 40 }, () =>
        store.update((current) => ({
          version: 1,
          value: current.value + 1,
        })),
      ),
    );

    expect(await store.read()).toEqual({ version: 1, value: 40 });
    expect(JSON.parse(await readFile(filePath, 'utf8'))).toEqual({
      version: 1,
      value: 40,
    });
    expect(JSON.parse(await readFile(`${filePath}.bak`, 'utf8'))).toEqual({
      version: 1,
      value: 39,
    });
  });

  it('migrates a real legacy record and makes corruption visible', async () => {
    const directory = await mkdtemp(path.join(os.tmpdir(), 'paxport-store-'));
    const filePath = path.join(directory, 'counter.json');
    await writeFile(filePath, JSON.stringify({ count: 7 }), 'utf8');
    const store = new AtomicJsonStore<CounterState>({
      filePath,
      empty: () => ({ version: 1, value: 0 }),
      parse: parseCounter,
      migrate: (input) => {
        const count = (input as { count?: unknown }).count;
        if (!Number.isSafeInteger(count)) throw new TypeError('Legacy invalid');
        return { version: 1, value: count as number };
      },
    });

    expect(await store.read()).toEqual({ version: 1, value: 7 });
    await writeFile(filePath, '{not-json', 'utf8');
    await expect(store.read()).rejects.toBeInstanceOf(DurableStoreCorruptError);
    expect(
      (await readdir(directory)).some((name) =>
        name.startsWith('counter.json.corrupt-'),
      ),
    ).toBe(true);
  });
});
