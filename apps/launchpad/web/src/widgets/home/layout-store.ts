'use client';
import { useCallback, useEffect, useMemo, useSyncExternalStore } from 'react';

export const widgetTypes = ['trending', 'hot-callouts', 'leaderboard', 'callouts', 'screener', 'watchlist'] as const;
export type WidgetType = typeof widgetTypes[number];
export type ColumnDensity = 'compact' | 'comfortable';
export type ColumnView = 'list' | 'cards';

export interface ColumnConfig {
  id: string;
  type: WidgetType;
  filter: string;
  density: ColumnDensity;
  view: ColumnView;
}

export interface DashboardLayout {
  version: 1;
  columns: ColumnConfig[];
}

export const layoutStorageKey = 'sidiora:dashboard-layout:v1';
export const maxColumns = 8;

const typeSet: ReadonlySet<string> = new Set(widgetTypes);
const densities: ReadonlySet<string> = new Set(['compact', 'comfortable']);
const views: ReadonlySet<string> = new Set(['list', 'cards']);

/**
 * The board a first visit gets, in pump.fun's order: Trending, Hot callouts, Leaderboard. The
 * watchlist is not a default column (signed out it is an empty card in prime position); Latest
 * callouts, Screener and Watchlist are one "Add column" away.
 */
export const defaultColumnTypes: readonly WidgetType[] = ['trending', 'hot-callouts', 'leaderboard'];

export function defaultLayout(): DashboardLayout {
  return {
    version: 1,
    columns: defaultColumnTypes.map((type, index) => ({ id: `${type}-${index + 1}`, type, filter: '', density: 'comfortable', view: 'list' })),
  };
}

/**
 * The first-release default board (Trending, Screener, Watchlist, Leaderboard, Latest callouts),
 * stored verbatim by every visitor who never touched it. Only that exact layout is migrated to the
 * current default; any layout a user edited (moved, removed, added, filtered, toggled density or
 * view) is theirs and stays as saved.
 */
export const legacyDefaultColumnTypes: readonly WidgetType[] = ['trending', 'screener', 'watchlist', 'leaderboard', 'callouts'];

export function legacyDefaultLayout(): DashboardLayout {
  return {
    version: 1,
    columns: legacyDefaultColumnTypes.map((type, index) => ({ id: `${type}-${index + 1}`, type, filter: '', density: 'comfortable', view: 'list' })),
  };
}

/** True when `layout` is the untouched first-release default board. */
export function isLegacyDefaultLayout(layout: DashboardLayout): boolean {
  return serializeLayout(layout) === serializeLayout(legacyDefaultLayout());
}

/** The current default board for an untouched legacy default; every other layout unchanged. */
export function migrateLayout(layout: DashboardLayout): DashboardLayout {
  return isLegacyDefaultLayout(layout) ? defaultLayout() : layout;
}

function parseColumn(value: unknown, seen: Set<string>): ColumnConfig | null {
  if (typeof value !== 'object' || value === null) return null;
  const record = value as Record<string, unknown>;
  const id = typeof record.id === 'string' && record.id.length > 0 && record.id.length <= 64 ? record.id : null;
  const type = typeof record.type === 'string' && typeSet.has(record.type) ? (record.type as WidgetType) : null;
  if (!id || !type || seen.has(id)) return null;
  seen.add(id);
  return {
    id,
    type,
    filter: typeof record.filter === 'string' ? record.filter.slice(0, 120) : '',
    density: typeof record.density === 'string' && densities.has(record.density) ? (record.density as ColumnDensity) : 'comfortable',
    view: typeof record.view === 'string' && views.has(record.view) ? (record.view as ColumnView) : 'list',
  };
}

/** Parse a persisted layout as saved, without migration; anything malformed falls back to the default board. */
export function parseSavedLayout(raw: string | null | undefined): DashboardLayout {
  if (!raw) return defaultLayout();
  try {
    const value: unknown = JSON.parse(raw);
    if (typeof value !== 'object' || value === null) return defaultLayout();
    const record = value as Record<string, unknown>;
    if (record.version !== 1 || !Array.isArray(record.columns)) return defaultLayout();
    const seen = new Set<string>();
    const columns = record.columns.map(column => parseColumn(column, seen)).filter((column): column is ColumnConfig => column !== null).slice(0, maxColumns);
    return { version: 1, columns };
  } catch {
    return defaultLayout();
  }
}

/**
 * Parse a persisted layout; anything malformed falls back to the default board. The untouched
 * first-release default migrates to the current default (see migrateLayout).
 */
export function parseLayout(raw: string | null | undefined): DashboardLayout {
  return migrateLayout(parseSavedLayout(raw));
}

export function serializeLayout(layout: DashboardLayout): string {
  return JSON.stringify(layout);
}

export function moveColumn(layout: DashboardLayout, id: string, toIndex: number): DashboardLayout {
  const from = layout.columns.findIndex(column => column.id === id);
  if (from === -1) return layout;
  const target = Math.max(0, Math.min(layout.columns.length - 1, toIndex));
  if (target === from) return layout;
  const columns = [...layout.columns];
  const [column] = columns.splice(from, 1);
  columns.splice(target, 0, column);
  return { version: 1, columns };
}

export function removeColumn(layout: DashboardLayout, id: string): DashboardLayout {
  const columns = layout.columns.filter(column => column.id !== id);
  return columns.length === layout.columns.length ? layout : { version: 1, columns };
}

export function newColumnId(layout: DashboardLayout, type: WidgetType): string {
  let index = layout.columns.length + 1;
  const ids = new Set(layout.columns.map(column => column.id));
  while (ids.has(`${type}-${index}`)) index++;
  return `${type}-${index}`;
}

/** Append a column, or insert it at `atIndex` (clamped) when given, e.g. right of the column whose header added it. */
export function addColumn(layout: DashboardLayout, type: WidgetType, atIndex?: number): DashboardLayout {
  if (layout.columns.length >= maxColumns) return layout;
  const column: ColumnConfig = { id: newColumnId(layout, type), type, filter: '', density: 'comfortable', view: 'list' };
  const columns = [...layout.columns];
  const index = atIndex === undefined ? columns.length : Math.max(0, Math.min(columns.length, Math.trunc(atIndex)));
  columns.splice(index, 0, column);
  return { version: 1, columns };
}

export function updateColumn(layout: DashboardLayout, id: string, patch: Partial<Omit<ColumnConfig, 'id'>>): DashboardLayout {
  const index = layout.columns.findIndex(column => column.id === id);
  if (index === -1) return layout;
  const current = layout.columns[index];
  const next: ColumnConfig = { ...current, ...patch, id };
  if (next.type === current.type && next.filter === current.filter && next.density === current.density && next.view === current.view) return layout;
  const columns = [...layout.columns];
  columns[index] = next.type === current.type ? next : { ...next, filter: '' };
  return { version: 1, columns };
}

export function changeColumnType(layout: DashboardLayout, id: string, type: WidgetType): DashboardLayout {
  return updateColumn(layout, id, { type });
}

type Listener = () => void;
const listeners = new Set<Listener>();
const initial = defaultLayout();
let current: DashboardLayout = initial;
let hydrated = false;

function storage(): Storage | null {
  try {
    return typeof window === 'undefined' ? null : window.localStorage;
  } catch {
    return null;
  }
}

function emit() {
  for (const listener of listeners) listener();
}

function persist(layout: DashboardLayout) {
  try {
    storage()?.setItem(layoutStorageKey, serializeLayout(layout));
  } catch {
    // Storage may be full or blocked; the in-memory layout still drives the board.
  }
}

export function readStoredLayout(): DashboardLayout {
  try {
    return parseLayout(storage()?.getItem(layoutStorageKey));
  } catch {
    return defaultLayout();
  }
}

export function setLayout(layout: DashboardLayout) {
  if (layout === current) return;
  current = layout;
  persist(layout);
  emit();
}

/** Load the stored layout once on the client; the SSR snapshot is the default board. */
export function hydrateLayout() {
  if (hydrated) return;
  hydrated = true;
  let raw: string | null | undefined = null;
  try {
    raw = storage()?.getItem(layoutStorageKey);
  } catch {
    raw = null;
  }
  const saved = parseSavedLayout(raw);
  const stored = migrateLayout(saved);
  // A migrated legacy default is written back once, so storage holds the board the user sees.
  if (stored !== saved) persist(stored);
  if (serializeLayout(stored) !== serializeLayout(current)) {
    current = stored;
    emit();
  }
}

export function resetLayoutStore() {
  current = defaultLayout();
  hydrated = false;
  try {
    storage()?.removeItem(layoutStorageKey);
  } catch {
    // Nothing to clear when storage is blocked.
  }
  emit();
}

function subscribe(listener: Listener) {
  listeners.add(listener);
  return () => { listeners.delete(listener); };
}

const snapshot = () => current;
const serverSnapshot = () => initial;

export function useDashboardLayout() {
  const layout = useSyncExternalStore(subscribe, snapshot, serverSnapshot);
  useEffect(() => {
    hydrateLayout();
    const onStorage = (event: StorageEvent) => { if (event.key === layoutStorageKey) setLayout(parseLayout(event.newValue)); };
    window.addEventListener('storage', onStorage);
    return () => window.removeEventListener('storage', onStorage);
  }, []);
  const move = useCallback((id: string, toIndex: number) => setLayout(moveColumn(current, id, toIndex)), []);
  const remove = useCallback((id: string) => setLayout(removeColumn(current, id)), []);
  const add = useCallback((type: WidgetType, atIndex?: number) => setLayout(addColumn(current, type, atIndex)), []);
  const changeType = useCallback((id: string, type: WidgetType) => setLayout(changeColumnType(current, id, type)), []);
  const update = useCallback((id: string, patch: Partial<Omit<ColumnConfig, 'id'>>) => setLayout(updateColumn(current, id, patch)), []);
  const reset = useCallback(() => setLayout(defaultLayout()), []);
  return useMemo(() => ({ columns: layout.columns, move, remove, add, changeType, update, reset }), [layout, move, remove, add, changeType, update, reset]);
}
