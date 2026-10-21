'use client';
import { useId } from 'react';
import { Search } from 'lucide-react';
import { Chip, Tooltip } from '@/ui/primitives';

export type HolderFilter = 'all' | 'in-profit' | 'at-a-loss';
export interface HolderSearch { query: string; filter: HolderFilter }

const pnlFilters: ReadonlyArray<{ id: Exclude<HolderFilter, 'all'>; label: string }> = [
  { id: 'in-profit', label: 'In profit' },
  { id: 'at-a-loss', label: 'At a loss' },
];

export default function HoldersFilters({ value, onChange, pnlReason }: { value: HolderSearch; onChange: (next: HolderSearch) => void; pnlReason: string }) {
  const inputId = useId();
  return <div className="flex flex-wrap items-center gap-2">
    <label htmlFor={inputId} className="sr-only">Search holders</label>
    <div className="flex h-control-height-sm min-w-0 flex-1 basis-56 items-center gap-2 rounded-sm bg-surface-2 px-3 focus-within:outline-2 focus-within:outline-focus">
      <Search aria-hidden="true" className="size-icon-sm shrink-0 text-text-faint" />
      <input
        id={inputId}
        type="search"
        value={value.query}
        onChange={event => onChange({ ...value, query: event.target.value })}
        placeholder="Search holder or wallet"
        autoComplete="off"
        spellCheck={false}
        className="min-w-0 flex-1 bg-transparent text-sm text-text outline-none placeholder:text-text-faint"
      />
    </div>
    <div role="group" aria-label="Profit filters" className="flex items-center gap-2">
      <Chip pressed={value.filter === 'all'} onClick={() => onChange({ ...value, filter: 'all' })}>All</Chip>
      {pnlFilters.map(filter => (
        <Tooltip key={filter.id} content={pnlReason}>
          <Chip pressed={false} aria-disabled="true" onClick={() => undefined} className="cursor-not-allowed text-text-disabled hover:bg-surface-2 hover:text-text-disabled">{filter.label}</Chip>
        </Tooltip>
      ))}
    </div>
    <p className="basis-full text-xs text-text-faint">Profit filters are coming soon.</p>
  </div>;
}
