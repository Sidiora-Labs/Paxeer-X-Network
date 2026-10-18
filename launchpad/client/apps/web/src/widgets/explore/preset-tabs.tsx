'use client';
import type { LucideIcon } from 'lucide-react';
import { Activity, ChartColumn, ChevronDown, Coins, Flame, Rocket, Sparkles, Zap } from 'lucide-react';
import type { RankingCategory } from '@/core/market/use-rankings';
import Link from 'next/link';
import { cn } from '@/lib/utils';
import { DropdownMenu, type MenuItem } from '@/ui/primitives';
import type { SortState } from './sort';
import { useMediaQuery } from './use-visible';

export type PresetId = 'movers' | 'new' | 'trending' | 'volume' | 'breakout' | 'unusual' | 'mcap';

export interface Preset {
  id: PresetId;
  label: string;
  icon: LucideIcon;
  /** Ranking snapshot the preset reads (IF_RANKINGS category). */
  category: RankingCategory;
  /** Client-side order applied on top of the snapshot; null keeps the server rank. */
  defaultSort: SortState;
  description: string;
}

export const presets: readonly Preset[] = Object.freeze([
  { id: 'movers', label: 'Movers', icon: Rocket, category: 'movers', defaultSort: null, description: 'Biggest price moves right now' },
  { id: 'new', label: 'New', icon: Sparkles, category: 'new', defaultSort: null, description: 'Newest coins' },
  { id: 'trending', label: 'Trending', icon: Flame, category: 'trending', defaultSort: null, description: 'Hottest coins right now' },
  { id: 'volume', label: 'Volume', icon: ChartColumn, category: 'volume', defaultSort: null, description: 'Most traded coins' },
  { id: 'breakout', label: 'Breakout', icon: Zap, category: 'breakout', defaultSort: null, description: 'Coins breaking out' },
  { id: 'unusual', label: 'Unusual', icon: Activity, category: 'unusual', defaultSort: null, description: 'Coins with unusual activity' },
  { id: 'mcap', label: 'Market cap', icon: Coins, category: 'trending', defaultSort: { key: 'mcap', direction: 'desc' }, description: 'Trending coins by market cap' },
]);

const defaultPreset: Preset = presets[0];

export function presetFor(value: string | null | undefined): Preset {
  return presets.find(preset => preset.id === value) ?? defaultPreset;
}

export function presetHref(preset: Preset): string {
  return preset.id === defaultPreset.id ? '/explore' : `/explore?preset=${preset.id}`;
}

/**
 * Responsive slots inside the single segmented container: the first preset is always inline,
 * the next two join it from md, the rest from xl. Whatever is not inline lives in "More".
 */
export type PresetTier = 'always' | 'md' | 'xl';

export function presetTier(index: number): PresetTier {
  return index === 0 ? 'always' : index <= 2 ? 'md' : 'xl';
}

const inlineClass: Record<PresetTier, string> = {
  always: 'flex',
  md: 'hidden md:flex',
  xl: 'hidden xl:flex',
};

/** Presets that sit in the "More" menu at a viewport tier (narrow: below md, medium: md to xl, wide: xl and up). */
export function overflowPresets(viewport: 'narrow' | 'medium' | 'wide'): readonly Preset[] {
  return presets.filter((_, index) => {
    const tier = presetTier(index);
    if (tier === 'always') return false;
    if (viewport === 'wide') return false;
    if (viewport === 'medium') return tier === 'xl';
    return true;
  });
}

const segmentClass = 'inline-flex h-7 items-center gap-1.5 whitespace-nowrap rounded-sm px-2.5 text-sm font-semibold transition-colors duration-fast focus-visible:outline-2 focus-visible:outline-focus [&_svg]:size-icon-sm';
const segmentIdle = 'text-text-muted hover:bg-surface-hover hover:text-text';
const segmentActive = 'bg-accent text-on-accent';

/* When the active preset is folded into "More", the trigger carries its label and the active fill. */
const moreActiveClass: Record<Exclude<PresetTier, 'always'>, { fill: string; label: string; more: string }> = {
  md: { fill: 'bg-accent text-on-accent hover:bg-accent hover:text-on-accent md:bg-transparent md:text-text-muted md:hover:bg-surface-hover md:hover:text-text', label: 'md:hidden', more: 'hidden md:inline' },
  xl: { fill: 'bg-accent text-on-accent hover:bg-accent hover:text-on-accent xl:bg-transparent xl:text-text-muted xl:hover:bg-surface-hover xl:hover:text-text', label: 'xl:hidden', more: 'hidden xl:inline' },
};

export interface PresetTabsProps {
  active: PresetId;
  className?: string;
}

export function PresetTabs({ active, className }: PresetTabsProps) {
  const medium = useMediaQuery('(min-width: 768px)');
  const wide = useMediaQuery('(min-width: 1280px)');
  const viewport = wide ? 'wide' : medium ? 'medium' : 'narrow';
  const activeIndex = Math.max(0, presets.findIndex(preset => preset.id === active));
  const activePreset = presets[activeIndex];
  const activeTier = presetTier(activeIndex);
  const folded = activeTier === 'always' ? null : moreActiveClass[activeTier];
  const ActiveIcon = activePreset.icon;
  const items: MenuItem[] = overflowPresets(viewport).map(preset => {
    const Icon = preset.icon;
    return { id: preset.id, label: preset.label, href: presetHref(preset), icon: <Icon />, selected: preset.id === active };
  });

  return (
    <nav aria-label="Explore presets" className={cn('min-w-0', className)}>
      <div className="inline-flex max-w-full items-center gap-0.5 rounded-md bg-surface-1 p-1">
        <ul className="flex min-w-0 items-center gap-0.5">
          {presets.map((preset, index) => {
            const Icon = preset.icon;
            const selected = preset.id === active;
            return (
              <li key={preset.id} className={cn('shrink-0', inlineClass[presetTier(index)])}>
                <Link
                  href={presetHref(preset)}
                  aria-current={selected ? 'page' : undefined}
                  title={preset.description}
                  className={cn(segmentClass, selected ? segmentActive : segmentIdle)}
                >
                  <Icon aria-hidden="true" />
                  {preset.label}
                </Link>
              </li>
            );
          })}
        </ul>
        <DropdownMenu
          label="More presets"
          menuLabel="More explore presets"
          align="start"
          size="sm"
          items={items}
          className="shrink-0 xl:hidden"
          triggerClassName={cn(segmentClass, 'gap-1 font-semibold', folded ? folded.fill : segmentIdle)}
          trigger={(
            <>
              {folded ? (
                <span className={cn('inline-flex items-center gap-1.5', folded.label)}>
                  <ActiveIcon aria-hidden="true" />
                  {activePreset.label}
                </span>
              ) : null}
              <span className={folded ? folded.more : undefined}>More</span>
              <ChevronDown aria-hidden="true" />
            </>
          )}
        />
      </div>
    </nav>
  );
}
