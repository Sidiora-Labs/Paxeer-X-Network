# rewards

Rewards section: shows when a campaign is selected (hidden for "general"). Uses LeaderboardTitle and RewardsDesktopUI.

## Files

| File | Language | Description |
|------|----------|-------------|
| `rewards.widget.tsx` | TSX | RewardsWidget — uses useCampaignsScript, LeaderboardTitle, RewardsDesktopUI |
| `rewards.desktop.ui.tsx` | TSX | RewardsDesktopUI (campaign, userdata, learn more, trade now, join) |
| `utils.ts` | TypeScript | Rewards helpers |

## Exports

- `RewardsWidget` (used by LeaderboardPage; not re-exported from package index)
