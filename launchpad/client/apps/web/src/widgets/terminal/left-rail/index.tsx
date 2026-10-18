'use client';
import { DiscoveryPanel } from './discovery-panel';
import { RecentPanel } from './recent-panel';

export interface TerminalLeftRailProps {
  activePool: string | null;
}

/** Terminal left column: Trending, Recently viewed and New pairs stacked; each list scrolls on its own. */
export default function TerminalLeftRail({ activePool }: TerminalLeftRailProps) {
  return (
    <div className="flex h-full min-h-0 flex-col gap-1 bg-canvas">
      <DiscoveryPanel title="Trending" category="trending" activePool={activePool} className="min-h-40 flex-[3]" />
      <RecentPanel activePool={activePool} className="max-h-[40%] shrink-0 overflow-y-auto" />
      <DiscoveryPanel title="New pairs" category="new" activePool={activePool} className="min-h-40 flex-[2]" />
    </div>
  );
}

