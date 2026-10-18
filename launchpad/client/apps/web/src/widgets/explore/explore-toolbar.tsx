'use client';
import type { ReactNode } from 'react';
import { LayoutGrid, ListFilter, Rows3, Settings2 } from 'lucide-react';
import { cn } from '@/lib/utils';
import { SegmentedControl } from '@/ui/primitives';
import type { ViewMode } from './column-settings';

export interface ExploreToolbarProps {
  /** Left slot: preset tabs on /explore, a heading or count on /watchlist. */
  leading?: ReactNode;
  filterOpen: boolean;
  filterCount: number;
  filterPanelId: string;
  onToggleFilter: () => void;
  view: ViewMode;
  onViewChange: (view: ViewMode) => void;
  settingsOpen: boolean;
  settingsPanelId: string;
  onToggleSettings: () => void;
}

const toolClass = 'relative inline-flex size-control-height-md shrink-0 items-center justify-center rounded-sm bg-surface-1 text-text-muted transition-colors duration-fast hover:bg-surface-hover hover:text-text focus-visible:outline-2 focus-visible:outline-focus [&_svg]:size-icon-md';
const toolPressed = 'bg-surface-3 text-text';

/**
 * One row: presets on the left, then Filter, the Grid/Table switch and column settings on the right
 * (the pump.fun toolbar anatomy without its quote-coin stack and Quick buy input; USDL is the only quote).
 * Below md the Grid/Table switch moves into the column settings panel so the presets keep the row.
 */
export function ExploreToolbar({ leading, filterOpen, filterCount, filterPanelId, onToggleFilter, view, onViewChange, settingsOpen, settingsPanelId, onToggleSettings }: ExploreToolbarProps) {
  return (
    <div className="flex items-center gap-2">
      <div className="min-w-0 flex-1">{leading}</div>
      <div className="flex shrink-0 items-center gap-2">
        <button
          type="button"
          aria-label={filterCount > 0 ? `Filter, ${filterCount} active` : 'Filter'}
          title="Filter"
          aria-expanded={filterOpen}
          aria-controls={filterPanelId}
          aria-pressed={filterCount > 0 || undefined}
          onClick={onToggleFilter}
          className={cn(toolClass, (filterOpen || filterCount > 0) && toolPressed)}
        >
          <ListFilter aria-hidden="true" />
          {filterCount > 0 ? (
            <span aria-hidden="true" data-numeric className="absolute -right-1 -top-1 inline-flex h-4 min-w-4 items-center justify-center rounded-pill bg-accent px-1 text-2xs font-bold tabular-nums text-on-accent">{filterCount}</span>
          ) : null}
        </button>
        <SegmentedControl<ViewMode>
          label="Layout"
          value={view}
          onChange={onViewChange}
          size="md"
          shape="sm"
          className="hidden bg-surface-1 md:inline-flex"
          options={[
            { value: 'grid', label: <span className="hidden lg:inline">Grid</span>, icon: <LayoutGrid />, ariaLabel: 'Grid' },
            { value: 'table', label: <span className="hidden lg:inline">Table</span>, icon: <Rows3 />, ariaLabel: 'Table' },
          ]}
        />
        <button
          type="button"
          aria-label="Column settings"
          title="Column settings"
          aria-pressed={settingsOpen}
          aria-expanded={settingsOpen}
          aria-controls={settingsPanelId}
          onClick={onToggleSettings}
          className={cn(toolClass, settingsOpen && toolPressed)}
        >
          <Settings2 aria-hidden="true" />
        </button>
      </div>
    </div>
  );
}
