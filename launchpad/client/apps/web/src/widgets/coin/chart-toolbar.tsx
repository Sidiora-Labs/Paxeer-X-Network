'use client';
import { useId } from 'react';
import { Info } from 'lucide-react';
import { AvatarStack, Button, DropdownMenu, humanErrorText, SegmentedControl, Toggle, Tooltip } from '@/ui/primitives';
import type { ChartFreshness, ChartMode } from '@/widgets/trading/udf-datafeed';
import { cn } from '@/lib/utils';
import { CHART_INTERVALS, type ChartInterval } from './use-chart-settings';

export interface ChartMarkerActor { wallet: string; name: string; avatarUrl: string | null }

export interface ChartToolbarProps {
  interval: ChartInterval;
  onIntervalChange: (interval: ChartInterval) => void;
  mode: ChartMode;
  onModeChange: (mode: ChartMode) => void;
  /** Set when market cap cannot be charted; the toggle is disabled and the reason is shown. */
  mcapDisabledReason: string | null;
  markers: boolean;
  onMarkersChange: (enabled: boolean) => void;
  markerActors: readonly ChartMarkerActor[];
  freshness: ChartFreshness;
  onRetry: () => void;
  className?: string;
}

export function freshnessLabel(status: ChartFreshness['connectionStatus']): string {
  switch (status) {
    case 'live': return 'Live';
    case 'offline': return 'Updates paused';
    case 'connecting':
    case 'snapshotting':
    case 'replaying': return 'Syncing...';
    case 'closed': return 'Chart closed';
    default: return 'May be out of date';
  }
}

export function ChartToolbar({ interval, onIntervalChange, mode, onModeChange, mcapDisabledReason, markers, onMarkersChange, markerActors, freshness, onRetry, className }: ChartToolbarProps) {
  const markersId = useId();
  const primary = CHART_INTERVALS.filter(option => option.primary);
  const secondary = CHART_INTERVALS.filter(option => !option.primary);
  const activeSecondary = secondary.find(option => option.value === interval);
  const segments = [...primary, ...(activeSecondary ? [activeSecondary] : [])].map(option => ({ value: option.value, label: option.label, ariaLabel: option.description }));
  const mcapDisabled = mcapDisabledReason !== null;
  const live = freshness.connectionStatus === 'live';

  return (
    <div className={cn('flex min-h-11 flex-wrap items-center gap-x-3 gap-y-2 bg-surface-1 px-3 py-2', className)}>
      <div className="flex items-center gap-1">
        <SegmentedControl label="Candle interval" options={segments} value={interval} onChange={onIntervalChange} shape="sm" />
        <DropdownMenu
          label="More intervals"
          size="sm"
          align="start"
          items={secondary.map(option => ({ id: option.value, label: `${option.label} (${option.description})`, selected: option.value === interval, onSelect: () => onIntervalChange(option.value) }))}
        />
      </div>

      <div className="flex items-center gap-1">
        <SegmentedControl<ChartMode>
          label="Chart value"
          options={[{ value: 'price', label: 'Price' }, { value: 'mcap', label: 'MCap', disabled: mcapDisabled }]}
          value={mode}
          onChange={onModeChange}
          shape="sm"
        />
        {mcapDisabled ? (
          <Tooltip content={mcapDisabledReason}>
            <span tabIndex={0} role="img" aria-label={`Market cap not available yet: ${mcapDisabledReason}`} className="inline-flex size-control-height-sm items-center justify-center rounded-sm text-text-faint hover:text-text">
              <Info aria-hidden="true" className="size-icon-sm" />
            </span>
          </Tooltip>
        ) : null}
      </div>

      <div className="flex items-center gap-2">
        <span id={markersId} className="text-sm font-bold text-text-muted">Trade markers</span>
        <Toggle checked={markers} onChange={onMarkersChange} labelledBy={markersId} size="sm" />
        {markers && markerActors.length > 0 ? (
          <AvatarStack
            items={markerActors.map(actor => ({ src: actor.avatarUrl, name: actor.name }))}
            size="xs"
            max={5}
            surface={1}
            label={`${markerActors.length} ${markerActors.length === 1 ? 'trader' : 'traders'} marked on the chart`}
          />
        ) : null}
      </div>

      <span title="Prices are shown in USDL" className="rounded-sm bg-surface-2 px-2 py-0.5 text-2xs font-bold uppercase tracking-[.04em] text-text-muted">USDL</span>

      <div role="status" aria-live="polite" className="ml-auto flex items-center gap-2 text-xs text-text-faint">
        <span className={cn('inline-flex items-center gap-1.5', live && 'text-success')}>
          <span aria-hidden="true" className={cn('size-1.5 rounded-pill', live ? 'bg-success' : freshness.connectionStatus === 'offline' ? 'bg-danger' : 'bg-text-faint')} />
          {freshnessLabel(freshness.connectionStatus)}
        </span>
        <span data-numeric className="hidden tabular-nums sm:inline">Block {freshness.lastSourceHeight ?? '—'}</span>
        {freshness.lagReason ? <span className="sr-only">{humanErrorText(freshness.lagReason, 'Live updates are paused')}</span> : null}
        {!live && freshness.connectionStatus !== 'closed' ? <Button variant="ghost" size="sm" onClick={onRetry}>Try again</Button> : null}
      </div>
    </div>
  );
}
