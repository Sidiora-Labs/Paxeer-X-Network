'use client';

import { FileDown, LayoutGrid, ListFilter, Table2 } from 'lucide-react';
import { DropdownMenu, IconButton, SegmentedControl, SelectPill, type MenuItem } from '@/ui/primitives';
import { UsdlBadge } from './holder-reward-token-card';
import { activeFilterCount, sortOptions, type HolderRewardsFilters, type HolderRewardsOrder, type HolderRewardsSortKey } from './holder-rewards-presentation';

export type HolderRewardsViewMode = 'table' | 'grid';

export interface HolderRewardsControlsProps {
  order: HolderRewardsOrder;
  onSortChange(key: HolderRewardsSortKey): void;
  view: HolderRewardsViewMode;
  onViewChange(view: HolderRewardsViewMode): void;
  filters: HolderRewardsFilters;
  onFiltersChange(filters: HolderRewardsFilters): void;
  /** Exports the rows currently shown; null disables the button (nothing loaded yet). */
  onExport: (() => void) | null;
}

export function filterMenuItems(filters: HolderRewardsFilters, onChange: (filters: HolderRewardsFilters) => void): MenuItem[] {
  return [
    { id: 'strategy-all', group: 'Fee strategy', label: 'All coins', selected: !filters.activeOnly, onSelect: () => onChange({ ...filters, activeOnly: false }) },
    { id: 'strategy-active', group: 'Fee strategy', label: 'Still paying holders', selected: filters.activeOnly, onSelect: () => onChange({ ...filters, activeOnly: true }) },
    { id: 'mcap-any', group: 'Market cap', label: 'Any', selected: !filters.pricedOnly, onSelect: () => onChange({ ...filters, pricedOnly: false }) },
    { id: 'mcap-priced', group: 'Market cap', label: 'Has a market cap', selected: filters.pricedOnly, onSelect: () => onChange({ ...filters, pricedOnly: true }) },
  ];
}

/** Sort and fixed USDL pairing on the left; view toggle, filters and CSV export on the right. Pills on canvas, no container. */
export default function HolderRewardsControls({ order, onSortChange, view, onViewChange, filters, onFiltersChange, onExport }: HolderRewardsControlsProps) {
  const active = activeFilterCount(filters);
  return (
    <div role="toolbar" aria-label="List controls" className="flex flex-wrap items-center gap-2">
      <SelectPill label="Sort by" showLabel options={sortOptions.map(option => ({ value: option.value, label: option.label }))} value={order.key} onChange={onSortChange} />
      <span className="inline-flex h-control-height-sm items-center gap-1.5 rounded-pill bg-surface-1 px-3 text-sm font-semibold text-text">
        <span className="text-text-faint">Paired with</span>
        <UsdlBadge />
      </span>
      <div className="ml-auto flex items-center gap-2">
        <SegmentedControl
          label="View"
          value={view}
          onChange={onViewChange}
          options={[
            { value: 'table', icon: <Table2 />, label: <span className="sr-only">Table</span>, ariaLabel: 'Table view' },
            { value: 'grid', icon: <LayoutGrid />, label: <span className="sr-only">Grid</span>, ariaLabel: 'Grid view' },
          ]}
        />
        <DropdownMenu
          label={active > 0 ? `Filters, ${active} active` : 'Filters'}
          menuLabel="Filters"
          align="end"
          size="sm"
          items={filterMenuItems(filters, onFiltersChange)}
          triggerClassName="rounded-pill bg-surface-1 px-3 text-text"
          trigger={(
            <>
              <ListFilter aria-hidden="true" className="size-icon-sm" />
              <span>Filters</span>
              {active > 0 ? <span data-numeric className="inline-flex size-4 items-center justify-center rounded-pill bg-accent text-2xs text-on-accent">{active}</span> : null}
            </>
          )}
        />
        <IconButton label="Download as CSV" icon={<FileDown />} variant="round" size="sm" disabled={onExport === null} onClick={onExport ?? undefined} />
      </div>
    </div>
  );
}
