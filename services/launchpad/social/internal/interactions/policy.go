package interactions

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/Sidiora-Technologies/KindleLaunch/media/social/internal/fanout"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type identityKey struct{}

func WithIdentity(ctx context.Context, principal fanout.Principal) context.Context {
	return context.WithValue(ctx, identityKey{}, principal)
}
func principalValue(ctx context.Context) (fanout.Principal, bool) {
	if ctx == nil {
		return fanout.Principal{}, false
	}
	p, ok := ctx.Value(identityKey{}).(fanout.Principal)
	if !ok {
		p, ok = fanout.PrincipalValue(ctx)
	}
	return p, ok
}
func Identity(ctx context.Context) (fanout.Principal, error) {
	p, ok := principalValue(ctx)
	if !ok || p.SessionID == "" || !p.ExpiresAt.After(time.Now()) || p.Validate == nil {
		return p, ErrDenied
	}
	w, e := Wallet(p.Wallet)
	if e != nil || w != p.Wallet {
		return p, ErrDenied
	}
	if e = p.Validate(ctx); e != nil {
		return p, ErrDependency
	}
	return p, nil
}

type PGPolicy struct {
	DB                                    *pgxpool.Pool
	Accounts                              pgx.Identifier
	Pools                                 pgx.Identifier
	Sessions                              pgx.Identifier
	IdentityAccounts                      pgx.Identifier
	WalletLinks                           pgx.Identifier
	Publisher                             *Publisher
	SessionGuard, AccountGuard, PoolGuard pgx.Identifier
}

func policyLocks(ctx context.Context, tx pgx.Tx, wallets ...string) error {
	sort.Strings(wallets)
	prior := ""
	for _, w := range wallets {
		if w == "" || w == prior {
			continue
		}
		if _, e := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,37))`, w); e != nil {
			return e
		}
		prior = w
	}
	return nil
}
func (p *PGPolicy) trusted(ctx context.Context, tx pgx.Tx, actor string) error {
	principal, ok := principalValue(ctx)
	if !ok || principal.Wallet != actor || principal.SessionID == "" || !principal.ExpiresAt.After(time.Now()) || principal.Validate == nil {
		return ErrDenied
	}
	if len(p.SessionGuard) > 0 {
		var valid bool
		e := tx.QueryRow(ctx, `SELECT `+p.SessionGuard.Sanitize()+`($1::uuid,$2::text)`, principal.SessionID, actor).Scan(&valid)
		if e != nil {
			return e
		}
		if !valid {
			return ErrDenied
		}
		return nil
	}
	if len(p.Sessions) == 0 || len(p.IdentityAccounts) == 0 || len(p.WalletLinks) == 0 {
		return ErrDependency
	}
	var wallet string
	e := tx.QueryRow(ctx, `SELECT s.wallet FROM `+p.Sessions.Sanitize()+` s JOIN `+p.IdentityAccounts.Sanitize()+` a USING(account_id) JOIN `+p.WalletLinks.Sanitize()+` w ON w.account_id=s.account_id AND w.wallet=s.wallet AND w.chain_id=125 AND w.state='verified' WHERE s.session_id=$1 AND s.wallet=$2 AND s.revoked_at IS NULL AND s.access_expires_at>clock_timestamp() AND s.auth_epoch=a.auth_epoch FOR SHARE OF s,a,w`, principal.SessionID, actor).Scan(&wallet)
	if errors.Is(e, pgx.ErrNoRows) {
		return ErrDenied
	}
	return e
}
func (p *PGPolicy) active(ctx context.Context, tx pgx.Tx, wallet string) error {
	if len(p.Accounts) == 0 {
		return ErrDependency
	}
	var active bool
	query := `SELECT NOT deleted FROM ` + p.Accounts.Sanitize() + ` WHERE wallet=$1 FOR SHARE`
	if len(p.AccountGuard) > 0 {
		query = `SELECT ` + p.AccountGuard.Sanitize() + `($1::text)`
	}
	if e := tx.QueryRow(ctx, query, wallet).Scan(&active); e != nil {
		if errors.Is(e, pgx.ErrNoRows) {
			return ErrDenied
		}
		return e
	}
	if !active {
		return ErrDenied
	}
	var erased bool
	if e := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM interaction_erased_wallets WHERE wallet=$1)`, wallet).Scan(&erased); e != nil {
		return e
	}
	if erased {
		return ErrDenied
	}
	return nil
}
func (p *PGPolicy) Check(ctx context.Context, tx pgx.Tx, r Request) error {
	e := p.trusted(ctx, tx, r.Actor)
	if e != nil {
		return e
	}
	var globalBanned bool
	if e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM safety_bans WHERE wallet=$1 AND scope='global' AND revoked_at IS NULL AND (expires_at IS NULL OR expires_at>clock_timestamp()))`, r.Actor).Scan(&globalBanned); e != nil {
		return ErrDependency
	}
	if globalBanned {
		return ErrDenied
	}
	target := ""
	if r.Action == "follow" {
		target = r.ID
	}
	if r.Action == "mention" {
		target = r.Parent
	}
	parentAuthor := ""
	if r.Action == "comment" && r.Parent != "" {
		if e = tx.QueryRow(ctx, `SELECT author FROM interaction_comments WHERE id=$1`, r.Parent).Scan(&parentAuthor); e != nil {
			if errors.Is(e, pgx.ErrNoRows) {
				return ErrInvalid
			}
			return e
		}
	}
	lockWallets := []string{r.Actor, target, parentAuthor}
	if r.Action == "callout_reference" {
		var metadata struct {
			Mentions   []string
			Attachment Attachment
		}
		if json.Unmarshal([]byte(r.Body), &metadata) != nil {
			return ErrInvalid
		}
		mentions, err := Mentions(metadata.Mentions)
		if err != nil {
			return err
		}
		lockWallets = append(lockWallets, mentions...)
	}
	if e = policyLocks(ctx, tx, lockWallets...); e != nil {
		return e
	}
	if parentAuthor != "" {
		if e = p.active(ctx, tx, parentAuthor); e != nil {
			return e
		}
	}
	if e = p.active(ctx, tx, r.Actor); e != nil {
		return e
	}
	if target != "" {
		if e = p.active(ctx, tx, target); e != nil {
			return e
		}
		var blocked bool
		if e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM interaction_blocks WHERE (owner=$1 AND target=$2) OR (owner=$2 AND target=$1))`, r.Actor, target).Scan(&blocked); e != nil {
			return e
		}
		if blocked {
			return ErrDenied
		}
		if r.Action == "mention" {
			var muted bool
			if e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM interaction_mutes WHERE owner=$1 AND target=$2)`, target, r.Actor).Scan(&muted); e != nil {
				return e
			}
			if muted {
				return ErrDenied
			}
		}
	}
	if r.Action == "comment" || r.Action == "comment_hide" || r.Action == "reaction" {
		pool := r.Pool
		if r.Action == "reaction" || pool == "" {
			if e = tx.QueryRow(ctx, `SELECT pool FROM interaction_comments WHERE id=$1`, r.ID).Scan(&pool); e != nil {
				return e
			}
		}
		if len(p.Pools) == 0 {
			return ErrDependency
		}
		var found string
		query := `SELECT pool_address FROM ` + p.Pools.Sanitize() + ` WHERE pool_address=$1 FOR SHARE`
		if len(p.PoolGuard) > 0 {
			query = `SELECT ` + p.PoolGuard.Sanitize() + `($1::text)`
		}
		if e = tx.QueryRow(ctx, query, pool).Scan(&found); e != nil {
			return e
		}
		if found == "" {
			return ErrDenied
		}
		var banned bool
		if e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM safety_bans WHERE wallet=$1 AND revoked_at IS NULL AND (expires_at IS NULL OR expires_at>now()) AND (scope='global' OR (scope='pool' AND resource=$2)))`, r.Actor, pool).Scan(&banned); e != nil {
			return e
		}
		if banned {
			return ErrDenied
		}
	}
	if r.Action == "callout_reference" || r.Action == "mention" {
		if _, e = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,37))`, "callout:"+r.ID); e != nil {
			return e
		}
		var actor string
		var removed bool
		if e = tx.QueryRow(ctx, `SELECT actor,removed FROM interaction_callout_authority a WHERE id=$1 AND revision=$2 AND NOT EXISTS(SELECT 1 FROM interaction_callout_authority newer WHERE newer.id=a.id AND newer.revision>a.revision) FOR SHARE OF a`, r.ID, r.Revision).Scan(&actor, &removed); e != nil {
			return e
		}
		if removed || actor != r.Actor {
			return ErrDenied
		}
	}
	return nil
}
func (p *PGPolicy) CheckCreator(ctx context.Context, tx pgx.Tx, r Request) error {
	if len(p.Pools) == 0 {
		return ErrDependency
	}
	var creator string
	query := `SELECT creator FROM ` + p.Pools.Sanitize() + ` WHERE pool_address=$1 FOR SHARE`
	if len(p.PoolGuard) > 0 {
		query = `SELECT ` + p.PoolGuard.Sanitize() + `($1::text)`
	}
	if e := tx.QueryRow(ctx, query, r.Pool).Scan(&creator); e != nil {
		return e
	}
	if creator != r.Actor {
		return ErrDenied
	}
	return nil
}
func (p *PGPolicy) RecordChange(ctx context.Context, tx pgx.Tx, r Request) error {
	if p.Publisher == nil {
		return ErrDependency
	}
	return p.Publisher.Record(ctx, tx, r)
}
func (p *PGPolicy) SendPolicy(ctx context.Context, tx pgx.Tx, in fanout.SendInput) error {
	e := p.trusted(ctx, tx, in.Actor)
	if e != nil {
		return e
	}
	if e = policyLocks(ctx, tx, in.Actor, in.Target); e != nil {
		return e
	}
	if e = p.active(ctx, tx, in.Actor); e != nil {
		return e
	}
	if in.Kind == "pool" {
		return p.Check(ctx, tx, Request{Actor: in.Actor, Pool: in.Target, Action: "comment"})
	}
	if in.Kind != "dm" {
		return ErrInvalid
	}
	if e = p.active(ctx, tx, in.Target); e != nil {
		return e
	}
	var blocked bool
	if e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM interaction_blocks WHERE (owner=$1 AND target=$2) OR (owner=$2 AND target=$1))`, in.Actor, in.Target).Scan(&blocked); e != nil {
		return e
	}
	if blocked {
		return ErrDenied
	}
	pair := []string{in.Actor, in.Target}
	sort.Strings(pair)
	conversation := uuid.NewSHA1(uuid.NameSpaceOID, []byte(pair[0]+":"+pair[1])).String()
	var banned bool
	if e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM safety_bans WHERE wallet IN ($1,$2) AND revoked_at IS NULL AND (expires_at IS NULL OR expires_at>clock_timestamp()) AND (scope='global' OR (scope='dm' AND resource=$3)))`, in.Actor, in.Target, conversation).Scan(&banned); e != nil {
		return e
	}
	if banned {
		return ErrDenied
	}
	var preference string
	if e = tx.QueryRow(ctx, `SELECT inbox FROM interaction_preferences WHERE wallet=$1 FOR SHARE`, in.Target).Scan(&preference); e != nil {
		if errors.Is(e, pgx.ErrNoRows) {
			return ErrDenied
		}
		return e
	}
	switch preference {
	case "everyone":
		return nil
	case "nobody":
		return ErrDenied
	case "followed_only":
		var follows bool
		if e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM interaction_follows WHERE follower=$1 AND followee=$2)`, in.Target, in.Actor).Scan(&follows); e != nil {
			return e
		}
		if follows {
			return nil
		}
		return ErrDenied
	default:
		return ErrDependency
	}
}

type Privacy struct {
	Inbox    string `json:"inbox"`
	Revision int64  `json:"revision,string"`
}

func (p *PGPolicy) Privacy(ctx context.Context) (Privacy, error) {
	var v Privacy
	principal, e := Identity(ctx)
	if e != nil {
		return v, e
	}
	if p.DB == nil {
		return v, ErrDependency
	}
	e = p.DB.QueryRow(ctx, `SELECT inbox,revision FROM interaction_preferences WHERE wallet=$1`, principal.Wallet).Scan(&v.Inbox, &v.Revision)
	return v, e
}
func (p *PGPolicy) SetPrivacy(ctx context.Context, inbox string, expected int64) error {
	if inbox != "everyone" && inbox != "followed_only" && inbox != "nobody" {
		return ErrInvalid
	}
	principal, e := Identity(ctx)
	if e != nil {
		return e
	}
	if p.DB == nil {
		return ErrDependency
	}
	tx, e := p.DB.Begin(ctx)
	if e != nil {
		return e
	}
	defer rollback(tx)
	if e = p.trusted(ctx, tx, principal.Wallet); e != nil {
		return e
	}
	if e = policyLocks(ctx, tx, principal.Wallet); e != nil {
		return e
	}
	if e = p.active(ctx, tx, principal.Wallet); e != nil {
		return e
	}
	tag, e := tx.Exec(ctx, `UPDATE interaction_preferences SET inbox=$2,revision=revision+1 WHERE wallet=$1 AND revision=$3`, principal.Wallet, inbox, expected)
	if e != nil {
		return e
	}
	if tag.RowsAffected() != 1 {
		return ErrRevision
	}
	return tx.Commit(ctx)
}
func (p *PGPolicy) SetBlock(ctx context.Context, target string, remove bool) error {
	principal, e := Identity(ctx)
	if e != nil {
		return e
	}
	target, e = Wallet(target)
	if e != nil || target == principal.Wallet {
		return ErrInvalid
	}
	if p.DB == nil {
		return ErrDependency
	}
	tx, e := p.DB.Begin(ctx)
	if e != nil {
		return e
	}
	defer rollback(tx)
	if e = p.trusted(ctx, tx, principal.Wallet); e != nil {
		return e
	}
	if e = policyLocks(ctx, tx, principal.Wallet, target); e != nil {
		return e
	}
	if e = p.active(ctx, tx, principal.Wallet); e != nil {
		return e
	}
	if remove {
		_, e = tx.Exec(ctx, `DELETE FROM interaction_blocks WHERE owner=$1 AND target=$2`, principal.Wallet, target)
	} else {
		_, e = tx.Exec(ctx, `INSERT INTO interaction_blocks(owner,target) VALUES($1,$2) ON CONFLICT DO NOTHING`, principal.Wallet, target)
		if e == nil {
			for _, pair := range [][2]string{{principal.Wallet, target}, {target, principal.Wallet}} {
				r := Request{Actor: pair[0], ID: pair[1], Action: "follow", Remove: true}
				tag, err := tx.Exec(ctx, `DELETE FROM interaction_follows WHERE follower=$1 AND followee=$2`, r.Actor, r.ID)
				if err != nil {
					return err
				}
				if tag.RowsAffected() > 0 {
					if err = recordChange(ctx, tx, r); err != nil {
						return err
					}
					if err = p.RecordChange(ctx, tx, r); err != nil {
						return err
					}
				}
			}
		}
	}
	if e != nil {
		return e
	}
	if _, e = tx.Exec(ctx, `UPDATE interaction_preferences SET revision=revision+1 WHERE wallet IN ($1,$2)`, principal.Wallet, target); e != nil {
		return e
	}
	return tx.Commit(ctx)
}
func rollback(tx pgx.Tx) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = tx.Rollback(ctx)
}
func (p *PGPolicy) ValidateSession(ctx context.Context, wallet, sessionID string) error {
	if p == nil || p.DB == nil || len(p.Sessions) == 0 || len(p.IdentityAccounts) == 0 || len(p.WalletLinks) == 0 {
		return ErrDependency
	}
	var active bool
	e := p.DB.QueryRow(ctx, `SELECT `+p.SessionGuard.Sanitize()+`($1::uuid,$2::text)`, sessionID, wallet).Scan(&active)
	if e != nil {
		return e
	}
	if !active {
		return ErrDenied
	}
	return nil
}

type CommittedReceiptVerifier struct {
	DB                   *pgxpool.Pool
	Transactions, Blocks pgx.Identifier
}

func (v CommittedReceiptVerifier) Verify(ctx context.Context, hash string, chain int) error {
	if chain != 125 || !regexp.MustCompile(`^0x[0-9a-fA-F]{64}$`).MatchString(hash) {
		return ErrInvalid
	}
	if v.DB == nil || len(v.Transactions) == 0 || len(v.Blocks) == 0 {
		return ErrDependency
	}
	var valid bool
	e := v.DB.QueryRow(ctx, `SELECT chat.social_receipt_committed($1::text)`, strings.ToLower(hash)).Scan(&valid)
	if e != nil {
		return e
	}
	if !valid {
		return ErrDenied
	}
	return nil
}

func ApplyCalloutAuthorityTx(ctx context.Context, tx pgx.Tx, id string, revision int64, actor string, removed bool) error {
	if id == "" || len(id) > 160 || revision < 1 {
		return ErrInvalid
	}
	wallet, e := Wallet(actor)
	if e != nil || wallet != actor {
		return ErrInvalid
	}
	if _, e = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,37))`, "callout:"+id); e != nil {
		return e
	}
	if _, e = tx.Exec(ctx, `INSERT INTO interaction_callout_authority(id,revision,actor,removed) VALUES($1,$2,$3,$4) ON CONFLICT DO NOTHING`, id, revision, actor, removed); e != nil {
		return e
	}
	var priorActor string
	var priorRemoved bool
	if e = tx.QueryRow(ctx, `SELECT actor,removed FROM interaction_callout_authority WHERE id=$1 AND revision=$2`, id, revision).Scan(&priorActor, &priorRemoved); e != nil {
		return e
	}
	if priorActor != actor || priorRemoved != removed {
		return ErrRevision
	}
	return nil
}

func (p *PGPolicy) GuardDDL() ([]string, error) {
	if len(p.SessionGuard) != 2 || len(p.AccountGuard) != 2 || len(p.PoolGuard) != 2 || len(p.Sessions) != 2 || len(p.IdentityAccounts) != 2 || len(p.WalletLinks) != 2 || len(p.Accounts) != 2 || len(p.Pools) != 2 {
		return nil, ErrInvalid
	}
	session := `CREATE FUNCTION ` + p.SessionGuard.Sanitize() + `(p_session uuid,p_actor text) RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,pg_temp AS $guard$ DECLARE live boolean; BEGIN SELECT true INTO live FROM ` + p.Sessions.Sanitize() + ` s JOIN ` + p.IdentityAccounts.Sanitize() + ` a USING(account_id) JOIN ` + p.WalletLinks.Sanitize() + ` w ON w.account_id=s.account_id AND w.wallet=s.wallet AND w.chain_id=125 AND w.state='verified' WHERE s.session_id=p_session AND s.wallet=p_actor AND s.revoked_at IS NULL AND s.access_expires_at>clock_timestamp() AND s.auth_epoch=a.auth_epoch FOR SHARE OF s,a,w; RETURN COALESCE(live,false); END $guard$; REVOKE ALL ON FUNCTION ` + p.SessionGuard.Sanitize() + `(uuid,text) FROM PUBLIC;`
	account := `CREATE FUNCTION ` + p.AccountGuard.Sanitize() + `(p_actor text) RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,pg_temp AS $guard$ DECLARE live boolean; BEGIN SELECT NOT deleted INTO live FROM ` + p.Accounts.Sanitize() + ` WHERE wallet=p_actor FOR SHARE; RETURN COALESCE(live,false); END $guard$; REVOKE ALL ON FUNCTION ` + p.AccountGuard.Sanitize() + `(text) FROM PUBLIC;`
	pool := `CREATE FUNCTION ` + p.PoolGuard.Sanitize() + `(p_pool text) RETURNS text LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,pg_temp AS $guard$ DECLARE creator_wallet text; BEGIN SELECT creator INTO creator_wallet FROM ` + p.Pools.Sanitize() + ` WHERE pool_address=p_pool FOR SHARE; RETURN creator_wallet; END $guard$; REVOKE ALL ON FUNCTION ` + p.PoolGuard.Sanitize() + `(text) FROM PUBLIC;`
	return []string{session, account, pool}, nil
}

func (p *PGPolicy) ReadChatPolicy(ctx context.Context, tx pgx.Tx, actor, conversation string) error {
	principal, e := Identity(ctx)
	if e != nil {
		return e
	}
	if principal.Wallet != actor || tx == nil {
		return ErrDenied
	}
	var denied bool
	e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM interaction_erased_wallets WHERE wallet=$1) OR NOT EXISTS(SELECT 1 FROM chat_members WHERE conversation_id=$2 AND wallet=$1)`, actor, conversation).Scan(&denied)
	if e != nil {
		return ErrDependency
	}
	if denied {
		return ErrDenied
	}
	return nil
}

const RuntimeAuthoritySQL = `
CREATE FUNCTION chat.social_asset_state(p_id uuid) RETURNS TABLE(owner_wallet text,state text,blocked bool) LANGUAGE sql SECURITY DEFINER SET search_path=pg_catalog,pg_temp AS $guard$ SELECT owner_wallet,state,blocked FROM metadata.account_assets_authority_v2 WHERE id=p_id $guard$;
REVOKE ALL ON FUNCTION chat.social_asset_state(uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION chat.social_asset_state(uuid) TO chat_runtime;
CREATE FUNCTION chat.social_canonical_pools() RETURNS TABLE(pool text,token text,creator text) LANGUAGE sql SECURITY DEFINER SET search_path=pg_catalog,pg_temp AS $guard$ SELECT pool,token,creator FROM indexer.indexer_canonical_creations_v1 $guard$;
REVOKE ALL ON FUNCTION chat.social_canonical_pools() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION chat.social_canonical_pools() TO chat_runtime;
CREATE TABLE chat.interaction_graph_revisions(wallet text PRIMARY KEY,revision bigint NOT NULL CHECK(revision>0));
INSERT INTO chat.interaction_graph_revisions(wallet,revision) SELECT wallet,1 FROM chat.interaction_preferences;
CREATE FUNCTION chat.social_graph_revision() RETURNS trigger LANGUAGE plpgsql SET search_path=pg_catalog,pg_temp AS $guard$ DECLARE a text; b text; BEGIN
IF TG_TABLE_NAME='interaction_follows' THEN a=COALESCE(NEW.follower,OLD.follower); b=COALESCE(NEW.followee,OLD.followee);
ELSIF TG_TABLE_NAME='interaction_blocks' THEN a=COALESCE(NEW.owner,OLD.owner);b=COALESCE(NEW.target,OLD.target);
ELSE a=COALESCE(NEW.wallet,OLD.wallet); END IF;
INSERT INTO chat.interaction_graph_revisions(wallet,revision)VALUES(a,1) ON CONFLICT(wallet) DO UPDATE SET revision=chat.interaction_graph_revisions.revision+1;
IF b IS NOT NULL AND b<>a THEN INSERT INTO chat.interaction_graph_revisions(wallet,revision)VALUES(b,1) ON CONFLICT(wallet) DO UPDATE SET revision=chat.interaction_graph_revisions.revision+1; END IF; RETURN NULL; END $guard$;
CREATE TRIGGER social_follow_revision AFTER INSERT OR DELETE ON chat.interaction_follows FOR EACH ROW EXECUTE FUNCTION chat.social_graph_revision();
CREATE TRIGGER social_block_revision AFTER INSERT OR DELETE ON chat.interaction_blocks FOR EACH ROW EXECUTE FUNCTION chat.social_graph_revision();
CREATE TRIGGER social_preference_revision AFTER INSERT OR UPDATE ON chat.interaction_preferences FOR EACH ROW EXECUTE FUNCTION chat.social_graph_revision();
CREATE TRIGGER social_erasure_revision AFTER INSERT ON chat.interaction_erased_wallets FOR EACH ROW EXECUTE FUNCTION chat.social_graph_revision();
CREATE FUNCTION chat.interaction_session_valid(p_session uuid,p_actor text) RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,pg_temp AS $guard$ DECLARE live boolean; BEGIN SELECT true INTO live FROM users.identity_sessions_v2 s JOIN users.identity_accounts_v2 a USING(account_id) JOIN users.identity_wallet_links_v2 w ON w.account_id=s.account_id AND w.wallet=s.wallet AND w.chain_id=125 AND w.state='verified' WHERE s.session_id=p_session AND s.wallet=p_actor AND s.revoked_at IS NULL AND s.access_expires_at>clock_timestamp() AND s.auth_epoch=a.auth_epoch; RETURN COALESCE(live,false); END $guard$;
CREATE FUNCTION chat.interaction_account_active(p_actor text) RETURNS boolean LANGUAGE sql SECURITY DEFINER SET search_path=pg_catalog,pg_temp AS $guard$ SELECT COALESCE((SELECT NOT deleted FROM users.account_state_v2 WHERE wallet=p_actor),false) $guard$;
CREATE FUNCTION chat.interaction_pool_creator(p_pool text) RETURNS text LANGUAGE sql SECURITY DEFINER SET search_path=pg_catalog,pg_temp AS $guard$ SELECT creator FROM indexer.indexer_canonical_creations_v1 WHERE pool=p_pool $guard$;
CREATE FUNCTION chat.social_account_for_wallet(p_actor text) RETURNS uuid LANGUAGE sql SECURITY DEFINER SET search_path=pg_catalog,pg_temp AS $guard$ SELECT DISTINCT w.account_id FROM users.identity_wallet_links_v2 w JOIN users.account_state_v2 a USING(account_id) WHERE w.wallet=p_actor AND w.chain_id=125 AND w.state='verified' AND NOT a.deleted $guard$;
CREATE FUNCTION chat.social_wallet_for_account(p_account uuid) RETURNS text LANGUAGE sql SECURITY DEFINER SET search_path=pg_catalog,pg_temp AS $guard$ SELECT a.wallet FROM users.account_state_v2 a JOIN users.identity_wallet_links_v2 w ON w.account_id=a.account_id AND w.wallet=a.wallet AND w.chain_id=125 AND w.state='verified' WHERE a.account_id=p_account AND NOT a.deleted $guard$;
CREATE FUNCTION chat.social_current_source_order() RETURNS TABLE(height text,transaction_index text,log_index text) LANGUAGE sql SECURITY DEFINER SET search_path=pg_catalog,pg_temp AS $guard$ SELECT l.height::text,l.transaction_index::text,l.log_index::text FROM indexer.indexer_raw_logs_v1 l JOIN indexer.indexer_block_completeness_v1 b ON b.chain_id=l.chain_id AND b.height=l.height AND b.block_hash=l.block_hash WHERE l.chain_id=125 AND b.fetch_complete AND b.required_decode_complete ORDER BY l.height DESC,l.transaction_index DESC,l.log_index DESC LIMIT 1 $guard$;
CREATE FUNCTION chat.social_receipt_committed(p_hash text) RETURNS boolean LANGUAGE sql SECURITY DEFINER SET search_path=pg_catalog,pg_temp AS $guard$ SELECT EXISTS(SELECT 1 FROM indexer.indexer_raw_logs_v1 l JOIN indexer.indexer_block_completeness_v1 b ON b.chain_id=l.chain_id AND b.height=l.height AND b.block_hash=l.block_hash WHERE l.chain_id=125 AND l.transaction_hash=p_hash AND b.fetch_complete AND b.required_decode_complete) $guard$;
REVOKE ALL ON FUNCTION chat.interaction_session_valid(uuid,text),chat.interaction_account_active(text),chat.interaction_pool_creator(text),chat.social_account_for_wallet(text),chat.social_wallet_for_account(uuid),chat.social_current_source_order(),chat.social_receipt_committed(text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION chat.interaction_session_valid(uuid,text),chat.interaction_account_active(text),chat.interaction_pool_creator(text),chat.social_account_for_wallet(text),chat.social_wallet_for_account(uuid),chat.social_current_source_order(),chat.social_receipt_committed(text) TO chat_runtime;
`
