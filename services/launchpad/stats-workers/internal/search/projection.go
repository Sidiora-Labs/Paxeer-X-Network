package search

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Sidiora-Technologies/KindleLaunch/shared/delivery"
	"github.com/jackc/pgx/v5"
)

var sourceName = regexp.MustCompile(`^[a-z][a-z0-9_]{0,62}$`)

type Change struct {
	Version       int                 `json:"version"`
	Kind          string              `json:"kind"`
	Revision      int64               `json:"revision"`
	PoolAddress   string              `json:"poolAddress"`
	Document      *IndexedDocument    `json:"document,omitempty"`
	Public        bool                `json:"public"`
	Tombstone     bool                `json:"tombstone"`
	ProfileWallet string              `json:"profileWallet,omitempty"`
	Preferences   *PreferenceSnapshot `json:"preferences,omitempty"`
}

func strictJSON(raw []byte, value any) error {
	if len(raw) == 0 || len(raw) > delivery.MaxEnvelopeBytes || !utf8.Valid(raw) || !bytes.HasPrefix(bytes.TrimSpace(raw), []byte("{")) {
		return ErrInvalid
	}
	tokens := json.NewDecoder(bytes.NewReader(raw))
	if uniqueJSON(tokens, 0) != nil {
		return ErrInvalid
	}
	if _, err := tokens.Token(); err != io.EOF {
		return ErrInvalid
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(value) != nil {
		return ErrInvalid
	}
	if d.Decode(new(any)) != io.EOF {
		return ErrInvalid
	}
	return nil
}
func uniqueJSON(d *json.Decoder, depth int) error {
	if depth > 16 {
		return ErrInvalid
	}
	token, err := d.Token()
	if err != nil {
		return err
	}
	delimiter, compound := token.(json.Delim)
	if !compound {
		return nil
	}
	switch delimiter {
	case '{':
		seen := map[string]bool{}
		for d.More() {
			key, err := d.Token()
			if err != nil {
				return err
			}
			name, ok := key.(string)
			name = strings.ToLower(name)
			if !ok || seen[name] {
				return ErrInvalid
			}
			seen[name] = true
			if err = uniqueJSON(d, depth+1); err != nil {
				return err
			}
		}
		end, err := d.Token()
		if err != nil || end != json.Delim('}') {
			return ErrInvalid
		}
	case '[':
		for d.More() {
			if err = uniqueJSON(d, depth+1); err != nil {
				return err
			}
		}
		end, err := d.Token()
		if err != nil || end != json.Delim(']') {
			return ErrInvalid
		}
	default:
		return ErrInvalid
	}
	return nil
}
func (c Change) Validate() error {
	if c.Version != 2 || c.Revision < 1 {
		return ErrInvalid
	}
	switch c.Kind {
	case "creation", "metadata", "lifecycle", "metrics":
		if c.Document == nil || c.Document.Validate() != nil || c.PoolAddress != c.Document.PoolAddress {
			return ErrInvalid
		}
	case "visibility":
		if !exactIdentity(c.PoolAddress) {
			return ErrInvalid
		}
	case "profile_deletion":
		if !exactIdentity(c.ProfileWallet) {
			return ErrInvalid
		}
	case "preferences":
		if c.Preferences == nil || c.Preferences.Validate() != nil || c.Preferences.Revision != c.Revision {
			return ErrInvalid
		}
	default:
		return ErrInvalid
	}
	return nil
}

type SourceRelation struct{ Schema, View string }
type Projection struct {
	Delivery              *delivery.Store
	ConsumerID            string
	InboxGeneration       string
	Sources               map[string]SourceRelation
	InvalidationSubject   string
	InvalidationEventType string
}

func (p *Projection) Consume(ctx context.Context, subject string, wire []byte, message delivery.Delivery) error {
	if p == nil || p.Delivery == nil || p.ConsumerID == "" || p.InboxGeneration == "" || p.InvalidationSubject == "" || p.InvalidationEventType == "" {
		return ErrUnavailable
	}
	bounded, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	return p.Delivery.Consume(bounded, p.ConsumerID, p.InboxGeneration, subject, wire, message, func(ctx context.Context, tx pgx.Tx, e delivery.Envelope) error {
		var c Change
		if strictJSON(e.Payload, &c) != nil || c.Validate() != nil {
			return ErrInvalid
		}
		if err := p.authorizeTx(ctx, tx, e, c); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, "SET LOCAL statement_timeout='2s'"); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, "SET LOCAL lock_timeout='100ms'"); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, "SELECT revision FROM stats_search.active_v2 WHERE singleton FOR UPDATE"); err != nil {
			return err
		}
		changed, err := applyChangeTx(ctx, tx, c)
		if err != nil || !changed {
			return err
		}
		var revision int64
		if err = tx.QueryRow(ctx, "UPDATE stats_search.active_v2 SET revision=nextval('stats_search.revision_v2'),privacy_revision=privacy_revision+1 WHERE singleton RETURNING revision").Scan(&revision); err != nil {
			return err
		}
		payload, _ := json.Marshal(map[string]any{"version": 2, "revision": strconv.FormatInt(revision, 10)})
		invalidation := delivery.Envelope{EventID: "search_" + delivery.Hash([]byte(e.EventID)), EventType: p.InvalidationEventType, SchemaVersion: 1, ChainID: 125, SourceOrder: e.SourceOrder, EmittedAt: e.EmittedAt, TraceID: e.TraceID, PayloadHash: delivery.Hash(payload), Payload: payload}
		return p.Delivery.Enqueue(ctx, tx, p.InvalidationSubject, invalidation)
	})
}
func (p *Projection) authorizeTx(ctx context.Context, tx pgx.Tx, e delivery.Envelope, c Change) error {
	if c.Kind == "creation" {
		d := c.Document
		if d.MetadataRevision != 1 || d.LifecycleRevision != 1 || d.MetricRevision != 0 || d.MetricsAt != nil || d.LiquidityRaw != nil || len(d.Volumes) != 0 || d.VerifiedQuoteAsset != nil || len(d.RiskFlags) != 0 || d.PublicExplanation != "" || d.ProfileWallet != "" || d.ProfileLabel != "" || d.PoolStatus != "active" {
			return delivery.ErrIntegrity
		}
		var raw []byte
		if err := tx.QueryRow(ctx, `SELECT payload FROM indexer.indexer_canonical_creations_v1 WHERE pool=$1 AND token=$2 AND creator=$3 AND height=$4::bigint AND block_hash=$5`, d.PoolAddress, d.TokenAddress, d.Creator, d.SourceHeight, d.SourceBlockHash).Scan(&raw); err != nil {
			return err
		}
		var fact struct {
			Pool                  string `json:"pool"`
			Token                 string `json:"token"`
			Creator               string `json:"creator"`
			BlockTimestampSeconds string `json:"blockTimestampSeconds"`
			Metadata              *struct {
				Name   string `json:"name"`
				Symbol string `json:"symbol"`
			} `json:"metadata"`
		}
		if json.Unmarshal(raw, &fact) != nil || fact.Metadata == nil || fact.Metadata.Name != d.Name || fact.Metadata.Symbol != d.Ticker || fact.Pool != d.PoolAddress || fact.Token != d.TokenAddress || fact.Creator != d.Creator {
			return delivery.ErrIntegrity
		}
		ts, err := strconv.ParseInt(fact.BlockTimestampSeconds, 10, 64)
		if err != nil || !time.Unix(ts, 0).UTC().Equal(d.CreatedAt) || d.SourceHeight != e.SourceOrder.Height {
			return delivery.ErrIntegrity
		}
		return nil
	}
	source, ok := p.Sources[c.Kind]
	if !ok || !sourceName.MatchString(source.Schema) || !sourceName.MatchString(source.View) || !strings.HasSuffix(source.View, "_v2") {
		return ErrUnsupported
	}
	relation := pgx.Identifier{source.Schema, source.View}.Sanitize()
	var raw []byte
	if err := tx.QueryRow(ctx, "SELECT payload FROM "+relation+" WHERE event_id=$1", e.EventID).Scan(&raw); err != nil {
		return err
	}
	var expected Change
	if strictJSON(raw, &expected) != nil {
		return delivery.ErrIntegrity
	}
	a, _ := json.Marshal(c)
	b, _ := json.Marshal(expected)
	if !bytes.Equal(a, b) {
		return delivery.ErrIntegrity
	}
	return nil
}
func applyChangeTx(ctx context.Context, tx pgx.Tx, c Change) (bool, error) {
	switch c.Kind {
	case "visibility":
		result, err := tx.Exec(ctx, `INSERT INTO stats_search.authority_v2(pool_address,visibility_revision,public,tombstone) VALUES($1,$2,$3,$4) ON CONFLICT(pool_address) DO UPDATE SET visibility_revision=excluded.visibility_revision,public=excluded.public,tombstone=excluded.tombstone WHERE stats_search.authority_v2.visibility_revision<excluded.visibility_revision`, c.PoolAddress, c.Revision, c.Public, c.Tombstone)
		return result.RowsAffected() > 0, err
	case "profile_deletion":
		result, err := tx.Exec(ctx, `INSERT INTO stats_search.profile_tombstones_v2(wallet,revision) VALUES($1,$2) ON CONFLICT(wallet) DO UPDATE SET revision=excluded.revision WHERE stats_search.profile_tombstones_v2.revision<excluded.revision`, c.ProfileWallet, c.Revision)
		if err != nil || result.RowsAffected() == 0 {
			return false, err
		}
		_, err = tx.Exec(ctx, `UPDATE stats_search.documents_v2 SET payload=jsonb_set(jsonb_set(payload,'{profileLabel}','""'::jsonb),'{profileWallet}','""'::jsonb) WHERE payload->>'profileWallet'=$1`, c.ProfileWallet)
		if err == nil {
			_, err = tx.Exec(ctx, `UPDATE stats_search.source_documents_v2 SET payload=jsonb_set(jsonb_set(payload,'{profileLabel}','""'::jsonb),'{profileWallet}','""'::jsonb) WHERE payload->>'profileWallet'=$1`, c.ProfileWallet)
		}
		if err == nil {
			_, err = tx.Exec(ctx, "DELETE FROM stats_search.profile_documents_v2 WHERE wallet=$1", c.ProfileWallet)
		}
		return true, err
	case "preferences":
		raw, _ := json.Marshal(c.Preferences)
		result, err := tx.Exec(ctx, `INSERT INTO stats_search.preferences_v2(wallet,revision,payload) VALUES($1,$2,$3) ON CONFLICT(wallet) DO UPDATE SET revision=excluded.revision,payload=excluded.payload WHERE stats_search.preferences_v2.revision<excluded.revision`, c.Preferences.Wallet, c.Revision, raw)
		return result.RowsAffected() > 0, err
	}
	d := *c.Document
	var oldRaw []byte
	err := tx.QueryRow(ctx, "SELECT payload FROM stats_search.source_documents_v2 WHERE pool_address=$1 FOR UPDATE", c.PoolAddress).Scan(&oldRaw)
	if err == nil {
		var old IndexedDocument
		if json.Unmarshal(oldRaw, &old) != nil {
			return false, ErrUnavailable
		}
		if old.TokenAddress != d.TokenAddress || old.Creator != d.Creator || !old.CreatedAt.Equal(d.CreatedAt) || old.CreationEventID != d.CreationEventID || old.SourceHeight != d.SourceHeight || old.SourceBlockHash != d.SourceBlockHash || old.ChainHeight != d.ChainHeight {
			return false, delivery.ErrIntegrity
		}
		switch c.Kind {
		case "creation":
			return false, nil
		case "metadata":
			if c.Revision <= old.MetadataRevision {
				return false, nil
			}
			if d.VisibilityRevision < old.VisibilityRevision {
				return false, delivery.ErrIntegrity
			}
			old.VisibilityRevision = d.VisibilityRevision
			old.Name, old.NameKey, old.Ticker, old.CanonicalTicker, old.MetadataRevision = d.Name, d.NameKey, d.Ticker, d.CanonicalTicker, c.Revision
			old.LanguageHints, old.ConfusableWarnings = d.LanguageHints, d.ConfusableWarnings
			old.ProfileWallet, old.ProfileLabel = d.ProfileWallet, d.ProfileLabel
		case "lifecycle":
			if c.Revision <= old.LifecycleRevision {
				return false, nil
			}
			old.PoolStatus, old.LifecycleRevision = d.PoolStatus, c.Revision
		case "metrics":
			if c.Revision <= old.MetricRevision {
				return false, nil
			}
			old.LiquidityRaw, old.Volumes, old.MetricsAt, old.MetricRevision = d.LiquidityRaw, d.Volumes, d.MetricsAt, c.Revision
			old.VerifiedQuoteAsset, old.RiskFlags, old.PublicExplanation = d.VerifiedQuoteAsset, d.RiskFlags, d.PublicExplanation
		}
		d = old
	} else if err != pgx.ErrNoRows {
		return false, err
	} else if c.Kind != "creation" {
		return false, ErrUnavailable
	}
	var deleted bool
	if d.ProfileWallet != "" {
		if err = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM stats_search.profile_tombstones_v2 WHERE wallet=$1)", d.ProfileWallet).Scan(&deleted); err != nil {
			return false, err
		}
		if deleted {
			d.ProfileWallet, d.ProfileLabel = "", ""
		}
	}
	d.IndexedAt = time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
	if err = d.Validate(); err != nil {
		return false, err
	}
	sourceRaw, _ := json.Marshal(d)
	if _, err = tx.Exec(ctx, `INSERT INTO stats_search.source_documents_v2(pool_address,payload) VALUES($1,$2) ON CONFLICT(pool_address) DO UPDATE SET payload=excluded.payload`, d.PoolAddress, sourceRaw); err != nil {
		return false, err
	}
	rows, err := tx.Query(ctx, "SELECT generation FROM stats_search.generations_v2 WHERE state IN ('active','building') ORDER BY generation FOR UPDATE")
	if err != nil {
		return false, err
	}
	generations := []string{}
	for rows.Next() {
		var g string
		if err = rows.Scan(&g); err != nil {
			rows.Close()
			return false, err
		}
		generations = append(generations, g)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return false, err
	}
	if len(generations) == 0 {
		return false, ErrUnavailable
	}
	changed := false
	for _, generation := range generations {
		raw, _ := json.Marshal(d)
		volume := func(window string) any {
			if v, ok := d.Volumes[window]; ok {
				return v
			}
			return nil
		}
		var liquidity any
		if d.LiquidityRaw != nil {
			liquidity = *d.LiquidityRaw
		}
		_, err = tx.Exec(ctx, `INSERT INTO stats_search.documents_v2(generation,pool_address,token_address,name_key,ticker_key,creator,created_at,pool_status,visibility_revision,revision,metrics_at,liquidity_raw,volume_1h,volume_24h,volume_7d,verified_quote,risk_flags,payload) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,nextval('stats_search.revision_v2'),$10,$11::numeric,$12::numeric,$13::numeric,$14::numeric,$15,$16,$17) ON CONFLICT(generation,pool_address) DO UPDATE SET name_key=excluded.name_key,ticker_key=excluded.ticker_key,pool_status=excluded.pool_status,visibility_revision=excluded.visibility_revision,revision=excluded.revision,metrics_at=excluded.metrics_at,liquidity_raw=excluded.liquidity_raw,volume_1h=excluded.volume_1h,volume_24h=excluded.volume_24h,volume_7d=excluded.volume_7d,verified_quote=excluded.verified_quote,risk_flags=excluded.risk_flags,payload=excluded.payload`, generation, d.PoolAddress, d.TokenAddress, d.NameKey, strings.ToLower(d.CanonicalTicker), d.Creator, d.CreatedAt, d.PoolStatus, d.VisibilityRevision, d.MetricsAt, liquidity, volume("1h"), volume("24h"), volume("7d"), d.VerifiedQuoteAsset, d.RiskFlags, raw)
		if err != nil {
			return false, err
		}
		changed = true
	}
	return changed, nil
}

type CanonicalCreation struct {
	Token                 string `json:"token"`
	Pool                  string `json:"pool"`
	Creator               string `json:"creator"`
	BlockTimestampSeconds string `json:"blockTimestampSeconds"`
	Position              struct {
		ChainID          string `json:"chainId"`
		Height           string `json:"height"`
		TransactionIndex string `json:"transactionIndex"`
		LogIndex         string `json:"logIndex"`
		TransactionHash  string `json:"transactionHash"`
		BlockHash        string `json:"blockHash"`
	} `json:"position"`
	Metadata *struct {
		Name   string `json:"name"`
		Symbol string `json:"symbol"`
	} `json:"metadata"`
}

func (p *Projection) ConsumeCanonicalCreation(ctx context.Context, subject string, wire []byte, message delivery.Delivery) error {
	if p == nil || p.Delivery == nil || p.InvalidationSubject == "" || p.InvalidationEventType == "" {
		return ErrUnavailable
	}
	bounded, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	return p.Delivery.Consume(bounded, p.ConsumerID, p.InboxGeneration, subject, wire, message, func(ctx context.Context, tx pgx.Tx, e delivery.Envelope) error {
		var fact CanonicalCreation
		if !utf8.Valid(e.Payload) || json.Unmarshal(e.Payload, &fact) != nil || fact.Metadata == nil || fact.Position.ChainID != "125" || fact.Position.Height != e.SourceOrder.Height || fact.Position.TransactionIndex != e.SourceOrder.TransactionIndex || fact.Position.LogIndex != e.SourceOrder.LogIndex {
			return ErrInvalid
		}
		var same bool
		if err := tx.QueryRow(ctx, "SELECT payload=$2::jsonb FROM indexer.indexer_canonical_creations_v1 WHERE pool=$1", fact.Pool, []byte(e.Payload)).Scan(&same); err != nil {
			return err
		}
		if !same {
			return delivery.ErrIntegrity
		}
		timestamp, err := strconv.ParseInt(fact.BlockTimestampSeconds, 10, 64)
		if err != nil || timestamp < 1 {
			return ErrInvalid
		}
		query, err := Normalize(fact.Metadata.Name)
		if err != nil {
			return err
		}
		d := IndexedDocument{Document: Document{Version: 1, ChainID: "125", PoolAddress: fact.Pool, TokenAddress: fact.Token, Name: fact.Metadata.Name, Ticker: fact.Metadata.Symbol, NameKey: query.Key, CanonicalTicker: canonicalTicker(fact.Metadata.Symbol), LanguageHints: []string{}, ConfusableWarnings: tickerWarnings(fact.Metadata.Symbol), CreationEventID: "125:" + fact.Position.TransactionHash + ":" + fact.Position.LogIndex, ChainHeight: fact.Position.Height, MetadataRevision: 1, VisibilityRevision: 1, PoolStatus: "active", IndexedAt: time.Now().UTC().Format("2006-01-02T15:04:05.000Z")}, Creator: fact.Creator, CreatedAt: time.Unix(timestamp, 0).UTC(), LifecycleRevision: 1, Volumes: map[string]string{}, RiskFlags: []string{}, SourceHeight: fact.Position.Height, SourceBlockHash: fact.Position.BlockHash}
		if d.Validate() != nil {
			return ErrInvalid
		}
		if _, err = tx.Exec(ctx, "SET LOCAL statement_timeout='2s'"); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, "SET LOCAL lock_timeout='100ms'"); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, "SELECT revision FROM stats_search.active_v2 WHERE singleton FOR UPDATE"); err != nil {
			return err
		}
		changed, err := applyChangeTx(ctx, tx, Change{Version: 2, Kind: "creation", Revision: 1, PoolAddress: d.PoolAddress, Document: &d})
		if err != nil || !changed {
			return err
		}
		var revision int64
		if err = tx.QueryRow(ctx, "UPDATE stats_search.active_v2 SET revision=nextval('stats_search.revision_v2'),privacy_revision=privacy_revision+1 WHERE singleton RETURNING revision").Scan(&revision); err != nil {
			return err
		}
		payload, _ := json.Marshal(map[string]any{"version": 2, "revision": strconv.FormatInt(revision, 10)})
		notification := delivery.Envelope{EventID: "search_" + delivery.Hash([]byte(e.EventID)), EventType: p.InvalidationEventType, SchemaVersion: 1, ChainID: 125, SourceOrder: e.SourceOrder, EmittedAt: e.EmittedAt, TraceID: e.TraceID, PayloadHash: delivery.Hash(payload), Payload: payload}
		return p.Delivery.Enqueue(ctx, tx, p.InvalidationSubject, notification)
	})
}

func (p *Projection) ConsumeAccountProfile(ctx context.Context, subject string, wire []byte, message delivery.Delivery) error {
	if p == nil || p.Delivery == nil || p.ConsumerID == "" || p.InboxGeneration == "" || p.InvalidationSubject == "" || p.InvalidationEventType == "" {
		return ErrUnavailable
	}
	bounded, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	return p.Delivery.Consume(bounded, p.ConsumerID, p.InboxGeneration, subject, wire, message, func(ctx context.Context, tx pgx.Tx, event delivery.Envelope) error {
		if event.EventType != "profile.updated" && event.EventType != "account.deletion.requested" && event.EventType != "account.backfill.completed" {
			return ErrInvalid
		}
		var wallet, label string
		var revision, sourceRevision int64
		var deleted, public bool
		var updated time.Time
		err := tx.QueryRow(ctx, `SELECT a.wallet,COALESCE(a.profile->>'displayName',''),a.revision,a.deleted,a.updated_at,o.revision,v.public FROM users.account_outbox o JOIN users.account_state a USING(account_id) JOIN users.account_public_search_v2 v ON v.wallet=a.wallet WHERE o.event_id=$1::uuid AND o.kind=$2 AND o.payload=$3::jsonb`, event.EventID, event.EventType, []byte(event.Payload)).Scan(&wallet, &label, &revision, &deleted, &updated, &sourceRevision, &public)
		if err != nil {
			return err
		}
		if !exactIdentity(wallet) || sourceRevision < 0 || strconv.FormatInt(sourceRevision, 10) != event.SourceOrder.Height || revision < sourceRevision {
			return delivery.ErrIntegrity
		}
		if _, err = tx.Exec(ctx, "SET LOCAL lock_timeout='100ms'"); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, "SELECT revision FROM stats_search.active_v2 WHERE singleton FOR UPDATE"); err != nil {
			return err
		}
		if deleted {
			if revision < 1 {
				return delivery.ErrIntegrity
			}
			if _, err = applyChangeTx(ctx, tx, Change{Version: 2, Kind: "profile_deletion", Revision: revision, ProfileWallet: wallet}); err != nil {
				return err
			}
			if _, err = tx.Exec(ctx, "DELETE FROM stats_search.profile_documents_v2 WHERE wallet=$1", wallet); err != nil {
				return err
			}
		} else if !public || label == "" {
			if _, err = tx.Exec(ctx, "DELETE FROM stats_search.profile_documents_v2 WHERE wallet=$1", wallet); err != nil {
				return err
			}
		} else {
			if !boundedDisplay(label, 80, 320) || updated.IsZero() {
				return ErrInvalid
			}
			var deletedBefore bool
			if err = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM stats_search.profile_tombstones_v2 WHERE wallet=$1)", wallet).Scan(&deletedBefore); err != nil {
				return err
			}
			if deletedBefore {
				return nil
			}
			if _, err = tx.Exec(ctx, `INSERT INTO stats_search.profile_documents_v2(wallet,display_name,name_key,revision,updated_at) VALUES($1,$2,$3,$4,$5) ON CONFLICT(wallet) DO UPDATE SET display_name=excluded.display_name,name_key=excluded.name_key,revision=excluded.revision,updated_at=excluded.updated_at WHERE stats_search.profile_documents_v2.revision<excluded.revision`, wallet, label, profileNameKey(label), revision, updated); err != nil {
				return err
			}
		}
		var searchRevision int64
		if err = tx.QueryRow(ctx, "UPDATE stats_search.active_v2 SET revision=nextval('stats_search.revision_v2'),privacy_revision=privacy_revision+1 WHERE singleton RETURNING revision").Scan(&searchRevision); err != nil {
			return err
		}
		payload, _ := json.Marshal(map[string]any{"version": 2, "revision": strconv.FormatInt(searchRevision, 10)})
		notification := delivery.Envelope{EventID: "search_" + delivery.Hash([]byte(event.EventID)), EventType: p.InvalidationEventType, SchemaVersion: 1, ChainID: 125, SourceOrder: event.SourceOrder, EmittedAt: event.EmittedAt, TraceID: event.TraceID, PayloadHash: delivery.Hash(payload), Payload: payload}
		return p.Delivery.Enqueue(ctx, tx, p.InvalidationSubject, notification)
	})
}
