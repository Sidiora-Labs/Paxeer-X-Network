# locale/module — Directory Index

## Directory responsibility

The `locale/module` directory contains domain-specific locale objects. Each file exports a key-value object (and usually a matching type). These are merged in `locale/en.ts` to form the default English bundle. Adding or changing keys here updates the `LocaleMessages` surface used by `t()`.

## Files

| File | Summary | Entry |
|------|---------|--------|
| [common.ts](common.md) | Shared labels (actions, sides, status, assets, etc.) | common, Common |
| [ui.ts](ui.md) | UI copy (empty state, pagination, picker) | ui, UI |
| [trading.ts](trading.md) | Trading layout, order book, asset/margin, funding, RWA | trading, Trading |
| [orders.ts](orders.md) | Order history, status, edit/cancel, toasts | orders, Orders |
| [positions.ts](positions.md) | Close position, funding fee, liquidation, adjust margin | positions, Positions |
| [orderEntry.ts](orderEntry.md) | Order types, size, TP/SL, errors, margin mode | orderEntry, OrderEntry |
| [markets.ts](markets.md) | Markets list, favorites, columns, funding, symbol bar | markets, Markets |
| [portfolio.ts](portfolio.md) | Fee tier, API keys, overview, performance, settings | portfolio, Portfolio |
| [connector.ts](connector.md) | Wallet connect, account, trading, network, Privy | connector, Connector |
| [scaffold.ts](scaffold.md) | Announcement, footer | scaffold, Scaffold |
| [chart.ts](chart.md) | Chart label | chart, Chart |
| [tpsl.ts](tpsl.md) | TP/SL labels and validation messages | tpsl, TPSL |
| [share.ts](share.md) | PnL share and display format | share, Share |
| [leverage.ts](leverage.md) | Leverage labels and tooltips | leverage, Leverage |
| [tradingRewards.ts](tradingRewards.md) | Rewards, epoch, claim, stake booster | tradingRewards, TradingRewards |
| [tradingView.ts](tradingView.md) | Chart intervals and line types | tradingView, TradingView |
| [transfer.ts](transfer.md) | Deposit, withdraw, internal transfer, swap, LTV | transfer, Transfer |
| [affiliate.ts](affiliate.md) | Referral, affiliate, commission, referees | affiliate, Affiliate |
| [tradingLeaderboard.ts](tradingLeaderboard.md) | Campaigns, leaderboard, tickets, rules | tradingLeaderboard, TradingLeaderboard |
| [tradingPoints.ts](tradingPoints.md) | Points, stages, referral, FAQ | tradingPoints, TradingPoints |
| [widget.ts](widget.md) | Asset history, link device, settle, language, maintenance | widget, Widget |
| [vaults.ts](vaults.md) | Vault list, deposit/withdraw, status | vaults, Vaults |
| [notification.ts](notification.md) | Announcement types and center | notification, Notification |
