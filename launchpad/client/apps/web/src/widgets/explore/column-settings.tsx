'use client';
import { useCallback, useEffect, useId, useState } from 'react';
import { Button, SegmentedControl, Toggle } from '@/ui/primitives';
import type { SortKey } from './sort';

export type ColumnId = 'coin' | 'graph' | 'mcap' | 'ath' | 'age' | 'txns' | 'volume24h' | 'traders' | 'change1h' | 'change6h' | 'change24h' | 'star';
export type OptionalColumn = Exclude<ColumnId, 'coin' | 'mcap' | 'star'>;
export type ColumnVisibility = Record<OptionalColumn, boolean>;
export type Density = 'dense' | 'default' | 'comfortable';
export type ViewMode = 'table' | 'grid';

export interface ColumnSpec {
  id: ColumnId;
  label: string;
  /** Spoken suffix that the compact header omits, e.g. "in the last 24 hours". */
  hint?: string;
  sortKey?: SortKey;
  align: 'left' | 'right' | 'center';
  width: number | string;
  /** Tailwind classes hiding the column below a breakpoint. Every other column stays and the table scrolls inside its card. */
  responsive: string;
}

export const columnSpecs: readonly ColumnSpec[] = Object.freeze([
  // The rank sits inside the coin cell (pump.fun anatomy), so there is no separate "#" column.
  { id: 'coin', label: 'Coin', align: 'left', width: 'auto', responsive: '' },
  { id: 'graph', label: 'Graph', hint: '24 hour price', align: 'left', width: 112, responsive: '' },
  { id: 'mcap', label: 'MCAP', hint: 'Market cap', sortKey: 'mcap', align: 'right', width: 110, responsive: '' },
  { id: 'ath', label: 'ATH', hint: 'All-time-high market cap', align: 'left', width: 120, responsive: '' },
  { id: 'age', label: 'Age', sortKey: 'age', align: 'right', width: 70, responsive: '' },
  { id: 'txns', label: 'TXNS', hint: 'Transactions in the last 24 hours', sortKey: 'txns', align: 'right', width: 90, responsive: '' },
  { id: 'volume24h', label: '24H VOL', hint: 'Volume in the last 24 hours', sortKey: 'volume24h', align: 'right', width: 110, responsive: '' },
  { id: 'traders', label: 'Traders', hint: 'Unique traders in the last 24 hours', sortKey: 'traders', align: 'right', width: 90, responsive: '' },
  { id: 'change1h', label: '1H', hint: 'Price change over 1 hour', sortKey: 'change1h', align: 'right', width: 90, responsive: '' },
  { id: 'change6h', label: '6H', hint: 'Price change over 6 hours', sortKey: 'change6h', align: 'right', width: 90, responsive: '' },
  { id: 'change24h', label: '24H', hint: 'Price change over 24 hours', sortKey: 'change24h', align: 'right', width: 90, responsive: '' },
  { id: 'star', label: 'Watchlist', align: 'center', width: 44, responsive: '' },
]);

const optionalColumns: readonly OptionalColumn[] = Object.freeze(['graph', 'ath', 'age', 'txns', 'volume24h', 'traders', 'change1h', 'change6h', 'change24h']);

export interface ExploreSettings {
  columns: ColumnVisibility;
  density: Density;
  view: ViewMode;
}

export const defaultSettings: ExploreSettings = Object.freeze({
  columns: Object.freeze({ graph: true, ath: true, age: true, txns: true, volume24h: true, traders: true, change1h: true, change6h: true, change24h: true }),
  density: 'default',
  view: 'table',
});

const storageKey = 'sidiora.explore.settings.v1';
const densities: readonly Density[] = ['dense', 'default', 'comfortable'];

/** Accept only well-formed stored settings; anything else falls back to the defaults field by field. */
function parseSettings(value: unknown): ExploreSettings {
  if (typeof value !== 'object' || value === null) return defaultSettings;
  const record = value as Record<string, unknown>;
  const columns = { ...defaultSettings.columns };
  if (typeof record.columns === 'object' && record.columns !== null) {
    for (const column of optionalColumns) {
      const flag = (record.columns as Record<string, unknown>)[column];
      if (typeof flag === 'boolean') columns[column] = flag;
    }
  }
  const density = densities.includes(record.density as Density) ? (record.density as Density) : defaultSettings.density;
  const view = record.view === 'grid' || record.view === 'table' ? record.view : defaultSettings.view;
  return { columns, density, view };
}

/** Per-device explore preferences (the preferences API has no fields for them yet). */
export function useExploreSettings(): [ExploreSettings, (patch: Partial<ExploreSettings>) => void] {
  const [settings, setSettings] = useState<ExploreSettings>(defaultSettings);
  useEffect(() => {
    try {
      const stored = window.localStorage.getItem(storageKey);
      if (stored) setSettings(parseSettings(JSON.parse(stored)));
    } catch {
      setSettings(defaultSettings);
    }
  }, []);
  const update = useCallback((patch: Partial<ExploreSettings>) => {
    setSettings(current => {
      const next = { ...current, ...patch, columns: { ...current.columns, ...(patch.columns ?? {}) } };
      try { window.localStorage.setItem(storageKey, JSON.stringify(next)); } catch { /* per-device convenience only */ }
      return next;
    });
  }, []);
  return [settings, update];
}

export const densityRowClass: Record<Density, string> = {
  dense: 'h-table-row-dense',
  default: 'h-table-row',
  comfortable: 'h-table-row-comfortable',
};

export interface ColumnSettingsProps {
  id: string;
  settings: ExploreSettings;
  onChange: (patch: Partial<ExploreSettings>) => void;
  onClose: () => void;
}

export function ColumnSettings({ id, settings, onChange, onClose }: ColumnSettingsProps) {
  const headingId = useId();
  const specs = columnSpecs.filter((spec): spec is ColumnSpec & { id: OptionalColumn } => (optionalColumns as readonly string[]).includes(spec.id));
  return (
    <section id={id} aria-labelledby={headingId} className="flex flex-col gap-4 rounded-md bg-surface-2 p-4">
      <div className="flex items-start justify-between gap-3">
        <div>
          <h2 id={headingId} className="text-md font-bold text-text">Columns</h2>
          <p className="text-xs text-text-faint">Saved on this device</p>
        </div>
        <Button variant="ghost" size="sm" onClick={onClose}>Done</Button>
      </div>
      <ul className="grid gap-2 sm:grid-cols-2 lg:grid-cols-3">
        {specs.map(spec => {
          const labelId = `${id}-${spec.id}-label`;
          return (
            <li key={spec.id} className="flex items-center justify-between gap-3 rounded-sm bg-surface-3 px-3 py-2">
              <span id={labelId} className="text-sm font-bold text-text">
                {spec.label}
                {spec.hint ? <span className="ml-1 font-semibold text-text-faint">{spec.hint}</span> : null}
              </span>
              <Toggle size="sm" checked={settings.columns[spec.id]} labelledBy={labelId} onChange={checked => onChange({ columns: { ...settings.columns, [spec.id]: checked } })} />
            </li>
          );
        })}
      </ul>
      <div className="flex flex-wrap items-center justify-between gap-3">
        <SegmentedControl<ViewMode>
          label="Layout"
          value={settings.view}
          onChange={view => onChange({ view })}
          className="md:hidden"
          options={[{ value: 'table', label: 'Table' }, { value: 'grid', label: 'Grid' }]}
        />
        <SegmentedControl<Density>
          label="Row density"
          value={settings.density}
          onChange={density => onChange({ density })}
          options={[{ value: 'dense', label: 'Dense' }, { value: 'default', label: 'Default' }, { value: 'comfortable', label: 'Comfortable' }]}
        />
        <Button variant="secondary" size="sm" onClick={() => onChange({ columns: { ...defaultSettings.columns }, density: defaultSettings.density })}>Reset columns</Button>
      </div>
    </section>
  );
}
