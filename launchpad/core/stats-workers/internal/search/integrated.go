package search

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"golang.org/x/text/cases"
	"golang.org/x/text/unicode/norm"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/jackc/pgx/v5"
)

type IndexedDocument struct {
	Document
	Creator            string            `json:"creator"`
	CreatedAt          time.Time         `json:"createdAt"`
	LifecycleRevision  int64             `json:"lifecycleRevision"`
	MetricRevision     int64             `json:"metricRevision"`
	MetricsAt          *time.Time        `json:"metricsAt"`
	LiquidityRaw       *string           `json:"liquidityRaw"`
	Volumes            map[string]string `json:"volumes"`
	VerifiedQuoteAsset *bool             `json:"verifiedQuoteAsset"`
	RiskFlags          []string          `json:"riskFlags"`
	PublicExplanation  string            `json:"publicExplanation"`
	ProfileWallet      string            `json:"profileWallet"`
	ProfileLabel       string            `json:"profileLabel"`
	SourceHeight       string            `json:"sourceHeight"`
	SourceBlockHash    string            `json:"sourceBlockHash"`
}

func (d IndexedDocument) Validate() error {
	if d.Document.Validate() != nil || !exactIdentity(d.Creator) || d.CreatedAt.IsZero() || d.CreatedAt.Location() != time.UTC || d.LifecycleRevision < 1 || d.MetricRevision < 0 || d.RiskFlags == nil || d.Volumes == nil || d.ChainHeight != d.SourceHeight || d.PoolAddress != strings.ToLower(d.PoolAddress) || d.TokenAddress != strings.ToLower(d.TokenAddress) || len(d.SourceBlockHash) != 66 || !strings.HasPrefix(d.SourceBlockHash, "0x") {
		return ErrInvalid
	}
	if _, err := hex.DecodeString(d.SourceBlockHash[2:]); err != nil {
		return ErrInvalid
	}
	if n, err := strconv.ParseUint(d.SourceHeight, 10, 64); err != nil || strconv.FormatUint(n, 10) != d.SourceHeight {
		return ErrInvalid
	}
	if d.ProfileWallet != "" && !exactIdentity(d.ProfileWallet) {
		return ErrInvalid
	}
	if d.ProfileLabel != "" && !boundedDisplay(d.ProfileLabel, 100, 400) {
		return ErrInvalid
	}
	if d.PublicExplanation != "" && (!strings.HasPrefix(d.PublicExplanation, "/safety/") || len(d.PublicExplanation) > 256 || strings.ContainsAny(d.PublicExplanation, "\r\n?")) {
		return ErrInvalid
	}
	if (d.LiquidityRaw != nil || len(d.Volumes) > 0 || d.VerifiedQuoteAsset != nil) && (d.MetricsAt == nil || d.MetricRevision < 1) {
		return ErrInvalid
	}
	if d.MetricsAt != nil && (d.MetricsAt.IsZero() || d.MetricsAt.Location() != time.UTC) {
		return ErrInvalid
	}
	if d.LiquidityRaw != nil {
		if _, ok := rawUInt(*d.LiquidityRaw); !ok {
			return ErrInvalid
		}
	}
	if len(d.Volumes) > 3 {
		return ErrInvalid
	}
	for window, value := range d.Volumes {
		if window != "1h" && window != "24h" && window != "7d" {
			return ErrInvalid
		}
		if _, ok := rawUInt(value); !ok {
			return ErrInvalid
		}
	}
	if (Filter{RiskDisclosureFlags: d.RiskFlags}).Validate() != nil {
		return ErrInvalid
	}
	return nil
}
func exactIdentity(s string) bool {
	q, e := Normalize(s)
	return e == nil && q.Kind == "exact_address" && q.Key == s && s != "0x"+strings.Repeat("0", 40)
}

type IntegratedRequest struct {
	Request
	Sort     string            `json:"sort"`
	Position *IntegratedCursor `json:"position,omitempty"`
}
type IntegratedCursor struct {
	Version              int       `json:"version"`
	NormalizationVersion int       `json:"normalizationVersion"`
	Binding              string    `json:"binding"`
	Generation           string    `json:"generation"`
	Snapshot             int64     `json:"snapshot"`
	PrivacyRevision      int64     `json:"privacyRevision"`
	ObservedAt           time.Time `json:"observedAt"`
	After                string    `json:"after"`
}

func integratedBinding(q Query, r IntegratedRequest) string {
	b, _ := json.Marshal([]any{2, Version, q, r.Kind, r.Limit, r.Filter, r.Sort})
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
func (r IntegratedRequest) Validate() (Query, error) {
	q, e := Normalize(r.Query)
	if e != nil {
		return q, e
	}
	if r.Limit < 1 || r.Limit > PublicMaxLimit || r.Kind != "pool" && r.Kind != "token" && r.Kind != "profile" || r.Sort != "address" || r.Cursor != nil || r.Filter.Validate() != nil {
		return q, ErrInvalid
	}
	if r.Kind == "profile" && (r.Filter.PoolStatus != "" || r.Filter.requiresProjection()) {
		return q, ErrInvalid
	}
	if r.Filter.CreationAge != nil {
		for _, n := range []*uint64{r.Filter.CreationAge.MinInclusiveSeconds, r.Filter.CreationAge.MaxExclusiveSeconds} {
			if n != nil && *n > 3155760000 {
				return q, ErrInvalid
			}
		}
	}
	if c := r.Position; c != nil {
		if c.Version != 2 || c.NormalizationVersion != Version || c.Binding != integratedBinding(q, r) || c.Generation == "" || len(c.Generation) > 128 || c.Snapshot < 1 || c.PrivacyRevision < 1 || !exactIdentity(c.After) || c.ObservedAt.IsZero() || c.ObservedAt.Location() != time.UTC {
			return q, ErrInvalid
		}
	}
	return q, nil
}
func ParseIntegratedRequest(values url.Values) (IntegratedRequest, error) {
	r := IntegratedRequest{Request: Request{Limit: PublicMaxLimit}, Sort: "address"}
	for key, v := range values {
		if len(v) != 1 || key != "q" && key != "type" && key != "limit" && key != "filter" && key != "cursor" && key != "sort" {
			return r, ErrInvalid
		}
		if len(v[0]) > 2048 {
			return r, ErrInvalid
		}
	}
	r.Query, r.Kind = values.Get("q"), values.Get("type")
	if s := values.Get("sort"); s != "" {
		r.Sort = s
	}
	if s := values.Get("limit"); s != "" {
		n, e := strconv.Atoi(s)
		if e != nil || strconv.Itoa(n) != s {
			return r, ErrInvalid
		}
		r.Limit = n
	}
	if raw, ok := values["filter"]; ok {
		if strictJSON([]byte(raw[0]), &r.Filter) != nil {
			return r, ErrInvalid
		}
	}
	if raw, ok := values["cursor"]; ok {
		r.Position = &IntegratedCursor{}
		if strictJSON([]byte(raw[0]), r.Position) != nil {
			return r, ErrInvalid
		}
	}
	_, e := r.Validate()
	return r, e
}

type IntegratedHit struct {
	Hit
	Ticker             string     `json:"ticker,omitempty"`
	CreatedAt          *time.Time `json:"createdAt,omitempty"`
	PoolStatus         string     `json:"poolStatus,omitempty"`
	RiskFlags          []string   `json:"riskFlags"`
	ConfusableWarnings []string   `json:"confusableWarnings"`
	PublicExplanation  string     `json:"publicExplanation,omitempty"`
	MetricsAt          *time.Time `json:"metricsAt"`
	QuoteExecutable    bool       `json:"quoteExecutable"`
	Creator            string     `json:"creator"`
	Followed           bool       `json:"followed,omitempty"`
	Saved              bool       `json:"saved,omitempty"`
}
type IntegratedPage struct {
	Items           []IntegratedHit   `json:"items"`
	MatchKind       string            `json:"matchKind"`
	Status          string            `json:"status"`
	Generation      string            `json:"generation"`
	Revision        int64             `json:"revision"`
	PrivacyRevision int64             `json:"privacyRevision"`
	ObservedAt      time.Time         `json:"observedAt"`
	Next            *IntegratedCursor `json:"nextCursor"`
}

func (s Store) SearchIntegrated(ctx context.Context, r IntegratedRequest) (IntegratedPage, error) {
	q, e := r.Validate()
	if e != nil {
		return IntegratedPage{}, e
	}
	if s.Pool == nil {
		return IntegratedPage{}, ErrUnavailable
	}
	if r.Kind == "profile" {
		return s.searchProfiles(ctx, q, r)
	}
	ctx, cancel := context.WithTimeout(ctx, 400*time.Millisecond)
	defer cancel()
	tx, e := s.Pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if e != nil {
		return IntegratedPage{}, fmt.Errorf("%w: begin", ErrUnavailable)
	}
	defer rollbackSearch(tx)
	if _, e = tx.Exec(ctx, "SET LOCAL statement_timeout='150ms'"); e != nil {
		return IntegratedPage{}, ErrUnavailable
	}
	p := IntegratedPage{Items: []IntegratedHit{}, MatchKind: q.Kind, Status: "ok", ObservedAt: time.Now().UTC()}
	if e = tx.QueryRow(ctx, "SELECT generation,revision,privacy_revision FROM stats_search.active_v2 WHERE singleton").Scan(&p.Generation, &p.Revision, &p.PrivacyRevision); e != nil {
		return IntegratedPage{}, ErrUnavailable
	}
	after := ""
	if c := r.Position; c != nil {
		if c.Generation != p.Generation || c.Snapshot != p.Revision || c.PrivacyRevision != p.PrivacyRevision {
			return IntegratedPage{}, ErrInvalid
		}
		if c.ObservedAt.After(p.ObservedAt) || p.ObservedAt.Sub(c.ObservedAt) > 10*time.Minute {
			return IntegratedPage{}, ErrInvalid
		}
		after = c.After
		p.ObservedAt = c.ObservedAt
	}
	query, args := integratedSQL(q, r, p, after)
	rows, e := tx.Query(ctx, query, args...)
	if e != nil {
		return IntegratedPage{}, fmt.Errorf("%w: query", ErrUnavailable)
	}
	for rows.Next() {
		var raw []byte
		var d IndexedDocument
		if e = rows.Scan(&raw); e != nil {
			rows.Close()
			return IntegratedPage{}, ErrUnavailable
		}
		if e = json.Unmarshal(raw, &d); e != nil || d.Validate() != nil {
			rows.Close()
			return IntegratedPage{}, ErrUnavailable
		}
		pool, token := d.PoolAddress, d.TokenAddress
		p.Items = append(p.Items, IntegratedHit{Hit: Hit{Kind: r.Kind, ID: "125:" + pool, Label: d.Name, PoolAddress: &pool, TokenAddress: &token, Score: "1"}, Ticker: d.Ticker, CreatedAt: &d.CreatedAt, PoolStatus: d.PoolStatus, RiskFlags: d.RiskFlags, ConfusableWarnings: d.ConfusableWarnings, PublicExplanation: d.PublicExplanation, MetricsAt: d.MetricsAt, Creator: d.Creator})
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return IntegratedPage{}, ErrUnavailable
	}
	if len(p.Items) > r.Limit {
		p.Items = p.Items[:r.Limit]
		p.Next = &IntegratedCursor{Version: 2, NormalizationVersion: Version, Binding: integratedBinding(q, r), Generation: p.Generation, Snapshot: p.Revision, PrivacyRevision: p.PrivacyRevision, ObservedAt: p.ObservedAt, After: *p.Items[len(p.Items)-1].PoolAddress}
	}
	if len(p.Items) == 0 && q.Kind == "exact_address" {
		p.Status = "not-indexed"
	}
	if e = tx.Commit(ctx); e != nil {
		return IntegratedPage{}, ErrUnavailable
	}
	return p, nil
}
func rollbackSearch(tx pgx.Tx) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_ = tx.Rollback(ctx)
}
func unavailable(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, ErrInvalid) || errors.Is(err, ErrUnsupported) {
		return err
	}
	return fmt.Errorf("%w: storage", ErrUnavailable)
}

func tickerWarnings(ticker string) []string {
	latin, cyrillic, greek := false, false, false
	for _, r := range ticker {
		latin = latin || unicode.Is(unicode.Latin, r)
		cyrillic = cyrillic || unicode.Is(unicode.Cyrillic, r)
		greek = greek || unicode.Is(unicode.Greek, r)
	}
	warnings := []string{}
	count := 0
	for _, present := range []bool{latin, cyrillic, greek} {
		if present {
			count++
		}
	}
	if count > 1 {
		warnings = append(warnings, "mixed_script")
	}
	if cyrillic || greek {
		warnings = append(warnings, "confusable_ticker")
	}
	return warnings
}

func (s Store) searchProfiles(ctx context.Context, q Query, r IntegratedRequest) (IntegratedPage, error) {
	bounded, cancel := context.WithTimeout(ctx, 400*time.Millisecond)
	defer cancel()
	tx, err := s.Pool.BeginTx(bounded, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return IntegratedPage{}, ErrUnavailable
	}
	defer rollbackSearch(tx)
	if _, err = tx.Exec(bounded, "SET LOCAL statement_timeout='150ms'"); err != nil {
		return IntegratedPage{}, ErrUnavailable
	}
	page := IntegratedPage{Items: []IntegratedHit{}, MatchKind: q.Kind, Status: "ok", ObservedAt: time.Now().UTC()}
	if err = tx.QueryRow(bounded, "SELECT generation,revision,privacy_revision FROM stats_search.active_v2 WHERE singleton").Scan(&page.Generation, &page.Revision, &page.PrivacyRevision); err != nil {
		return IntegratedPage{}, ErrUnavailable
	}
	after := ""
	if c := r.Position; c != nil {
		if c.Generation != page.Generation || c.Snapshot != page.Revision || c.PrivacyRevision != page.PrivacyRevision || c.ObservedAt.After(page.ObservedAt) || page.ObservedAt.Sub(c.ObservedAt) > 10*time.Minute {
			return IntegratedPage{}, ErrInvalid
		}
		after = c.After
		page.ObservedAt = c.ObservedAt
	}
	predicate := `d.name_key LIKE $1 ESCAPE '\'`
	pattern := like(q.Key)
	if q.Kind == "exact_address" {
		predicate = `d.wallet=$2`
	}
	if q.Kind == "partial_address" {
		predicate = `d.wallet LIKE $1 ESCAPE '\'`
		pattern = q.Key + "%"
	}
	rows, err := tx.Query(bounded, `SELECT d.wallet,d.display_name FROM stats_search.profile_documents_v2 d JOIN users.account_public_search_v2 a USING(wallet) WHERE $1::text IS NOT NULL AND $2::text IS NOT NULL AND a.public AND NOT a.deleted AND a.revision=d.revision AND a.display_name=d.display_name AND NOT EXISTS(SELECT 1 FROM stats_search.profile_tombstones_v2 t WHERE t.wallet=d.wallet) AND (`+predicate+`) AND ($3='' OR d.wallet>$3) ORDER BY d.wallet LIMIT $4`, pattern, q.Key, after, r.Limit+1)
	if err != nil {
		return IntegratedPage{}, ErrUnavailable
	}
	for rows.Next() {
		var wallet, label string
		if err = rows.Scan(&wallet, &label); err != nil {
			rows.Close()
			return IntegratedPage{}, ErrUnavailable
		}
		if !exactIdentity(wallet) || !boundedDisplay(label, 80, 320) {
			rows.Close()
			return IntegratedPage{}, ErrUnavailable
		}
		address := wallet
		page.Items = append(page.Items, IntegratedHit{Hit: Hit{Kind: "profile", ID: "125:" + wallet, Label: label, Wallet: &address, Score: "1"}, Creator: wallet, RiskFlags: []string{}, ConfusableWarnings: []string{}})
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return IntegratedPage{}, ErrUnavailable
	}
	if len(page.Items) > r.Limit {
		page.Items = page.Items[:r.Limit]
		page.Next = &IntegratedCursor{Version: 2, NormalizationVersion: Version, Binding: integratedBinding(q, r), Generation: page.Generation, Snapshot: page.Revision, PrivacyRevision: page.PrivacyRevision, ObservedAt: page.ObservedAt, After: *page.Items[len(page.Items)-1].Wallet}
	}
	if len(page.Items) == 0 && q.Kind == "exact_address" {
		page.Status = "not-indexed"
	}
	if err = tx.Commit(bounded); err != nil {
		return IntegratedPage{}, ErrUnavailable
	}
	return page, nil
}

func profileNameKey(label string) string {
	label = strings.Map(func(r rune) rune {
		if r == 0x061c || r == 0x200e || r == 0x200f || r >= 0x202a && r <= 0x202e || r >= 0x2066 && r <= 0x2069 {
			return -1
		}
		return r
	}, label)
	return strings.TrimSpace(cases.Fold().String(norm.NFKC.String(label)))
}

func integratedSQL(q Query, r IntegratedRequest, p IntegratedPage, after string) (string, []any) {
	args := []any{p.Generation, like(q.Key), q.Key, p.Revision, after, r.Limit + 1}
	predicate := `(d.name_key LIKE $2 ESCAPE '\' OR d.ticker_key LIKE $2 ESCAPE '\')`
	if q.Kind == "exact_address" {
		predicate = `(d.pool_address=$3 OR d.token_address=$3)`
	}
	if q.Kind == "partial_address" {
		predicate = `(d.pool_address LIKE $2 ESCAPE '\' OR d.token_address LIKE $2 ESCAPE '\')`
		args[1] = q.Key + "%"
	}
	add := func(expression string, value any) {
		args = append(args, value)
		predicate += " AND " + fmt.Sprintf(expression, len(args))
	}
	if r.Filter.PoolStatus != "" {
		add("d.pool_status=$%d", r.Filter.PoolStatus)
	}
	if r.Filter.Creator != "" {
		add("d.creator=$%d", strings.ToLower(r.Filter.Creator))
	}
	if r.Filter.VerifiedQuoteAsset != nil {
		add("d.verified_quote=$%d", *r.Filter.VerifiedQuoteAsset)
	}
	fresh := r.Filter.LiquidityRange != nil || r.Filter.VolumeWindow != nil || r.Filter.VerifiedQuoteAsset != nil
	if fresh {
		add("d.metrics_at >= $%d", p.ObservedAt.Add(-30*time.Second))
		add("d.metrics_at <= $%d", p.ObservedAt)
	}
	if a := r.Filter.CreationAge; a != nil {
		if a.MinInclusiveSeconds != nil {
			add("d.created_at <= $%d", p.ObservedAt.Add(-time.Duration(*a.MinInclusiveSeconds)*time.Second))
		}
		if a.MaxExclusiveSeconds != nil {
			add("d.created_at > $%d", p.ObservedAt.Add(-time.Duration(*a.MaxExclusiveSeconds)*time.Second))
		}
	}
	rangePredicate := func(column string, bounds RawRange) {
		if bounds.MinInclusive != nil {
			add(column+">=$%d::numeric", *bounds.MinInclusive)
		}
		if bounds.MaxExclusive != nil {
			add(column+"<$%d::numeric", *bounds.MaxExclusive)
		}
	}
	if l := r.Filter.LiquidityRange; l != nil {
		rangePredicate("d.liquidity_raw", l.Range)
	}
	if v := r.Filter.VolumeWindow; v != nil {
		column := map[string]string{"1h": "d.volume_1h", "24h": "d.volume_24h", "7d": "d.volume_7d"}[v.Window]
		rangePredicate(column, v.Range)
	}
	if len(r.Filter.RiskDisclosureFlags) > 0 {
		add("d.risk_flags @> $%d::text[]", r.Filter.RiskDisclosureFlags)
	}
	return `SELECT d.payload FROM stats_search.documents_v2 d JOIN stats_search.authority_v2 a USING(pool_address) WHERE d.generation=$1 AND $2::text IS NOT NULL AND (` + predicate + `) AND a.public AND NOT a.tombstone AND a.visibility_revision>=d.visibility_revision AND d.revision<=$4 AND ($5='' OR (CASE WHEN d.pool_address=$3 THEN 0 ELSE 1 END,d.pool_address)>(CASE WHEN $5=$3 THEN 0 ELSE 1 END,$5)) ORDER BY (d.pool_address=$3) DESC,d.pool_address ASC LIMIT $6`, args
}

type PlanMeasurement struct {
	Plan       json.RawMessage `json:"plan"`
	Generation string          `json:"generation"`
	Revision   int64           `json:"revision"`
	ObservedAt time.Time       `json:"observedAt"`
	SQLDigest  string          `json:"sqlDigest"`
}

func (s Store) MeasurePlan(ctx context.Context, r IntegratedRequest) (PlanMeasurement, error) {
	q, err := r.Validate()
	if err != nil || r.Kind == "profile" || r.Position != nil {
		return PlanMeasurement{}, ErrInvalid
	}
	if s.Pool == nil {
		return PlanMeasurement{}, ErrUnavailable
	}
	bounded, cancel := context.WithTimeout(ctx, 400*time.Millisecond)
	defer cancel()
	tx, err := s.Pool.BeginTx(bounded, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return PlanMeasurement{}, unavailable(err)
	}
	defer rollbackSearch(tx)
	if _, err = tx.Exec(bounded, "SET LOCAL statement_timeout='150ms'"); err != nil {
		return PlanMeasurement{}, unavailable(err)
	}
	page := IntegratedPage{ObservedAt: time.Now().UTC()}
	if err = tx.QueryRow(bounded, "SELECT generation,revision,privacy_revision FROM stats_search.active_v2 WHERE singleton").Scan(&page.Generation, &page.Revision, &page.PrivacyRevision); err != nil {
		return PlanMeasurement{}, unavailable(err)
	}
	query, args := integratedSQL(q, r, page, "")
	var raw []byte
	if err = tx.QueryRow(bounded, "EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) "+query, args...).Scan(&raw); err != nil {
		return PlanMeasurement{}, unavailable(err)
	}
	if err = tx.Commit(bounded); err != nil {
		return PlanMeasurement{}, unavailable(err)
	}
	digest := sha256.Sum256([]byte(query))
	return PlanMeasurement{Plan: raw, Generation: page.Generation, Revision: page.Revision, ObservedAt: page.ObservedAt, SQLDigest: hex.EncodeToString(digest[:])}, nil
}
