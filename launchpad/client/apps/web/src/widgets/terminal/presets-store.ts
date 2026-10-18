'use client';
import { useCallback, useSyncExternalStore } from 'react';

export type PresetId = 1 | 2 | 3;
export interface TradePreset {
  buyAmounts: [string, string, string, string];
  sellPercents: [number, number, number, number];
  slippageBps: number;
}
export interface PresetsState { active: PresetId; presets: Record<PresetId, TradePreset> }

export const presetsStorageKey = 'sidiora-terminal-presets';
export const presetIds: readonly PresetId[] = [1, 2, 3];
export const minSlippageBps = 1;
export const maxSlippageBps = 5000;

const defaults: Record<PresetId, TradePreset> = {
  1: { buyAmounts: ['10', '25', '50', '100'], sellPercents: [10, 25, 50, 100], slippageBps: 100 },
  2: { buyAmounts: ['50', '100', '250', '500'], sellPercents: [25, 50, 75, 100], slippageBps: 300 },
  3: { buyAmounts: ['250', '500', '1000', '2500'], sellPercents: [25, 50, 75, 100], slippageBps: 500 },
};

export function defaultPresets(): PresetsState {
  return { active: 1, presets: { 1: clonePreset(defaults[1]), 2: clonePreset(defaults[2]), 3: clonePreset(defaults[3]) } };
}

function clonePreset(preset: TradePreset): TradePreset {
  return { buyAmounts: [...preset.buyAmounts], sellPercents: [...preset.sellPercents], slippageBps: preset.slippageBps };
}

/** A positive plain decimal USDL amount with at most 6 fraction digits. */
export function validBuyAmount(value: string): boolean {
  return /^(0|[1-9]\d{0,11})(\.\d{1,6})?$/.test(value) && /[1-9]/.test(value);
}
export function validSellPercent(value: number): boolean {
  return Number.isInteger(value) && value >= 1 && value <= 100;
}
export function validSlippage(value: number): boolean {
  return Number.isInteger(value) && value >= minSlippageBps && value <= maxSlippageBps;
}

function parsePreset(value: unknown, fallback: TradePreset): TradePreset {
  if (typeof value !== 'object' || value === null) return clonePreset(fallback);
  const row = value as Record<string, unknown>;
  const buys = Array.isArray(row.buyAmounts) && row.buyAmounts.length === 4 && row.buyAmounts.every(item => typeof item === 'string' && validBuyAmount(item))
    ? row.buyAmounts as TradePreset['buyAmounts'] : fallback.buyAmounts;
  const sells = Array.isArray(row.sellPercents) && row.sellPercents.length === 4 && row.sellPercents.every(item => typeof item === 'number' && validSellPercent(item))
    ? row.sellPercents as TradePreset['sellPercents'] : fallback.sellPercents;
  const slippage = typeof row.slippageBps === 'number' && validSlippage(row.slippageBps) ? row.slippageBps : fallback.slippageBps;
  return { buyAmounts: [...buys], sellPercents: [...sells], slippageBps: slippage };
}

/** Parse the persisted JSON; any malformed field falls back to its default, field by field. */
export function parsePresets(raw: string | null): PresetsState {
  if (!raw) return defaultPresets();
  let value: unknown;
  try { value = JSON.parse(raw); } catch { return defaultPresets(); }
  if (typeof value !== 'object' || value === null) return defaultPresets();
  const row = value as Record<string, unknown>;
  const stored = typeof row.presets === 'object' && row.presets !== null ? row.presets as Record<string, unknown> : {};
  const active = row.active === 1 || row.active === 2 || row.active === 3 ? row.active : 1;
  return { active, presets: { 1: parsePreset(stored['1'], defaults[1]), 2: parsePreset(stored['2'], defaults[2]), 3: parsePreset(stored['3'], defaults[3]) } };
}

/** Apply a partial edit; invalid values are rejected (the previous state is returned unchanged). */
export function applyPresetUpdate(state: PresetsState, id: PresetId, patch: Partial<TradePreset>): PresetsState {
  const current = state.presets[id];
  const next: TradePreset = {
    buyAmounts: patch.buyAmounts ? [...patch.buyAmounts] : current.buyAmounts,
    sellPercents: patch.sellPercents ? [...patch.sellPercents] : current.sellPercents,
    slippageBps: patch.slippageBps ?? current.slippageBps,
  };
  if (next.buyAmounts.length !== 4 || !next.buyAmounts.every(validBuyAmount)) return state;
  if (next.sellPercents.length !== 4 || !next.sellPercents.every(validSellPercent)) return state;
  if (!validSlippage(next.slippageBps)) return state;
  return { ...state, presets: { ...state.presets, [id]: next } };
}

const listeners = new Set<() => void>();
let cache: { raw: string | null; state: PresetsState } | null = null;
const serverState = defaultPresets();

function readRaw(): string | null {
  try { return window.localStorage.getItem(presetsStorageKey); } catch { return null; }
}

function snapshot(): PresetsState {
  const raw = readRaw();
  if (!cache || cache.raw !== raw) cache = { raw, state: parsePresets(raw) };
  return cache.state;
}

function write(state: PresetsState) {
  const raw = JSON.stringify(state);
  try { window.localStorage.setItem(presetsStorageKey, raw); } catch { /* storage blocked: keep the in-memory value */ }
  cache = { raw: readRaw() ?? raw, state };
  for (const listener of listeners) listener();
}

function subscribe(listener: () => void) {
  listeners.add(listener);
  const onStorage = (event: StorageEvent) => { if (event.key === null || event.key === presetsStorageKey) listener(); };
  window.addEventListener('storage', onStorage);
  return () => { listeners.delete(listener); window.removeEventListener('storage', onStorage); };
}

/** Per-device trade presets P1 to P3 (localStorage `sidiora-terminal-presets`). */
export function useTradePresets() {
  const state = useSyncExternalStore(subscribe, snapshot, () => serverState);
  const setActive = useCallback((id: PresetId) => { const current = snapshot(); if (current.active !== id) write({ ...current, active: id }); }, []);
  const updatePreset = useCallback((id: PresetId, patch: Partial<TradePreset>) => {
    const current = snapshot();
    const next = applyPresetUpdate(current, id, patch);
    if (next !== current) write(next);
    return next !== current;
  }, []);
  return { active: state.active, presets: state.presets, setActive, updatePreset };
}
