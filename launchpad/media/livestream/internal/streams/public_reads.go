package streams

import (
	"errors"

	"github.com/jackc/pgx/v5/pgxpool"
	goredis "github.com/redis/go-redis/v9"
)

// PublicReadOptions are the stores of the public livestream read surface.
type PublicReadOptions struct {
	Pool        *pgxpool.Pool
	Redis       *goredis.Client
	Schema      string
	WalletLimit int
	GlobalLimit int
	// ViewerTokenKey signs anonymous viewer sessions (>= 32 bytes). It must be
	// stable across restarts and replicas so issued sessions stay verifiable.
	ViewerTokenKey []byte
}

// NewPublicReads wires the public read surface (directory, pool listing,
// stream document, viewer sessions and heartbeats) over the real lifecycle
// store, the chat safety bans, the Redis viewer counter and the market-cap
// projection, for a runtime without a broadcast provider: every
// provider-bound route (create, go-live, end, edit, key rotation, ingest
// credentials, private playback, operation status, provider webhooks) answers
// the stable BROADCAST_NOT_PROVISIONED contract error. The production
// platform runtime (RUNTIME_PROFILE=platform) serves it directly; development
// reaches it through NewDevelopmentReads.
func NewPublicReads(o PublicReadOptions) (Deps, error) {
	if o.Pool == nil || o.Redis == nil {
		return Deps{}, ErrUnavailable
	}
	if len(o.ViewerTokenKey) < 32 {
		return Deps{}, errors.New("livestream viewer token key requires at least 32 bytes")
	}
	if o.Schema == "" {
		o.Schema = "livestream"
	}
	global := o.GlobalLimit
	if global < o.WalletLimit {
		global = o.WalletLimit
	}
	lifecycle, err := NewLifecycleStore(o.Pool, o.Schema, o.WalletLimit, global)
	if err != nil {
		return Deps{}, err
	}
	viewers, err := NewViewerCounter(o.Redis, o.Schema, 100000)
	if err != nil {
		return Deps{}, err
	}
	return Deps{
		Lifecycle:              lifecycle,
		Restrictions:           &RestrictionReader{Pool: o.Pool},
		Viewers:                viewers,
		ViewerTokens:           &ViewerTokenAuthority{Key: append([]byte(nil), o.ViewerTokenKey...)},
		MarketCaps:             &MarketCapReader{Pool: o.Pool},
		MaxStreamsPerWallet:    o.WalletLimit,
		Redis:                  o.Redis,
		BroadcastUnprovisioned: true,
	}, nil
}
