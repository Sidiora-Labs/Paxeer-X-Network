# pages

## Directory Responsibility

All affiliate app pages: dashboard (tab + content), home (landing, become affiliate, cards, top, subtitle, title), affiliate (referral link, codes, summary, title statistic, commission and referees), trader (summary, title statistic, rebates), and multiLevel (landing, affiliate page with referral table, commission chart, referral code form, summary, referral info). Each section may have .ui, .widget, .script and index exports.

## Subdirectories

| Directory | Description | Index |
|-----------|-------------|--------|
| [dashboard](dashboard/index.md) | Dashboard container and tab (affiliate/trader) | [dashboard/index.md](dashboard/index.md) |
| [home](home/index.md) | Home landing: title, top, card, subtitle, become affiliate, as trader, as affiliate | [home/index.md](home/index.md) |
| [affiliate](affiliate/index.md) | Affiliate tab content: referral link, referral codes, summary, title statistic, commission and referees | [affiliate/index.md](affiliate/index.md) |
| [trader](trader/index.md) | Trader tab content: summary, title statistic, rebates | [trader/index.md](trader/index.md) |
| [multiLevel](multiLevel/index.md) | Multi-level referral: landing, affiliate page, commission chart, referral table, referral code form | [multiLevel/index.md](multiLevel/index.md) |

## Key Entities

| Entity | Location | Responsibility |
|--------|----------|----------------|
| Dashboard | pages/dashboard | Tab + page container |
| HomePage | pages/home | Home landing |
| AffiliatePage | pages/affiliate | Affiliate tab content |
| TraderPage | pages/trader | Trader tab content |
| MultiLevelAffiliatePage | pages/multiLevel/affiliate | Multi-level affiliate dashboard |
| LandingPage | pages/multiLevel/landing | Multi-level landing |
