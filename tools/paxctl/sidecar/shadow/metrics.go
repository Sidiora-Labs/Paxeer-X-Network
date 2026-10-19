package shadow

import "github.com/prometheus/client_golang/prometheus"

var (
	// BlocksCompared counts blocks the comparator has processed.
	// rate(...)==0 indicates the comparator has stopped advancing — typically
	// shadow RPC unreachable or the local node has stopped producing blocks.
	// pod_name differentiates two shadow candidates for the same chain so
	// alerts can route to a specific candidate image.
	BlocksCompared = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "seictl_shadow_blocks_compared_total",
			Help: "Total blocks compared between the shadow node and the canonical chain.",
		},
		[]string{"chain_id", "pod_name"},
	)

	// Divergences counts app-hash divergences detected. Increments at most
	// once per process lifetime — the comparison loop exits on first divergence.
	// divergence_layer is "0" for header-hash mismatch, "1" when Layer 1
	// isolated specific tx-receipt mismatches.
	Divergences = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "seictl_shadow_divergences_total",
			Help: "App-hash divergences detected by the shadow comparator. Increments once per process lifetime since the loop exits on first divergence.",
		},
		[]string{"chain_id", "pod_name", "divergence_layer"},
	)
)

func init() {
	prometheus.MustRegister(BlocksCompared)
	prometheus.MustRegister(Divergences)
}
