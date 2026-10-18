// Package search owns canonical public search projection contracts. These are
// internal v1 contracts; the frozen HTTP DTO deliberately remains narrower.
package search

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ethereum/go-ethereum/common"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
	"golang.org/x/text/unicode/norm"
)

const Version = 1
const PublicMaxLimit = 25
const InternalMaxLimit = 50

var ErrInvalid = errors.New("invalid search request")
var ErrUnavailable = errors.New("search unavailable")
var ErrUnsupported = fmt.Errorf("%w: capability not integrated", ErrUnavailable)

type Query struct {
	Key  string
	Kind string
}

func Normalize(raw string) (Query, error) {
	if len(raw) > 1024 || strings.ContainsRune(raw, 0) || !utf8.ValidString(raw) || utf8.RuneCountInString(raw) > 256 {
		return Query{}, fmt.Errorf("%w: query bounds or UTF-8", ErrInvalid)
	}
	s := strings.TrimSpace(raw)
	if strings.HasPrefix(strings.ToLower(s), "0x") {
		if len(s) == 42 {
			if !common.IsHexAddress(s) {
				return Query{}, fmt.Errorf("%w: address", ErrInvalid)
			}
			body := s[2:]
			if body != strings.ToLower(body) && body != strings.ToUpper(body) && common.HexToAddress(s).Hex() != s {
				return Query{}, fmt.Errorf("%w: checksum", ErrInvalid)
			}
			return Query{strings.ToLower(s), "exact_address"}, nil
		}
		if len(s) == 66 {
			return Query{}, fmt.Errorf("%w: transaction lookup unsupported", ErrInvalid)
		}
		if len(s) > 42 {
			return Query{}, fmt.Errorf("%w: address length", ErrInvalid)
		}
		if len(s) < 4 {
			return Query{}, fmt.Errorf("%w: partial address", ErrInvalid)
		}
		if _, err := hex.DecodeString(s[2:] + strings.Repeat("0", len(s[2:])%2)); err != nil {
			return Query{}, fmt.Errorf("%w: partial address", ErrInvalid)
		}
		return Query{strings.ToLower(s), "partial_address"}, nil
	}
	s = strings.Map(func(r rune) rune {
		if r == 0x061c || r == 0x200e || r == 0x200f || (r >= 0x202a && r <= 0x202e) || (r >= 0x2066 && r <= 0x2069) {
			return -1
		}
		return r
	}, s)
	s = strings.TrimSpace(cases.Fold().String(norm.NFKC.String(s)))
	if len(s) > 1024 || utf8.RuneCountInString(s) > 256 {
		return Query{}, fmt.Errorf("%w: normalized query bounds", ErrInvalid)
	}
	if utf8.RuneCountInString(s) < 2 {
		return Query{}, fmt.Errorf("%w: minimum two code points", ErrInvalid)
	}
	return Query{s, "text"}, nil
}

type Document struct {
	Version            int      `json:"version"`
	ChainID            string   `json:"chainId"`
	PoolAddress        string   `json:"poolAddress"`
	TokenAddress       string   `json:"tokenAddress"`
	Name               string   `json:"name"`
	Ticker             string   `json:"ticker"`
	NameKey            string   `json:"nameKey"`
	CanonicalTicker    string   `json:"canonicalTicker"`
	LanguageHints      []string `json:"languageHints"`
	ConfusableWarnings []string `json:"confusableWarnings"`
	CreationEventID    string   `json:"creationEventId"`
	ChainHeight        string   `json:"chainHeight"`
	MetadataRevision   int64    `json:"metadataRevision"`
	VisibilityRevision int64    `json:"visibilityDecisionRevision"`
	PoolStatus         string   `json:"poolStatus"`
	IndexedAt          string   `json:"indexedAt"`
}

func (d Document) Key() string { return d.ChainID + ":" + strings.ToLower(d.PoolAddress) }

// Only allowlisted canonical pool state is public. Unknown authority fails closed.
func (d Document) Validate() error {
	if d.Version != Version || d.ChainID != "125" || d.CreationEventID == "" || d.MetadataRevision < 1 || d.VisibilityRevision < 1 || (d.PoolStatus != "active" && d.PoolStatus != "paused") {
		return ErrInvalid
	}
	for _, a := range []string{d.PoolAddress, d.TokenAddress} {
		q, e := Normalize(a)
		if e != nil || q.Kind != "exact_address" || common.HexToAddress(a) == (common.Address{}) {
			return ErrInvalid
		}
	}
	if !boundedDisplay(d.Name, 100, 400) || !boundedDisplay(d.Ticker, 32, 128) {
		return ErrInvalid
	}
	n, e := Normalize(d.Name)
	if e != nil || d.NameKey != n.Key || d.CanonicalTicker != canonicalTicker(d.Ticker) || d.CanonicalTicker == "" {
		return ErrInvalid
	}
	if len(d.LanguageHints) > 8 || len(d.ConfusableWarnings) > 2 {
		return ErrInvalid
	}
	for _, hint := range d.LanguageHints {
		if len(hint) > 35 {
			return ErrInvalid
		}
		if _, e := language.Parse(hint); e != nil {
			return ErrInvalid
		}
	}
	seenWarnings := map[string]bool{}
	for _, warning := range d.ConfusableWarnings {
		if (warning != "mixed_script" && warning != "confusable_ticker") || seenWarnings[warning] {
			return ErrInvalid
		}
		seenWarnings[warning] = true
	}
	if len(d.CreationEventID) > 256 {
		return ErrInvalid
	}
	for _, r := range d.CreationEventID {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("-_:./", r)) {
			return ErrInvalid
		}
	}
	height, e := strconv.ParseUint(d.ChainHeight, 10, 64)
	if e != nil || strconv.FormatUint(height, 10) != d.ChainHeight {
		return ErrInvalid
	}
	// Frozen timestamp spelling: UTC with exactly millisecond precision.
	stamp, e := time.Parse("2006-01-02T15:04:05.000Z", d.IndexedAt)
	if e != nil || stamp.Year() < 1 || stamp.Format("2006-01-02T15:04:05.000Z") != d.IndexedAt {
		return ErrInvalid
	}
	return nil
}
func boundedDisplay(s string, points, bytes int) bool {
	return utf8.ValidString(s) && !strings.ContainsRune(s, 0) && len(s) <= bytes && utf8.RuneCountInString(s) <= points && strings.TrimSpace(s) != ""
}

type Filter struct {
	Version             int              `json:"version,omitempty"`
	PoolStatus          string           `json:"poolStatus,omitempty"`
	CreationAge         *AgeRange        `json:"creationAge,omitempty"`
	VerifiedQuoteAsset  *bool            `json:"verifiedQuoteAsset,omitempty"`
	VolumeWindow        *VolumeFilter    `json:"volumeWindow,omitempty"`
	LiquidityRange      *LiquidityFilter `json:"liquidityRange,omitempty"`
	Creator             string           `json:"creator,omitempty"`
	RiskDisclosureFlags []string         `json:"riskDisclosureFlags,omitempty"`
}
type Request struct {
	Query  string
	Kind   string
	Limit  int
	Filter Filter
	Cursor *Cursor
}

// Cursor is an internal contract, not an extension of frozen IF_CURSOR (which
// has no search sort kind). Integrator must approve its public representation.
type Cursor struct {
	Version              int    `json:"version"`
	NormalizationVersion int    `json:"normalizationVersion"`
	FilterDigest         string `json:"filterDigest"`
	Generation           string `json:"generation"`
	Snapshot             int64  `json:"snapshot"`
	After                string `json:"after"`
}

func Binding(q Query, r Request) string {
	b, _ := json.Marshal([]any{Version, q, r.Kind, r.Limit, r.Filter})
	h := sha256.Sum256(b)
	return "0x" + hex.EncodeToString(h[:])
}
func ValidateRequest(r Request) (Query, error) {
	q, e := Normalize(r.Query)
	if e != nil {
		return q, e
	}
	if r.Limit < 1 || r.Limit > InternalMaxLimit || r.Kind != "pool" && r.Kind != "token" {
		return q, ErrInvalid
	}
	if e := r.Filter.Validate(); e != nil {
		return q, e
	}
	if r.Filter.requiresProjection() {
		return q, fmt.Errorf("%w: filter projection not integrated", ErrUnsupported)
	}
	if c := r.Cursor; c != nil {
		if c.Version != Version || c.NormalizationVersion != Version || c.FilterDigest != Binding(q, r) || c.Generation == "" || c.Snapshot < 1 || !common.IsHexAddress(c.After) {
			return q, ErrInvalid
		}
	}
	return q, nil
}

type Hit struct {
	Kind         string  `json:"kind"`
	ID           string  `json:"id"`
	Label        string  `json:"label"`
	PoolAddress  *string `json:"poolAddress"`
	TokenAddress *string `json:"tokenAddress"`
	Wallet       *string `json:"wallet"`
	Score        string  `json:"score"`
}
type Page struct {
	Items      []Hit   `json:"items"`
	MatchKind  string  `json:"matchKind"`
	Status     string  `json:"status"`
	Generation string  `json:"generation"`
	Watermark  int64   `json:"watermark"`
	Next       *Cursor `json:"next,omitempty"`
}

func like(s string) string {
	return "%" + strings.NewReplacer("\\", "\\\\", "%", "\\%", "_", "\\_").Replace(s) + "%"
}

type PublicRequestError struct {
	Status int
	Code   string
}

func (e PublicRequestError) Error() string { return e.Code }

func ParsePublicRequest(values url.Values) (Request, error) {
	r := Request{Limit: PublicMaxLimit}
	invalid := func(status int, code string) (Request, error) { return r, PublicRequestError{status, code} }
	for key, vs := range values {
		if key != "q" && key != "type" && key != "limit" && key != "filter" && key != "cursor" {
			return invalid(400, "QUERY_INVALID")
		}
		if len(vs) != 1 {
			return invalid(400, "QUERY_INVALID")
		}
		maximum := 2048
		if key == "q" {
			maximum = 1024
		}
		if len(vs[0]) > maximum {
			return invalid(413, "PAYLOAD_TOO_LARGE")
		}
	}
	r.Query, r.Kind = values.Get("q"), values.Get("type")
	if _, err := Normalize(r.Query); err != nil {
		return invalid(422, "FIELD_INVALID")
	}
	if r.Kind != "pool" && r.Kind != "token" && r.Kind != "profile" {
		return invalid(422, "FIELD_INVALID")
	}
	if raw, ok := values["limit"]; ok {
		n, err := strconv.Atoi(raw[0])
		if err != nil || strconv.Itoa(n) != raw[0] || n < 1 || n > PublicMaxLimit {
			return invalid(422, "FIELD_INVALID")
		}
		r.Limit = n
	}
	decode := func(raw string, target any) error {
		return strictJSON([]byte(raw), target)
	}
	if raw, ok := values["filter"]; ok {
		if err := decode(raw[0], &r.Filter); err != nil {
			return invalid(400, "QUERY_INVALID")
		}
	}
	filter, err := r.Filter.Public()
	if err != nil {
		if errors.Is(err, ErrUnsupported) {
			return invalid(501, "FEATURE_UNAVAILABLE")
		}
		return invalid(422, "FIELD_INVALID")
	}
	r.Filter = filter
	if raw, ok := values["cursor"]; ok {
		r.Cursor = &Cursor{}
		if err := decode(raw[0], r.Cursor); err != nil {
			return invalid(400, "CURSOR_INVALID")
		}
	}
	if r.Kind == "profile" {
		return invalid(412, "PRECONDITION_FAILED")
	}
	if _, err := ValidateRequest(r); err != nil {
		if errors.Is(err, ErrUnsupported) {
			return invalid(501, "FEATURE_UNAVAILABLE")
		}
		if r.Cursor != nil {
			return invalid(400, "CURSOR_INVALID")
		}
		return invalid(422, "FIELD_INVALID")
	}
	return r, nil
}
