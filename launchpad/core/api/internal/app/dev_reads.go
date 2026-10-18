package app

import (
	"github.com/go-chi/chi/v5"

	"github.com/Sidiora-Technologies/KindleLaunch/core/api/internal/cache"
	"github.com/Sidiora-Technologies/KindleLaunch/core/api/internal/config"
	"github.com/Sidiora-Technologies/KindleLaunch/core/api/internal/rest"
	"github.com/Sidiora-Technologies/KindleLaunch/core/api/internal/store"
)

// developmentReadDeps carries the development-mode providers for the v2/v3
// read surface. Production mounts the same paths from RegisterProductionReads
// over the accounting read model; outside production the handlers below are
// backed by the development store (indexer tables, Redis rankings).
type developmentReadDeps struct {
	cfg       *config.Config
	store     *store.Store
	cache     *cache.Cache
	admission *rest.Admission
}

// registerDevelopmentReads mounts the non-production v2/v3 read surface on the
// rate-limited REST group. Each work package registers from its own file and
// adds exactly one call here.
func registerDevelopmentReads(r chi.Router, deps developmentReadDeps) error {
	// market reads (pools, stats, rankings, leaderboards, holder rewards, platform metrics)
	if err := registerDevelopmentMarketReads(r, deps); err != nil {
		return err
	}
	// trade history, candles, search, discovery public reads
	registerDevelopmentHistorySearchDiscovery(r, deps)
	// network authority and preflight
	registerDevelopmentNetworkAuthority(r, deps)
	// deployment manifest, transaction status, cups and the not-served v2 operations
	registerDevelopmentNetworkReads(r, deps)
	_ = r
	_ = deps
	return nil
}
