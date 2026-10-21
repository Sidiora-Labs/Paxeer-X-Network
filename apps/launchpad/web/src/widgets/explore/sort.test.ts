import { describe, expect, it } from 'vitest';
import { activeFilterCount, applyFilters, cycleSort, emptyFilters, parseFilterNumber, sortDisclosure, sortLabels, sortRows, type FilterableRow, type SortState } from './sort';

const row = (rank: number, overrides: Partial<FilterableRow> = {}): FilterableRow & { id: string } => ({
  id: `p${rank}`,
  rank,
  mcap: rank * 1000,
  ageMs: rank * 3_600_000,
  txns: rank * 10,
  volume24h: rank * 500,
  traders: rank,
  change1h: rank - 2,
  change6h: null,
  change24h: 10 - rank,
  holders: rank * 3,
  ...overrides,
});

describe('cycleSort', () => {
  it('cycles descending, ascending, then back to the preset order', () => {
    const first = cycleSort(null, 'mcap');
    expect(first).toEqual({ key: 'mcap', direction: 'desc' });
    const second = cycleSort(first, 'mcap');
    expect(second).toEqual({ key: 'mcap', direction: 'asc' });
    expect(cycleSort(second, 'mcap')).toBeNull();
  });
  it('starts a new column descending regardless of the previous column direction', () => {
    expect(cycleSort({ key: 'age', direction: 'asc' }, 'txns')).toEqual({ key: 'txns', direction: 'desc' });
  });
});

describe('sortRows', () => {
  const rows = [row(3), row(1), row(2)];
  it('keeps the server rank when no sort is active', () => {
    expect(sortRows(rows, null).map(r => r.rank)).toEqual([1, 2, 3]);
  });
  it('sorts by the mapped field in both directions', () => {
    expect(sortRows(rows, { key: 'age', direction: 'desc' }).map(r => r.rank)).toEqual([3, 2, 1]);
    expect(sortRows(rows, { key: 'age', direction: 'asc' }).map(r => r.rank)).toEqual([1, 2, 3]);
    expect(sortRows(rows, { key: 'change24h', direction: 'desc' }).map(r => r.rank)).toEqual([1, 2, 3]);
  });
  it('puts rows without a value last and breaks ties by rank', () => {
    const mixed = [row(2, { mcap: null }), row(3, { mcap: 5 }), row(1, { mcap: 5 })];
    expect(sortRows(mixed, { key: 'mcap', direction: 'desc' }).map(r => r.rank)).toEqual([1, 3, 2]);
    expect(sortRows(mixed, { key: 'mcap', direction: 'asc' }).map(r => r.rank)).toEqual([1, 3, 2]);
  });
  it('sorts on the served 6h change and keeps rows without a complete baseline last', () => {
    const sixHour = [row(1, { change6h: -5 }), row(2, { change6h: 12 }), row(3, { change6h: null })];
    expect(sortRows(sixHour, { key: 'change6h', direction: 'desc' }).map(r => r.rank)).toEqual([2, 1, 3]);
    expect(sortRows(sixHour, { key: 'change6h', direction: 'asc' }).map(r => r.rank)).toEqual([1, 2, 3]);
  });
  it('does not mutate the input', () => {
    const input = [row(2), row(1)];
    const sort: SortState = { key: 'rank' as never, direction: 'desc' };
    expect(() => sortRows(input, sort)).not.toThrow();
    expect(input.map(r => r.rank)).toEqual([2, 1]);
  });
});

describe('filters', () => {
  const rows = [row(1), row(2), row(3, { holders: null })];
  it('counts active filters and returns a copy when none are active', () => {
    expect(activeFilterCount(emptyFilters)).toBe(0);
    const out = applyFilters(rows, emptyFilters);
    expect(out).toEqual(rows);
    expect(out).not.toBe(rows);
  });
  it('hides rows outside the bounds and rows missing the filtered value', () => {
    expect(applyFilters(rows, { ...emptyFilters, maxAgeHours: 2 }).map(r => r.rank)).toEqual([1, 2]);
    expect(applyFilters(rows, { ...emptyFilters, minMcap: 2000, maxMcap: 2500 }).map(r => r.rank)).toEqual([2]);
    expect(applyFilters(rows, { ...emptyFilters, minVolume24h: 1000 }).map(r => r.rank)).toEqual([2, 3]);
    expect(applyFilters(rows, { ...emptyFilters, minHolders: 1 }).map(r => r.rank)).toEqual([1, 2]);
  });
  it('parses filter text leniently and rejects negatives', () => {
    expect(parseFilterNumber('')).toBeNull();
    expect(parseFilterNumber('  ')).toBeNull();
    expect(parseFilterNumber('1,250.5')).toBe(1250.5);
    expect(parseFilterNumber('-3')).toBeNull();
    expect(parseFilterNumber('abc')).toBeNull();
  });
});

describe('sortDisclosure', () => {
  it('is silent when the server rank is shown unfiltered', () => {
    expect(sortDisclosure(null, 50, 50, 0, 'coins')).toBeNull();
  });
  it('names the column, direction and loaded-row scope, then the filter match count', () => {
    const text = sortDisclosure({ key: 'mcap', direction: 'desc' }, 50, 12, 2, 'coins');
    expect(text).toBe(`Sorted by ${sortLabels.mcap}, high to low (50 coins shown) · 12 of 50 coins match`);
    expect(sortDisclosure({ key: 'age', direction: 'asc' }, 10, 10, 0, 'coins')).toBe(`Sorted by ${sortLabels.age}, low to high (10 coins shown)`);
    expect(sortDisclosure(null, 10, 3, 1, 'coins')).toBe('3 of 10 coins match');
  });
});
