# deprecated

Legacy components and pages kept for reference or migration. Prefer the non-deprecated modules under `components/`, `pages/`, and `hooks/`.

## Structure (mirror of src/deprecated)

- **deprecated/utils.ts**, **deprecated/type.ts** — Old utils and types
- **deprecated/pages/leaderboard/page.tsx** — Old leaderboard page
- **deprecated/components/** — Legacy versions of:
  - **rule/** — terms, rule.widget, rule, description, constants
  - **rewards/** — utils, rewards.widget, rewards.desktop.ui
  - **ranking128/** — shared (util, ranking.ui, column), generalRanking, campaignRanking
  - **ranking/** — shared (volumeColumnTitle, util, ranking.ui, pnLColumnTitle, column), generalRanking, campaignRanking
  - **leaderboard128/** — shared (LeaderboardTabs, LeaderboardFilter), generalLeaderboard, campaignLeaderboard
  - **leaderboard/** — shared (LeaderboardTabs, LeaderboardFilter), generalLeaderboard, campaignLeaderboard
  - **provider/index.tsx**
  - **campaigns/** — type, utils, pricePool, header, components (countdown, axis, time.desktop.ui, axis-example), campaign.item.ui, campaigns.widget, campaigns.script, DefaultCampaign, campaigns.content.desktop.ui
  - **background/index.tsx**

Use the current implementations under `src/components/`, `src/pages/`, and `src/hooks/` instead of these deprecated paths.
