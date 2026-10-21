package db

// Cross-schema table references (L3), ported from shared/src/db/cross-schema.ts.
// Go services read another service's schema through these schema-qualified
// identifiers (and the typed row structs below) instead of re-declaring inline
// copies, keeping column definitions in sync. Used e.g. by media/user reading
// indexer.pools for a creator's created-pools.

// Schema names.
const (
	SchemaIndexer  = "indexer"
	SchemaStats    = "stats"
	SchemaMetadata = "metadata"
)

// These constants are legacy read adapters only. They confer no ownership or
// permission to write; new foreign reads require granted versioned views.
// Schema-qualified table names (safe to interpolate: fixed identifiers, never
// user input — SQL values still go through bound parameters, SECTION 17).
const (
	TableIndexerPools          = "indexer.pools"
	TableStatsPoolStats        = "stats.pool_stats"
	TableMetadataTokenMetadata = "metadata.token_metadata" //nolint:gosec // G101 false positive: schema-qualified table name, not a credential
)

// IndexerPool mirrors indexer.pools (cross-schema.ts indexerPools).
type IndexerPool struct {
	PoolAddress  string `db:"pool_address"`
	TokenAddress string `db:"token_address"`
	Creator      string `db:"creator"`
	CreatedAt    int64  `db:"created_at"`
}

// StatsPoolStats mirrors stats.pool_stats (cross-schema.ts statsPoolStats).
// Money/price fields are decimal strings (text columns) — never float (i1).
type StatsPoolStats struct {
	PoolAddress          string `db:"pool_address"`
	TokenAddress         string `db:"token_address"`
	Price                string `db:"price"`
	PriceChange1m        string `db:"price_change_1m"`
	PriceChange5m        string `db:"price_change_5m"`
	PriceChange15m       string `db:"price_change_15m"`
	PriceChange1h        string `db:"price_change_1h"`
	PriceChange24h       string `db:"price_change_24h"`
	PriceChangeDollar1m  string `db:"price_change_dollar_1m"`
	PriceChangeDollar5m  string `db:"price_change_dollar_5m"`
	PriceChangeDollar15m string `db:"price_change_dollar_15m"`
	PriceChangeDollar1h  string `db:"price_change_dollar_1h"`
	PriceChangeDollar24h string `db:"price_change_dollar_24h"`
	Volume24h            string `db:"volume_24h"`
	Volume1h             string `db:"volume_1h"`
	Volume5m             string `db:"volume_5m"`
	MarketCap            string `db:"market_cap"`
	BuyCount24h          int32  `db:"buy_count_24h"`
	SellCount24h         int32  `db:"sell_count_24h"`
	UniqueTraders24h     int32  `db:"unique_traders_24h"`
	HolderCount          int32  `db:"holder_count"`
	UpdatedAt            int64  `db:"updated_at"`
}

// MetadataTokenMetadata mirrors metadata.token_metadata
// (cross-schema.ts metadataTokenMetadata).
type MetadataTokenMetadata struct {
	TokenAddress string  `db:"token_address"`
	PoolAddress  string  `db:"pool_address"`
	Name         *string `db:"name"`
	Symbol       *string `db:"symbol"`
	Description  *string `db:"description"`
	CreatedBy    string  `db:"created_by"`
	CreatedAt    int64   `db:"created_at"`
}
