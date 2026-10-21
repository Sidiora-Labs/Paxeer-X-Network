'use client';
import type { ProfileBlocks } from '@/widgets/profile/profile-view';
import { LeaderboardPanel } from '@/widgets/profile/leaderboard-panel';
import { PortfolioBlock } from '@/widgets/profile/portfolio-block';
import { PnlSummary } from '@/widgets/profile/pnl-summary';
import { TopTrades } from '@/widgets/profile/top-trades';
import { PositionsOpenClosed } from '@/widgets/profile/positions-open-closed';
import { UtilityRows } from '@/widgets/profile/utility-rows';
import { ActivityTab } from '@/widgets/profile/activity-tab';
import { RepliesTab } from '@/widgets/profile/replies-tab';

/** Rows the top-trades rail ranks; the reference rail is a full-height list. */
export const profileTopTradesRows = 12;

export function profileBlocks({ wallet, isOwner }: { wallet: string; isOwner: boolean }): ProfileBlocks {
  return {
    left: <LeaderboardPanel className="rounded-none bg-transparent px-2 pb-4 pt-2" />,
    centerExtra: (
      <>
        <PortfolioBlock wallet={wallet} isOwner={isOwner} className="rounded-none bg-transparent px-4 pb-2 pt-5 md:px-5" />
        <PnlSummary wallet={wallet} isOwner={isOwner} className="rounded-none bg-transparent px-4 pb-4 pt-2 md:px-5" />
      </>
    ),
    rightExtra: <TopTrades wallet={wallet} isOwner={isOwner} limit={profileTopTradesRows} className="rounded-none bg-transparent px-2 pb-4 pt-3" />,
    panels: {
      open: <PositionsOpenClosed wallet={wallet} mode="open" isOwner={isOwner} />,
      closed: <PositionsOpenClosed wallet={wallet} mode="closed" isOwner={isOwner} />,
      activity: (
        <>
          <UtilityRows wallet={wallet} isOwner={isOwner} />
          <ActivityTab wallet={wallet} isOwner={isOwner} />
        </>
      ),
      replies: <RepliesTab wallet={wallet} isOwner={isOwner} />,
    },
  };
}
