# Release v6.12.0 contracts

The contract files of release v6.12.0. The spec tasks cite them by heading; resolution feature 9 (spec/release-v6-12-0) settles the rules that more than one file relies on.

## Contract files

- README.md: this index, the file list and the reading order.
- address-table.md: every precompile address, module account and token of the release, with constructor, module, activation rule and owning contract file.
- pricefeed.md: the pricefeed module and the precompile at 0x1020, its markets, feed keys, submission message, aggregation, read API, activation and price sidecar (feature 5).
- perps.md: the perpetuals precompile interface at 0x1015, its units, events, storage, activation and versioning and EndBlock (feature 3).
- perps-parameters.md: the perpetuals parameter sheet, trading fees, margin, funding, borrowing fee, liquidation, insurance fund and the spot fee schedule (feature 3).
- perps-markets.md: the perpetuals markets at activation (feature 3).
- spot.md: the spot venue at 0x1021 to 0x1025, its address set, token model, opening pairs, fees, EndBlock execution, activation and errors (feature 4).
- launchpad.md: the launchpad precompile at 0x1017, its interface by role, surfaces, quote asset, migration record, optical hooks, storage and activation (feature 2).
- application.md: the application on the v6.12.0 precompiles, its address table, amounts and error handling (feature 6).
- data-services.md: the indexer, stream, event decoding, websocket protocol, GraphQL operations, explorer routes, portal pages and service layout (feature 7).
- release.md: the release build and plan, release artifacts, upgrade rehearsal and rollout (feature 8).
- ../docs/rollout-runbook.md: the rollout runbook and its operator inputs (feature 8).

## Address table

address-table.md is the single list of addresses, module accounts and tokens. A contract file that names an address, account or denom names the same value as that table; where they differ, the table and resolution feature 9 decide.

## Reading order

1. README.md
2. address-table.md
3. pricefeed.md
4. perps.md
5. perps-parameters.md
6. perps-markets.md
7. spot.md
8. launchpad.md
9. application.md
10. data-services.md
11. release.md
12. ../docs/rollout-runbook.md
