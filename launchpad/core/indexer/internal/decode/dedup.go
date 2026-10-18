package decode

import (
	"fmt"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"reflect"
)

const MaxRawBatchLogs = 4096
const MaxRawBatchBytes = 16 << 20

// DeduplicateRaw admits a bounded batch before allocating output or inspecting
// payload contents. The caps include duplicates, preventing repeated deliveries
// from bypassing admission. No raw payload is hex-materialized by this helper.
// Native events and mirrors remain distinct. Cross-batch durable deduplication
// remains the caller's responsibility.
func DeduplicateRaw(logs []ethtypes.Log) ([]ethtypes.Log, error) {
	if len(logs) > MaxRawBatchLogs {
		return nil, fmt.Errorf("raw batch exceeds %d logs", MaxRawBatchLogs)
	}
	total := 0
	for _, l := range logs {
		if !rawSizeAllowed(l) {
			return nil, fmt.Errorf("raw log exceeds envelope limit")
		}
		size := len(l.Data) + 32*len(l.Topics)
		if size > MaxRawBatchBytes-total {
			return nil, fmt.Errorf("raw batch exceeds aggregate byte limit")
		}
		total += size
		if !validIdentity(l) || l.Removed {
			return nil, fmt.Errorf("invalid committed raw log identity")
		}
	}
	seen := make(map[RawIdentity]int, len(logs))
	out := make([]ethtypes.Log, 0, len(logs))
	for i, l := range logs {
		identity := RawIdentity{125, l.BlockHash.Hex(), l.TxHash.Hex(), l.Index}
		if prior, ok := seen[identity]; ok {
			if !reflect.DeepEqual(logs[prior], l) {
				return nil, fmt.Errorf("conflicting raw log identity %v", identity)
			}
			continue
		}
		seen[identity] = i
		out = append(out, l)
	}
	return out, nil
}
