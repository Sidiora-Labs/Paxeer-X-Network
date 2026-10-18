package l16

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"strconv"
	"strings"
	"time"
)

type TimedLeg struct {
	Leg         Leg
	CommittedAt time.Time
}
type Checkpoint struct {
	Partition            string
	Day                  string
	Algorithm            string
	InputDigest          string
	LastIdentity         string
	LastHeight           uint64
	LastTransactionIndex uint32
	LastLogIndex         uint32
	ParentDigest         string
	State                Inventory
	CompleteHistory      bool
}

// RebuildDay folds a bounded immutable archive in canonical order, excluding the
// next UTC day. Hash binds algorithm, seed, complete-history flag and input legs.
// Activation and committed-node balance parity are separate serial integration.
func RebuildDay(ctx context.Context, seed Inventory, legs []TimedLeg, day time.Time, complete bool) (Checkpoint, error) {
	if len(legs) > 10000 {
		return Checkpoint{}, errors.New("rebuild page exceeds 10000; use incremental checkpoints")
	}
	if _, e := seed.values(); e != nil {
		return Checkpoint{}, e
	}
	if seed.Known != "0" || seed.Unknown != "0" || seed.Cost != "0" || seed.Realized != "0" || seed.UnknownProceeds != "0" || seed.IncompleteRealized {
		return Checkpoint{}, errors.New("nonempty seed requires checkpoint predecessor/partition integration")
	}
	return rebuildDay(ctx, Checkpoint{State: seed, CompleteHistory: complete}, legs, day)
}

func ContinueDay(ctx context.Context, previous Checkpoint, legs []TimedLeg, day time.Time) (Checkpoint, error) {
	priorDay, err := time.Parse("2006-01-02", previous.Day)
	if err != nil || previous.Algorithm != Algorithm || len(previous.InputDigest) != 64 {
		return Checkpoint{}, errors.New("invalid prior checkpoint")
	}
	if _, err = hex.DecodeString(previous.InputDigest); err != nil {
		return Checkpoint{}, errors.New("invalid prior digest")
	}
	nextDay := time.Date(day.UTC().Year(), day.UTC().Month(), day.UTC().Day(), 0, 0, 0, 0, time.UTC)
	if !priorDay.AddDate(0, 0, 1).Equal(nextDay) {
		return Checkpoint{}, errors.New("checkpoint day gap")
	}
	if _, err = previous.State.values(); err != nil {
		return Checkpoint{}, err
	}
	if err = validateCheckpointPartition(previous); err != nil {
		return Checkpoint{}, err
	}
	return rebuildDay(ctx, previous, legs, day)
}

func rebuildDay(ctx context.Context, previous Checkpoint, legs []TimedLeg, day time.Time) (Checkpoint, error) {
	if len(legs) > 10000 {
		return Checkpoint{}, errors.New("rebuild page exceeds 10000")
	}
	ordered := append([]TimedLeg(nil), legs...)
	sort.Slice(ordered, func(i, j int) bool {
		a, b := ordered[i].Leg, ordered[j].Leg
		if a.Height != b.Height {
			return a.Height < b.Height
		}
		if a.TransactionIndex != b.TransactionIndex {
			return a.TransactionIndex < b.TransactionIndex
		}
		return a.Identity.Log < b.Identity.Log
	})
	start := time.Date(day.UTC().Year(), day.UTC().Month(), day.UTC().Day(), 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 0, 1)
	cp := Checkpoint{Partition: previous.Partition, Day: start.Format("2006-01-02"), Algorithm: Algorithm, State: previous.State, CompleteHistory: previous.CompleteHistory, LastIdentity: previous.LastIdentity, LastHeight: previous.LastHeight, LastTransactionIndex: previous.LastTransactionIndex, LastLogIndex: previous.LastLogIndex, ParentDigest: previous.InputDigest}
	h := sha256.New()
	head, _ := json.Marshal(cp)
	h.Write(head)
	seen := map[string]string{}
	hasPrevious := cp.LastIdentity != ""
	for _, x := range ordered {
		if e := ctx.Err(); e != nil {
			return Checkpoint{}, e
		}
		if x.CommittedAt.Before(start) || !x.CommittedAt.Before(end) {
			continue
		}
		id, e := x.Leg.Identity.Key()
		if e != nil {
			return Checkpoint{}, e
		}
		b, _ := json.Marshal(x)
		if old, ok := seen[id]; ok {
			if old != string(b) {
				return Checkpoint{}, errors.New("conflicting archive identity")
			}
			continue
		}
		partition := "125:" + x.Leg.Wallet + ":" + x.Leg.Token
		if cp.Partition == "" {
			cp.Partition = partition
		} else if cp.Partition != partition {
			return Checkpoint{}, errors.New("mixed checkpoint partition")
		}
		if hasPrevious && (x.Leg.Height < cp.LastHeight || x.Leg.Height == cp.LastHeight && (x.Leg.TransactionIndex < cp.LastTransactionIndex || x.Leg.TransactionIndex == cp.LastTransactionIndex && x.Leg.Identity.Log <= cp.LastLogIndex)) {
			return Checkpoint{}, errors.New("contradictory canonical position")
		}
		if x.Leg.Predecessor != cp.LastIdentity {
			return Checkpoint{}, errors.New("checkpoint predecessor gap")
		}
		seen[id] = string(b)
		hasPrevious = true
		cp.State, e = Apply(cp.State, x.Leg)
		if e != nil {
			return Checkpoint{}, e
		}
		cp.LastIdentity = id
		cp.LastHeight, cp.LastTransactionIndex, cp.LastLogIndex = x.Leg.Height, x.Leg.TransactionIndex, x.Leg.Identity.Log
		h.Write(b)
	}
	cp.InputDigest = hex.EncodeToString(h.Sum(nil))
	return cp, nil
}

func RebuildDayPages(ctx context.Context, previous Checkpoint, legs []TimedLeg, day time.Time) (Checkpoint, error) {
	if len(legs) > 1000000 {
		return Checkpoint{}, errors.New("checkpoint archive exceeds bounded replay")
	}
	start := time.Date(day.UTC().Year(), day.UTC().Month(), day.UTC().Day(), 0, 0, 0, 0, time.UTC)
	if previous.Day == "" {
		if previous.Partition != "" || previous.LastIdentity != "" || previous.InputDigest != "" || previous.LastHeight != 0 || previous.LastTransactionIndex != 0 || previous.LastLogIndex != 0 || previous.State != (Inventory{}) && previous.State != Empty() {
			return Checkpoint{}, errors.New("nonempty seed requires checkpoint predecessor")
		}
		complete := previous.State == (Inventory{}) || previous.CompleteHistory
		previous = Checkpoint{State: Empty(), CompleteHistory: complete}
	} else {
		prior, err := time.Parse("2006-01-02", previous.Day)
		if err != nil || !prior.AddDate(0, 0, 1).Equal(start) || previous.Algorithm != Algorithm || len(previous.InputDigest) != 64 {
			return Checkpoint{}, errors.New("checkpoint day gap")
		}
		if _, err = hex.DecodeString(previous.InputDigest); err != nil {
			return Checkpoint{}, err
		}
		if _, err = previous.State.values(); err != nil {
			return Checkpoint{}, err
		}
		if err = validateCheckpointPartition(previous); err != nil {
			return Checkpoint{}, err
		}
	}
	ordered := append([]TimedLeg(nil), legs...)
	sort.Slice(ordered, func(i, j int) bool {
		a, b := ordered[i].Leg, ordered[j].Leg
		if a.Height != b.Height {
			return a.Height < b.Height
		}
		if a.TransactionIndex != b.TransactionIndex {
			return a.TransactionIndex < b.TransactionIndex
		}
		return a.Identity.Log < b.Identity.Log
	})
	cp := Checkpoint{Partition: previous.Partition, Day: start.Format("2006-01-02"), Algorithm: Algorithm, State: previous.State, CompleteHistory: previous.CompleteHistory, LastIdentity: previous.LastIdentity, LastHeight: previous.LastHeight, LastTransactionIndex: previous.LastTransactionIndex, LastLogIndex: previous.LastLogIndex, ParentDigest: previous.InputDigest}
	h := sha256.New()
	head, err := json.Marshal(cp)
	if err != nil {
		return Checkpoint{}, err
	}
	h.Write(head)
	seen := map[string]string{}
	for offset := 0; offset < len(ordered); offset += 10000 {
		end := offset + 10000
		if end > len(ordered) {
			end = len(ordered)
		}
		for _, x := range ordered[offset:end] {
			if err = ctx.Err(); err != nil {
				return Checkpoint{}, err
			}
			if x.CommittedAt.Before(start) || !x.CommittedAt.Before(start.AddDate(0, 0, 1)) {
				return Checkpoint{}, errors.New("page outside UTC day")
			}
			id, e := x.Leg.Identity.Key()
			if e != nil {
				return Checkpoint{}, e
			}
			raw, e := json.Marshal(x)
			if e != nil {
				return Checkpoint{}, e
			}
			if old, ok := seen[id]; ok {
				if old != string(raw) {
					return Checkpoint{}, errors.New("conflicting archive identity")
				}
				continue
			}
			seen[id] = string(raw)
			partition := "125:" + x.Leg.Wallet + ":" + x.Leg.Token
			if cp.Partition == "" {
				cp.Partition = partition
			} else if cp.Partition != partition {
				return Checkpoint{}, errors.New("mixed checkpoint partition")
			}
			if cp.LastIdentity != "" && (x.Leg.Height < cp.LastHeight || x.Leg.Height == cp.LastHeight && (x.Leg.TransactionIndex < cp.LastTransactionIndex || x.Leg.TransactionIndex == cp.LastTransactionIndex && x.Leg.Identity.Log <= cp.LastLogIndex)) {
				return Checkpoint{}, errors.New("contradictory canonical position")
			}
			if x.Leg.Predecessor != cp.LastIdentity {
				return Checkpoint{}, errors.New("checkpoint predecessor gap")
			}
			cp.State, e = Apply(cp.State, x.Leg)
			if e != nil {
				return Checkpoint{}, e
			}
			cp.LastIdentity = id
			cp.LastHeight, cp.LastTransactionIndex, cp.LastLogIndex = x.Leg.Height, x.Leg.TransactionIndex, x.Leg.Identity.Log
			h.Write(raw)
		}
	}
	cp.InputDigest = hex.EncodeToString(h.Sum(nil))
	return cp, nil
}

func validateCheckpointPartition(previous Checkpoint) error {
	if previous.Partition == "" {
		if previous.State != Empty() || previous.LastIdentity != "" || previous.LastHeight != 0 || previous.LastTransactionIndex != 0 || previous.LastLogIndex != 0 {
			return errors.New("missing prior partition")
		}
		return nil
	}
	parts := strings.Split(previous.Partition, ":")
	identity := strings.Split(previous.LastIdentity, ":")
	if len(parts) != 3 || parts[0] != "125" || !address.MatchString(parts[1]) || !address.MatchString(parts[2]) || len(identity) != 4 || identity[0] != "125" || !address.MatchString(identity[1]) || !hash.MatchString(identity[2]) || identity[3] != strconv.FormatUint(uint64(previous.LastLogIndex), 10) || previous.LastHeight == 0 || previous.LastHeight > 1<<63-1 {
		return errors.New("invalid prior partition or canonical frontier")
	}
	return nil
}
