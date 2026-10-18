package policy

import (
	"math"
	"sort"
	"time"
)

type AttackProfile struct {
	API, Writes, Quotes, RedisLossWrites int
	ReadP95, ReadP99, MiddlewareP99      time.Duration
}

func FrozenAttackProfiles() map[string]AttackProfile {
	return map[string]AttackProfile{"steady": {10000, 2000, 2000, 2000, 150 * time.Millisecond, 400 * time.Millisecond, 10 * time.Millisecond}, "surge": {20000, 4000, 4000, 2000, 150 * time.Millisecond, 400 * time.Millisecond, 10 * time.Millisecond}}
}

type AdversarialSample struct {
	Class                            string
	RequestID                        string
	Observed                         time.Time
	RedisUnavailable, CachedRead     bool
	Valid, Admitted                  bool
	Status                           int
	Duration, Authorization, Limiter time.Duration
}
type AdversarialEvidence struct {
	ArtifactDigest, ConfigurationRevision string
	Started, Ended                        time.Time
	RedisLossStarted, RedisLossEnded      time.Time
	Replicas, GeneratorRPS                int
	CPUCores, MemoryBytes                 int64
	Samples                               []AdversarialSample
}

func ValidateAdversarialEvidence(profile AttackProfile, e AdversarialEvidence) error {
	frozen := false
	for _, candidate := range FrozenAttackProfiles() {
		if profile == candidate {
			frozen = true
		}
	}
	if !frozen || !digestPattern.MatchString(e.ArtifactDigest) || e.ConfigurationRevision == "" || !e.Ended.After(e.Started) || e.RedisLossStarted.Before(e.Started) || !e.RedisLossEnded.After(e.RedisLossStarted) || e.RedisLossEnded.After(e.Ended) || e.Replicas < 1 || e.GeneratorRPS < profile.API || e.CPUCores < 1 || e.MemoryBytes < 1 || len(e.Samples) == 0 {
		return ErrDenied
	}
	reads := []time.Duration{}
	auth := []time.Duration{}
	limiter := []time.Duration{}
	valid, writes, quotes, rejectedAttacks, outageWrites, outageCached := 0, 0, 0, 0, 0, 0
	seen := map[string]bool{}
	for _, s := range e.Samples {
		if validateAdversarialSample(s) != nil || seen[s.RequestID] || s.Observed.Before(e.Started) || !s.Observed.Before(e.Ended) {
			return ErrDenied
		}
		seen[s.RequestID] = true
		if !validRouteClass(s.Class) && s.Class != "authentication" {
			return ErrDenied
		}
		if !s.Valid {
			rejectedAttacks++
		}
		if s.RedisUnavailable != (!s.Observed.Before(e.RedisLossStarted) && s.Observed.Before(e.RedisLossEnded)) {
			return ErrDenied
		}
		if s.RedisUnavailable {
			if s.Class == "account-write" {
				outageWrites++
			}
			if s.Class == "account-write" || s.Class == "quote" || s.Class == "authentication" {
				if s.Admitted || s.Status != 401 && s.Status != 403 && s.Status != 503 {
					return ErrDenied
				}
			}
			if s.Admitted {
				if !s.Valid || s.Class != "public-read" || !s.CachedRead {
					return ErrDenied
				}
				outageCached++
			}
		}
		if !s.Valid && s.Admitted {
			return ErrDenied
		}
		auth = append(auth, s.Authorization)
		limiter = append(limiter, s.Limiter)
		if s.Valid && s.Admitted && !s.RedisUnavailable {
			valid++
			switch s.Class {
			case "public-read", "account-read":
				reads = append(reads, s.Duration)
			case "account-write":
				writes++
			case "quote":
				quotes++
			default:
				return ErrDenied
			}
		}
	}
	seconds := (e.Ended.Sub(e.Started) - e.RedisLossEnded.Sub(e.RedisLossStarted)).Seconds()
	outageSeconds := e.RedisLossEnded.Sub(e.RedisLossStarted).Seconds()
	if seconds <= 0 {
		return ErrDenied
	}
	if rejectedAttacks == 0 || outageCached == 0 || float64(outageWrites)/outageSeconds < float64(profile.RedisLossWrites) || float64(valid)/seconds < float64(profile.API) || float64(writes)/seconds < float64(profile.Writes) || float64(quotes)/seconds < float64(profile.Quotes) || len(reads) == 0 {
		return ErrDenied
	}
	percentile := func(v []time.Duration, p float64) time.Duration {
		sort.Slice(v, func(i, j int) bool { return v[i] < v[j] })
		return v[int(math.Ceil(float64(len(v))*p))-1]
	}
	if percentile(reads, .95) > profile.ReadP95 || percentile(reads, .99) > profile.ReadP99 || percentile(auth, .99) > profile.MiddlewareP99 || percentile(limiter, .99) > profile.MiddlewareP99 {
		return ErrDenied
	}
	return nil
}

func validateAdversarialSample(s AdversarialSample) error {
	if s.RequestID == "" || len(s.RequestID) > 128 || s.Observed.IsZero() || s.Status < 200 || s.Status > 599 || s.Duration <= 0 || s.Authorization < 0 || s.Limiter < 0 || s.Authorization > s.Duration || s.Limiter > s.Duration || s.Admitted && s.Status >= 300 || !s.Admitted && s.Status < 400 || !s.Valid && s.Admitted {
		return ErrDenied
	}
	if !validRouteClass(s.Class) && s.Class != "authentication" {
		return ErrDenied
	}
	if s.RedisUnavailable && (s.Class == "account-write" || s.Class == "quote" || s.Class == "authentication") && (s.Admitted || s.Status != 401 && s.Status != 403 && s.Status != 503) {
		return ErrDenied
	}
	if s.RedisUnavailable && s.Admitted && (!s.Valid || s.Class != "public-read" || !s.CachedRead) {
		return ErrDenied
	}
	return nil
}
