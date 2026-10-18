'use client';
import { useCallback, useEffect, useSyncExternalStore } from 'react';
import { terminalResetColumnsEvent } from './status-bar/model';

export const terminalColumnsStorageKey = 'sidiora-terminal-columns';

export const columnLimits = Object.freeze({
  leftMin: 240,
  leftDefault: 296,
  rightMin: 280,
  rightDefault: 312,
  centerMin: 560,
  sideMax: 640,
  collapsed: 24,
  gutter: 4,
});

export interface TerminalColumnsState {
  leftWidth: number;
  rightWidth: number;
  leftCollapsed: boolean;
  rightCollapsed: boolean;
}

export const defaultColumns: TerminalColumnsState = Object.freeze({
  leftWidth: columnLimits.leftDefault,
  rightWidth: columnLimits.rightDefault,
  leftCollapsed: false,
  rightCollapsed: false,
});

function clampWidth(value: unknown, min: number, fallback: number): number {
  if (typeof value !== 'number' || !Number.isFinite(value)) return fallback;
  return Math.max(min, Math.min(columnLimits.sideMax, Math.round(value)));
}

/** Stored JSON to a valid state; anything malformed falls back field by field to the defaults. */
export function parseColumns(raw: string | null): TerminalColumnsState {
  if (!raw) return defaultColumns;
  let value: unknown;
  try { value = JSON.parse(raw); } catch { return defaultColumns; }
  if (typeof value !== 'object' || value === null || Array.isArray(value)) return defaultColumns;
  const record = value as Record<string, unknown>;
  return {
    leftWidth: clampWidth(record.leftWidth, columnLimits.leftMin, columnLimits.leftDefault),
    rightWidth: clampWidth(record.rightWidth, columnLimits.rightMin, columnLimits.rightDefault),
    leftCollapsed: record.leftCollapsed === true,
    rightCollapsed: record.rightCollapsed === true,
  };
}

let cached: TerminalColumnsState | null = null;
const listeners = new Set<() => void>();

function read(): TerminalColumnsState {
  if (cached) return cached;
  let raw: string | null = null;
  try { raw = window.localStorage.getItem(terminalColumnsStorageKey); } catch { raw = null; }
  cached = parseColumns(raw);
  return cached;
}

function write(next: TerminalColumnsState) {
  cached = next;
  try { window.localStorage.setItem(terminalColumnsStorageKey, JSON.stringify(next)); } catch { /* storage blocked: the state still applies for this page */ }
  for (const listener of listeners) listener();
}

function update(patch: (current: TerminalColumnsState) => Partial<TerminalColumnsState>) {
  const current = read();
  const next = parseColumns(JSON.stringify({ ...current, ...patch(current) }));
  if (next.leftWidth === current.leftWidth && next.rightWidth === current.rightWidth && next.leftCollapsed === current.leftCollapsed && next.rightCollapsed === current.rightCollapsed) return;
  write(next);
}

/** Back to the default widths with both side columns open. */
export function resetTerminalColumns(): void {
  write(defaultColumns);
}

/** Restores the default layout whenever another terminal widget dispatches `sidiora:terminal-reset-columns`. */
export function useResetColumnsEvent(): void {
  useEffect(() => {
    const reset = () => resetTerminalColumns();
    window.addEventListener(terminalResetColumnsEvent, reset);
    return () => window.removeEventListener(terminalResetColumnsEvent, reset);
  }, []);
}

function subscribe(listener: () => void) {
  listeners.add(listener);
  const onStorage = (event: StorageEvent) => {
    if (event.key !== null && event.key !== terminalColumnsStorageKey) return;
    cached = null;
    listener();
  };
  window.addEventListener('storage', onStorage);
  return () => {
    listeners.delete(listener);
    window.removeEventListener('storage', onStorage);
  };
}

const serverSnapshot = () => defaultColumns;

export interface TerminalColumns extends TerminalColumnsState {
  setLeftWidth(width: number): void;
  setRightWidth(width: number): void;
  toggleLeft(): void;
  toggleRight(): void;
}

/** Side column widths and collapsed flags, persisted per device in localStorage `sidiora-terminal-columns`. */
export function useTerminalColumns(): TerminalColumns {
  const state = useSyncExternalStore(subscribe, read, serverSnapshot);
  const setLeftWidth = useCallback((width: number) => update(() => ({ leftWidth: width })), []);
  const setRightWidth = useCallback((width: number) => update(() => ({ rightWidth: width })), []);
  const toggleLeft = useCallback(() => update(current => ({ leftCollapsed: !current.leftCollapsed })), []);
  const toggleRight = useCallback(() => update(current => ({ rightCollapsed: !current.rightCollapsed })), []);
  return { ...state, setLeftWidth, setRightWidth, toggleLeft, toggleRight };
}

export interface SideInput { present: boolean; width: number; collapsed: boolean; min: number }
export interface SideLayout { mode: 'absent' | 'open' | 'collapsed'; width: number; max: number; autoCollapsed: boolean }
export interface ColumnLayout { left: SideLayout; right: SideLayout }

/**
 * Effective column widths for a container width. Stored widths shrink toward
 * their minimums when the centre would drop below its 560 minimum; if even
 * the minimums do not fit, the right column and then the left one collapse
 * to their strips for this render only (the stored preference is untouched).
 */
export function layoutColumns(container: number, left: SideInput, right: SideInput): ColumnLayout {
  const sides: Array<{ key: 'left' | 'right'; input: SideInput }> = [{ key: 'left', input: left }, { key: 'right', input: right }];
  const present = sides.filter(side => side.input.present);
  const available = Math.max(0, container - columnLimits.gutter * 2 - columnLimits.gutter * present.length);
  const mode: Record<'left' | 'right', SideLayout['mode']> = {
    left: left.present ? (left.collapsed ? 'collapsed' : 'open') : 'absent',
    right: right.present ? (right.collapsed ? 'collapsed' : 'open') : 'absent',
  };
  const auto: Record<'left' | 'right', boolean> = { left: false, right: false };
  const width: Record<'left' | 'right', number> = {
    left: mode.left === 'open' ? Math.max(left.min, left.width) : 0,
    right: mode.right === 'open' ? Math.max(right.min, right.width) : 0,
  };
  const used = (key: 'left' | 'right') => mode[key] === 'open' ? width[key] : mode[key] === 'collapsed' ? columnLimits.collapsed : 0;
  const overflow = () => used('left') + used('right') + columnLimits.centerMin - available;

  for (const key of ['right', 'left'] as const) {
    const excess = overflow();
    if (excess <= 0) break;
    if (mode[key] !== 'open') continue;
    const min = key === 'left' ? left.min : right.min;
    width[key] = Math.max(min, width[key] - excess);
  }
  for (const key of ['right', 'left'] as const) {
    if (overflow() <= 0) break;
    if (mode[key] !== 'open') continue;
    mode[key] = 'collapsed';
    auto[key] = true;
  }

  const result = (key: 'left' | 'right', input: SideInput): SideLayout => {
    const other = key === 'left' ? used('right') : used('left');
    const max = Math.max(input.min, Math.min(columnLimits.sideMax, available - columnLimits.centerMin - other));
    return { mode: mode[key], width: mode[key] === 'open' ? width[key] : mode[key] === 'collapsed' ? columnLimits.collapsed : 0, max, autoCollapsed: auto[key] };
  };
  return { left: result('left', left), right: result('right', right) };
}
