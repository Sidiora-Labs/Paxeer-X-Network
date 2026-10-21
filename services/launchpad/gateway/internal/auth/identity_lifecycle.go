package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"net"
	"time"
)

const IdentityOwnerDDL = `CREATE TABLE identity_owner_requests(account_id uuid NOT NULL REFERENCES identity_accounts,request_id uuid NOT NULL,operation text NOT NULL,body_hash bytea NOT NULL,response jsonb NOT NULL,retain_until timestamptz NOT NULL,PRIMARY KEY(account_id,request_id,operation));
CREATE TABLE identity_revisions(account_id uuid PRIMARY KEY REFERENCES identity_accounts,revision bigint NOT NULL CHECK(revision>0));
CREATE TABLE identity_revision_inbox(event_id text PRIMARY KEY,account_id uuid NOT NULL,revision bigint NOT NULL CHECK(revision>0),payload_hash text NOT NULL);
CREATE TABLE identity_remote_revisions(account_id uuid PRIMARY KEY,revision bigint NOT NULL CHECK(revision>0));
CREATE FUNCTION identity_verified_at_immutable() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF OLD.linked_at IS NOT NULL AND OLD.linked_at IS DISTINCT FROM NEW.linked_at THEN RAISE EXCEPTION 'verified timestamp immutable' USING ERRCODE='23514'; END IF; RETURN NEW; END $$;
CREATE TRIGGER identity_verified_at_immutable BEFORE UPDATE OF linked_at ON identity_wallet_links FOR EACH ROW EXECUTE FUNCTION identity_verified_at_immutable();`

type LinkChallengeData struct {
	Challenge Challenge `json:"challenge"`
	LinkID    string    `json:"linkId"`
}

func (s *Identity) ownerMutation(ctx context.Context, p Principal, key, operation string, body []byte, effect func(pgx.Tx) (any, error)) (json.RawMessage, error) {
	if !validUUID(key) || len(body) > 16384 {
		return nil, invalid()
	}
	if err := s.enter(); err != nil {
		return nil, err
	}
	defer s.leave()
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, unavailable()
	}
	defer tx.Rollback(ctx)
	if err = s.AuthorizeTx(ctx, tx, p); err != nil {
		return nil, err
	}
	var prior, raw []byte
	err = tx.QueryRow(ctx, `SELECT body_hash,response FROM identity_owner_requests WHERE account_id=$1 AND request_id=$2 AND operation=$3`, p.accountID, key, operation).Scan(&prior, &raw)
	if err == nil {
		if !bytes.Equal(prior, digest(string(body))) {
			return nil, identityError("IDEMPOTENCY_CONFLICT", 409)
		}
		if err = tx.Commit(ctx); err != nil {
			return nil, unavailable()
		}
		return raw, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, unavailable()
	}
	data, err := effect(tx)
	if err != nil {
		return nil, err
	}
	raw, err = json.Marshal(data)
	if err != nil {
		return nil, unavailable()
	}
	_, err = tx.Exec(ctx, `INSERT INTO identity_owner_requests(account_id,request_id,operation,body_hash,response,retain_until) VALUES($1,$2,$3,$4,$5::jsonb,clock_timestamp()+interval '24 hours')`, p.accountID, key, operation, digest(string(body)), raw)
	if err != nil {
		return nil, unavailable()
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, unavailable()
	}
	return raw, nil
}

func (s *Identity) IssueLinkIdempotent(ctx context.Context, p Principal, r WalletLinkChallengeRequest, ip, key string, body []byte) (json.RawMessage, error) {
	wallet, err := normalizeWallet(r.Wallet)
	if err != nil || net.ParseIP(ip) == nil || r.ChainID != "125" || (r.Visibility != "private" && r.Visibility != "public_optin") {
		return nil, invalid()
	}
	result, mutationErr := s.ownerMutation(ctx, p, key, "link.challenge", body, func(tx pgx.Tx) (any, error) {
		if err := s.fresh(ctx, p, tx); err != nil {
			return nil, err
		}
		var owned bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM identity_wallet_links WHERE chain_id=125 AND wallet=$1 AND state='verified')`, wallet).Scan(&owned); err != nil {
			return nil, unavailable()
		}
		if owned {
			return nil, conflict()
		}
		link := uuid.NewString()
		if _, err := tx.Exec(ctx, `INSERT INTO identity_wallet_links(link_id,account_id,chain_id,wallet,visibility,state) VALUES($1,$2,125,$3,$4,'pending')`, link, p.accountID, wallet, r.Visibility); err != nil {
			return nil, unavailable()
		}
		challenge, err := s.issueTx(ctx, tx, wallet, "link_wallet", p.accountID, p.sessionID, link, ip)
		if err != nil {
			return nil, err
		}
		return LinkChallengeData{challenge, link}, nil
	})
	if mutationErr != nil {
		if auditErr := s.recordIdentityDenial(ctx, p, "wallet_link_denied"); auditErr != nil {
			return nil, auditErr
		}
	}
	return result, mutationErr
}
func (s *Identity) VerifyLinkIdempotent(ctx context.Context, p Principal, r WalletLinkVerifyRequest, key string, body []byte) (json.RawMessage, error) {
	result, mutationErr := s.ownerMutation(ctx, p, key, "link.verify:"+r.LinkID, body, func(tx pgx.Tx) (any, error) {
		if err := s.fresh(ctx, p, tx); err != nil {
			return nil, err
		}
		record, err := s.verifyChallengeWith(ctx, tx, r.ChallengeID, r.Proof, "link_wallet", p.accountID, p.sessionID, r.LinkID)
		if err != nil {
			return nil, err
		}
		if err = consume(ctx, tx, r.ChallengeID, s.now()); err != nil {
			return nil, err
		}
		var link WalletLink
		var at time.Time
		err = tx.QueryRow(ctx, `UPDATE identity_wallet_links SET state='verified',linked_at=$4 WHERE link_id=$1 AND account_id=$2 AND wallet=$3 AND state='pending' RETURNING link_id::text,account_id::text,wallet,visibility,linked_at`, r.LinkID, p.accountID, record.wallet, s.now()).Scan(&link.LinkID, &link.AccountID, &link.Wallet, &link.Visibility, &at)
		if err != nil {
			var pg *pgconn.PgError
			if errors.As(err, &pg) && pg.Code == "23505" {
				return nil, conflict()
			}
			if errors.Is(err, pgx.ErrNoRows) {
				return nil, conflict()
			}
			return nil, unavailable()
		}
		link.ChainID = "125"
		link.State = "verified"
		link.LinkedAt = &at
		if err = s.audit(ctx, tx, p, "wallet_link"); err != nil {
			return nil, err
		}
		return struct {
			Link WalletLink `json:"link"`
		}{link}, nil
	})
	if mutationErr != nil {
		if auditErr := s.recordIdentityDenial(ctx, p, "wallet_link_denied"); auditErr != nil {
			return nil, auditErr
		}
	}
	return result, mutationErr
}
func (s *Identity) UnlinkIdempotent(ctx context.Context, p Principal, r WalletLinkRemoveRequest, key string, body []byte) (json.RawMessage, error) {
	if !validUUID(r.LinkID) || len(r.ReauthProof) != 64 {
		return nil, invalid()
	}
	result, mutationErr := s.ownerMutation(ctx, p, key, "link.remove:"+r.LinkID, body, func(tx pgx.Tx) (any, error) {
		if err := s.fresh(ctx, p, tx); err != nil {
			return nil, err
		}
		tag, err := tx.Exec(ctx, `UPDATE identity_reauth SET consumed_at=clock_timestamp() WHERE proof_hash=$1 AND account_id=$2 AND session_id=$3 AND action='wallet_unlink' AND consumed_at IS NULL AND expires_at>clock_timestamp()`, digest(r.ReauthProof), p.accountID, p.sessionID)
		if err != nil {
			return nil, unavailable()
		}
		if tag.RowsAffected() != 1 {
			return nil, unauthorized()
		}
		var count int
		if err = tx.QueryRow(ctx, `SELECT count(*) FROM identity_wallet_links WHERE account_id=$1 AND state='verified'`, p.accountID).Scan(&count); err != nil {
			return nil, unavailable()
		}
		if count <= 1 {
			s.observe("last_wallet_unlink_denied")
			return nil, conflict()
		}
		var wallet string
		err = tx.QueryRow(ctx, `UPDATE identity_wallet_links SET state='revoked' WHERE link_id=$1 AND account_id=$2 AND state='verified' RETURNING wallet`, r.LinkID, p.accountID).Scan(&wallet)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, unauthorized()
		}
		if err != nil {
			return nil, unavailable()
		}
		if _, err = tx.Exec(ctx, `UPDATE identity_sessions SET revoked_at=clock_timestamp() WHERE account_id=$1 AND wallet=$2 AND revoked_at IS NULL`, p.accountID, wallet); err != nil {
			return nil, unavailable()
		}
		if err = s.audit(ctx, tx, p, "wallet_unlink"); err != nil {
			return nil, err
		}
		return nil, nil
	})
	if mutationErr != nil {
		if auditErr := s.recordIdentityDenial(ctx, p, "wallet_unlink_denied"); auditErr != nil {
			return nil, auditErr
		}
	}
	return result, mutationErr
}
func (s *Identity) WalletLinks(ctx context.Context, p Principal) ([]WalletLink, error) {
	if err := s.CheckSocket(ctx, p); err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, `SELECT link_id::text,account_id::text,wallet,visibility,state,linked_at FROM identity_wallet_links WHERE account_id=$1 ORDER BY link_id LIMIT 101`, p.accountID)
	if err != nil {
		return nil, unavailable()
	}
	defer rows.Close()
	links := []WalletLink{}
	for rows.Next() {
		var link WalletLink
		if err = rows.Scan(&link.LinkID, &link.AccountID, &link.Wallet, &link.Visibility, &link.State, &link.LinkedAt); err != nil {
			return nil, unavailable()
		}
		link.ChainID = "125"
		links = append(links, link)
	}
	if rows.Err() != nil {
		return nil, unavailable()
	}
	if len(links) > 100 {
		return nil, identityError("RATE_LIMITED", 429)
	}
	return links, nil
}

func optionalAccessDigest(accessID string) []byte {
	if accessID == "" {
		return nil
	}
	return digest(accessID)
}
