package metadata

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	sharedauth "github.com/Sidiora-Technologies/KindleLaunch/shared/auth"
	"github.com/ethereum/go-ethereum/accounts"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPublicationExactBodyBounds(t *testing.T) {
	b := PublicationBody{DraftID: uuid.NewString(), OperationID: uuid.NewString(), CreationTxHash: "0x" + strings.Repeat("a", 64), Metadata: json.RawMessage(`{"expectedRevision":"0","description":"` + strings.Repeat("界", 4096) + `","tags":[],"socialLinks":[]}`), UploadRefs: []string{}}
	raw, e := json.Marshal(b)
	if e != nil {
		t.Fatal(e)
	}
	parsed, p, e := ParsePublicationBody(raw)
	if e != nil || p.Description == nil || len(*p.Description) != 4096*3 || !bytes.Equal(parsed.Metadata, b.Metadata) {
		t.Fatal("valid bounded exact body rejected", e)
	}
	for _, invalid := range [][]byte{
		bytes.Replace(raw, []byte(strings.Repeat("界", 4096)), []byte(strings.Repeat("界", 4097)), 1),
		bytes.Replace(raw, []byte(`"uploadRefs":[]`), []byte(`"uploadRefs":null`), 1),
		bytes.Replace(raw, []byte(`"operationId":`), []byte(`"operationId":null,"operationId":`), 1),
		bytes.Replace(raw, []byte(`"description":`), []byte(`"Description":`), 1),
		append(bytes.TrimSuffix(raw, []byte("}")), []byte(`,"unknown":true}`)...),
	} {
		if _, _, e = ParsePublicationBody(invalid); e == nil {
			t.Fatal("invalid publication body accepted")
		}
	}
	b.UploadRefs = []string{uuid.NewString()}
	raw, _ = json.Marshal(b)
	if _, _, e = ParsePublicationBody(raw); e == nil {
		t.Fatal("unbound upload reference accepted")
	}
}

func TestPublicationRealCreatorQueueRecovery(t *testing.T) {
	required := func(name string) string {
		t.Helper()
		v := os.Getenv(name)
		if v == "" {
			t.Fatalf("actual owned runtime authority missing: %s", name)
		}
		return v
	}
	origin := strings.TrimSuffix(required("KINDLE_PUBLICATION_TEST_URL"), "/")
	schema := required("KINDLE_PUBLICATION_TEST_SCHEMA")
	if !strings.HasPrefix(schema, "publication_test_") {
		t.Fatal("unique owned publication_test_ schema required")
	}
	token := required("KINDLE_PUBLICATION_TEST_TOKEN")
	creationTx := required("KINDLE_PUBLICATION_TEST_CREATION_TX_HASH")
	bearer := required("KINDLE_PUBLICATION_TEST_ACCESS_TOKEN")
	renewedBearer := required("KINDLE_PUBLICATION_TEST_REAUTHORIZED_ACCESS_TOKEN")
	identitySchema := required("KINDLE_PUBLICATION_TEST_IDENTITY_SCHEMA")
	if identitySchema != "users" {
		t.Fatal("actual owning users identity guard schema required")
	}
	key, e := crypto.HexToECDSA(strings.TrimPrefix(required("KINDLE_PUBLICATION_TEST_CREATOR_PRIVATE_KEY"), "0x"))
	if e != nil {
		t.Fatal("owned creator key invalid")
	}
	creator := strings.ToLower(crypto.PubkeyToAddress(key.PublicKey).Hex())
	image, e := os.ReadFile(required("KINDLE_PUBLICATION_TEST_IMAGE"))
	if e != nil {
		t.Fatal(e)
	}
	mime := http.DetectContentType(image)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	db, e := pgxpool.New(ctx, required("KINDLE_PUBLICATION_TEST_DATABASE_URL"))
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	client := &http.Client{Timeout: 20 * time.Second}
	request := func(method, path string, raw []byte, headers map[string]string) (int, []byte, error) {
		req, e := http.NewRequestWithContext(ctx, method, origin+path, bytes.NewReader(raw))
		if e != nil {
			return 0, nil, e
		}
		req.Header.Set("Authorization", "Bearer "+bearer)
		req.Header.Set("X-Request-ID", uuid.NewString())
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		response, e := client.Do(req)
		if e != nil {
			return 0, nil, e
		}
		defer response.Body.Close()
		body, e := io.ReadAll(io.LimitReader(response.Body, 1<<20))
		return response.StatusCode, body, e
	}
	var authority struct {
		Data struct {
			SchemaVersion   int    `json:"schemaVersion"`
			Environment     string `json:"environment"`
			EnvelopeHeader  string `json:"envelopeHeader"`
			SignatureHeader string `json:"signatureHeader"`
			Action          string `json:"action"`
		}
	}
	status, raw, e := request("GET", "/v3/metadata-publication-authority", nil, nil)
	if e != nil || status != 200 || json.Unmarshal(raw, &authority) != nil || authority.Data.SchemaVersion != 3 || authority.Data.Action != PublicationAction {
		t.Fatal("actual signing authority unavailable", status, e)
	}
	var current struct {
		Data struct {
			Revision string `json:"revision"`
		}
	}
	status, raw, e = request("GET", "/v2/tokens/"+token+"/metadata", nil, nil)
	if e != nil || status != 200 || json.Unmarshal(raw, &current) != nil || !revisionValid(current.Data.Revision) {
		t.Fatal("real canonical token metadata unavailable", status, e)
	}
	assetID := uuid.NewString()
	metadata, e := json.Marshal(map[string]any{"expectedRevision": current.Data.Revision, "description": strings.Repeat("界", 4096), "tags": []string{}, "socialLinks": []V2Social{}, "logoAssetId": assetID})
	if e != nil {
		t.Fatal(e)
	}
	body := PublicationBody{DraftID: uuid.NewString(), OperationID: uuid.NewString(), CreationTxHash: creationTx, Metadata: metadata, UploadRefs: []string{assetID}}
	raw, e = json.Marshal(body)
	if e != nil {
		t.Fatal(e)
	}
	path := "/v3/tokens/" + token + "/metadata-publications"
	nonceBytes := make([]byte, 32)
	if _, e = rand.Read(nonceBytes); e != nil {
		t.Fatal(e)
	}
	issued := time.Now().Unix()
	envelope := sharedauth.OperationEnvelope{Version: 2, ChainID: "125", Environment: authority.Data.Environment, Actor: creator, Action: PublicationAction, Method: "POST", Path: path, BodyHash: publicationHash(raw), Nonce: hex.EncodeToString(nonceBytes), IssuedAt: issued, ExpiresAt: issued + 300}
	statement, e := envelope.Statement()
	if e != nil {
		t.Fatal(e)
	}
	sig, e := crypto.Sign(accounts.TextHash([]byte(statement)), key)
	if e != nil {
		t.Fatal(e)
	}
	encoded, e := json.Marshal(envelope)
	if e != nil {
		t.Fatal(e)
	}
	headers := map[string]string{"Content-Type": "application/json", "Idempotency-Key": body.OperationID, authority.Data.EnvelopeHeader: base64.RawURLEncoding.EncodeToString(encoded), authority.Data.SignatureHeader: "0x" + hex.EncodeToString(sig)}
	var admission struct {
		Data PublicationStatus `json:"data"`
	}
	status, response, e := request("POST", path, raw, headers)
	if e != nil || status != 200 || json.Unmarshal(response, &admission) != nil || admission.Data.Status != "metadata_pending" || admission.Data.Creator != creator || admission.Data.ContentHash != publicationHash(metadata) {
		t.Fatal("actual creator enqueue rejected", status, e)
	}
	statuses := make(chan int, 16)
	var wg sync.WaitGroup
	for range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			code, data, err := request("POST", path, raw, headers)
			if err != nil || !bytes.Equal(data, response) {
				statuses <- 0
				return
			}
			statuses <- code
		}()
	}
	wg.Wait()
	close(statuses)
	for code := range statuses {
		if code != 200 {
			t.Fatal("idempotent enqueue result changed", code)
		}
	}
	readStatus := func() PublicationStatus {
		t.Helper()
		code, data, err := request("GET", path+"/"+body.OperationID, nil, nil)
		var result struct {
			Data PublicationStatus `json:"data"`
		}
		if err != nil || code != 200 || json.Unmarshal(data, &result) != nil {
			t.Fatal("durable status unavailable", code, err)
		}
		return result.Data
	}
	wait := func(predicate func(PublicationStatus) bool) PublicationStatus {
		t.Helper()
		for {
			got := readStatus()
			if predicate(got) {
				return got
			}
			if got.Status == "rejected" || got.Status == "authorization_required" {
				t.Fatal("actual publication rejected", got.LastErrorCode)
			}
			select {
			case <-ctx.Done():
				t.Fatal("real worker did not progress", got.Status, got.LastErrorCode)
			case <-time.After(time.Second):
			}
		}
	}
	pending := wait(func(v PublicationStatus) bool {
		return v.AttemptCount >= 1 && v.Status == "metadata_pending" && v.LastErrorCode == "asset_pending"
	})
	if pending.Revision != nil {
		t.Fatal("missing image claimed published metadata")
	}
	var originalSession string
	e = db.QueryRow(ctx, "SELECT session_id::text FROM "+pgx.Identifier{schema, "metadata_publication_grants_v3"}.Sanitize()+" WHERE operation=$1", body.OperationID).Scan(&originalSession)
	if e != nil {
		t.Fatal(e)
	}
	tag, e := db.Exec(ctx, "UPDATE "+pgx.Identifier{identitySchema, "identity_sessions"}.Sanitize()+" SET revoked_at=clock_timestamp() WHERE session_id=$1 AND revoked_at IS NULL", originalSession)
	if e != nil || tag.RowsAffected() != 1 {
		t.Fatal("owned original session revocation failed", e)
	}
	if _, e = db.Exec(ctx, "UPDATE "+pgx.Identifier{schema, "metadata_publication_jobs_v3"}.Sanitize()+" SET next_at=clock_timestamp() WHERE operation=$1 AND state='metadata_pending'", body.OperationID); e != nil {
		t.Fatal(e)
	}
	var revoked PublicationStatus
	for {
		revoked = readStatus()
		if revoked.Status == "authorization_required" {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("revoked creator intent did not stop")
		case <-time.After(time.Second):
		}
	}
	if revoked.AttemptCount != pending.AttemptCount+1 {
		t.Fatal("revocation altered automatic attempt accounting")
	}
	bearer = renewedBearer
	if _, e = rand.Read(nonceBytes); e != nil {
		t.Fatal(e)
	}
	envelope.Nonce = hex.EncodeToString(nonceBytes)
	envelope.IssuedAt = time.Now().Unix()
	envelope.ExpiresAt = envelope.IssuedAt + 300
	statement, e = envelope.Statement()
	if e != nil {
		t.Fatal(e)
	}
	sig, e = crypto.Sign(accounts.TextHash([]byte(statement)), key)
	if e != nil {
		t.Fatal(e)
	}
	encoded, e = json.Marshal(envelope)
	if e != nil {
		t.Fatal(e)
	}
	headers[authority.Data.EnvelopeHeader] = base64.RawURLEncoding.EncodeToString(encoded)
	headers[authority.Data.SignatureHeader] = "0x" + hex.EncodeToString(sig)
	status, _, e = request("POST", path, raw, headers)
	if e != nil || status != 200 {
		t.Fatal("current original creator reauthorization failed", status, e)
	}
	resumed := readStatus()
	if resumed.AttemptCount < revoked.AttemptCount || !resumed.AcceptedAt.Equal(pending.AcceptedAt) || !resumed.ExpiresAt.Equal(pending.ExpiresAt) {
		t.Fatal("reauthorization reset immutable budget")
	}

	uploadStatus, _, e := request("PUT", "/v2/tokens/"+token+"/assets/"+assetID, image, map[string]string{"Content-Type": mime, "Idempotency-Key": assetID})
	if e != nil || uploadStatus != 200 {
		t.Fatal("actual image scanner/object pipeline unavailable", uploadStatus, e)
	}
	published := wait(func(v PublicationStatus) bool { return v.Status == "published" })
	if published.Revision == nil || published.AttemptCount > 5 || published.ExpiresAt.Sub(published.AcceptedAt) != 30*time.Minute {
		t.Fatal("publication retry budget/result invalid")
	}
	var intents, grants, events int
	e = db.QueryRow(ctx, "SELECT (SELECT count(*) FROM "+pgx.Identifier{schema, "metadata_publication_intents_v3"}.Sanitize()+" WHERE operation=$1),(SELECT count(*) FROM "+pgx.Identifier{schema, "metadata_publication_grants_v3"}.Sanitize()+" WHERE operation=$1),(SELECT count(*) FROM "+pgx.Identifier{schema, "v2_outbox"}.Sanitize()+" WHERE payload->>'publicationOperation'=$1::text)", body.OperationID).Scan(&intents, &grants, &events)
	if e != nil || intents != 1 || grants != 2 || events != 1 {
		t.Fatal("actual durable effect count", intents, grants, events, e)
	}
	tampered := bytes.Replace(raw, []byte(body.DraftID), []byte(uuid.NewString()), 1)
	status, _, e = request("POST", path, tampered, headers)
	if e != nil || status != 403 {
		t.Fatal("body-substituted signature accepted", status, e)
	}
	t.Logf("real creator queue recovery operation=%s revision=%s attempts=%d intents=%d grants=%d outbox=%d", body.OperationID, *published.Revision, published.AttemptCount, intents, grants, events)
}

func TestPublicationMissingRuntimeUnavailable(t *testing.T) {
	if service, e := NewPublicationService(ConnectedMedia{}, sharedauth.OperationAuthorizer{}, sharedauth.PostgresIdentityResolver{}); e == nil || service.ready() {
		t.Fatal("missing actual runtime authority admitted")
	}
}
