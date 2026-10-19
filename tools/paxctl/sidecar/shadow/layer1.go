package shadow

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"

	"github.com/sei-protocol/seictl/sidecar/rpc"
)

// compareLayer1 fetches block_results from both chains and compares
// individual transaction receipts to identify which transactions diverged.
func (c *Comparator) compareLayer1(ctx context.Context, height int64) (*Layer1Result, error) {
	shadowResults, err := queryBlockResults(ctx, c.shadowClient, height)
	if err != nil {
		return nil, fmt.Errorf("querying shadow block_results at height %d: %w", height, err)
	}
	canonicalResults, err := queryBlockResults(ctx, c.canonicalClient, height)
	if err != nil {
		return nil, fmt.Errorf("querying canonical block_results at height %d: %w", height, err)
	}

	sTxs := shadowResults.TxsResults
	cTxs := canonicalResults.TxsResults

	result := &Layer1Result{
		TotalTxs:     max(len(sTxs), len(cTxs)),
		TxCountMatch: len(sTxs) == len(cTxs),
	}

	// Compare the overlapping transactions.
	minLen := min(len(sTxs), len(cTxs))
	for i := 0; i < minLen; i++ {
		divergence := compareTxReceipts(i, sTxs[i], cTxs[i])
		if divergence != nil {
			result.Divergences = append(result.Divergences, *divergence)
		}
	}

	// Any extra transactions on either side are divergences by definition.
	if len(sTxs) > minLen {
		for i := minLen; i < len(sTxs); i++ {
			result.Divergences = append(result.Divergences, TxDivergence{
				TxIndex: i,
				Fields: []FieldDivergence{{
					Field:     "presence",
					Shadow:    "present",
					Canonical: "missing",
				}},
			})
		}
	}
	if len(cTxs) > minLen {
		for i := minLen; i < len(cTxs); i++ {
			result.Divergences = append(result.Divergences, TxDivergence{
				TxIndex: i,
				Fields: []FieldDivergence{{
					Field:     "presence",
					Shadow:    "missing",
					Canonical: "present",
				}},
			})
		}
	}

	return result, nil
}

// compareTxReceipts compares critical fields from two transaction results.
// Returns nil when the receipts match.
func compareTxReceipts(idx int, shadow, canonical rpc.TxResult) *TxDivergence {
	var fields []FieldDivergence

	if shadow.Code != canonical.Code {
		fields = append(fields, FieldDivergence{
			Field: "code", Shadow: shadow.Code, Canonical: canonical.Code,
		})
	}
	if shadow.GasUsed != canonical.GasUsed {
		fields = append(fields, FieldDivergence{
			Field: "gasUsed", Shadow: shadow.GasUsed, Canonical: canonical.GasUsed,
		})
	}
	if shadow.GasWanted != canonical.GasWanted {
		fields = append(fields, FieldDivergence{
			Field: "gasWanted", Shadow: shadow.GasWanted, Canonical: canonical.GasWanted,
		})
	}
	if shadow.Log != canonical.Log {
		fields = append(fields, FieldDivergence{
			Field: "log", Shadow: shadow.Log, Canonical: canonical.Log,
		})
	}
	if !reflect.DeepEqual(shadow.Events, canonical.Events) {
		fields = append(fields, FieldDivergence{
			Field: "events", Shadow: shadow.Events, Canonical: canonical.Events,
		})
	}

	if len(fields) == 0 {
		return nil
	}
	return &TxDivergence{TxIndex: idx, Fields: fields}
}

// queryBlockResults fetches /block_results at the given height.
func queryBlockResults(ctx context.Context, client *rpc.Client, height int64) (*rpc.BlockResultsResult, error) {
	raw, err := client.Get(ctx, fmt.Sprintf("/block_results?height=%d", height))
	if err != nil {
		return nil, err
	}

	var result rpc.BlockResultsResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, fmt.Errorf("decoding /block_results response: %w", err)
	}
	return &result, nil
}
