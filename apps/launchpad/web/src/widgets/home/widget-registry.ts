import type { ComponentType } from 'react';
import { Flame, LayoutGrid, Megaphone, MessageSquareText, Star, Trophy, type LucideIcon } from 'lucide-react';
import type { ColumnDensity, ColumnView, WidgetType } from './layout-store';
import { TrendingWidget } from './trending-widget';
import { ScreenerWidget } from './screener-widget';
import { WatchlistWidget } from './watchlist-widget';
import { LeaderboardWidget } from './leaderboard-widget';
import { CalloutsWidget, HotCalloutsWidget } from './callouts-widget';

export interface WidgetProps {
  columnId: string;
  filter: string;
  density: ColumnDensity;
  view: ColumnView;
}

export interface WidgetDefinition {
  type: WidgetType;
  title: string;
  description: string;
  icon: LucideIcon;
  supports: { filter: boolean; density: boolean; view: boolean };
  Component: ComponentType<WidgetProps>;
}

export const widgetRegistry: Record<WidgetType, WidgetDefinition> = {
  trending: { type: 'trending', title: 'Trending', description: 'Hottest coins right now', icon: Flame, supports: { filter: true, density: true, view: false }, Component: TrendingWidget },
  screener: { type: 'screener', title: 'Screener', description: 'New coins, movers and breakouts', icon: LayoutGrid, supports: { filter: true, density: false, view: true }, Component: ScreenerWidget },
  watchlist: { type: 'watchlist', title: 'Watchlist', description: 'Coins you starred', icon: Star, supports: { filter: true, density: true, view: false }, Component: WatchlistWidget },
  leaderboard: { type: 'leaderboard', title: 'Leaderboard', description: 'Top traders and creators', icon: Trophy, supports: { filter: true, density: false, view: false }, Component: LeaderboardWidget },
  'hot-callouts': { type: 'hot-callouts', title: 'Hot callouts', description: 'The most talked-about calls', icon: Megaphone, supports: { filter: true, density: false, view: false }, Component: HotCalloutsWidget },
  callouts: { type: 'callouts', title: 'Latest callouts', description: 'The newest calls', icon: MessageSquareText, supports: { filter: true, density: false, view: false }, Component: CalloutsWidget },
};

/** Add-column and column-type menu order: the default board first, then the optional columns. */
export const widgetOrder: readonly WidgetType[] = ['trending', 'hot-callouts', 'leaderboard', 'callouts', 'screener', 'watchlist'];

export function widgetTitle(type: WidgetType): string {
  return widgetRegistry[type].title;
}
