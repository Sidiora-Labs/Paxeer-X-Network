package account

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type BackfillRange struct {
	ID    string
	After string
	Limit int
}
type BackfillCounts struct {
	Scanned        int    `json:"scanned"`
	Linked         int    `json:"linked"`
	Quarantined    int    `json:"quarantined"`
	Verified       int    `json:"verified"`
	WatchlistCount int    `json:"watchlistCount"`
	LastWallet     string `json:"lastWallet"`
	Checksum       string `json:"checksum"`
}

func (s Store) BackfillLegacyRange(ctx context.Context, r BackfillRange) (BackfillCounts, error) {
	out := BackfillCounts{}
	if r.ID == "" || len(r.ID) > 128 || r.Limit < 1 || r.Limit > 500 {
		return out, ErrInvalid
	}
	if r.After != "" {
		normalized, e := Wallet(r.After)
		if e != nil {
			return out, e
		}
		r.After = normalized
	}
	tx, e := s.DB.Begin(ctx)
	if e != nil {
		return out, e
	}
	defer tx.Rollback(ctx)
	if _, e = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "account-range:"+r.ID); e != nil {
		return out, e
	}
	var previous []byte
	e = tx.QueryRow(ctx, `SELECT result FROM account_backfill_range WHERE range_id=$1 AND after_wallet=$2 AND batch_limit=$3`, r.ID, r.After, r.Limit).Scan(&previous)
	if e == nil {
		e = json.Unmarshal(previous, &out)
		return out, e
	}
	if !errors.Is(e, pgx.ErrNoRows) {
		return out, e
	}
	rows, e := tx.Query(ctx, `SELECT lower(wallet_address),count(*),min(coalesce(display_name,'')),min(coalesce(bio,'')) FROM users.user_profiles WHERE lower(wallet_address)>$1 GROUP BY lower(wallet_address) ORDER BY lower(wallet_address) LIMIT $2`, strings.ToLower(r.After), r.Limit)
	if e != nil {
		return out, e
	}
	type source struct {
		wallet    string
		count     int
		name, bio string
	}
	sources := []source{}
	for rows.Next() {
		var v source
		if e = rows.Scan(&v.wallet, &v.count, &v.name, &v.bio); e != nil {
			rows.Close()
			return out, e
		}
		sources = append(sources, v)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return out, e
	}
	hash := sha256.New()
	for _, source := range sources {
		if _, e = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "l35-source:legacy:"+source.wallet); e != nil {
			return out, e
		}
		if _, e = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "l35-wallet:"+source.wallet); e != nil {
			return out, e
		}
		out.Scanned++
		out.LastWallet = source.wallet
		watchRows, e := tx.Query(ctx, `SELECT lower(pool_address),min(added_at) FROM users.watchlists WHERE lower(wallet_address)=$1 GROUP BY lower(pool_address) ORDER BY lower(pool_address)`, source.wallet)
		if e != nil {
			return out, e
		}
		type legacyWatch struct {
			Pool    string `json:"pool"`
			AddedAt int64  `json:"addedAt"`
		}
		watchlist := []legacyWatch{}
		for watchRows.Next() {
			var watch legacyWatch
			if e = watchRows.Scan(&watch.Pool, &watch.AddedAt); e != nil {
				watchRows.Close()
				return out, e
			}
			watchlist = append(watchlist, watch)
		}
		e = watchRows.Err()
		watchRows.Close()
		if e != nil {
			return out, e
		}
		raw, e := json.Marshal(map[string]any{"wallet": source.wallet, "count": source.count, "displayName": source.name, "bio": source.bio, "watchlist": watchlist})
		if e != nil {
			return out, e
		}
		hash.Write(raw)
		digest := sha256.Sum256(raw)
		var account uuid.UUID
		e = tx.QueryRow(ctx, `SELECT account_id FROM identity_wallet_links WHERE chain_id=125 AND wallet=$1 AND state='verified'`, source.wallet).Scan(&account)
		if e != nil && !errors.Is(e, pgx.ErrNoRows) {
			return out, e
		}
		if source.count > 1 || errors.Is(e, pgx.ErrNoRows) {
			out.Quarantined++
			if _, e = tx.Exec(ctx, `INSERT INTO account_backfill VALUES($1,$2,NULL,'quarantined','ambiguous legacy source or no verified identity') ON CONFLICT(source_key) DO NOTHING`, "legacy:"+source.wallet, digest[:]); e != nil {
				return out, e
			}
			continue
		}
		var deleted bool
		var oldWallet string
		e = tx.QueryRow(ctx, `SELECT wallet,deleted FROM account_state WHERE account_id=$1 FOR UPDATE`, account).Scan(&oldWallet, &deleted)
		if e != nil && !errors.Is(e, pgx.ErrNoRows) {
			return out, e
		}
		if e == nil && (deleted || oldWallet != source.wallet) {
			out.Quarantined++
			continue
		}
		var priorHash []byte
		e = tx.QueryRow(ctx, `SELECT source_hash FROM account_backfill WHERE source_key=$1`, "legacy:"+source.wallet).Scan(&priorHash)
		if e != nil && !errors.Is(e, pgx.ErrNoRows) {
			return out, e
		}
		if e == nil && string(priorHash) != string(digest[:]) {
			if _, e = tx.Exec(ctx, `UPDATE account_backfill SET state='quarantined',reason='source bytes changed; reviewed resolution required' WHERE source_key=$1`, "legacy:"+source.wallet); e != nil {
				return out, e
			}
			out.Quarantined++
			continue
		}
		var quarantine bool
		if e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM account_backfill WHERE source_key=$1 AND state='quarantined')`, "legacy:"+source.wallet).Scan(&quarantine); e != nil {
			return out, e
		}
		if quarantine {
			out.Quarantined++
			continue
		}
		name := source.name
		patch := ProfilePatch{ExpectedRevision: "0", DisplayName: &name, Bio: &source.bio}
		if e = patch.Validate(); e != nil {
			out.Quarantined++
			if _, e = tx.Exec(ctx, `INSERT INTO account_backfill VALUES($1,$2,$3,'quarantined','legacy profile violates current validation') ON CONFLICT(source_key) DO UPDATE SET state='quarantined',reason=EXCLUDED.reason`, "legacy:"+source.wallet, digest[:], account); e != nil {
				return out, e
			}
			continue
		}
		count := len(watchlist)
		invalidPool := false
		for _, watch := range watchlist {
			if _, e := Wallet(watch.Pool); e != nil {
				invalidPool = true
			}
		}
		var combinedCount int
		if e = tx.QueryRow(ctx, `SELECT count(*) FROM (SELECT pool FROM account_watch WHERE account_id=$1 UNION SELECT lower(pool_address) FROM users.watchlists WHERE lower(wallet_address)=$2) pools`, account, source.wallet).Scan(&combinedCount); e != nil {
			return out, e
		}
		if count > 500 || combinedCount > 500 || invalidPool {
			out.Quarantined++
			if _, e = tx.Exec(ctx, `INSERT INTO account_backfill VALUES($1,$2,$3,'quarantined','legacy watchlist violates frozen capacity or pool validation') ON CONFLICT(source_key) DO UPDATE SET state='quarantined',reason=EXCLUDED.reason`, "legacy:"+source.wallet, digest[:], account); e != nil {
				return out, e
			}
			continue
		}
		pr, _ := json.Marshal(Profile{PublicProfile: PublicProfile{Wallet: source.wallet, DisplayName: *patch.DisplayName, Bio: source.bio, Revision: "0"}, SchemaVersion: 1, UpdatedAt: s.now()})
		pf, _ := json.Marshal(DefaultPreferences())
		if _, e = tx.Exec(ctx, `INSERT INTO account_state(account_id,wallet,profile,preferences) VALUES($1,$2,$3,$4) ON CONFLICT(account_id) DO NOTHING`, account, source.wallet, pr, pf); e != nil {
			return out, e
		}
		_, e = tx.Exec(ctx, `INSERT INTO account_watch(account_id,chain_id,pool,added_at,revision,alert_enabled) SELECT $1,125,lower(pool_address),to_timestamp(min(added_at)),0,false FROM users.watchlists WHERE lower(wallet_address)=$2 GROUP BY lower(pool_address) ON CONFLICT DO NOTHING`, account, source.wallet)
		if e != nil {
			return out, e
		}
		linkedTag, e := tx.Exec(ctx, `INSERT INTO account_backfill VALUES($1,$2,$3,'linked','') ON CONFLICT(source_key) DO NOTHING`, "legacy:"+source.wallet, digest[:], account)
		if e != nil {
			return out, e
		}
		out.Linked++
		out.Verified++
		out.WatchlistCount += count
		a, e := load(ctx, tx, account)
		if e != nil {
			return out, e
		}
		eventID := uuid.NewSHA1(uuid.NameSpaceOID, append([]byte("kindle-account-backfill:"+source.wallet+":"), digest[:]...))
		if linkedTag.RowsAffected() == 1 {
			if e = emitIdentity(ctx, tx, a, eventID, "account.backfill.completed", map[string]any{"accountId": account, "sourceDigest": hex.EncodeToString(digest[:]), "watchlistCount": count}); e != nil {
				return out, e
			}
		}
	}
	out.Checksum = hex.EncodeToString(hash.Sum(nil))
	result, e := json.Marshal(out)
	if e != nil {
		return out, e
	}
	if _, e = tx.Exec(ctx, `INSERT INTO account_backfill_range(range_id,after_wallet,batch_limit,result,completed_at) VALUES($1,$2,$3,$4,$5)`, r.ID, r.After, r.Limit, result, s.now()); e != nil {
		return out, e
	}
	if e = tx.Commit(ctx); e != nil {
		return out, e
	}
	return out, nil
}
func (s Store) SetStableAccountReads(ctx context.Context, enabled bool) error {
	_, e := s.DB.Exec(ctx, `INSERT INTO account_read_switch(singleton,stable_reads,changed_at,generation) VALUES(true,$1,$2,1) ON CONFLICT(singleton) DO UPDATE SET stable_reads=EXCLUDED.stable_reads,changed_at=EXCLUDED.changed_at,generation=account_read_switch.generation+CASE WHEN account_read_switch.stable_reads<>EXCLUDED.stable_reads THEN 1 ELSE 0 END`, enabled, time.Now().UTC())
	return e
}

type ReadProjection struct {
	Profile    PublicProfile
	Source     string
	Generation int64
}

func (s Store) PublicFromReadSwitch(ctx context.Context, wallet string) (ReadProjection, error) {
	current, e := s.Public(ctx, wallet)
	if e != nil {
		return ReadProjection{}, e
	}
	var stable, deleted bool
	var generation int64
	if e = s.reader(ctx).QueryRow(ctx, `SELECT coalesce((SELECT stable_reads FROM account_read_switch WHERE singleton),true),deleted,coalesce((SELECT generation FROM account_read_switch WHERE singleton),1) FROM account_state WHERE wallet=$1`, current.Wallet).Scan(&stable, &deleted, &generation); e != nil {
		return ReadProjection{}, e
	}
	if stable || deleted {
		return ReadProjection{Profile: current, Source: "stable", Generation: generation}, nil
	}
	var name, bio string
	var website *string
	e = s.reader(ctx).QueryRow(ctx, `SELECT coalesce(display_name,''),coalesce(bio,''),nullif(website,'') FROM users.user_profiles WHERE lower(wallet_address)=$1 ORDER BY wallet_address LIMIT 1`, current.Wallet).Scan(&name, &bio, &website)
	if errors.Is(e, pgx.ErrNoRows) {
		return ReadProjection{Profile: current, Source: "stable-only-record", Generation: generation}, nil
	}
	if e != nil {
		return ReadProjection{}, e
	}
	patch := ProfilePatch{ExpectedRevision: current.Revision, DisplayName: &name, Bio: &bio}
	if e = patch.Validate(); e != nil {
		return ReadProjection{}, e
	}
	if website != nil {
		if e = SafeLink(*website); e != nil {
			return ReadProjection{}, e
		}
	}
	return ReadProjection{Profile: PublicProfile{Wallet: current.Wallet, DisplayName: *patch.DisplayName, Bio: bio, Website: website, Revision: current.Revision}, Source: "legacy", Generation: generation}, nil
}

func (h HTTP) BackfillLegacyRange(ctx context.Context, r BackfillRange) (BackfillCounts, error) {
	if h.Runtime == nil {
		return BackfillCounts{}, ErrInvalid
	}
	ctx = context.WithValue(ctx, runtimeKey{}, h.Runtime)
	started := time.Now()
	counts, e := h.Store.BackfillLegacyRange(ctx, r)
	outcome := "success"
	if e != nil {
		outcome = "failed"
	}
	h.Runtime.Metrics.Observe("backfill", outcome, time.Since(started))
	return counts, e
}
