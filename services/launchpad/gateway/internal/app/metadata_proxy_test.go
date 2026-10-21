package app

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/tls"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Sidiora-Technologies/KindleLaunch/media/gateway/internal/proxy"
	sharedauth "github.com/Sidiora-Technologies/KindleLaunch/shared/auth"
)

// metadataChannelRequest is what the metadata private listener observed.
type metadataChannelRequest struct {
	method, uri, wallet string
	header              http.Header
	body                []byte
}

// metadataPrivateListener is media/metadata's production (METADATA_INGRESS=mtls)
// ingress contract, served on a TLS 1.3 listener that requires a client
// certificate of the private CA: the peer must be the pinned gateway client
// certificate; a write (or any /assets/ read) must carry the gateway's actor
// assertion in X-Internal-Assertion / X-Internal-Signature for audience
// "metadata" over the exact method, request URI and body (bounded by
// metadata's per-route limits), and the forwarded session must re-authenticate
// at the gateway identity listener as issuer "metadata" to the same wallet and
// session (internal/app metadataPrivateChannel). The publication authority and
// status reads authenticate the forwarded session themselves
// (PublicationService.authority/status).
func metadataPrivateListener(t *testing.T, e *identityE2E, seen func(metadataChannelRequest)) string {
	t.Helper()
	peer := e.peers["metadata"]
	identity, err := e.peerClient("metadata", peer.leaf, peer.key)
	if err != nil {
		t.Fatal(err)
	}
	gatewayKey := e.gatewayKey.Private.Public().(ed25519.PublicKey)
	deny := func(w http.ResponseWriter, status int) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(map[string]string{"code": "media_channel_unavailable"})
	}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.TLS == nil || len(r.TLS.VerifiedChains) == 0 {
			deny(w, 403)
			return
		}
		pin := sha256.Sum256(r.TLS.PeerCertificates[0].Raw)
		if hex.EncodeToString(pin[:]) != e.gatewayClient.SHA256 {
			deny(w, 403)
			return
		}
		limit := int64(32768)
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/metadata-publications") {
			limit = proxy.MetadataPublicationBodyLimit
		}
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, limit))
		if err != nil {
			deny(w, 413)
			return
		}
		observed := metadataChannelRequest{method: r.Method, uri: r.URL.RequestURI(), header: r.Header.Clone(), body: body}
		authenticated := r.Method != http.MethodGet || strings.Contains(r.URL.Path, "/assets/") || strings.HasPrefix(r.URL.Path, "/v3/")
		if r.Method != http.MethodGet || strings.Contains(r.URL.Path, "/assets/") {
			values, signatures := r.Header.Values("X-Internal-Assertion"), r.Header.Values("X-Internal-Signature")
			if len(values) != 1 || len(signatures) != 1 {
				deny(w, 403)
				return
			}
			raw, err := base64.RawURLEncoding.Strict().DecodeString(values[0])
			if err != nil {
				deny(w, 403)
				return
			}
			signature, err := base64.RawURLEncoding.Strict().DecodeString(signatures[0])
			if err != nil {
				deny(w, 403)
				return
			}
			var assertion sharedauth.ServiceAssertion
			decoder := json.NewDecoder(bytes.NewReader(raw))
			decoder.DisallowUnknownFields()
			if decoder.Decode(&assertion) != nil {
				deny(w, 403)
				return
			}
			if canonical, _ := json.Marshal(assertion); !bytes.Equal(canonical, raw) {
				deny(w, 403)
				return
			}
			principal, err := sharedauth.VerifyServiceAssertion(assertion, signature, gatewayKey, r.TLS, e.gatewayClient.SHA256, "gateway", "metadata", sharedauth.RequestDigest(r.Method, r.URL.RequestURI(), body), time.Now())
			if err != nil {
				deny(w, 403)
				return
			}
			current, err := identity.AuthenticateRequest(r.Context(), r)
			if err != nil || current.Wallet != principal.Wallet || current.SessionID != principal.SessionID {
				deny(w, 403)
				return
			}
			observed.wallet = principal.Wallet
		} else if authenticated {
			current, err := identity.AuthenticateRequest(r.Context(), r)
			if err != nil {
				deny(w, 403)
				return
			}
			observed.wallet = current.Wallet
		}
		seen(observed)
		switch {
		case strings.HasPrefix(r.URL.Path, "/logo/"):
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write(append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, 64)...))
		case r.URL.Path == "/v3/metadata-publication-authority":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":{"schemaVersion":3,"environment":"paxeer125-production","envelopeHeader":"X-Kindle-Operation","signatureHeader":"X-Kindle-Operation-Signature","action":"metadata.publish"}}`))
		case strings.HasPrefix(r.URL.Path, "/v3/"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":{"schemaVersion":3,"status":"metadata_pending","wallet":"` + observed.wallet + `"}}`))
		default:
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{}`))
		}
	})
	server := httptest.NewUnstartedServer(handler)
	server.TLS = &tls.Config{MinVersion: tls.VersionTLS13, ClientAuth: tls.RequireAndVerifyClientCert, ClientCAs: e.pki.Roots(), Certificates: []tls.Certificate{e.pki.Leaf("metadata-server", "metadata.railway.internal").Pair}}
	server.StartTLS()
	t.Cleanup(server.Close)
	return server.URL
}

// The token-create flow's metadata calls reach media/metadata only through
// the gateway: the production connected proxy (the metadata upstream binding
// plus MetadataConnectedRoutes, as newGatewayConnectedRuntime builds it)
// authenticates the browser session at the real identity listener, forwards
// the session and the operation headers over mTLS with the gateway client
// certificate, signs the actor assertion metadata verifies, and bounds the
// publication body at metadata's own limit.
func TestMetadataPublicationThroughGatewayProxy(t *testing.T) {
	e := newIdentityE2E(t)
	alice := e.signIn()
	var mu sync.Mutex
	var observed []metadataChannelRequest
	upstream := metadataPrivateListener(t, e, func(r metadataChannelRequest) {
		mu.Lock()
		defer mu.Unlock()
		observed = append(observed, r)
	})
	last := func() metadataChannelRequest {
		mu.Lock()
		defer mu.Unlock()
		if len(observed) == 0 {
			t.Fatal("metadata saw no request")
		}
		return observed[len(observed)-1]
	}
	count := func() int { mu.Lock(); defer mu.Unlock(); return len(observed) }

	// The rendered gateway-runtime metadata binding: the v2 media routes
	// with MetadataConnectedRoutes appended, exactly as the runtime does.
	routes := []proxy.ConnectedRoute{{Method: http.MethodGet, Template: "/v2/tokens/{tokenAddress}/metadata"}, {Method: http.MethodPatch, Template: "/v2/tokens/{tokenAddress}/metadata", Authenticate: true}}
	routes = append(routes, proxy.MetadataConnectedRoutes()...)
	handler, err := proxy.NewConnected(proxy.ConnectedDeps{Name: "metadata", URL: upstream, Routes: routes, Identity: e.authorityForGateway, SigningKey: e.gatewayKey.Private, TLS: e.gatewayClientTLS()})
	if err != nil {
		t.Fatal(err)
	}
	edge := httptest.NewServer(handler)
	t.Cleanup(edge.Close)

	token := "0xce20da50f92d21d8bee497910c782e9aa25b6263"
	operation := uuid.NewString()
	bearer := map[string]string{"Authorization": "Bearer " + alice.token, "Origin": e2eOrigin}

	t.Run("publication authority", func(t *testing.T) {
		resp, raw := e2eSend(t, nil, http.MethodGet, edge.URL+"/v3/metadata-publication-authority", nil, bearer)
		if resp.StatusCode != 200 || !strings.Contains(string(raw), `"envelopeHeader":"X-Kindle-Operation"`) || resp.Header.Get("Cache-Control") != "no-store" {
			t.Fatalf("authority: %d %s", resp.StatusCode, raw)
		}
		if seen := last(); seen.wallet != alice.wallet || seen.header.Get("Authorization") != "Bearer "+alice.token || seen.header.Get("Origin") != e2eOrigin {
			t.Fatalf("authority forwarding: %+v", seen)
		}
		before := count()
		if resp, raw = e2eSend(t, nil, http.MethodGet, edge.URL+"/v3/metadata-publication-authority", nil, nil); resp.StatusCode != 401 && resp.StatusCode != 403 {
			t.Fatalf("anonymous authority: %d %s", resp.StatusCode, raw)
		}
		if resp, raw = e2eSend(t, nil, http.MethodGet, edge.URL+"/v3/metadata-publication-authority", nil, map[string]string{"Authorization": "Bearer " + alice.token + "x"}); resp.StatusCode/100 != 4 {
			t.Fatalf("forged authority: %d %s", resp.StatusCode, raw)
		}
		if count() != before {
			t.Fatal("unauthenticated authority request crossed the private channel")
		}
	})

	t.Run("publication enqueue", func(t *testing.T) {
		body := map[string]any{"draftId": uuid.NewString(), "operationId": operation, "creationTxHash": "0x" + strings.Repeat("ab", 32), "metadata": map[string]any{"expectedRevision": "1", "displayName": "Kindle"}, "uploadRefs": []string{}}
		headers := map[string]string{"Authorization": "Bearer " + alice.token, "Origin": e2eOrigin, "Idempotency-Key": operation, "X-Request-ID": uuid.NewString(), "X-Kindle-Operation": "envelope-value", "X-Kindle-Operation-Signature": "signature-value", "X-Action-Wallet": alice.wallet, "Cookie": "theme=dark", "X-Internal-Assertion": "forged", "X-Forwarded-For": "203.0.113.9"}
		resp, raw := e2eSend(t, nil, http.MethodPost, edge.URL+"/v3/tokens/"+token+"/metadata-publications", body, headers)
		if resp.StatusCode != 200 || !strings.Contains(string(raw), `"wallet":"`+alice.wallet+`"`) {
			t.Fatalf("enqueue: %d %s", resp.StatusCode, raw)
		}
		seen := last()
		encoded, _ := json.Marshal(body)
		if seen.method != http.MethodPost || seen.uri != "/v3/tokens/"+token+"/metadata-publications" || !bytes.Equal(seen.body, encoded) {
			t.Fatalf("enqueue request: %s %s %s", seen.method, seen.uri, seen.body)
		}
		for name, want := range map[string]string{"Authorization": "Bearer " + alice.token, "Idempotency-Key": operation, "X-Request-ID": headers["X-Request-ID"], "X-Kindle-Operation": "envelope-value", "X-Kindle-Operation-Signature": "signature-value", "Content-Type": "application/json", "Origin": e2eOrigin, "X-Action-Wallet": alice.wallet} {
			if got := seen.header.Get(name); got != want {
				t.Fatalf("forwarded %s = %q, want %q", name, got, want)
			}
		}
		if seen.header.Get("Cookie") != "" || seen.header.Get("X-Forwarded-For") != "" || len(seen.header.Values("X-Internal-Assertion")) != 1 || seen.header.Get("X-Internal-Assertion") == "forged" {
			t.Fatalf("client headers crossed the private channel: %v", seen.header)
		}
		// Another wallet named by the client is refused at the edge.
		headers["X-Action-Wallet"] = "0x" + strings.Repeat("1", 40)
		if resp, raw = e2eSend(t, nil, http.MethodPost, edge.URL+"/v3/tokens/"+token+"/metadata-publications", body, headers); resp.StatusCode != 403 {
			t.Fatalf("actor mismatch: %d %s", resp.StatusCode, raw)
		}
	})

	t.Run("publication body limit", func(t *testing.T) {
		before := count()
		headers := map[string]string{"Authorization": "Bearer " + alice.token, "Idempotency-Key": operation, "X-Request-ID": uuid.NewString()}
		oversized := map[string]string{"pad": strings.Repeat("x", proxy.MetadataPublicationBodyLimit)}
		if resp, raw := e2eSend(t, nil, http.MethodPost, edge.URL+"/v3/tokens/"+token+"/metadata-publications", oversized, headers); resp.StatusCode != 413 || !strings.Contains(string(raw), "request_too_large") {
			t.Fatalf("oversized publication: %d %s", resp.StatusCode, raw)
		}
		if count() != before {
			t.Fatal("oversized publication crossed the private channel")
		}
		// Just under the limit is forwarded whole.
		fits := map[string]string{"pad": strings.Repeat("x", proxy.MetadataPublicationBodyLimit-16)}
		if resp, raw := e2eSend(t, nil, http.MethodPost, edge.URL+"/v3/tokens/"+token+"/metadata-publications", fits, headers); resp.StatusCode != 200 || len(last().body) != proxy.MetadataPublicationBodyLimit-6 {
			t.Fatalf("publication at the limit: %d %s (%d bytes upstream)", resp.StatusCode, raw, len(last().body))
		}
	})

	t.Run("publication status", func(t *testing.T) {
		resp, raw := e2eSend(t, nil, http.MethodGet, edge.URL+"/v3/tokens/"+token+"/metadata-publications/"+operation, nil, bearer)
		if resp.StatusCode != 200 || last().uri != "/v3/tokens/"+token+"/metadata-publications/"+operation || last().wallet != alice.wallet {
			t.Fatalf("status: %d %s", resp.StatusCode, raw)
		}
		for _, method := range []string{http.MethodDelete, http.MethodPut} {
			if resp, _ := e2eSend(t, nil, method, edge.URL+"/v3/tokens/"+token+"/metadata-publications/"+operation, nil, bearer); resp.StatusCode != 404 {
				t.Fatalf("%s on status admitted: %d", method, resp.StatusCode)
			}
		}
	})

	t.Run("legacy public reads", func(t *testing.T) {
		resp, raw := e2eSend(t, nil, http.MethodGet, edge.URL+"/metadata/batch?addresses="+token, nil, nil)
		if resp.StatusCode != 200 || string(raw) != `{}` || last().uri != "/metadata/batch?addresses="+token || last().header.Get("Authorization") != "" {
			t.Fatalf("batch: %d %s %+v", resp.StatusCode, raw, last())
		}
		if resp, raw = e2eSend(t, nil, http.MethodGet, edge.URL+"/metadata/"+token, nil, nil); resp.StatusCode != 200 || last().uri != "/metadata/"+token {
			t.Fatalf("single: %d %s", resp.StatusCode, raw)
		}
		// The bearer of a public read is not forwarded.
		if resp, _ = e2eSend(t, nil, http.MethodGet, edge.URL+"/metadata/"+token, nil, bearer); resp.StatusCode != 200 || last().header.Get("Authorization") != "" || last().header.Get("X-Internal-Assertion") != "" {
			t.Fatalf("public read carried identity: %v", last().header)
		}
		resp, raw = e2eSend(t, nil, http.MethodGet, edge.URL+"/logo/"+token+".png", nil, nil)
		if resp.StatusCode != 200 || resp.Header.Get("Content-Type") != "image/png" || !bytes.HasPrefix(raw, []byte("\x89PNG\r\n\x1a\n")) || resp.Header.Get("X-Content-Type-Options") != "nosniff" {
			t.Fatalf("logo: %d %v", resp.StatusCode, resp.Header)
		}
	})

	t.Run("mtls is required upstream", func(t *testing.T) {
		noCert := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS13, RootCAs: e.pki.Roots()}}}
		if _, err := noCert.Get(upstream + "/v3/metadata-publication-authority"); err == nil {
			t.Fatal("metadata listener served a client without a certificate")
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		other := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS13, RootCAs: e.pki.Roots(), Certificates: []tls.Certificate{e.peers["social"].leaf.Pair}}}}
		request, _ := http.NewRequestWithContext(ctx, http.MethodGet, upstream+"/metadata/"+token, nil)
		resp, err := other.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != 403 {
			t.Fatalf("non-gateway peer served: %d", resp.StatusCode)
		}
	})
}
