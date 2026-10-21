// Page module barrel — exports each page-module's default for routing.
// Pages router shims at src/pages/*.tsx re-export from this layer.
export { default as HomePage }         from "./home";
export { default as AnalyticsPage }    from "./analytics";
export { default as CompetitionsPage } from "./competitions";
export { default as ExplorerPage }     from "./explorer";
export { default as LeaderboardPage }  from "./leaderboard";
export { default as MarketsPage }      from "./markets";
export { default as PerpsPage }        from "./perps";
export { default as PortfolioPage }    from "./portfolio";
export { default as ReferralsPage }    from "./referrals";
export { default as BlockDetailPage }  from "./block-detail";
export { default as TxDetailPage }     from "./tx-detail";
