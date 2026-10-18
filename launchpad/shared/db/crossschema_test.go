package db

import "testing"

func TestCrossSchemaTableNames(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		TableIndexerPools:          "indexer.pools",
		TableStatsPoolStats:        "stats.pool_stats",
		TableMetadataTokenMetadata: "metadata.token_metadata",
	}
	for got, want := range cases {
		if got != want {
			t.Errorf("table name = %q, want %q", got, want)
		}
	}
	if SchemaIndexer != "indexer" || SchemaStats != "stats" || SchemaMetadata != "metadata" {
		t.Error("schema name constant drift")
	}
}
