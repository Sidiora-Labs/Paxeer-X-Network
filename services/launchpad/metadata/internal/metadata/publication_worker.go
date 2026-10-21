package metadata

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	sharedauth "github.com/Sidiora-Technologies/KindleLaunch/shared/auth"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type publicationLease struct {
	Operation, Owner string
	Fence            int64
	Attempt          int
	Pool             string
}

func (s PublicationService) RunPublicationBatch(ctx context.Context, limit int) (int, error) {
	if !s.ready() || limit < 1 || limit > 10 {
		return 0, ErrValidation
	}
	processed := 0
	for processed < limit {
		lease, e := s.claimPublication(ctx)
		if errors.Is(e, pgx.ErrNoRows) {
			return processed, nil
		}
		if e != nil {
			return processed, e
		}
		bounded, cancel := context.WithTimeout(ctx, 15*time.Second)
		e = s.Media.syncModeration(bounded, lease.Pool)
		if e == nil {
			e = s.applyPublication(bounded, lease)
		}
		cancel()
		if e != nil {
			cleanup, stop := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
			recordErr := s.failPublication(cleanup, lease, e)
			stop()
			if recordErr != nil {
				return processed, recordErr
			}
		}
		processed++
	}
	return processed, nil
}
func (s PublicationService) claimPublication(ctx context.Context) (publicationLease, error) {
	var l publicationLease
	store := s.Media.Metadata.Store
	l.Owner = uuid.NewString()
	_, e := store.Pool.Exec(ctx, "UPDATE "+store.table("metadata_publication_jobs_v3")+" j SET state='rejected',last_error_code='retry_exhausted',lease_owner=NULL,lease_until=NULL,updated_at=clock_timestamp() FROM "+store.table("metadata_publication_intents_v3")+" i WHERE j.operation=i.operation AND j.state IN('metadata_pending','publishing') AND (j.lease_until IS NULL OR j.lease_until<=clock_timestamp()) AND (j.attempt_count>=5 OR i.expires_at<=clock_timestamp())")
	if e != nil {
		return l, e
	}
	e = store.Pool.QueryRow(ctx, "WITH due AS (SELECT j.operation FROM "+store.table("metadata_publication_jobs_v3")+" j JOIN "+store.table("metadata_publication_intents_v3")+" i USING(operation) WHERE j.state IN('metadata_pending','publishing') AND j.next_at<=clock_timestamp() AND (j.lease_until IS NULL OR j.lease_until<=clock_timestamp()) AND j.attempt_count<5 AND i.expires_at>clock_timestamp() ORDER BY j.next_at,j.operation LIMIT 1 FOR UPDATE OF j SKIP LOCKED) UPDATE "+store.table("metadata_publication_jobs_v3")+" j SET state='publishing',attempt_count=attempt_count+1,fence=fence+1,lease_owner=$1,lease_until=clock_timestamp()+interval '30 seconds',updated_at=clock_timestamp() FROM due WHERE j.operation=due.operation RETURNING j.operation::text,j.fence,j.attempt_count,(SELECT pool FROM "+store.table("metadata_publication_intents_v3")+" i WHERE i.operation=j.operation)", l.Owner).Scan(&l.Operation, &l.Fence, &l.Attempt, &l.Pool)
	return l, e
}
func (s PublicationService) failPublication(ctx context.Context, l publicationLease, cause error) error {
	state, code := "metadata_pending", "dependency_unavailable"
	switch {
	case errors.Is(cause, ErrForbidden) || errors.Is(cause, sharedauth.ErrScopeDenied):
		state, code = "authorization_required", "creator_authorization_required"
	case errors.Is(cause, ErrConflict):
		state, code = "rejected", "stale_revision"
	case errors.Is(cause, ErrValidation) || errors.Is(cause, ErrUnrepresentable):
		state, code = "rejected", "invalid_intent"
	case errors.Is(cause, ErrMissing):
		code = "asset_pending"
	}
	if l.Attempt >= 5 && state == "metadata_pending" {
		state, code = "rejected", "retry_exhausted"
	}
	delays := []int{30, 120, 300, 600, 600}
	delay := delays[l.Attempt-1]
	store := s.Media.Metadata.Store
	_, e := store.Pool.Exec(ctx, "UPDATE "+store.table("metadata_publication_jobs_v3")+" SET state=$4,last_error_code=$5,lease_owner=NULL,lease_until=NULL,next_at=clock_timestamp()+make_interval(secs=>$6),updated_at=clock_timestamp() WHERE operation=$1 AND lease_owner=$2 AND fence=$3 AND state='publishing'", l.Operation, l.Owner, l.Fence, state, code, delay)
	return e
}
func (s PublicationService) applyPublication(ctx context.Context, l publicationLease) error {
	store := s.Media.Metadata.Store
	tx, e := store.Pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer rollbackV2(ctx, tx)
	var live bool
	e = tx.QueryRow(ctx, "SELECT lease_owner=$2 AND fence=$3 AND state='publishing' AND lease_until>clock_timestamp() FROM "+store.table("metadata_publication_jobs_v3")+" WHERE operation=$1 FOR UPDATE", l.Operation, l.Owner, l.Fence).Scan(&live)
	if e != nil {
		return e
	}
	if !live {
		return fmt.Errorf("publication_fence_lost")
	}
	var raw, metadata, env, admission []byte
	var token, pool, creator, creationTx, contentHash, bodyHash, envelopeHash, nonce, requestID, accountID, sessionID string
	var epoch int64
	e = tx.QueryRow(ctx, "SELECT i.body,i.metadata,g.envelope,g.response,i.token,i.pool,i.creator,i.creation_tx_hash,i.content_hash,g.body_hash,g.envelope_digest,g.nonce,g.request_id::text,g.account_id::text,g.session_id::text,g.auth_epoch FROM "+store.table("metadata_publication_intents_v3")+" i JOIN "+store.table("metadata_publication_jobs_v3")+" j USING(operation) JOIN "+store.table("metadata_publication_grants_v3")+" g ON g.operation=i.operation AND g.nonce=j.grant_nonce WHERE i.operation=$1 AND i.expires_at>clock_timestamp() AND g.body_hash=i.body_hash", l.Operation).Scan(&raw, &metadata, &env, &admission, &token, &pool, &creator, &creationTx, &contentHash, &bodyHash, &envelopeHash, &nonce, &requestID, &accountID, &sessionID, &epoch)
	if e != nil {
		return e
	}
	b, p, e := ParsePublicationBody(raw)
	if e != nil {
		return e
	}
	var envelope sharedauth.OperationEnvelope
	if json.Unmarshal(env, &envelope) != nil {
		return ErrUnrepresentable
	}
	statement, e := envelope.Statement()
	if e != nil {
		return ErrUnrepresentable
	}
	if b.OperationID != l.Operation || !bytes.Equal(b.Metadata, metadata) || b.CreationTxHash != creationTx || publicationHash(raw) != bodyHash || publicationHash(metadata) != contentHash || publicationHash([]byte(statement)) != envelopeHash || envelope.Version != 2 || envelope.Actor != creator || envelope.Action != PublicationAction || envelope.Method != "POST" || envelope.Path != "/v3/tokens/"+token+"/metadata-publications" || envelope.BodyHash != bodyHash || envelope.Nonce != nonce || envelope.Environment != s.Operations.Ledger.Environment {
		return ErrForbidden
	}
	var storedResponse []byte
	var storedHash, storedBody, storedRequest string
	e = tx.QueryRow(ctx, "SELECT response,envelope_digest,body_digest,request_id::text FROM "+store.table("operation_ledger")+" WHERE actor=$1 AND action=$2 AND nonce=$3", creator, PublicationAction, nonce).Scan(&storedResponse, &storedHash, &storedBody, &storedRequest)
	if e != nil {
		return e
	}
	if storedHash != envelopeHash || storedBody != bodyHash || storedRequest != requestID || !bytes.Equal(storedResponse, admission) {
		return ErrForbidden
	}
	var accepted bool
	e = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM "+store.table("security_audit_outbox")+" WHERE request_id=$1 AND action=$2 AND result='accepted' AND request_digest=$3)", requestID, PublicationAction, envelopeHash).Scan(&accepted)
	if e != nil {
		return e
	}
	if !accepted {
		return ErrForbidden
	}
	current, e := s.Identities.ConfirmTx(ctx, tx, sharedauth.IdentityPrincipal{Wallet: creator, SessionID: sessionID, AccountID: accountID, AuthEpoch: epoch})
	if e != nil {
		if errors.Is(e, sharedauth.ErrScopeDenied) {
			return ErrForbidden
		}
		return e
	}
	if current.AccountID != accountID || current.AuthEpoch != epoch || current.Wallet != creator || current.SessionID != sessionID {
		return ErrForbidden
	}
	identity, e := s.Media.Metadata.identity(ctx, tx, token)
	if e != nil {
		return e
	}
	if identity.Creator != creator || identity.PoolAddress != pool || identity.Token.SourcePosition.TransactionHash != creationTx {
		return ErrForbidden
	}
	var storedPool, storedCreator string
	e = tx.QueryRow(ctx, "SELECT pool,creator FROM "+store.table("pools")+" WHERE token=$1 FOR UPDATE", token).Scan(&storedPool, &storedCreator)
	if e != nil {
		return e
	}
	if storedPool != pool || storedCreator != creator {
		return ErrForbidden
	}
	var blocked bool
	e = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM "+store.table("documents")+" WHERE pool=$1 AND blocked)", pool).Scan(&blocked)
	if e != nil {
		return e
	}
	if blocked {
		return ErrForbidden
	}
	var oldPool, oldActor, oldHash string
	var result []byte
	var data V2Data
	e = tx.QueryRow(ctx, "SELECT pool,actor,body_hash,result FROM "+store.table("v2_operations")+" WHERE operation=$1", l.Operation).Scan(&oldPool, &oldActor, &oldHash, &result)
	if e == nil {
		if oldPool != pool || oldActor != creator || oldHash != contentHash {
			return ErrConflict
		}
		if json.Unmarshal(result, &data) != nil {
			return ErrUnrepresentable
		}
	} else {
		if !errors.Is(e, pgx.ErrNoRows) {
			return e
		}
		if _, e = tx.Exec(ctx, "INSERT INTO "+store.table("v2_documents")+"(pool) VALUES($1) ON CONFLICT DO NOTHING", pool); e != nil {
			return e
		}
		var current string
		e = tx.QueryRow(ctx, "SELECT revision::text FROM "+store.table("v2_documents")+" WHERE pool=$1 FOR UPDATE", pool).Scan(&current)
		if e != nil {
			return e
		}
		if current != p.ExpectedRevision || current == "115792089237316195423570985008687907853269984665640564039457584007913129639935" {
			return ErrConflict
		}
		if p.LogoAssetID != nil {
			var state string
			e = tx.QueryRow(ctx, "SELECT state FROM "+store.table("assets")+" WHERE id=$1 AND pool=$2 FOR SHARE", *p.LogoAssetID, pool).Scan(&state)
			if errors.Is(e, pgx.ErrNoRows) {
				return ErrMissing
			}
			if e != nil {
				return e
			}
			if state == "REJECTED" || state == "DELETED" {
				return ErrForbidden
			}
			if state != "APPROVED" {
				return ErrMissing
			}
		}
		doc := map[string]any{}
		if p.Name != nil {
			doc["name"] = *p.Name
		}
		if p.Symbol != nil {
			doc["symbol"] = *p.Symbol
		}
		if p.Description != nil {
			doc["description"] = *p.Description
		}
		if p.Tags != nil {
			doc["tags"] = *p.Tags
		}
		if p.Socials != nil {
			doc["socials"] = *p.Socials
		}
		patch, e := json.Marshal(doc)
		if e != nil {
			return e
		}
		_, e = tx.Exec(ctx, "UPDATE "+store.table("v2_documents")+" SET revision=revision+1,document=document||$2::jsonb,asset_id=COALESCE($3,asset_id),updated_at=clock_timestamp() WHERE pool=$1", pool, string(patch), p.LogoAssetID)
		if e != nil {
			return e
		}
		data, e = store.readV2(ctx, tx, token)
		if e != nil {
			return e
		}
		projection := map[string]string{"description": normalizedSearchText(data.Display.Description)}
		if data.Display.Name != nil {
			projection["name"] = normalizedSearchText(*data.Display.Name)
		}
		if data.Display.Symbol != nil {
			projection["symbol"] = normalizedSearchText(*data.Display.Symbol)
		}
		search, e := json.Marshal(projection)
		if e != nil {
			return e
		}
		if _, e = tx.Exec(ctx, "UPDATE "+store.table("v2_documents")+" SET search_document=$2::jsonb WHERE pool=$1", pool, string(search)); e != nil {
			return e
		}
		if _, e = V2ContractData(data, identity); e != nil {
			return e
		}
		result, e = json.Marshal(data)
		if e != nil {
			return e
		}
		if _, e = tx.Exec(ctx, "INSERT INTO "+store.table("v2_operations")+"(operation,pool,actor,body_hash,revision,result) VALUES($1,$2,$3,$4,$5::numeric,$6::jsonb)", l.Operation, pool, creator, contentHash, data.Revision, string(result)); e != nil {
			return e
		}
		if _, e = tx.Exec(ctx, "INSERT INTO "+store.table("v2_outbox")+"(pool,revision,payload) VALUES($1,$2::numeric,jsonb_build_object('metadata',$3::jsonb,'actor',$4::text,'previousRevision',$5::text,'publicationOperation',$6::text))", pool, data.Revision, string(result), creator, current, l.Operation); e != nil {
			return e
		}
	}
	tag, e := tx.Exec(ctx, "UPDATE "+store.table("metadata_publication_jobs_v3")+" SET state='published',result=$4,revision=$5,last_error_code='',lease_owner=NULL,lease_until=NULL,updated_at=clock_timestamp() WHERE operation=$1 AND lease_owner=$2 AND fence=$3 AND state='publishing' AND lease_until>clock_timestamp()", l.Operation, l.Owner, l.Fence, result, data.Revision)
	if e != nil {
		return e
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("publication_fence_lost")
	}
	return tx.Commit(ctx)
}
