package streams

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	v2 "github.com/Sidiora-Technologies/KindleLaunch/shared/interfaces/v2"
)

// VerifyProviderWebhook verifies Livepeer's HMAC of the exact raw body and
// requires its signed JSON timestamp to equal the header timestamp. Timestamp
// unit is pinned by deployment (seconds or milliseconds), never guessed.
// Provider event decoding/correlation is a separate, version-pinned adapter.
// Source: https://docs.livepeer.org/developers/guides/setup-and-listen-to-webhooks
func VerifyProviderWebhook(w http.ResponseWriter, r *http.Request, secret string, now time.Time, milliseconds bool) ([]byte, bool) {
	if len(secret) < 32 {
		writeContractError(w, http.StatusServiceUnavailable, "DEPENDENCY_UNAVAILABLE", "Webhook signing unavailable", true, nil, &v2.ErrorDetails{Dependency: v2.Optional[string]{Present: true, Value: "webhook_signing"}})
		return nil, false
	}
	if len(r.Header.Get("Livepeer-Signature")) > 1024 {
		writeSignatureError(w, "Invalid provider signature")
		return nil, false
	}
	raw, e := io.ReadAll(io.LimitReader(r.Body, (1<<20)+1))
	if e != nil {
		writeRequestError(w, http.StatusBadRequest, ruleBodyUnreadable, "Invalid webhook body")
		return nil, false
	}
	if len(raw) > 1<<20 {
		writeRequestError(w, http.StatusRequestEntityTooLarge, ruleBodyTooLarge, "Webhook too large")
		return nil, false
	}
	var timestamp string
	var signatures []string
	for _, part := range strings.Split(r.Header.Get("Livepeer-Signature"), ",") {
		kv := strings.SplitN(strings.TrimSpace(part), "=", 2)
		if len(kv) != 2 {
			writeSignatureError(w, "Invalid provider signature")
			return nil, false
		}
		switch kv[0] {
		case "t":
			if timestamp != "" {
				writeSignatureError(w, "Invalid provider signature")
				return nil, false
			}
			timestamp = kv[1]
		case "v1":
			signatures = append(signatures, kv[1])
		}
	}
	n, e := strconv.ParseInt(timestamp, 10, 64)
	if e != nil || n < 1 {
		writeSignatureError(w, "Invalid provider signature")
		return nil, false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(raw)
	valid := false
	for _, sig := range signatures {
		b, e := hex.DecodeString(sig)
		if e == nil && hmac.Equal(mac.Sum(nil), b) {
			valid = true
		}
	}
	if !valid {
		writeSignatureError(w, "Invalid provider signature")
		return nil, false
	}
	// Reject duplicate top-level keys before timestamp interpretation; encoding/json
	// otherwise accepts multiple conflicting signed timestamps.
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	token, e := dec.Token()
	if e != nil || token != json.Delim('{') {
		writeRequestError(w, http.StatusBadRequest, ruleJSON, "Invalid webhook JSON")
		return nil, false
	}
	seen := map[string]bool{}
	var signedTimestamp json.RawMessage
	for dec.More() {
		t, e := dec.Token()
		if e != nil {
			writeRequestError(w, http.StatusBadRequest, ruleJSON, "Invalid webhook JSON")
			return nil, false
		}
		key, ok := t.(string)
		if !ok || seen[key] {
			writeRequestError(w, http.StatusBadRequest, ruleJSON, "Invalid webhook JSON")
			return nil, false
		}
		seen[key] = true
		var v json.RawMessage
		if dec.Decode(&v) != nil {
			writeRequestError(w, http.StatusBadRequest, ruleJSON, "Invalid webhook JSON")
			return nil, false
		}
		if key == "timestamp" {
			signedTimestamp = v
		}
	}
	if _, e = dec.Token(); e != nil {
		writeRequestError(w, http.StatusBadRequest, ruleJSON, "Invalid webhook JSON")
		return nil, false
	}
	var extra any
	if dec.Decode(&extra) != io.EOF {
		writeRequestError(w, http.StatusBadRequest, ruleJSON, "Invalid webhook JSON")
		return nil, false
	}
	var value string
	if len(signedTimestamp) > 0 && signedTimestamp[0] == '"' {
		if json.Unmarshal(signedTimestamp, &value) != nil {
			value = ""
		}
	} else {
		value = string(signedTimestamp)
	}
	if value != timestamp {
		writeSignatureError(w, "Unsigned provider timestamp")
		return nil, false
	}
	var observed time.Time
	if milliseconds {
		observed = time.UnixMilli(n)
	} else {
		observed = time.Unix(n, 0)
	}
	if observed.Before(now.Add(-300*time.Second)) || observed.After(now.Add(300*time.Second)) {
		writeSignatureError(w, "Expired provider signature")
		return nil, false
	}
	return raw, true
}
