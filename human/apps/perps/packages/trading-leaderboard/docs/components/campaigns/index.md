# campaigns

Campaign configuration types, header widget, price pool, campaign item, and campaign list widget.

## Files and subdirectories

| Path | Language | Description | Link |
|------|----------|-------------|------|
| `type.ts` | TypeScript | CampaignConfig, PrizePool, UserData, CampaignStatistics, etc. | [type.md](./type.md) |
| `header/` | TSX | CampaignsHeaderWidget, desktop/mobile UI | [header/index.md](./header/index.md) |
| `pricePool/` | TSX/TS | Price pool widget, script, utils | — |
| `campaign.item.ui.tsx` | TSX | Single campaign card/item | — |
| `campaigns.widget.tsx` | TSX | Campaign list and banner | — |
| `campaigns.script.ts` | TypeScript | useCampaignsScript | — |
| `components/` | TSX | countdown, axis, time.desktop.ui, axis-example | — |

## Main exports (from package)

- `CampaignsHeaderWidget` from `./header`
- Types from `./type` (CampaignConfig, UserData, PrizePool, etc.)
