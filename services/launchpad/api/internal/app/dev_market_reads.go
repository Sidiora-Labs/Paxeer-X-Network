package app

import (
	"context"

	"github.com/go-chi/chi/v5"

	"github.com/Sidiora-Technologies/KindleLaunch/core/api/internal/rest"
)

// registerDevelopmentMarketReads mounts the development v2 market reads
// (pools, stats, rankings, leaderboards, holder rewards, platform metrics,
// holders). Block hashes and event positions the development indexer does not
// persist are resolved through NETWORK_RPC_URL when it is set.
func registerDevelopmentMarketReads(r chi.Router, deps developmentReadDeps) error {
	return rest.RegisterDevelopmentMarketReads(context.Background(), r, rest.DevelopmentMarketOptions{Store: deps.store, Admission: deps.admission, ChainRPCURL: deps.cfg.NetworkRPCURL})
}
