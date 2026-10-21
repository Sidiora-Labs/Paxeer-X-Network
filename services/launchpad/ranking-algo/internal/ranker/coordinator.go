package ranker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"

	v1 "github.com/Sidiora-Technologies/KindleLaunch/shared/interfaces/v1"
	"github.com/google/uuid"
)

var ErrUnavailable = errors.New("ranking unavailable")
var ErrSuperseded = errors.New("ranking completion superseded")
var hashGrammar = regexp.MustCompile(`^[0-9a-f]{64}$`)
var uuidGrammar = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[1-5][0-9a-fA-F]{3}-[89abAB][0-9a-fA-F]{3}-[0-9a-fA-F]{12}$`)

var heavyCategories = [...]string{"trending", "breakout", "volume", "movers", "unusual"}

type Provenance struct {
	Watermark          v1.Watermark `json:"watermark"`
	UniverseDigest     string       `json:"universeDigest"`
	StatsRevision      string       `json:"statsRevision"`
	ModerationRevision string       `json:"moderationRevision"`
	ActivityRevision   string       `json:"activityRevision"`
	WindowStart        int64        `json:"windowStart"`
	WindowEnd          int64        `json:"windowEnd"`
}

func (p Provenance) Validate(in InputSnapshot) error {
	height, err := strconv.ParseUint(string(p.Watermark.Height), 10, 64)
	observed, e := time.Parse(time.RFC3339Nano, p.Watermark.ObservedAt)
	if err != nil || e != nil || height != in.CutoffHeight || p.Watermark.ChainId != "125" || p.Watermark.Integrity != "healthy" || !regexp.MustCompile(`^0x[0-9a-fA-F]{64}$`).MatchString(p.Watermark.BlockHash) || observed.UTC().Format("2006-01-02T15:04:05.000Z") != p.Watermark.ObservedAt || observed.Unix() <= 0 || observed.Unix() > in.CutoffTimeUTC || !hashGrammar.MatchString(p.UniverseDigest) || p.StatsRevision == "" || len(p.StatsRevision) > 64 || p.ModerationRevision == "" || len(p.ModerationRevision) > 64 || p.ActivityRevision == "" || len(p.ActivityRevision) > 64 || p.WindowEnd != in.CutoffTimeUTC || p.WindowStart < 0 || p.WindowStart >= p.WindowEnd {
		return ErrUnavailable
	}
	revision, err := strconv.ParseUint(string(p.Watermark.ProjectionRevision), 10, 64)
	if err != nil || revision < 1 {
		return ErrUnavailable
	}
	return nil
}

type FrozenSource interface {
	Freeze(context.Context, string, time.Time) (InputSnapshot, error)
}
type CompletedSink interface {
	Complete(context.Context, []Snapshot) error
}
type Coordinator struct {
	source FrozenSource
	sink   CompletedSink
}

func NewCoordinator(source FrozenSource, sink CompletedSink) (*Coordinator, error) {
	if source == nil || sink == nil {
		return nil, ErrUnavailable
	}
	return &Coordinator{source, sink}, nil
}
func (c *Coordinator) Refresh(ctx context.Context, runID string, cutoff time.Time, newOnly bool) ([]Snapshot, error) {
	if c == nil || !uuidGrammar.MatchString(runID) || cutoff.Unix() <= 0 {
		return nil, ErrUnavailable
	}
	in, err := c.source.Freeze(ctx, runID, cutoff)
	if err != nil {
		return nil, err
	}
	if in.RunID != runID || in.CutoffTimeUTC != cutoff.Unix() || in.Provenance == nil || in.Provenance.Validate(in) != nil {
		return nil, ErrUnavailable
	}
	snapshots, err := ComputeRun(in, newOnly)
	if err != nil {
		return nil, err
	}
	if err = c.sink.Complete(ctx, snapshots); err != nil {
		return nil, err
	}
	return snapshots, nil
}
func ComputeRun(in InputSnapshot, newOnly bool) ([]Snapshot, error) {
	if newOnly {
		s, err := BuildSnapshot(in, "new", nil)
		if err != nil {
			return nil, err
		}
		return []Snapshot{s}, nil
	}
	trending, err := BuildSnapshot(in, "trending", nil)
	if err != nil {
		return nil, err
	}
	out := []Snapshot{trending}
	for _, category := range heavyCategories[1:] {
		var dependency *Snapshot
		if category == "movers" {
			dependency = &trending
		}
		s, err := buildFrozenSnapshot(trending.input, category, dependency)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, nil
}
func (s Snapshot) Input() InputSnapshot {
	b, _ := json.Marshal(s.input)
	var out InputSnapshot
	_ = json.Unmarshal(b, &out)
	return out
}
func (s Snapshot) CutoffTime() int64 { return s.input.CutoffTimeUTC }

type Header struct {
	RunID            string     `json:"runId"`
	Category         string     `json:"category"`
	InputDigest      string     `json:"inputDigest"`
	OutputDigest     string     `json:"outputDigest"`
	AlgorithmVersion string     `json:"algorithmVersion"`
	CutoffTime       int64      `json:"cutoffTime"`
	CompletedAt      time.Time  `json:"completedAt"`
	Revision         int64      `json:"revision"`
	Total            int64      `json:"total"`
	Provenance       Provenance `json:"provenance"`
}

func (h Header) Validate() error {
	if !uuidGrammar.MatchString(h.RunID) || !hashGrammar.MatchString(h.InputDigest) || !hashGrammar.MatchString(h.OutputDigest) || h.AlgorithmVersion != AlgorithmVersion || h.CutoffTime <= 0 || h.CutoffTime > math.MaxInt64-900 || h.CompletedAt.IsZero() || h.Revision < 1 || h.Total < 0 || h.Total > 1000000 {
		return ErrUnavailable
	}
	_, err := Policy(h.Category)
	return err
}
func (h Header) ETag() string {
	p, _ := Policy(h.Category)
	return `"` + digest([]string{h.RunID, h.Category, h.InputDigest, h.OutputDigest, p.Version}) + `"`
}
func (h Header) Stale(now time.Time) bool {
	p, _ := Policy(h.Category)
	return now.Unix() < h.CutoffTime || now.Unix()-h.CutoffTime > p.FreshnessSeconds
}

type RankedItem struct {
	Address string `json:"poolAddress"`
	Score   string `json:"score"`
	Rank    int64  `json:"rank"`
}
type StoredPage struct {
	Header Header       `json:"header"`
	Items  []RankedItem `json:"items"`
	Offset int64        `json:"offset"`
}

func (p StoredPage) Validate() error {
	if err := p.Header.Validate(); err != nil {
		return err
	}
	if len(p.Items) > 100 || p.Offset < 0 || p.Offset > p.Header.Total || int64(len(p.Items)) > p.Header.Total-p.Offset {
		return ErrUnavailable
	}
	policy, _ := Policy(p.Header.Category)
	seen := make(map[string]bool, len(p.Items))
	for i, item := range p.Items {
		score, e := strconv.ParseFloat(item.Score, 64)
		if !addressGrammar.MatchString(item.Address) || e != nil || !decimalGrammar.MatchString(item.Score) || math.IsNaN(score) || math.IsInf(score, 0) || score < 0 || score > policy.MaxScore || item.Address != strings.ToLower(item.Address) || seen[item.Address] || item.Rank != p.Offset+int64(i)+1 {
			return fmt.Errorf("invalid persisted ranking page")
		}
		seen[item.Address] = true
		if i > 0 && p.Header.Category != "new" {
			previous, _ := strconv.ParseFloat(p.Items[i-1].Score, 64)
			if previous < score || (previous == score && p.Items[i-1].Address >= item.Address) {
				return fmt.Errorf("invalid persisted ranking order")
			}
		}
	}
	return nil
}

func (s Snapshot) Metadata() InputSnapshot {
	out := s.input
	out.Candidates = nil
	if s.input.Provenance != nil {
		p := *s.input.Provenance
		out.Provenance = &p
	}
	return out
}

func (c *Coordinator) Run(ctx context.Context, newOnly bool, report func(error)) error {
	if c == nil || report == nil {
		return ErrUnavailable
	}
	interval := 30 * time.Second
	if newOnly {
		interval = 10 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		_, err := c.Refresh(ctx, uuid.NewString(), time.Now().UTC().Truncate(time.Second), newOnly)
		if err != nil {
			report(err)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func HeavyCategoryNames() []string { return append([]string(nil), heavyCategories[:]...) }
