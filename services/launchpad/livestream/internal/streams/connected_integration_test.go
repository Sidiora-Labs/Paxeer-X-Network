package streams

import (
	"context"
	"errors"
	"fmt"
	"github.com/Sidiora-Technologies/KindleLaunch/media/livestream/internal/livepeer"
	sharedauth "github.com/Sidiora-Technologies/KindleLaunch/shared/auth"
	"github.com/ethereum/go-ethereum/accounts"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/jackc/pgx/v5"
	"os"
	"strings"
	"testing"
	"time"
)

func TestConnectedTransactionRollbackDoesNotConsumeAdmission(t *testing.T) {
	s := fixture(t)
	ctx := context.Background()
	r, d := intent("connected-rollback")
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	o, err := s.CreateTx(ctx, tx, r, d, true)
	if err != nil {
		_ = tx.Rollback(ctx)
		t.Fatal(err)
	}
	if err = tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Get(ctx, o.ID); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatal("rolled-back mutation became visible", err)
	}
	again, err := s.Create(ctx, r, d, true)
	if err != nil || again.ID == o.ID {
		t.Fatal(again, err)
	}
	var count int
	if err = s.pool.QueryRow(ctx, fmt.Sprintf("SELECT count(*) FROM %s.stream_outbox", s.schema)).Scan(&count); err != nil || count != 1 {
		t.Fatal(count, err)
	}
}
func TestConnectedTitleCASAndCursorBindingUseActualPostgres(t *testing.T) {
	s := fixture(t)
	ctx := context.Background()
	r, d := intent("connected-edit")
	o, err := s.Create(ctx, r, d, true)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	edited, err := s.EditTx(ctx, tx, o.ID, o.Wallet, "A revised title", o.Revision)
	if err != nil {
		_ = tx.Rollback(ctx)
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if edited.Revision != o.Revision+1 {
		t.Fatal(edited)
	}
	tx, err = s.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.EditTx(ctx, tx, o.ID, o.Wallet, "Stale write", o.Revision)
	_ = tx.Rollback(ctx)
	if !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
	r.IdempotencyKey = "connected-second"
	if _, err = s.Create(ctx, r, d, true); err != nil {
		t.Fatal(err)
	}
	rows, cursor, err := s.Page(ctx, o.Pool, false, "", "1")
	if err != nil || len(rows) != 1 || cursor == nil {
		t.Fatal(rows, cursor, err)
	}
	next, _, err := s.Page(ctx, o.Pool, false, *cursor, "1")
	if err != nil || len(next) != 1 || next[0].ID == rows[0].ID {
		t.Fatal(next, err)
	}
	if _, _, err = s.Page(ctx, "", false, *cursor, "1"); !errors.Is(err, ErrInvalid) {
		t.Fatal("cursor crossed pool binding", err)
	}
}
func TestEncryptedProviderBindingRetainsNoPlaintext(t *testing.T) {
	s := fixture(t)
	ctx := context.Background()
	if _, err := s.pool.Exec(ctx, s.ConnectedSchemaSQL()); err != nil {
		t.Fatal(err)
	}
	r, d := intent("connected-encrypted")
	o, err := s.Create(ctx, r, d, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.BeginProvision(ctx, o.ID); err != nil {
		t.Fatal(err)
	}
	vault := actualVault(t)
	cipher, err := vault.Seal(ctx, o.ID, "provider_fixture", "secret_fixture")
	if err != nil {
		t.Fatal(err)
	}
	bound, err := s.BindEncryptedProvider(ctx, o.ID, "provider_fixture", cipher, vault.Key)
	if err != nil || bound.State != "READY" {
		t.Fatal(bound, err)
	}
	var persisted string
	if err = s.pool.QueryRow(ctx, fmt.Sprintf("SELECT ciphertext FROM %s.stream_credentials WHERE operation_id=$1", s.schema), o.ID).Scan(&persisted); err != nil || persisted == "secret_fixture" {
		t.Fatal("plaintext persistence or read failure", err)
	}
	plain, err := vault.Open(ctx, o.ID, "provider_fixture", persisted)
	if err != nil || plain != "secret_fixture" {
		t.Fatal("actual KMS decrypt failed", err)
	}
	if _, err = vault.Open(ctx, o.ID, "other_provider", persisted); err == nil {
		t.Fatal("KMS accepted wrong associated provider")
	}
}

func actualVault(t *testing.T) *VaultTransit {
	t.Helper()
	endpoint := os.Getenv("L40_TEST_VAULT_URL")
	token := os.Getenv("L40_TEST_VAULT_TOKEN")
	key := os.Getenv("L40_TEST_VAULT_KEY")
	if endpoint == "" || token == "" || key == "" {
		t.Fatal("real TLS Vault transit fixture is required; no silent skip")
	}
	return &VaultTransit{Endpoint: endpoint, Token: token, Key: key}
}

func TestConnectedSignedLedgerAndLifecycleCommitTogether(t *testing.T) {
	s := fixture(t)
	ctx := context.Background()
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer rollbackBounded(tx)
	if _, err = tx.Exec(ctx, "SET LOCAL search_path="+s.schema+",pg_catalog"); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, sharedauth.LedgerDDL); err != nil {
		t.Fatal(err)
	}
	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	wallet := strings.ToLower(crypto.PubkeyToAddress(key.PublicKey).Hex())
	r, d := intent("signed-atomic")
	r.Wallet = wallet
	d.Wallet = wallet
	body := []byte(`{"poolAddress":"` + r.Pool + `","title":"A real stream request"}`)
	hash, err := sharedauth.BodyDigest(body)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().Unix()
	envelope := sharedauth.OperationEnvelope{Version: 1, ChainID: "125", Environment: "test", Actor: wallet, Action: "stream.create", Method: "POST", Path: "/v2/streams", BodyHash: hash, Nonce: strings.Repeat("a", 64), IssuedAt: now, ExpiresAt: now + 300}
	statement, err := envelope.Statement()
	if err != nil {
		t.Fatal(err)
	}
	signature, err := crypto.Sign(accounts.TextHash([]byte(statement)), key)
	if err != nil {
		t.Fatal(err)
	}
	request := sharedauth.OperationRequest{Envelope: envelope, Signature: hexutil.Encode(signature), Body: body, Actor: wallet, Action: "stream.create", Method: "POST", Path: "/v2/streams", RequestID: "10000000-0000-4000-8000-000000000040", PolicyVersion: "test"}
	ledger := sharedauth.Ledger{Pool: s.pool, Environment: "test", AuditKey: []byte(strings.Repeat("a", 32))}
	verify := func(ctx context.Context, wallet, message, signature string) error {
		_, err := sharedauth.VerifyEOA(wallet, message, signature, nil)
		return err
	}
	mutation := func(ctx context.Context, tx pgx.Tx, _ []byte) ([]byte, error) {
		o, err := s.CreateTx(ctx, tx, r, d, true)
		if err != nil {
			return nil, err
		}
		return []byte(o.ID), nil
	}
	first, duplicate, err := ledger.ExecuteValidatedTx(ctx, tx, request, verify, mutation)
	if err != nil || duplicate {
		t.Fatal(duplicate, err)
	}
	second, duplicate, err := ledger.ExecuteValidatedTx(ctx, tx, request, verify, mutation)
	if err != nil || !duplicate || string(first) != string(second) {
		t.Fatal(duplicate, err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	var count int
	if err = s.pool.QueryRow(ctx, fmt.Sprintf("SELECT count(*)FROM %s.stream_outbox", s.schema)).Scan(&count); err != nil || count != 1 {
		t.Fatal(count, err)
	}
}

func TestRotationIntentWaitsForRevocationAndRejectsStaleOwnerWrite(t *testing.T) {
	s := fixture(t)
	ctx := context.Background()
	if _, err := s.pool.Exec(ctx, s.ConnectedSchemaSQL()+s.RotationSchemaSQL()); err != nil {
		t.Fatal(err)
	}
	r, d := intent("rotation-intent")
	o, err := s.Create(ctx, r, d, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.BeginProvision(ctx, o.ID); err != nil {
		t.Fatal(err)
	}
	vault := actualVault(t)
	cipher, err := vault.Seal(ctx, o.ID, "rotation_provider", "rotation_secret")
	if err != nil {
		t.Fatal(err)
	}
	o, err = s.BindEncryptedProvider(ctx, o.ID, "rotation_provider", cipher, vault.Key)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	rotation, err := s.RequestRotationTx(ctx, tx, o.ID, o.Wallet, o.Revision)
	if err != nil {
		_ = tx.Rollback(ctx)
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if rotation.State != "ENDING" || rotation.Intent != "rotate" || rotation.Revision != o.Revision+1 {
		t.Fatal(rotation)
	}
	job, err := s.GetKeyRotation(ctx, o.ID)
	if err != nil || job.NewStreamID != nil || job.Status != "revoking" {
		t.Fatal(job, err)
	}
	tx, err = s.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.RequestRotationTx(ctx, tx, o.ID, o.Wallet, o.Revision)
	_ = tx.Rollback(ctx)
	if !errors.Is(err, ErrConflict) {
		t.Fatal("stale rotation was accepted", err)
	}
}

func TestExpiredIdempotencyRetirementPreservesOperationAndAudit(t *testing.T) {
	s := fixture(t)
	ctx := context.Background()
	r, d := intent("retained-terminal-key")
	o, err := s.Create(ctx, r, d, true)
	if err != nil {
		t.Fatal(err)
	}
	failed, err := s.FailBeforeProvision(ctx, o.ID, o.Revision, ProvisionPolicyRevoked)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.pool.Exec(ctx, fmt.Sprintf("UPDATE %s.stream_operations SET terminal_at=clock_timestamp()-interval '25 hours' WHERE id=$1", s.schema), failed.ID); err != nil {
		t.Fatal(err)
	}
	count, err := s.ExpireTerminalKeys(ctx)
	if err != nil || count != 1 {
		t.Fatal(count, err)
	}
	var key string
	if err = s.pool.QueryRow(ctx, fmt.Sprintf("SELECT idem_key FROM %s.stream_operations WHERE id=$1", s.schema), o.ID).Scan(&key); err != nil || key != "expired_"+o.ID {
		t.Fatal(key, err)
	}
	replay, err := s.Create(ctx, r, d, true)
	if err != nil || replay.ID == o.ID {
		t.Fatal(replay, err)
	}
	var audits int
	if err = s.pool.QueryRow(ctx, fmt.Sprintf("SELECT count(*) FROM %s.stream_audit WHERE operation_id=$1", s.schema), o.ID).Scan(&audits); err != nil || audits == 0 {
		t.Fatal(audits, err)
	}
	count, err = s.ExpireTerminalKeys(ctx)
	if err != nil || count != 0 {
		t.Fatal(count, err)
	}
}

func TestPrivatePlaybackPolicyInvalidatesEarlierViewerTokens(t *testing.T) {
	s := fixture(t)
	ctx := context.Background()
	if _, err := s.pool.Exec(ctx, s.PlaybackPrivacySchemaSQL()); err != nil {
		t.Fatal(err)
	}
	r, d := intent("private-playback-revision")
	o, err := s.Create(ctx, r, d, true)
	if err != nil {
		t.Fatal(err)
	}
	authority := &ViewerTokenAuthority{Key: []byte(strings.Repeat("k", 32))}
	session, err := randomID()
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	publicToken, _, err := authority.IssuePolicyBound(o.ID, session, 0, now)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	edited, err := s.EditTx(ctx, tx, o.ID, o.Wallet, o.Title, o.Revision)
	if err != nil {
		_ = tx.Rollback(ctx)
		t.Fatal(err)
	}
	if err = s.SetPlaybackPrivacyTx(ctx, tx, edited, true); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	private, revision, err := s.PlaybackPrivacy(ctx, o.ID)
	if err != nil || !private || revision != edited.Revision {
		t.Fatal(private, revision, err)
	}
	if err = authority.VerifyPolicyBound(publicToken, o.ID, session, revision, now); err == nil {
		t.Fatal("public token survived private policy revision")
	}
	privateToken, _, err := authority.IssuePolicyBound(o.ID, session, revision, now)
	if err != nil {
		t.Fatal(err)
	}
	if err = authority.VerifyPolicyBound(privateToken, o.ID, session, revision, now); err != nil {
		t.Fatal(err)
	}
	if err = authority.VerifyPolicyBound(privateToken, o.ID, session, revision+1, now); err == nil {
		t.Fatal("token crossed a policy revision")
	}
	var auditCount int
	if err = s.pool.QueryRow(ctx, fmt.Sprintf("SELECT count(*) FROM %s.stream_audit WHERE operation_id=$1 AND reason='playback_made_private'", s.schema), o.ID).Scan(&auditCount); err != nil || auditCount != 1 {
		t.Fatal(auditCount, err)
	}
}

func TestInterruptedProvisionBindingPreservesStopIntent(t *testing.T) {
	for _, state := range []string{"ENDING", "SUSPENDED"} {
		t.Run(state, func(t *testing.T) {
			s := fixture(t)
			ctx := context.Background()
			if _, err := s.pool.Exec(ctx, s.ConnectedSchemaSQL()); err != nil {
				t.Fatal(err)
			}
			r, d := intent("interrupted-" + strings.ToLower(state))
			o, err := s.Create(ctx, r, d, true)
			if err != nil {
				t.Fatal(err)
			}
			o, err = s.BeginProvision(ctx, o.ID)
			if err != nil {
				t.Fatal(err)
			}
			if state == "ENDING" {
				o, err = s.Transition(ctx, o.ID, o.Wallet, "end", o.Revision, ActionDecision{o.Wallet, o.ID, "end", o.Revision, true, time.Now().Add(time.Minute)})
			} else {
				o, err = s.Suspend(ctx, o.ID, SuspensionDecision{o.ID, o.Revision, "moderator", "abuse", true, time.Now().Add(time.Minute)})
			}
			if err != nil {
				t.Fatal(err)
			}
			vault := actualVault(t)
			cipher, err := vault.Seal(ctx, o.ID, "interrupted_provider", "interrupted_key")
			if err != nil {
				t.Fatal(err)
			}
			adopted, err := s.BindEncryptedProvider(ctx, o.ID, "interrupted_provider", cipher, vault.Key)
			if err != nil || adopted.State != state || adopted.Intent != "end" || adopted.PlaybackAllowed() || adopted.Revision != o.Revision+1 {
				t.Fatal(adopted, err)
			}
			again, err := s.BindEncryptedProvider(ctx, o.ID, "interrupted_provider", cipher, vault.Key)
			if err != nil || again.Revision != adopted.Revision || again.State != state {
				t.Fatal(again, err)
			}
			credentials := &EncryptedCredentials{Store: s, Transit: vault, IngestEndpoint: "rtmps://rtmp.livepeer.studio/live"}
			if _, err = credentials.LoadKey(ctx, adopted); !errors.Is(err, ErrUnavailable) {
				t.Fatal("stopped stream exposed ingest key", err)
			}
		})
	}
}

func TestUnallocatedStopNeverCertifiesAmbiguousCreate(t *testing.T) {
	for _, sent := range []bool{false, true} {
		t.Run(fmt.Sprint(sent), func(t *testing.T) {
			s := fixture(t)
			ctx := context.Background()
			if _, err := s.pool.Exec(ctx, s.ConnectedSchemaSQL()); err != nil {
				t.Fatal(err)
			}
			r, d := intent("unallocated-stop")
			o, err := s.Create(ctx, r, d, true)
			if err != nil {
				t.Fatal(err)
			}
			o, err = s.Transition(ctx, o.ID, o.Wallet, "end", o.Revision, ActionDecision{o.Wallet, o.ID, "end", o.Revision, true, time.Now().Add(time.Minute)})
			if err != nil {
				t.Fatal(err)
			}
			owner, err := randomID()
			if err != nil {
				t.Fatal(err)
			}
			claimed, err := s.claimWorker(ctx, o.ID, owner)
			if err != nil || !claimed {
				t.Fatal(claimed, err)
			}
			if sent {
				if _, err = s.pool.Exec(ctx, fmt.Sprintf("UPDATE %s.stream_worker_attempts SET create_sent=true WHERE operation_id=$1", s.schema), o.ID); err != nil {
					t.Fatal(err)
				}
			}
			err = s.finishUnallocated(ctx, o.ID, owner)
			if sent && !errors.Is(err, ErrUnavailable) || !sent && err != nil {
				t.Fatal(sent, err)
			}
			current, err := s.Get(ctx, o.ID)
			if err != nil {
				t.Fatal(err)
			}
			if sent && (current.State != "ENDING" || current.Revision != o.Revision) || !sent && (current.State != "ENDED" || current.Revision != o.Revision+1) {
				t.Fatal(current)
			}
		})
	}
}

func TestActualProviderCreationSurvivesCredentialPersistenceInterruption(t *testing.T) {
	endpoint, key, webhook := os.Getenv("L40_TEST_LIVEPEER_URL"), os.Getenv("L40_TEST_LIVEPEER_KEY"), os.Getenv("L40_TEST_PLAYBACK_WEBHOOK_ID")
	if endpoint == "" || key == "" || webhook == "" {
		t.Fatal("actual admitted Livepeer provider and registered playback webhook required")
	}
	s := fixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if _, err := s.pool.Exec(ctx, s.ConnectedSchemaSQL()); err != nil {
		t.Fatal(err)
	}
	r, d := intent("provider-db-interruption")
	o, err := s.Create(ctx, r, d, true)
	if err != nil {
		t.Fatal(err)
	}
	o, err = s.BeginProvision(ctx, o.ID)
	if err != nil {
		t.Fatal(err)
	}
	provider := livepeer.New(key, endpoint, nil, livepeer.WithWebhookPlayback(webhook))
	created, err := provider.CreateCorrelated(ctx, o.ID)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanup, c := context.WithTimeout(context.Background(), 15*time.Second)
		defer c()
		if err := provider.DeleteStream(cleanup, created.ID); err != nil {
			t.Error("actual provider cleanup failed", err)
		}
	})
	vault := actualVault(t)
	cipher, err := vault.Seal(ctx, o.ID, created.ID, created.StreamKey)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.pool.Exec(ctx, fmt.Sprintf("ALTER TABLE %s.stream_credentials ADD CONSTRAINT interrupt_write CHECK(operation_id<> '%s')", s.schema, o.ID)); err != nil {
		t.Fatal(err)
	}
	if _, err = s.BindEncryptedProvider(ctx, o.ID, created.ID, cipher, vault.Key); err == nil {
		t.Fatal("database interruption did not abort adoption")
	}
	interrupted, err := s.Get(ctx, o.ID)
	if err != nil || interrupted.State != "PROVISIONING" || interrupted.ProviderID != "" || interrupted.Revision != o.Revision {
		t.Fatal(interrupted, err)
	}
	var count int
	if err = s.pool.QueryRow(ctx, fmt.Sprintf("SELECT count(*) FROM %s.stream_credentials", s.schema)).Scan(&count); err != nil || count != 0 {
		t.Fatal(count, err)
	}
	if _, err = s.pool.Exec(ctx, fmt.Sprintf("ALTER TABLE %s.stream_credentials DROP CONSTRAINT interrupt_write", s.schema)); err != nil {
		t.Fatal(err)
	}
	restarted, err := NewLifecycleStore(s.pool, s.schema, 3, 20)
	if err != nil {
		t.Fatal(err)
	}
	remote, err := provider.FindCorrelated(ctx, o.ID)
	if err != nil || remote.ID != created.ID {
		t.Fatal("restart did not identify unique existing allocation", err)
	}
	if err = provider.EnsurePlaybackPolicy(ctx, remote.ID, o.ID); err != nil {
		t.Fatal(err)
	}
	adopted, err := restarted.BindEncryptedProvider(ctx, o.ID, remote.ID, cipher, vault.Key)
	if err != nil || adopted.ProviderID != created.ID || adopted.State != "READY" {
		t.Fatal(adopted, err)
	}
	remote, err = provider.FindCorrelated(ctx, o.ID)
	if err != nil || remote.ID != adopted.ProviderID {
		t.Fatal("duplicate provider allocation or inventory disagreement", err)
	}
}

func TestInboxAndObservationRollbackTogetherAtOutboxFailure(t *testing.T) {
	s := fixture(t)
	ctx := context.Background()
	r, d := intent("observation-interruption")
	o, err := s.Create(ctx, r, d, true)
	if err != nil {
		t.Fatal(err)
	}
	o, err = s.BindProvider(ctx, o.ID, "observation_provider")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.pool.Exec(ctx, fmt.Sprintf("ALTER TABLE %s.stream_outbox ADD CONSTRAINT interrupt_observation CHECK(kind<>'provider_state')", s.schema)); err != nil {
		t.Fatal(err)
	}
	event := ProviderEvent{EventID: "interrupted_event", ProviderID: o.ProviderID, State: "LIVE", ObservedAt: time.Now().UTC().Truncate(time.Microsecond)}
	if _, err = s.Observe(ctx, event); err == nil {
		t.Fatal("outbox failure did not abort observation")
	}
	current, err := s.Get(ctx, o.ID)
	if err != nil || current.State != o.State || current.Revision != o.Revision {
		t.Fatal(current, err)
	}
	var count int
	if err = s.pool.QueryRow(ctx, fmt.Sprintf("SELECT count(*) FROM %s.stream_inbox WHERE event_id=$1", s.schema), event.EventID).Scan(&count); err != nil || count != 0 {
		t.Fatal(count, err)
	}
	if _, err = s.pool.Exec(ctx, fmt.Sprintf("ALTER TABLE %s.stream_outbox DROP CONSTRAINT interrupt_observation", s.schema)); err != nil {
		t.Fatal(err)
	}
	if outcome, err := s.Observe(ctx, event); err != nil || outcome != "applied" {
		t.Fatal(outcome, err)
	}
	if outcome, err := s.Observe(ctx, event); err != nil || outcome != "duplicate" {
		t.Fatal(outcome, err)
	}
}
