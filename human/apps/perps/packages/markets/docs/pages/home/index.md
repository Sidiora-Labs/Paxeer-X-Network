# pages/home — Directory Index

## Directory Responsibilities

The `home` folder contains the markets home page: tabbed layout (Markets / Funding), markets header widget, markets data list widget, and funding widget. Desktop and mobile layouts are handled inside the page and respective widgets.

## Key Entities

| Entity | File | Role |
|--------|------|------|
| MarketsHomePage | page.tsx | Root page component; wraps with MarketsProvider; renders Tabs (Markets / Funding) and lazy-loads header, data list, funding |
| MarketsHeaderWidget | marketsHeader | Header with search and navigation |
| MarketsDataListWidget | marketsDataList | Main markets table/list |
| FundingWidget | funding | Funding overview/comparison content |

## Files

| File | Language | Responsibility | Link |
|------|----------|----------------|------|
| page.tsx | TSX | MarketsHomePage, MarketsDesktopContent, MarketsMobileContent | [page.md](page.md) |
| marketsHeader.ui.tsx | TSX | MarketsHeader presentational | — |
| marketsHeader.script.tsx | TSX | useMarketsHeaderScript | — |
| marketsHeader.widget.tsx | TSX | MarketsHeaderWidget | — |
| marketsHeader/index.ts | TS | Re-exports | — |
| marketsDataList.ui.tsx | TSX | MarketsDataList presentational | — |
| marketsDataList.script.ts | TS | useMarketsDataListScript | — |
| marketsDataList.widget.tsx | TSX | MarketsDataListWidget | — |
| marketsDataList.mobile.ui.tsx | TSX | Mobile data list UI | — |
| marketsDataList/index.ts | TS | Re-exports | — |
| funding.ui.tsx | TSX | Funding presentational | — |
| funding.script.tsx | TSX | useFundingScript | — |
| funding.widget.tsx | TSX | FundingWidget | — |
| funding.mobile.ui.tsx | TSX | Mobile funding UI | — |
| funding/index.ts | TS | Re-exports | — |
