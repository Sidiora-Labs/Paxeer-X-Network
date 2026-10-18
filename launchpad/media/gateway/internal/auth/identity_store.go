package auth

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	sharedauth "github.com/Sidiora-Technologies/KindleLaunch/shared/auth"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"net"
	"strings"
	"time"
	"unicode/utf8"
)

func (p Principal) ProofAt() time.Time { return p.reauthenticatedAt }
func nullableID(v string) any {
	if v == "" {
		return nil
	}
	return v
}

// IssueLogin trusts only the caller's transport IP, never a forwarded header.
func (s *Identity) IssueLogin(ctx context.Context, r ChallengeRequest, ip string) (Challenge, error) {
	if e := s.enter(); e != nil {
		return Challenge{}, e
	}
	defer s.leave()
	w, e := normalizeWallet(r.Wallet)
	if e != nil || !s.origins[r.Origin] || net.ParseIP(ip) == nil {
		return Challenge{}, invalid()
	}
	return s.issue(ctx, w, "login", "", "", "", ip)
}
func (s *Identity) issue(ctx context.Context, w, purpose, account, session, link, ip string) (Challenge, error) {
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return Challenge{}, unavailable()
	}
	defer tx.Rollback(ctx)
	c, e := s.issueTx(ctx, tx, w, purpose, account, session, link, ip)
	if e != nil {
		return Challenge{}, e
	}
	if e = tx.Commit(ctx); e != nil {
		return Challenge{}, unavailable()
	}
	return c, nil
}
func (s *Identity) issueTx(ctx context.Context, tx pgx.Tx, w, purpose, account, session, link, ip string) (Challenge, error) {
	now := s.now().UTC().Truncate(time.Millisecond)
	nonce, e := randomSecret()
	if e != nil {
		return Challenge{}, e
	}
	c := Challenge{ChallengeID: uuid.NewString(), Wallet: w, Nonce: nonce, Domain: s.domain, URI: s.uri, ChainID: "125", Purpose: purpose, IssuedAt: now, ExpiresAt: now.Add(300 * time.Second)}
	c.Message, e = BuildSIWE(c, account)
	if e != nil {
		return c, e
	}
	for _, k := range []struct {
		key string
		n   int
	}{{"wallet:" + w, 10}, {"ip:" + ip, 30}} {
		mac := hmac.New(sha256.New, s.limiterKey)
		mac.Write([]byte(k.key))
		var n int
		e = tx.QueryRow(ctx, `INSERT INTO identity_limits(key_hash,window_at,requests) VALUES($1,$2,1) ON CONFLICT(key_hash) DO UPDATE SET requests=CASE WHEN identity_limits.window_at<=$2-interval '60 seconds' THEN 1 ELSE identity_limits.requests+1 END,window_at=CASE WHEN identity_limits.window_at<=$2-interval '60 seconds' THEN $2 ELSE identity_limits.window_at END RETURNING requests`, mac.Sum(nil), now).Scan(&n)
		if e != nil {
			return Challenge{}, unavailable()
		}
		if n > k.n {
			x := identityError("RATE_LIMITED", 429).(*PublicError)
			ms := 60000
			x.RetryAfterMS = &ms
			s.observe("throttled")
			return Challenge{}, x
		}
	}
	var n int
	e = tx.QueryRow(ctx, `SELECT count(*) FROM identity_challenges WHERE wallet=$1 AND consumed_at IS NULL AND expires_at>$2`, w, now).Scan(&n)
	if e != nil {
		return Challenge{}, unavailable()
	}
	if n >= 10 {
		return Challenge{}, identityError("RATE_LIMITED", 429)
	}
	_, e = tx.Exec(ctx, `INSERT INTO identity_challenges(challenge_id,wallet,nonce_hash,message,message_hash,domain,uri,purpose,account_id,session_id,link_id,issued_at,expires_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`, c.ChallengeID, w, digest(nonce), c.Message, digest(c.Message), c.Domain, c.URI, purpose, nullableID(account), nullableID(session), nullableID(link), now, c.ExpiresAt)
	if e != nil {
		return Challenge{}, unavailable()
	}
	s.observe("issued")
	return c, nil
}

type challengeRecord struct {
	wallet, message, purpose, account, session, link, domain, uri string
	hash                                                          []byte
	expires                                                       time.Time
}

func (s *Identity) verifyChallenge(ctx context.Context, id string, p WalletProof, purpose, account, session, link string) (challengeRecord, error) {
	return s.verifyChallengeWith(ctx, s.pool, id, p, purpose, account, session, link)
}

type identityRowQuerier interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func (s *Identity) verifyChallengeWith(ctx context.Context, q identityRowQuerier, id string, p WalletProof, purpose, account, session, link string) (challengeRecord, error) {
	var c challengeRecord
	if !validUUID(id) {
		return c, unauthorized()
	}
	w, e := normalizeWallet(p.Wallet)
	if e != nil {
		return c, unauthorized()
	}
	e = q.QueryRow(ctx, `SELECT wallet,message,message_hash,purpose,domain,uri,coalesce(account_id::text,''),coalesce(session_id::text,''),coalesce(link_id::text,''),expires_at FROM identity_challenges WHERE challenge_id=$1 AND consumed_at IS NULL`, id).Scan(&c.wallet, &c.message, &c.hash, &c.purpose, &c.domain, &c.uri, &c.account, &c.session, &c.link, &c.expires)
	if errors.Is(e, pgx.ErrNoRows) {
		s.observe("unknown_or_used")
		return c, unauthorized()
	}
	if e != nil {
		return c, unavailable()
	}
	if !s.now().Before(c.expires) {
		s.observe("challenge_expired")
		return c, unauthorized()
	}
	for _, mismatch := range []struct {
		failed bool
		reason string
	}{{c.domain != s.domain, "challenge_domain"}, {c.uri != s.uri, "challenge_uri"}, {c.wallet != w, "challenge_wallet"}, {c.purpose != purpose, "challenge_purpose"}, {c.account != account || c.session != session || c.link != link, "challenge_binding"}, {!bytes.Equal(digest(c.message), c.hash), "challenge_digest"}} {
		if mismatch.failed {
			s.observe(mismatch.reason)
			return c, unauthorized()
		}
	}
	if len(p.Signature) > 8194 || !strings.HasPrefix(p.Signature, "0x") || (p.Kind != "eoa_eip191" && p.Kind != "erc1271") {
		return c, unauthorized()
	}
	verificationStarted := time.Now()
	e = s.verifier.VerifyIdentity(ctx, c.message, p)
	if p.Kind == "eoa_eip191" {
		s.measureEOACount.Add(1)
		s.measureEOAWallNS.Add(uint64(time.Since(verificationStarted).Nanoseconds()))
	} else {
		s.measureContractCount.Add(1)
	}
	if e != nil {
		var public *PublicError
		if errors.As(e, &public) && public.Status == 503 {
			return c, unavailable()
		}
		return c, unauthorized()
	}
	s.observe("verified")
	return c, nil
}
func consume(ctx context.Context, tx pgx.Tx, id string, now time.Time) error {
	tag, e := tx.Exec(ctx, `UPDATE identity_challenges SET consumed_at=clock_timestamp(),consumption_id=$3 WHERE challenge_id=$1 AND consumed_at IS NULL AND expires_at>$2 AND expires_at>clock_timestamp()`, id, now, uuid.NewString())
	if e != nil {
		return unavailable()
	}
	if tag.RowsAffected() != 1 {
		return unauthorized()
	}
	return nil
}
func (s *Identity) Login(ctx context.Context, r LoginRequest) (Credentials, error) {
	if e := s.enter(); e != nil {
		return Credentials{}, e
	}
	defer s.leave()
	if !utf8.ValidString(r.DeviceLabel) || utf8.RuneCountInString(r.DeviceLabel) > 80 {
		return Credentials{}, invalid()
	}
	c, e := s.verifyChallenge(ctx, r.ChallengeID, r.Proof, "login", "", "", "")
	if e != nil {
		return Credentials{}, e
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return Credentials{}, unavailable()
	}
	defer tx.Rollback(ctx)
	now := s.now().UTC()
	if e = consume(ctx, tx, r.ChallengeID, now); e != nil {
		return Credentials{}, e
	}
	if _, e = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, c.wallet); e != nil {
		return Credentials{}, unavailable()
	}
	var account string
	var epoch int64
	e = tx.QueryRow(ctx, `SELECT a.account_id::text,a.auth_epoch FROM identity_accounts a JOIN identity_wallet_links w USING(account_id) WHERE w.wallet=$1 AND w.chain_id=125 AND w.state='verified' FOR UPDATE OF a`, c.wallet).Scan(&account, &epoch)
	if errors.Is(e, pgx.ErrNoRows) {
		account = uuid.NewString()
		_, e = tx.Exec(ctx, `INSERT INTO identity_accounts(account_id,created_at) VALUES($1,$2)`, account, now)
		if e == nil {
			_, e = tx.Exec(ctx, `INSERT INTO identity_wallet_links(link_id,account_id,chain_id,wallet,visibility,state,linked_at) VALUES($1,$2,125,$3,'private','verified',$4)`, uuid.NewString(), account, c.wallet, now)
		}
	}
	if e != nil {
		return Credentials{}, unavailable()
	}
	rtn, e := s.createSession(ctx, tx, account, c.wallet, epoch, r.DeviceLabel, now)
	if e != nil {
		return Credentials{}, e
	}
	if e = tx.Commit(ctx); e != nil {
		return Credentials{}, unavailable()
	}
	s.observe("consumed")
	return rtn, nil
}
func (s *Identity) createSession(ctx context.Context, tx pgx.Tx, account, w string, epoch int64, label string, now time.Time) (Credentials, error) {
	access, e := randomSecret()
	if e != nil {
		return Credentials{}, e
	}
	refresh, e := randomSecret()
	if e != nil {
		return Credentials{}, e
	}
	salt, e := randomSecret()
	if e != nil {
		return Credentials{}, e
	}
	refresh = salt + "." + refresh
	csrf := s.refreshCSRFSecret(refresh)
	sess := Session{SessionID: uuid.NewString(), Wallet: w, AccessExpiresAt: now.Add(15 * time.Minute), RefreshExpiresAt: now.Add(30 * 24 * time.Hour), Scopes: []string{"account:owner"}, DeviceLabel: label}
	_, e = tx.Exec(ctx, `INSERT INTO identity_sessions(session_id,account_id,wallet,auth_epoch,access_hash,csrf_hash,issued_at,access_expires_at,refresh_expires_at,reauthenticated_at,last_seen_at,device_label) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$7,$7,$10)`, sess.SessionID, account, w, epoch, digest(access), digest(csrf), now, sess.AccessExpiresAt, sess.RefreshExpiresAt, label)
	if e != nil {
		return Credentials{}, unavailable()
	}
	_, e = tx.Exec(ctx, `INSERT INTO identity_refresh(refresh_hash,salt,session_id,issued_at,expires_at) VALUES($1,$2,$3,$4,$5)`, digest(refresh), salt, sess.SessionID, now, sess.RefreshExpiresAt)
	if e != nil {
		return Credentials{}, unavailable()
	}
	if s.tokens != nil {
		access, e = s.tokens.Mint(ctx, Principal{accountID: account, sessionID: sess.SessionID, wallet: w, epoch: epoch, reauthenticatedAt: now, accessID: access})
		if e != nil {
			return Credentials{}, e
		}
	}
	if s.events != nil {
		if e = s.audit(ctx, tx, Principal{accountID: account, sessionID: sess.SessionID, wallet: w, epoch: epoch}, "session_created"); e != nil {
			return Credentials{}, e
		}
	}
	return Credentials{LoginData: LoginData{Session: sess, AccessToken: access}, RefreshSecret: refresh, CSRFSecret: csrf}, nil
}

// Authenticate returns authority only after an authoritative database check.
func (s *Identity) Authenticate(ctx context.Context, access string) (Principal, error) {
	var p Principal
	if len(access) != 64 {
		return p, unauthorized()
	}
	e := s.pool.QueryRow(ctx, `SELECT a.account_id::text,s.session_id::text,s.wallet,s.auth_epoch,s.reauthenticated_at FROM identity_sessions s JOIN identity_accounts a USING(account_id) JOIN identity_wallet_links w ON w.account_id=a.account_id AND w.wallet=s.wallet AND w.state='verified' WHERE access_hash=$1 AND s.revoked_at IS NULL AND s.access_expires_at>$2 AND s.auth_epoch=a.auth_epoch`, digest(access), s.now()).Scan(&p.accountID, &p.sessionID, &p.wallet, &p.epoch, &p.reauthenticatedAt)
	if errors.Is(e, pgx.ErrNoRows) {
		return Principal{}, unauthorized()
	}
	if e != nil {
		return Principal{}, unavailable()
	}
	// The opaque access secret is the session's access identifier (its digest
	// is access_hash), so a principal authenticated by it carries it exactly
	// as one authenticated by a signed access token does: re-minting and the
	// access-bound session checks then bind to the same session credential.
	p.accessID = access
	return p, nil
}
func (s *Identity) fresh(ctx context.Context, p Principal, tx pgx.Tx) error {
	if e := s.AuthorizeTx(ctx, tx, p); e != nil {
		return e
	}
	if !validUUID(p.accountID) || !validUUID(p.sessionID) {
		return unauthorized()
	}
	var epoch int64
	e := tx.QueryRow(ctx, `SELECT auth_epoch FROM identity_accounts WHERE account_id=$1 FOR UPDATE`, p.accountID).Scan(&epoch)
	if errors.Is(e, pgx.ErrNoRows) {
		return unauthorized()
	}
	if e != nil {
		return unavailable()
	}
	if epoch != p.epoch {
		return unauthorized()
	}
	var at time.Time
	e = tx.QueryRow(ctx, `SELECT reauthenticated_at FROM identity_sessions WHERE session_id=$1 AND account_id=$2 AND revoked_at IS NULL AND auth_epoch=$3 AND access_expires_at>$4`, p.sessionID, p.accountID, p.epoch, s.now()).Scan(&at)
	if errors.Is(e, pgx.ErrNoRows) {
		return unauthorized()
	}
	if e != nil {
		return unavailable()
	}
	if s.now().Before(at) || s.now().Sub(at) > 5*time.Minute {
		return unauthorized()
	}
	return nil
}
func (s *Identity) IssueLink(ctx context.Context, p Principal, r WalletLinkChallengeRequest, ip string) (Challenge, string, error) {
	w, e := normalizeWallet(r.Wallet)
	if e != nil || r.ChainID != "125" || (r.Visibility != "private" && r.Visibility != "public_optin") || net.ParseIP(ip) == nil {
		return Challenge{}, "", invalid()
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return Challenge{}, "", unavailable()
	}
	defer tx.Rollback(ctx)
	if e = s.fresh(ctx, p, tx); e != nil {
		return Challenge{}, "", e
	}
	link := uuid.NewString()
	_, e = tx.Exec(ctx, `INSERT INTO identity_wallet_links(link_id,account_id,chain_id,wallet,visibility,state) VALUES($1,$2,125,$3,$4,'pending')`, link, p.accountID, w, r.Visibility)
	if e != nil {
		return Challenge{}, "", unavailable()
	}
	c, e := s.issueTx(ctx, tx, w, "link_wallet", p.accountID, p.sessionID, link, ip)
	if e != nil {
		return Challenge{}, "", e
	}
	if e = tx.Commit(ctx); e != nil {
		return Challenge{}, "", unavailable()
	}
	return c, link, nil
}
func (s *Identity) VerifyLink(ctx context.Context, p Principal, r WalletLinkVerifyRequest) (WalletLink, error) {
	c, e := s.verifyChallenge(ctx, r.ChallengeID, r.Proof, "link_wallet", p.accountID, p.sessionID, r.LinkID)
	if e != nil {
		return WalletLink{}, e
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return WalletLink{}, unavailable()
	}
	defer tx.Rollback(ctx)
	if e = s.fresh(ctx, p, tx); e != nil {
		return WalletLink{}, e
	}
	if e = consume(ctx, tx, r.ChallengeID, s.now()); e != nil {
		return WalletLink{}, e
	}
	var w WalletLink
	var at time.Time
	e = tx.QueryRow(ctx, `UPDATE identity_wallet_links SET state='verified',linked_at=$4 WHERE link_id=$1 AND account_id=$2 AND wallet=$3 AND state='pending' RETURNING link_id::text,account_id::text,wallet,visibility,linked_at`, r.LinkID, p.accountID, c.wallet, s.now()).Scan(&w.LinkID, &w.AccountID, &w.Wallet, &w.Visibility, &at)
	if e != nil {
		return WalletLink{}, conflict()
	}
	w.State = "verified"
	w.ChainID = "125"
	w.LinkedAt = &at
	if e = s.audit(ctx, tx, p, "wallet_link"); e != nil {
		return WalletLink{}, e
	}
	if e = tx.Commit(ctx); e != nil {
		return WalletLink{}, unavailable()
	}
	return w, nil
}
func (s *Identity) Unlink(ctx context.Context, p Principal, r WalletLinkRemoveRequest) error {
	linkID := r.LinkID
	if !validUUID(linkID) {
		return invalid()
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return unavailable()
	}
	defer tx.Rollback(ctx)
	if e = s.fresh(ctx, p, tx); e != nil {
		return e
	}
	if len(r.ReauthProof) != 64 {
		return unauthorized()
	}
	tag, e := tx.Exec(ctx, `UPDATE identity_reauth SET consumed_at=clock_timestamp() WHERE proof_hash=$1 AND account_id=$2 AND session_id=$3 AND action='wallet_unlink' AND consumed_at IS NULL AND expires_at>clock_timestamp()`, digest(r.ReauthProof), p.accountID, p.sessionID)
	if e != nil {
		return unavailable()
	}
	if tag.RowsAffected() != 1 {
		return unauthorized()
	}
	var n int
	e = tx.QueryRow(ctx, `SELECT count(*) FROM identity_wallet_links WHERE account_id=$1 AND state='verified'`, p.accountID).Scan(&n)
	if e != nil {
		return unavailable()
	}
	if n <= 1 {
		return conflict()
	}
	var w string
	e = tx.QueryRow(ctx, `UPDATE identity_wallet_links SET state='revoked' WHERE link_id=$1 AND account_id=$2 AND state='verified' RETURNING wallet`, linkID, p.accountID).Scan(&w)
	if e != nil {
		return unauthorized()
	}
	_, e = tx.Exec(ctx, `UPDATE identity_sessions SET revoked_at=$3 WHERE account_id=$1 AND wallet=$2 AND revoked_at IS NULL`, p.accountID, w, s.now())
	if e != nil {
		return unavailable()
	}
	if e = s.audit(ctx, tx, p, "wallet_unlink"); e != nil {
		return e
	}
	if e = tx.Commit(ctx); e != nil {
		return unavailable()
	}
	return nil
}
func (s *Identity) audit(ctx context.Context, tx pgx.Tx, p Principal, action string) error {
	if len(s.securityAuditKey) > 0 {
		wallet := p.wallet
		if wallet == "" {
			if err := tx.QueryRow(ctx, `SELECT wallet FROM identity_sessions WHERE session_id=$1 AND account_id=$2`, p.sessionID, p.accountID).Scan(&wallet); err != nil {
				return unavailable()
			}
		}
		result := "accepted"
		if strings.HasSuffix(action, "_denied") {
			result = "denied"
		}
		requestID := uuid.NewString()
		eventDigest := sharedauth.RequestDigest("IDENTITY", action, []byte(p.accountID+"\x00"+p.sessionID))
		if err := sharedauth.AppendAudit(ctx, tx, s.securityAuditKey, sharedauth.AuditEvent{RequestID: requestID, Actor: wallet, Action: action, Result: result, PolicyVersion: "identity-v2", RequestDigest: eventDigest}); err != nil {
			return unavailable()
		}
	}
	if s.events != nil {
		if err := s.events.enqueue(ctx, tx, p, action); err != nil {
			return err
		}
	}
	_, e := tx.Exec(ctx, `INSERT INTO identity_outbox(event_id,account_id,session_id,action,created_at) VALUES($1,$2,$3,$4,$5)`, uuid.NewString(), p.accountID, p.sessionID, action, s.now())
	if e != nil {
		return unavailable()
	}
	return nil
}
func (s *Identity) Logout(ctx context.Context, p Principal, all bool) error {
	if !validUUID(p.accountID) || !validUUID(p.sessionID) {
		return unauthorized()
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return unavailable()
	}
	defer tx.Rollback(ctx)
	var epoch int64
	e = tx.QueryRow(ctx, `SELECT auth_epoch FROM identity_accounts WHERE account_id=$1 FOR UPDATE`, p.accountID).Scan(&epoch)
	if errors.Is(e, pgx.ErrNoRows) {
		return unauthorized()
	}
	if e != nil {
		return unavailable()
	}
	if epoch != p.epoch {
		return unauthorized()
	}
	var live bool
	e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM identity_sessions WHERE session_id=$1 AND account_id=$2 AND auth_epoch=$3 AND revoked_at IS NULL AND access_expires_at>clock_timestamp())`, p.sessionID, p.accountID, p.epoch).Scan(&live)
	if e != nil {
		return unavailable()
	}
	if !live {
		return unauthorized()
	}

	if all {
		_, e = tx.Exec(ctx, `UPDATE identity_accounts SET auth_epoch=auth_epoch+1 WHERE account_id=$1 AND auth_epoch<9223372036854775807`, p.accountID)
	}
	if e != nil {
		return unavailable()
	}
	_, e = tx.Exec(ctx, `UPDATE identity_sessions SET revoked_at=$3 WHERE account_id=$1 AND ($4 OR session_id=$2) AND revoked_at IS NULL`, p.accountID, p.sessionID, s.now(), all)
	if e != nil {
		return unavailable()
	}
	if e = s.audit(ctx, tx, p, "session_revoked"); e != nil {
		return e
	}
	if e = tx.Commit(ctx); e != nil {
		return unavailable()
	}
	return nil
}
func (s *Identity) Refresh(ctx context.Context, secret string) (Credentials, error) {
	if e := s.enter(); e != nil {
		return Credentials{}, e
	}
	defer s.leave()
	if len(secret) != 129 || secret[64] != '.' {
		return Credentials{}, unauthorized()
	}
	if _, e := hex.DecodeString(strings.ReplaceAll(secret, ".", "")); e != nil {
		return Credentials{}, unauthorized()
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return Credentials{}, unavailable()
	}
	defer tx.Rollback(ctx)
	var sid string
	var used *time.Time
	var expires time.Time
	e = tx.QueryRow(ctx, `SELECT session_id::text,consumed_at,expires_at FROM identity_refresh WHERE refresh_hash=$1 FOR UPDATE`, digest(secret)).Scan(&sid, &used, &expires)
	if errors.Is(e, pgx.ErrNoRows) {
		return Credentials{}, unauthorized()
	}
	if e != nil {
		return Credentials{}, unavailable()
	}
	if used != nil {
		_, e = tx.Exec(ctx, `UPDATE identity_sessions SET revoked_at=$2 WHERE session_id=$1`, sid, s.now())
		if e != nil {
			return Credentials{}, unavailable()
		}
		var account string
		if e = tx.QueryRow(ctx, `SELECT account_id::text FROM identity_sessions WHERE session_id=$1`, sid).Scan(&account); e != nil {
			return Credentials{}, unavailable()
		}
		if e = s.audit(ctx, tx, Principal{accountID: account, sessionID: sid}, "refresh_reuse"); e != nil {
			return Credentials{}, e
		}
		if e = tx.Commit(ctx); e != nil {
			return Credentials{}, unavailable()
		}
		s.observe("refresh_reuse")
		return Credentials{}, unauthorized()
	}
	if !s.now().Before(expires) {
		return Credentials{}, unauthorized()
	}
	var wallet, label, account string
	var epoch int64
	e = tx.QueryRow(ctx, `SELECT s.wallet,s.device_label,s.account_id::text,s.auth_epoch FROM identity_sessions s JOIN identity_accounts a USING(account_id) WHERE session_id=$1 AND revoked_at IS NULL AND s.auth_epoch=a.auth_epoch AND s.refresh_expires_at>clock_timestamp() FOR UPDATE OF s`, sid).Scan(&wallet, &label, &account, &epoch)
	if errors.Is(e, pgx.ErrNoRows) {
		return Credentials{}, unauthorized()
	}
	if e != nil {
		return Credentials{}, unavailable()
	}
	access, e := randomSecret()
	if e != nil {
		return Credentials{}, e
	}
	next, e := randomSecret()
	if e != nil {
		return Credentials{}, e
	}
	salt, e := randomSecret()
	if e != nil {
		return Credentials{}, e
	}
	next = salt + "." + next
	csrf := s.refreshCSRFSecret(next)
	now := s.now()
	accessExpires := now.Add(15 * time.Minute)
	_, e = tx.Exec(ctx, `UPDATE identity_refresh SET consumed_at=$2 WHERE refresh_hash=$1`, digest(secret), now)
	if e != nil {
		return Credentials{}, unavailable()
	}
	_, e = tx.Exec(ctx, `INSERT INTO identity_refresh(refresh_hash,salt,session_id,parent_hash,issued_at,expires_at) VALUES($1,$2,$3,$4,$5,$6)`, digest(next), salt, sid, digest(secret), now, expires)
	if e != nil {
		return Credentials{}, unavailable()
	}
	_, e = tx.Exec(ctx, `UPDATE identity_sessions SET access_hash=$2,csrf_hash=$3,access_expires_at=$4,last_seen_at=$5 WHERE session_id=$1`, sid, digest(access), digest(csrf), accessExpires, now)
	if e != nil {
		return Credentials{}, unavailable()
	}
	if s.tokens != nil {
		access, e = s.tokens.Mint(ctx, Principal{accountID: account, sessionID: sid, wallet: wallet, epoch: epoch, accessID: access})
		if e != nil {
			return Credentials{}, e
		}
	}
	if s.events != nil {
		if e = s.audit(ctx, tx, Principal{accountID: account, sessionID: sid, wallet: wallet, epoch: epoch}, "session_refreshed"); e != nil {
			return Credentials{}, e
		}
	}
	if e = tx.Commit(ctx); e != nil {
		return Credentials{}, unavailable()
	}
	return Credentials{LoginData: LoginData{AccessToken: access, Session: Session{SessionID: sid, Wallet: wallet, AccessExpiresAt: accessExpires, RefreshExpiresAt: expires, DeviceLabel: label, Scopes: []string{"account:owner"}}}, RefreshSecret: next, CSRFSecret: csrf}, nil
}
func validEndpoint(v string) bool {
	return len(v) > 0 && len(v) <= 256 && strings.HasPrefix(v, "/") && !strings.ContainsAny(v, "?#\r\n")
}
func (s *Identity) IssueTicket(ctx context.Context, p Principal, origin, endpoint string) (string, error) {
	if !s.origins[origin] || !validEndpoint(endpoint) {
		return "", invalid()
	}
	secret, e := randomSecret()
	if e != nil {
		return "", e
	}
	now := s.now()
	tag, e := s.pool.Exec(ctx, `INSERT INTO identity_tickets(ticket_hash,session_id,origin,endpoint,issued_at,expires_at) SELECT $1,session_id,$3,$4,$5,$6 FROM identity_sessions s JOIN identity_accounts a USING(account_id) WHERE s.session_id=$2 AND s.account_id=$7 AND s.revoked_at IS NULL AND s.auth_epoch=$8 AND a.auth_epoch=$8 AND s.wallet=$9 AND s.access_expires_at>$5`, digest(secret), p.sessionID, origin, endpoint, now, now.Add(30*time.Second), p.accountID, p.epoch, p.wallet)
	if e != nil {
		return "", unavailable()
	}
	if tag.RowsAffected() != 1 {
		return "", unauthorized()
	}
	return secret, nil
}
func (s *Identity) ConsumeTicket(ctx context.Context, secret, origin, endpoint string) (Principal, error) {
	if len(secret) != 64 || !s.origins[origin] || !validEndpoint(endpoint) {
		return Principal{}, unauthorized()
	}
	var p Principal
	e := s.pool.QueryRow(ctx, `UPDATE identity_tickets t SET consumed_at=$4 FROM identity_sessions s JOIN identity_accounts a USING(account_id) WHERE ticket_hash=$1 AND t.session_id=s.session_id AND t.origin=$2 AND t.endpoint=$3 AND t.consumed_at IS NULL AND t.expires_at>$4 AND s.revoked_at IS NULL AND s.auth_epoch=a.auth_epoch AND s.access_expires_at>$4 RETURNING a.account_id::text,s.session_id::text,s.wallet,s.auth_epoch,s.reauthenticated_at`, digest(secret), origin, endpoint, s.now()).Scan(&p.accountID, &p.sessionID, &p.wallet, &p.epoch, &p.reauthenticatedAt)
	if errors.Is(e, pgx.ErrNoRows) {
		return Principal{}, unauthorized()
	}
	if e != nil {
		return Principal{}, unavailable()
	}
	return p, nil
}
func (s *Identity) CheckSocket(ctx context.Context, p Principal) error {
	wallet, err := normalizeWallet(p.wallet)
	if !validUUID(p.accountID) || !validUUID(p.sessionID) || p.epoch < 0 || err != nil || wallet != p.wallet {
		return unauthorized()
	}
	var ok bool
	e := s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM identity_sessions s JOIN identity_accounts a USING(account_id) JOIN identity_wallet_links w ON w.account_id=s.account_id AND w.wallet=s.wallet AND w.chain_id=125 AND w.state='verified' WHERE s.session_id=$1 AND s.account_id=$2 AND s.auth_epoch=$3 AND a.auth_epoch=$3 AND s.revoked_at IS NULL AND s.access_expires_at>$4 AND s.wallet=$5)`, p.sessionID, p.accountID, p.epoch, s.now(), p.wallet).Scan(&ok)
	if e != nil {
		return unavailable()
	}
	if !ok {
		return unauthorized()
	}
	return nil
}

func (s *Identity) AuthorizeTx(ctx context.Context, tx pgx.Tx, p Principal) error {
	if tx == nil || !validUUID(p.accountID) || !validUUID(p.sessionID) {
		return unauthorized()
	}
	var epoch int64
	err := tx.QueryRow(ctx, `SELECT auth_epoch FROM identity_accounts WHERE account_id=$1 FOR UPDATE`, p.accountID).Scan(&epoch)
	if errors.Is(err, pgx.ErrNoRows) {
		return unauthorized()
	}
	if err != nil {
		return unavailable()
	}
	if epoch != p.epoch {
		return unauthorized()
	}
	var wallet string
	err = tx.QueryRow(ctx, `SELECT s.wallet FROM identity_sessions s JOIN identity_wallet_links w ON w.account_id=s.account_id AND w.wallet=s.wallet AND w.chain_id=125 AND w.state='verified' WHERE s.account_id=$1 AND s.session_id=$2 AND s.auth_epoch=$3 AND s.revoked_at IS NULL AND s.access_expires_at>clock_timestamp() AND ($4::bytea IS NULL OR s.access_hash=$4) FOR UPDATE OF s`, p.accountID, p.sessionID, p.epoch, optionalAccessDigest(p.accessID)).Scan(&wallet)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && wallet != p.wallet) {
		return unauthorized()
	}
	if err != nil {
		return unavailable()
	}
	return nil
}

func (s *Identity) LogoutSession(ctx context.Context, p Principal, id string) error {
	if !validUUID(id) {
		return invalid()
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return unavailable()
	}
	defer tx.Rollback(ctx)
	if err = s.AuthorizeTx(ctx, tx, p); err != nil {
		return err
	}
	var owner string
	err = tx.QueryRow(ctx, `SELECT account_id::text FROM identity_sessions WHERE session_id=$1 FOR UPDATE`, id).Scan(&owner)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && owner != p.accountID) {
		return unauthorized()
	}
	if err != nil {
		return unavailable()
	}
	tag, err := tx.Exec(ctx, `UPDATE identity_sessions SET revoked_at=clock_timestamp() WHERE account_id=$1 AND session_id=$2 AND revoked_at IS NULL`, p.accountID, id)
	if err != nil {
		return unavailable()
	}
	if tag.RowsAffected() == 1 {
		target := p
		target.sessionID = id
		if err = s.audit(ctx, tx, target, "session_revoked"); err != nil {
			return err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return unavailable()
	}
	return nil
}

func (s *Identity) IssueLoginIdempotent(ctx context.Context, r ChallengeRequest, ip, key string, body []byte) (Challenge, error) {
	if err := s.enter(); err != nil {
		return Challenge{}, err
	}
	defer s.leave()
	wallet, err := normalizeWallet(r.Wallet)
	if err != nil || !s.origins[r.Origin] || net.ParseIP(ip) == nil || !validUUID(key) {
		return Challenge{}, invalid()
	}
	hash := sha256.Sum256(body)
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Challenge{}, unavailable()
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, wallet+":"+key); err != nil {
		return Challenge{}, unavailable()
	}
	var prior, raw []byte
	err = tx.QueryRow(ctx, `SELECT body_hash,response FROM identity_challenge_requests WHERE wallet=$1 AND request_id=$2`, wallet, key).Scan(&prior, &raw)
	if err == nil {
		if !bytes.Equal(prior, hash[:]) {
			return Challenge{}, identityError("IDEMPOTENCY_CONFLICT", 409)
		}
		var challenge Challenge
		if json.Unmarshal(raw, &challenge) != nil {
			return Challenge{}, unavailable()
		}
		if err = tx.Commit(ctx); err != nil {
			return Challenge{}, unavailable()
		}
		return challenge, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return Challenge{}, unavailable()
	}
	challenge, err := s.issueTx(ctx, tx, wallet, "login", "", "", "", ip)
	if err != nil {
		return Challenge{}, err
	}
	raw, err = json.Marshal(challenge)
	if err != nil {
		return Challenge{}, unavailable()
	}
	if _, err = tx.Exec(ctx, `INSERT INTO identity_challenge_requests(wallet,request_id,body_hash,response,retain_until) VALUES($1,$2,$3,$4::jsonb,clock_timestamp()+interval '24 hours')`, wallet, key, hash[:], raw); err != nil {
		return Challenge{}, unavailable()
	}
	if err = tx.Commit(ctx); err != nil {
		return Challenge{}, unavailable()
	}
	return challenge, nil
}

func (s *Identity) refreshCSRFSecret(secret string) string {
	mac := hmac.New(sha256.New, s.limiterKey)
	mac.Write([]byte("identity-refresh-csrf-v2\x00"))
	mac.Write([]byte(secret))
	return hex.EncodeToString(mac.Sum(nil))
}
