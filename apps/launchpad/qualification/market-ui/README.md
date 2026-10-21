Market presentation phase 1 checks

Run from client/apps/web:
`./node_modules/.bin/vitest run src/page-modules/token/market-presentation.test.tsx --reporter=verbose`

These are actual React DOM tests in jsdom plus synthetic arithmetic/chain-order fixtures, not chain receipts, live network journeys or browser capacity proof. They exercise actual-decimal normalization, 78-digit balances, unavailable quality, malicious identity text, identity disagreement, missing valuation, real versus virtual backing, bounded ranking compatibility, numeric holder sorting, trade deduplication and freshness transitions.

Frozen generated DTOs are imported as types directly; no schemas or SDKs are copied. Legacy hook adaptation remains a D08 integration prerequisite. The card is reusable and not mounted to routes until those DTOs are available. Existing MarketCapDisplay callers remain compatible and show unavailable rather than unqualified legacy valuation. Full D10 acceptance, explorer navigation, integrated controls, media layout, cursor/live merge and performance qualification remain pending.
