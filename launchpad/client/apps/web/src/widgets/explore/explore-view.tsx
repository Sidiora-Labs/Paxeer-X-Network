'use client';
import { useCallback, useEffect, useId, useMemo, useState, type ReactNode } from 'react';
import { SlidersHorizontal } from 'lucide-react';
import { useWatchlist } from '@/hooks/ui/use-watchlist';
import { Button, EmptyState } from '@/ui/primitives';
import { ColumnSettings, useExploreSettings } from './column-settings';
import { ExploreGrid } from './explore-grid';
import { ExploreTable } from './explore-table';
import { ExploreToolbar } from './explore-toolbar';
import { FilterPanel } from './filter-panel';
import { activeFilterCount, applyFilters, cycleSort, emptyFilters, sortDisclosure, sortRows, type ExploreFilters, type SortKey, type SortState } from './sort';
import type { ExploreRow } from './use-explore-rows';

export interface ExploreViewProps {
  rows: ExploreRow[];
  loading: boolean;
  hasMore: boolean;
  loadingMore: boolean;
  moreError: Error | null;
  onLoadMore: () => void;
  onRetryMore: () => void;
  /** Order the preset applies on top of the server rank; null keeps the rank. */
  defaultSort: SortState;
  /** Changing this resets the user's header sort (preset switch). */
  sortResetKey: string;
  /** Explains where the rows come from in the "sorted within loaded rows" disclosure. */
  sourceLabel: string;
  leading?: ReactNode;
  empty: ReactNode;
  caption: string;
}

export function ExploreView({ rows, loading, hasMore, loadingMore, moreError, onLoadMore, onRetryMore, defaultSort, sortResetKey, sourceLabel, leading, empty, caption }: ExploreViewProps) {
  const [settings, updateSettings] = useExploreSettings();
  const [filterOpen, setFilterOpen] = useState(false);
  const [settingsOpen, setSettingsOpen] = useState(false);
  const [filters, setFilters] = useState<ExploreFilters>({ ...emptyFilters });
  const [userSort, setUserSort] = useState<SortState>(null);
  const watchlist = useWatchlist();
  const filterPanelId = useId();
  const settingsPanelId = useId();

  useEffect(() => { setUserSort(null); }, [sortResetKey]);

  const sort = userSort ?? defaultSort;
  const onSort = useCallback((key: SortKey) => {
    setUserSort(current => cycleSort(current ?? defaultSort, key));
  }, [defaultSort]);

  const filtered = useMemo(() => applyFilters(rows, filters), [rows, filters]);
  const sorted = useMemo(() => sortRows(filtered, sort), [filtered, sort]);
  const filterCount = activeFilterCount(filters);
  const disclosure = sortDisclosure(sort, rows.length, filtered.length, filterCount, sourceLabel);
  const loadMore = moreError ? onRetryMore : onLoadMore;
  const isStarred = watchlist.check;
  const toggleStar = watchlist.toggle;
  const emptyView = rows.length > 0 && filtered.length === 0 && filterCount > 0 ? (
    <EmptyState
      variant="panel"
      icon={SlidersHorizontal}
      title="No coins match these filters"
      body="Try changing or clearing your filters"
      action={<Button variant="secondary" size="sm" onClick={() => setFilters({ ...emptyFilters })}>Clear filters</Button>}
      announce
    />
  ) : empty;

  return (
    <div className="flex flex-col gap-4">
      <ExploreToolbar
        leading={leading}
        filterOpen={filterOpen}
        filterCount={filterCount}
        filterPanelId={filterPanelId}
        onToggleFilter={() => setFilterOpen(open => !open)}
        view={settings.view}
        onViewChange={view => updateSettings({ view })}
        settingsOpen={settingsOpen}
        settingsPanelId={settingsPanelId}
        onToggleSettings={() => setSettingsOpen(open => !open)}
      />
      {filterOpen ? (
        <FilterPanel id={filterPanelId} filters={filters} onChange={setFilters} loadedCount={rows.length} matchCount={filtered.length} onClose={() => setFilterOpen(false)} />
      ) : null}
      {settingsOpen ? (
        <ColumnSettings id={settingsPanelId} settings={settings} onChange={updateSettings} onClose={() => setSettingsOpen(false)} />
      ) : null}
      {settings.view === 'grid' ? (
        <ExploreGrid
          rows={sorted}
          isStarred={isStarred}
          onToggleStar={toggleStar}
          loading={loading}
          hasMore={hasMore}
          loadingMore={loadingMore}
          moreError={moreError !== null}
          onLoadMore={loadMore}
          empty={emptyView}
          disclosure={disclosure}
          label={caption}
        />
      ) : (
        <ExploreTable
          rows={sorted}
          sort={sort}
          onSort={onSort}
          columns={settings.columns}
          density={settings.density}
          isStarred={isStarred}
          onToggleStar={toggleStar}
          loading={loading}
          hasMore={hasMore}
          loadingMore={loadingMore}
          moreError={moreError !== null}
          onLoadMore={loadMore}
          empty={emptyView}
          disclosure={disclosure}
          caption={caption}
        />
      )}
    </div>
  );
}
