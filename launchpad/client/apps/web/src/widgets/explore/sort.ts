export type SortKey = 'mcap' | 'age' | 'txns' | 'volume24h' | 'traders' | 'change1h' | 'change6h' | 'change24h';
export type SortDirectionValue = 'asc' | 'desc';
export type SortState = { key: SortKey; direction: SortDirectionValue } | null;

export const sortLabels: Record<SortKey, string> = {
  mcap: 'market cap',
  age: 'age',
  txns: '24h transactions',
  volume24h: '24h volume',
  traders: '24h traders',
  change1h: '1h change',
  change6h: '6h change',
  change24h: '24h change',
};

/** Header click cycle: descending, then ascending, then back to the preset's own order. */
export function cycleSort(current: SortState, key: SortKey): SortState {
  if (current === null || current.key !== key) return { key, direction: 'desc' };
  if (current.direction === 'desc') return { key, direction: 'asc' };
  return null;
}

export interface SortableRow {
  rank: number;
  mcap: number | null;
  ageMs: number | null;
  txns: number | null;
  volume24h: number | null;
  traders: number | null;
  change1h: number | null;
  change6h: number | null;
  change24h: number | null;
}

const sortField: Record<SortKey, keyof SortableRow> = {
  mcap: 'mcap',
  age: 'ageMs',
  txns: 'txns',
  volume24h: 'volume24h',
  traders: 'traders',
  change1h: 'change1h',
  change6h: 'change6h',
  change24h: 'change24h',
};

export function sortValue(row: SortableRow, key: SortKey): number | null {
  const value = row[sortField[key]];
  return value === null || !Number.isFinite(value) ? null : value;
}

/**
 * Order the loaded rows. `null` keeps the ranking order (rank ascending).
 * Rows without a value for the active key always sort last; ties fall back
 * to the server rank so the result is stable between renders.
 */
export function sortRows<T extends SortableRow>(rows: readonly T[], sort: SortState): T[] {
  const ordered = [...rows];
  if (sort === null) return ordered.sort((a, b) => a.rank - b.rank);
  const read = (row: T): number | null => sortValue(row, sort.key);
  const sign = sort.direction === 'desc' ? -1 : 1;
  return ordered.sort((a, b) => {
    const x = read(a), y = read(b);
    if (x === null && y === null) return a.rank - b.rank;
    if (x === null) return 1;
    if (y === null) return -1;
    if (x === y) return a.rank - b.rank;
    return x < y ? -sign : sign;
  });
}

export interface ExploreFilters {
  /** Hours; rows older than this are hidden. */
  maxAgeHours: number | null;
  minMcap: number | null;
  maxMcap: number | null;
  minVolume24h: number | null;
  minHolders: number | null;
}

export const emptyFilters: ExploreFilters = Object.freeze({ maxAgeHours: null, minMcap: null, maxMcap: null, minVolume24h: null, minHolders: null });

export function activeFilterCount(filters: ExploreFilters): number {
  return Object.values(filters).filter(value => value !== null).length;
}

export interface FilterableRow extends SortableRow {
  holders: number | null;
}

/** Rows missing the field a filter targets are hidden; a filter asks for a known value. */
export function applyFilters<T extends FilterableRow>(rows: readonly T[], filters: ExploreFilters): T[] {
  if (activeFilterCount(filters) === 0) return [...rows];
  return rows.filter(row => {
    if (filters.maxAgeHours !== null && (row.ageMs === null || row.ageMs > filters.maxAgeHours * 3_600_000)) return false;
    if (filters.minMcap !== null && (row.mcap === null || row.mcap < filters.minMcap)) return false;
    if (filters.maxMcap !== null && (row.mcap === null || row.mcap > filters.maxMcap)) return false;
    if (filters.minVolume24h !== null && (row.volume24h === null || row.volume24h < filters.minVolume24h)) return false;
    if (filters.minHolders !== null && (row.holders === null || row.holders < filters.minHolders)) return false;
    return true;
  });
}

/** Parse a filter input; empty or non-numeric text clears the filter, negatives are rejected. */
export function parseFilterNumber(text: string): number | null {
  const trimmed = text.trim();
  if (trimmed === '') return null;
  const value = Number(trimmed.replace(/,/g, ''));
  return Number.isFinite(value) && value >= 0 ? value : null;
}

export function sortDisclosure(sort: SortState, loaded: number, matched: number, filterCount: number, sourceLabel: string): string | null {
  const parts: string[] = [];
  if (sort) parts.push(`Sorted by ${sortLabels[sort.key]}, ${sort.direction === 'desc' ? 'high to low' : 'low to high'} (${loaded} ${sourceLabel} shown)`);
  if (filterCount > 0) parts.push(`${matched} of ${loaded} coins match`);
  return parts.length === 0 ? null : parts.join(' · ');
}
