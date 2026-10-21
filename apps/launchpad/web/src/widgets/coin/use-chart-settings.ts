'use client';
import { useCallback, useSyncExternalStore } from 'react';
import type { ChartMode } from '@/widgets/trading/udf-datafeed';

export type ChartInterval = '1' | '5' | '15' | '60' | '240' | '1D' | '1W';

export interface ChartIntervalOption { value: ChartInterval; label: string; description: string; primary: boolean }

/** Toolbar order: the primary five sit inline, the rest live behind the caret. */
export const CHART_INTERVALS: readonly ChartIntervalOption[] = [
  { value: '1', label: '1m', description: '1 minute', primary: true },
  { value: '5', label: '5m', description: '5 minutes', primary: true },
  { value: '15', label: '15m', description: '15 minutes', primary: false },
  { value: '60', label: '1h', description: '1 hour', primary: true },
  { value: '240', label: '4h', description: '4 hours', primary: true },
  { value: '1D', label: 'D', description: '1 day', primary: true },
  { value: '1W', label: '1W', description: '1 week', primary: false },
];

export const CHART_MIN_HEIGHT = 320;
export const CHART_DEFAULT_HEIGHT = 380;
export const CHART_MAX_HEIGHT = 960;

export interface ChartSettings {
  height: number;
  interval: ChartInterval;
  mode: ChartMode;
  markers: boolean;
}

export const defaultChartSettings: ChartSettings = { height: CHART_DEFAULT_HEIGHT, interval: '5', mode: 'price', markers: true };

export const CHART_SETTINGS_KEY = 'sidiora_chart_settings_v1';

export function isChartInterval(value: unknown): value is ChartInterval {
  return typeof value === 'string' && CHART_INTERVALS.some(option => option.value === value);
}

export function clampChartHeight(value: unknown): number {
  const numeric = typeof value === 'number' ? value : typeof value === 'string' && value.trim() !== '' ? Number(value) : NaN;
  if (!Number.isFinite(numeric)) return CHART_DEFAULT_HEIGHT;
  return Math.max(CHART_MIN_HEIGHT, Math.min(CHART_MAX_HEIGHT, Math.round(numeric)));
}

/** Tolerant decode: every field falls back on its own, so one corrupt value never resets the rest. */
export function parseChartSettings(raw: string | null | undefined): ChartSettings {
  if (!raw) return { ...defaultChartSettings };
  let value: unknown;
  try { value = JSON.parse(raw); } catch { return { ...defaultChartSettings }; }
  if (value === null || typeof value !== 'object' || Array.isArray(value)) return { ...defaultChartSettings };
  const record = value as Record<string, unknown>;
  return {
    height: 'height' in record ? clampChartHeight(record.height) : CHART_DEFAULT_HEIGHT,
    interval: isChartInterval(record.interval) ? record.interval : defaultChartSettings.interval,
    mode: record.mode === 'mcap' || record.mode === 'price' ? record.mode : defaultChartSettings.mode,
    markers: typeof record.markers === 'boolean' ? record.markers : defaultChartSettings.markers,
  };
}

export function serializeChartSettings(settings: ChartSettings): string {
  return JSON.stringify({ height: clampChartHeight(settings.height), interval: settings.interval, mode: settings.mode, markers: settings.markers });
}

let cached: ChartSettings | null = null;
const listeners = new Set<() => void>();
let storageBound = false;

function storage(): Storage | null {
  if (typeof window === 'undefined') return null;
  try { return window.localStorage; } catch { return null; }
}

function notify() { for (const listener of listeners) listener(); }

function onStorage(event: StorageEvent) {
  if (event.key !== null && event.key !== CHART_SETTINGS_KEY) return;
  cached = null;
  notify();
}

export function chartSettings(): ChartSettings {
  if (cached) return cached;
  const store = storage();
  let raw: string | null = null;
  if (store) { try { raw = store.getItem(CHART_SETTINGS_KEY); } catch { raw = null; } }
  cached = parseChartSettings(raw);
  return cached;
}

export function updateChartSettings(patch: Partial<ChartSettings>): ChartSettings {
  const next: ChartSettings = { ...chartSettings(), ...patch, height: clampChartHeight(patch.height ?? chartSettings().height) };
  cached = next;
  const store = storage();
  if (store) { try { store.setItem(CHART_SETTINGS_KEY, serializeChartSettings(next)); } catch {} }
  notify();
  return next;
}

export function resetChartSettingsCache() { cached = null; }

function subscribe(listener: () => void) {
  listeners.add(listener);
  if (!storageBound && typeof window !== 'undefined') { window.addEventListener('storage', onStorage); storageBound = true; }
  return () => {
    listeners.delete(listener);
    if (listeners.size === 0 && storageBound && typeof window !== 'undefined') { window.removeEventListener('storage', onStorage); storageBound = false; }
  };
}

const serverSnapshot = () => defaultChartSettings;

export function useChartSettings(): { settings: ChartSettings; update: (patch: Partial<ChartSettings>) => void } {
  const settings = useSyncExternalStore(subscribe, chartSettings, serverSnapshot);
  const update = useCallback((patch: Partial<ChartSettings>) => { updateChartSettings(patch); }, []);
  return { settings, update };
}
