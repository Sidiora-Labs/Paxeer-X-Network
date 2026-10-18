# pages — Directory Index

## Directory Responsibilities

The `pages` folder contains the top-level markets page structure: the markets home page (Markets / Funding tabs), markets header, markets data list, and funding tab content. It does not implement individual list or funding components; those live under `components`.

## Key Entities

| Entity | Location | Role | Dependency |
|--------|----------|------|-------------|
| MarketsHomePage | home/page.tsx | Main page with Markets/Funding tabs | MarketsProvider, lazy widgets |
| MarketsHeader / MarketsHeaderWidget | home/marketsHeader | Header with search and nav | useMarketsHeaderScript |
| MarketsDataList / MarketsDataListWidget | home/marketsDataList | Main markets list on page | useMarketsDataListScript |
| Funding / FundingWidget | home/funding | Funding tab content | useFundingScript |

## Subdirectory: home

| File | Language | Responsibility | Detail |
|------|----------|----------------|--------|
| page.tsx | TSX | MarketsHomePage, desktop/mobile content | [home/page.md](home/page.md) |
| marketsHeader/* | TS/TSX | Header UI and script | — |
| marketsDataList/* | TS/TSX | Data list UI and script | — |
| funding/* | TS/TSX | Funding UI and script | — |

## Subdirectories (Links)

- [home](home/index.md) — Markets home page, header, data list, funding.
