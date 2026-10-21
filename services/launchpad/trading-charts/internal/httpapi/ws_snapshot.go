package httpapi

import (
	"encoding/json"
	"fmt"
	"math/big"
	"regexp"
	"strings"
	"sync"
	"time"
)

var poolPattern = regexp.MustCompile(`^0x[0-9a-f]{40}$`)
var decimalPattern = regexp.MustCompile(`^(0|[1-9][0-9]*)(\.[0-9]*[1-9])?$`)
var revisionPattern = regexp.MustCompile(`^[1-9][0-9]*$`)

// CandleSnapshot contains human-unit exact decimal values, never raw-unit guesses.
// Cursor is opaque delivery metadata and is never compared with Revision.
type CandleSnapshot struct {
	Version          int    `json:"version"`
	Chain            int    `json:"chain"`
	PoolAddress      string `json:"poolAddress"`
	Timeframe        string `json:"timeframe"`
	Start            int64  `json:"start"`
	Revision         string `json:"revision"`
	AlgorithmVersion string `json:"algorithmVersion"`
	SourceWatermark  string `json:"sourceWatermark"`
	Cursor           string `json:"cursor"`
	Open             string `json:"open"`
	High             string `json:"high"`
	Low              string `json:"low"`
	Close            string `json:"close"`
	VolumeUsdl       string `json:"volumeUsdl"`
	VolumeToken      string `json:"volumeToken"`
	BuyVolumeUsdl    string `json:"buyVolumeUsdl"`
	SellVolumeUsdl   string `json:"sellVolumeUsdl"`
	McapOpen         string `json:"mcapOpen"`
	McapHigh         string `json:"mcapHigh"`
	McapLow          string `json:"mcapLow"`
	McapClose        string `json:"mcapClose"`
	TradeCount       uint64 `json:"tradeCount"`
	UniqueTraders    uint64 `json:"uniqueTraders"`
	LargeTradeCount  uint64 `json:"largeTradeCount"`
}

func EncodeCandle(c CandleSnapshot) ([]byte, error) {
	if c.Version != 1 || c.Chain != 125 || c.Start < 0 || !revisionPattern.MatchString(c.Revision) || len(c.Revision) > 78 || c.AlgorithmVersion == "" || c.SourceWatermark == "" || c.Cursor == "" || len(c.Cursor) > 80 || len(c.AlgorithmVersion) > 64 || len(c.SourceWatermark) > 256 {
		return nil, fmt.Errorf("invalid snapshot metadata")
	}
	if _, _, e := validateSubscriptions([]string{c.PoolAddress}, []string{c.Timeframe}); e != nil {
		return nil, e
	}
	revision, _ := new(big.Int).SetString(c.Revision, 10)
	if revision.BitLen() > 256 {
		return nil, fmt.Errorf("revision exceeds uint256")
	}
	c.PoolAddress = strings.ToLower(c.PoolAddress)
	for _, v := range []string{c.Open, c.High, c.Low, c.Close, c.VolumeUsdl, c.VolumeToken, c.BuyVolumeUsdl, c.SellVolumeUsdl, c.McapOpen, c.McapHigh, c.McapLow, c.McapClose} {
		if len(v) > 160 || !decimalPattern.MatchString(v) {
			return nil, fmt.Errorf("noncanonical decimal")
		}
	}
	return json.Marshal(struct {
		Type string         `json:"type"`
		Data CandleSnapshot `json:"data"`
	}{"candle.updated", c})
}
func newer(a, b string) bool { return len(a) > len(b) || (len(a) == len(b) && a > b) }

// Coalescer bounds pending identities and emits at most once per 500ms window.
// Revision knowledge survives drains. New identities fail closed when retention
// reaches its bound; a durable snapshot/replay owner must renew the coalescer.
// Durable replay is supplied by the common gateway, not this volatile cache.
type CandleCoalescer struct {
	mu       sync.Mutex
	pending  map[string]CandleSnapshot
	retained map[string]CandleSnapshot
	max      int
	last     time.Time
}

func NewCandleCoalescer(max int) *CandleCoalescer {
	if max < 1 {
		max = 1
	}
	return &CandleCoalescer{pending: map[string]CandleSnapshot{}, retained: map[string]CandleSnapshot{}, max: max}
}
func candleKey(c CandleSnapshot) string {
	return fmt.Sprintf("%d:%s:%s:%d", c.Chain, strings.ToLower(c.PoolAddress), c.Timeframe, c.Start)
}
func (c *CandleCoalescer) Add(v CandleSnapshot) error {
	v.PoolAddress = strings.ToLower(v.PoolAddress)
	if _, e := EncodeCandle(v); e != nil {
		return e
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	k := candleKey(v)
	if old, ok := c.retained[k]; ok {
		if old.Revision == v.Revision {
			old.Cursor = v.Cursor
			a, _ := EncodeCandle(old)
			b, _ := EncodeCandle(v)
			if string(a) != string(b) {
				return fmt.Errorf("revision integrity conflict")
			}
			return nil
		}
		if !newer(v.Revision, old.Revision) {
			return nil
		}
	} else if len(c.retained) >= c.max {
		return fmt.Errorf("coalescer revision retention capacity; resync_required")
	}
	c.retained[k] = v
	c.pending[k] = v
	return nil
}
func (c *CandleCoalescer) Drain(now time.Time) []CandleSnapshot {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.last.IsZero() && now.Sub(c.last) < 500*time.Millisecond {
		return nil
	}
	c.last = now
	out := make([]CandleSnapshot, 0, len(c.pending))
	for _, v := range c.pending {
		out = append(out, v)
	}
	clear(c.pending)
	return out
}
