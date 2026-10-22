import { execFileSync } from 'node:child_process';
import { readFileSync } from 'node:fs';
import { describe, expect, it } from 'vitest';
import { toFunctionSelector, toFunctionSignature, type Abi } from 'viem';
import {
  EIP712_ABI,
  PRECOMPILE_ADDRESSES,
  PrecompileNotServedError,
  SCHEDULER_ABI,
  STREAMS_ABI,
  TEE_ATTESTOR_ABI,
  UNSERVED_PRECOMPILES,
  eip712,
  scheduler,
  streams,
  teeAttestor,
} from '../src/precompiles.js';

function readRepositoryFile(path: string): string {
  return readFileSync(new URL(`../../../../${path}`, import.meta.url), 'utf8');
}

const addressTable = readRepositoryFile('spec/layerx-merge/contracts/address-table.md');

function tableRows(start: string, end: string): string[][] {
  const startOffset = addressTable.indexOf(start);
  const endOffset = addressTable.indexOf(end, startOffset + start.length);
  if (startOffset < 0 || endOffset < 0) {
    throw new Error(`Missing address-table section: ${start}`);
  }
  return addressTable
    .slice(startOffset + start.length, endOffset)
    .split('\n')
    .filter((line) => line.startsWith('| 0x'))
    .map((line) =>
      line
        .split('|')
        .slice(1, -1)
        .map((cell) => cell.trim())
    );
}

const addressRows = tableRows('## (a) Address table', '### (a2) Method selectors');
const selectorRows = tableRows('### (a2) Method selectors', '### 0x101A resolution');
const expectedRoutes = {
  orob: '0x0000000000000000000000000000000000000901',
  clearing: '0x0000000000000000000000000000000000000902',
  oracle: '0x0000000000000000000000000000000000000903',
  pofq: '0x0000000000000000000000000000000000000904',
  scheduler: '0x0000000000000000000000000000000000000905',
  streams: '0x0000000000000000000000000000000000000906',
  teeAttestor: '0x0000000000000000000000000000000000000907',
  eip712: '0x0000000000000000000000000000000000000908',
  staking: '0x0000000000000000000000000000000000001005',
  bech32: '0x0000000000000000000000000000000000001004',
  p256: '0x0000000000000000000000000000000000001011',
} as const;
const routeKeys = Object.keys(expectedRoutes) as (keyof typeof expectedRoutes)[];
const registrationSources = [
  { key: 'staking', package: 'staking', symbol: 'StakingAddress', file: 'staking.go' },
  { key: 'bech32', package: 'addr', symbol: 'AddrAddress', file: 'addr.go' },
  { key: 'p256', package: 'p256', symbol: 'P256VerifyAddress', file: 'p256.go' },
] as const;
const setup = readRepositoryFile('precompiles/setup.go')
  .split('func GetCustomPrecompiles(')[1]
  ?.split('\nfunc InitializePrecompiles(')[0];
const abiRoutes = [
  { key: 'scheduler', abi: SCHEDULER_ABI, count: 5 },
  { key: 'streams', abi: STREAMS_ABI, count: 6 },
  { key: 'eip712', abi: EIP712_ABI, count: 3 },
  { key: 'teeAttestor', abi: TEE_ATTESTOR_ABI, count: 4 },
] as const;

function registeredAddress(source: (typeof registrationSources)[number]): string {
  const file = `precompiles/${source.package}/${source.file}`;
  const match = readRepositoryFile(file).match(
    new RegExp(`\\b${source.symbol}\\s*=\\s*"(0x[0-9a-fA-F]{40})"`)
  );
  if (!match?.[1]) throw new Error(`Missing ${source.key} address constant in ${file}`);
  return match[1].toLowerCase();
}

describe('route map', () => {
  it('retains exactly the eleven distinct route keys', () => {
    expect(Object.keys(PRECOMPILE_ADDRESSES).sort()).toEqual([...routeKeys].sort());
    expect(new Set(Object.values(PRECOMPILE_ADDRESSES)).size).toBe(11);
  });

  it.each(routeKeys)('%s resolves to its exact address in contract section (a)', (key) => {
    const address = PRECOMPILE_ADDRESSES[key];
    expect(address, key).toBe(expectedRoutes[key]);
    expect(
      addressRows.some((row) => row[0]?.toLowerCase() === address),
      key
    ).toBe(true);
  });

  it.each(registrationSources)(
    '$key matches its Go constant and GetCustomPrecompiles entry',
    (source) => {
      expect(PRECOMPILE_ADDRESSES[source.key], source.key).toBe(registeredAddress(source));
      expect(setup, source.key).toMatch(
        new RegExp(`ecommon\\.HexToAddress\\(\\s*${source.package}\\.${source.symbol}\\s*\\)`)
      );
    }
  );

  it.each(routeKeys)('%s resolves to a Go registration or a new merge entry', (key) => {
    const source = registrationSources.find((entry) => entry.key === key);
    if (source) {
      expect(PRECOMPILE_ADDRESSES[key], key).toBe(registeredAddress(source));
      expect(setup, key).toMatch(
        new RegExp(`ecommon\\.HexToAddress\\(\\s*${source.package}\\.${source.symbol}\\s*\\)`)
      );
    } else {
      const entry = addressRows.find((row) => row[0]?.toLowerCase() === PRECOMPILE_ADDRESSES[key]);
      expect(entry?.[3], key).toBe('new');
      expect(entry?.[2], key).toContain('precompiles/layerxagent/');
      expect(entry?.[5], key).toBe('merge plan');
    }
  });

  it('bech32 targets the addr ABI getPaxAddr and getEvmAddr views', () => {
    const abi = JSON.parse(readRepositoryFile('precompiles/addr/abi.json')) as Abi;
    const methods = abi.filter(
      (item) => item.type === 'function' && ['getPaxAddr', 'getEvmAddr'].includes(item.name)
    );
    expect(PRECOMPILE_ADDRESSES.bech32).toBe(registeredAddress(registrationSources[1]));
    expect(
      methods
        .map((item) => {
          if (item.type !== 'function') throw new Error('Expected addr function');
          return [
            toFunctionSignature(item),
            item.stateMutability,
            item.outputs.map((output) => output.type),
          ];
        })
        .sort()
    ).toEqual([
      ['getEvmAddr(string)', 'view', ['address']],
      ['getPaxAddr(address)', 'view', ['string']],
    ]);
  });
});

describe('agent ABI selectors', () => {
  it.each(abiRoutes)(
    '$key exactly matches every section (a2) signature and selector',
    ({ key, abi, count }) => {
      const address = `0x${PRECOMPILE_ADDRESSES[key].slice(-4)}`;
      const expected = selectorRows
        .filter((row) => row[0]?.toLowerCase() === address)
        .map((row) => [row[1], row[2]])
        .sort();
      const actual = abi
        .map((method) => [toFunctionSignature(method), toFunctionSelector(method)])
        .sort();
      expect(expected, key).toHaveLength(count);
      expect(actual, key).toEqual(expected);
      expect(new Set(actual.map(([, selector]) => selector)).size, key).toBe(count);
    }
  );
});

function goSources(): string[] {
  return execFileSync('git', ['ls-files', '*.go'], {
    cwd: new URL('../../../../', import.meta.url),
    encoding: 'utf8',
    maxBuffer: 64 * 1024 * 1024,
  })
    .split('\n')
    .filter((file) => file.endsWith('.go') && !file.endsWith('_test.go'));
}

describe('unserved agent precompiles', () => {
  it('lists every 0x09xx route while no node package links a layerxagent precompile', () => {
    const agentKeys = routeKeys.filter((key) => !registrationSources.some((source) => source.key === key));
    expect([...UNSERVED_PRECOMPILES].sort()).toEqual([...agentKeys].sort());
    const linking = goSources()
      .filter((file) => !file.startsWith('precompiles/layerxagent/'))
      .filter((file) => /"[^"]*\/precompiles\/layerxagent\/(?!common")[^"]*"/.test(readRepositoryFile(file)));
    expect(linking).toEqual([]);
  });

  it('refuses to build a scheduler deposit or a stream to an unserved address', () => {
    expect(() =>
      scheduler.schedule({
        target: '0x000000000000000000000000000000000000dEaD',
        executeAtBlock: 1n,
        gasLimit: 1n,
        depositWei: 1n,
      })
    ).toThrowError(PrecompileNotServedError);
    expect(() => scheduler.cancel(1n)).toThrowError(PrecompileNotServedError);
    expect(() =>
      streams.open({
        payee: '0x000000000000000000000000000000000000dEaD',
        token: '0x000000000000000000000000000000000000dEaD',
        ratePerSecond: 1n,
        cap: 1n,
      })
    ).toThrowError(PrecompileNotServedError);
  });

  it('refuses reads of an unserved address before any RPC', async () => {
    await expect(scheduler.pending('0x000000000000000000000000000000000000dEaD')).rejects.toBeInstanceOf(
      PrecompileNotServedError
    );
    await expect(eip712.hashTypedData(`0x${'11'.repeat(32)}`, `0x${'22'.repeat(32)}`)).rejects.toBeInstanceOf(
      PrecompileNotServedError
    );
    await expect(teeAttestor.rootCount(0)).rejects.toBeInstanceOf(PrecompileNotServedError);
  });
});
