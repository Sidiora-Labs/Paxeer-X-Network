// Package stream fixes the PERPS_V1 JetStream stream and its subject layout.
// ValidFilter and Matches originate in Launch-x shared/delivery/stream.go.
package stream

import (
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/nats-io/nats.go/jetstream"
)

// Name is the one JetStream stream of the perps data services.
const Name = "PERPS_V1"

// MaxMessageBytes bounds every message of the stream.
const MaxMessageBytes = 1048576

// Family is a subject family of contracts/data-services.md section 3.
type Family string

const (
	FamilyMarket Family = "market"
	FamilyUser   Family = "user"
	FamilyChain  Family = "chain"
	FamilySpot   Family = "spot"
)

var kinds = map[Family][]string{
	FamilyMarket: {"fill", "order", "funding", "index", "liq", "mark", "oi", "market"},
	FamilyUser:   {"position", "order", "fill", "margin", "funding", "ledger"},
	FamilyChain:  {"block", "tx", "rewind"},
	FamilySpot:   {"swap", "order", "vault"},
}

var subjectName = regexp.MustCompile(`^[A-Za-z0-9_-]+(\.[A-Za-z0-9_-]+)*$`)

// Config returns the PERPS_V1 stream configuration for chain.
func Config(chain uint64) jetstream.StreamConfig {
	return jetstream.StreamConfig{
		Name:       Name,
		Subjects:   []string{prefix(chain) + ">"},
		Storage:    jetstream.FileStorage,
		Retention:  jetstream.LimitsPolicy,
		Replicas:   3,
		MaxAge:     72 * time.Hour,
		Discard:    jetstream.DiscardNew,
		DenyDelete: true,
		DenyPurge:  true,
		Duplicates: 2 * time.Minute,
		MaxMsgSize: MaxMessageBytes,
	}
}

func prefix(chain uint64) string {
	return "perps." + strconv.FormatUint(chain, 10) + "."
}

// Subject builds the subject of kind in family for chain. entity is the
// decimal market id, the lower-case hex account or token, and empty for the
// chain family. A kind outside the family's table is refused.
func Subject(chain uint64, family Family, entity, kind string) (string, error) {
	table, ok := kinds[family]
	if !ok {
		return "", fmt.Errorf("stream: unknown subject family %q", family)
	}
	if !slices.Contains(table, kind) {
		return "", fmt.Errorf("stream: unknown %s kind %q", family, kind)
	}
	var subject string
	switch family {
	case FamilyMarket:
		subject = prefix(chain) + entity + "." + kind
	case FamilyChain:
		if entity != "" {
			return "", fmt.Errorf("stream: chain subject takes no entity, got %q", entity)
		}
		subject = prefix(chain) + "chain." + kind
	default:
		subject = prefix(chain) + string(family) + "." + entity + "." + kind
	}
	if len(subject) > 256 || !subjectName.MatchString(subject) {
		return "", fmt.Errorf("stream: invalid subject %q", subject)
	}
	return subject, nil
}

// MarketSubject builds perps.<chain>.<market>.<kind>.
func MarketSubject(chain, market uint64, kind string) (string, error) {
	return Subject(chain, FamilyMarket, strconv.FormatUint(market, 10), kind)
}

// UserSubject builds perps.<chain>.user.<account>.<kind>.
func UserSubject(chain uint64, account common.Address, kind string) (string, error) {
	return Subject(chain, FamilyUser, strings.ToLower(account.Hex()), kind)
}

// ChainSubject builds perps.<chain>.chain.<kind>.
func ChainSubject(chain uint64, kind string) (string, error) {
	return Subject(chain, FamilyChain, "", kind)
}

// SpotSubject builds perps.<chain>.spot.<token>.<kind>.
func SpotSubject(chain uint64, token common.Address, kind string) (string, error) {
	return Subject(chain, FamilySpot, strings.ToLower(token.Hex()), kind)
}

var filterToken = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

// ValidFilter reports whether filter is a bounded NATS subject filter
// ('*' matches one token, '>' one or more trailing tokens).
func ValidFilter(filter string) bool {
	if len(filter) == 0 || len(filter) > 256 {
		return false
	}
	tokens := strings.Split(filter, ".")
	for i, t := range tokens {
		if t == ">" && i == len(tokens)-1 {
			continue
		}
		if t != "*" && !filterToken.MatchString(t) {
			return false
		}
	}
	return true
}

// Matches reports whether a literal subject is captured by filter.
func Matches(filter, subject string) bool {
	f, s := strings.Split(filter, "."), strings.Split(subject, ".")
	for i, t := range f {
		if t == ">" {
			return len(s) > i
		}
		if i >= len(s) || (t != "*" && t != s[i]) {
			return false
		}
	}
	return len(f) == len(s)
}
