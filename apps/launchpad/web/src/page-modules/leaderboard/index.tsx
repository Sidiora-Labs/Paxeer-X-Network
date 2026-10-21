'use client';

import { LeaderboardView } from '@/widgets/leaderboard/leaderboard-view';
import { CalloutsRail } from '@/widgets/social/callouts-rail';

export default function LeaderboardModule() {
  return <LeaderboardView rail={<CalloutsRail />} />;
}
