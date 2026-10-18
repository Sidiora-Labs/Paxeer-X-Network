# L31 account disclosure primitive checks

Run from repository root:

`python3 client/qualification/account-ui/run-check.py`

`client/apps/web/node_modules/.bin/tsc -p client/qualification/account-ui/tsconfig.json`

Use the coordinator bounded frontend executor. Dependencies reuse existing ignored node_modules targets; no install or lock changes. The test configuration is private to this owned path.

The11 native tests exercise real domain code, server-side React rendering, jsdom DOM structure, and mounted existing RewardsView on deliberately rejected RPC reads and incomplete NFT enumeration. Inputs and NFT-balance hooks are synthetic; there is no provider success, chain receipt or live browser/service qualification claim. The synthetic HTML artifacts illustrate partial-account and anonymous selected-field preview output. Failed module-resolution attempts remain in the evidence directory.

Strict typechecking covers new disclosure primitives against the generated frozen PortfolioSummary contract. It is not a complete client build or cross-lane qualification.
