package v1

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

const fixtureSession = `{"sessionId":"00000000-0000-4000-8000-000000000001","wallet":"0x1111111111111111111111111111111111111111","accessExpiresAt":"2026-10-09T01:00:00.000Z","refreshExpiresAt":"2026-11-08T00:00:00.000Z","scopes":["profile:read"],"deviceLabel":"fixture","revokedAt":null}`
const fixtureLogin = `{"requestId":"00000000-0000-4000-8000-000000000002","data":{"session":` + fixtureSession + `,"accessToken":"fixture-not-a-credential"},"watermark":null,"revision":"1","quality":"complete","nextCursor":null,"generatedAt":"2026-10-09T00:00:00.000Z"}`

func TestTypedDecodeNullableAndAdditiveCompatibility(t *testing.T) {
	raw := strings.TrimSuffix(fixtureLogin, "}") + `,"futureAdditiveField":{"minor":2}}`
	value, err := DecodeLoginResponse([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	if value.Data.Session.Wallet != "0x1111111111111111111111111111111111111111" || value.Watermark != nil || value.NextCursor != nil || value.Data.Session.RevokedAt != nil {
		t.Fatal("typed/null data changed")
	}
	if err := ValidateJSONVersion(2, "LoginResponse", []byte(raw)); err == nil {
		t.Fatal("unknown major accepted")
	}
	missing := strings.Replace(fixtureLogin, `"watermark":null,`, "", 1)
	if _, err := DecodeLoginResponse([]byte(missing)); err == nil {
		t.Fatal("missing required nullable field accepted")
	}
	// Wire compatibility is an isolated generated contract proof, not an existing app journey.
}
func TestExactIntegerStringBoundaries(t *testing.T) {
	maximum := "115792089237316195423570985008687907853269984665640564039457584007913129639935"
	value, err := DecodeUInt256([]byte(`"` + maximum + `"`))
	if err != nil || string(value) != maximum {
		t.Fatalf("precision lost: %v", err)
	}
	for _, bad := range []string{`"115792089237316195423570985008687907853269984665640564039457584007913129639936"`, `123`, `"01"`, `"-1"`, `"1e5"`} {
		if err := ValidateJSON("UInt256", []byte(bad)); err == nil {
			t.Fatalf("accepted %s", bad)
		}
	}
	if err := ValidateJSON("AggregateUInt", []byte(`"`+strings.Repeat("9", 156)+`"`)); err != nil {
		t.Fatal(err)
	}
	if err := ValidateJSON("AggregateUInt", []byte(`"`+strings.Repeat("9", 157)+`"`)); err == nil {
		t.Fatal("aggregate digit overflow accepted")
	}
	if err := ValidateJSON("PriceComponent", []byte(`"`+strings.Repeat("9", 512)+`"`)); err != nil {
		t.Fatal(err)
	}
	if err := ValidateJSON("PriceComponent", []byte(`"`+strings.Repeat("9", 513)+`"`)); err == nil {
		t.Fatal("price digit overflow accepted")
	}
	if err := ValidateJSON("UInt63", []byte(`"9223372036854775808"`)); err == nil {
		t.Fatal("sequence exhaustion accepted")
	}
}
func TestClosedMutationAndProofVariants(t *testing.T) {
	eoa := `{"challengeId":"00000000-0000-4000-8000-000000000003","proof":{"kind":"eoa_eip191","wallet":"0x1111111111111111111111111111111111111111","signature":"0x` + strings.Repeat("22", 65) + `"},"deviceLabel":"fixture"}`
	if _, err := DecodeLoginBody([]byte(eoa)); err != nil {
		t.Fatal(err)
	}
	if err := ValidateJSON("LoginBody", []byte(strings.TrimSuffix(eoa, "}")+`,"admin":true}`)); err == nil {
		t.Fatal("extra mutation property accepted")
	}
	contract := strings.Replace(strings.Replace(eoa, "eoa_eip191", "erc1271", 1), strings.Repeat("22", 65), strings.Repeat("33", 96), 1)
	if _, err := DecodeLoginBody([]byte(contract)); err != nil {
		t.Fatal(err)
	}
	short := strings.Replace(eoa, strings.Repeat("22", 65), "22", 1)
	if err := ValidateJSON("LoginBody", []byte(short)); err == nil {
		t.Fatal("short EOA signature accepted")
	}
	// These signatures are schema fixtures only, not valid cryptographic proofs.
}
func TestSchemaReferencesAndPatternsCompile(t *testing.T) {
	for name, raw := range definitions {
		var schema schemaRule
		if err := json.Unmarshal(raw, &schema); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if schema.Ref != "" {
			if _, ok := definitions[strings.TrimPrefix(schema.Ref, "#/$defs/")]; !ok {
				t.Fatalf("%s unresolved", name)
			}
		}
	}
}

func TestOptionalPresenceRoundtrip(t *testing.T) {
	for _, raw := range []string{`{"expectedRevision":"1","description":""}`, `{"expectedRevision":"1","tags":[]}`, `{"expectedRevision":"1"}`} {
		value, err := DecodeMetadataPatchBody([]byte(raw))
		if err != nil {
			t.Fatal(err)
		}
		encoded, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		if canonical(encoded) != canonical([]byte(raw)) {
			t.Fatalf("presence changed: %s -> %s", raw, encoded)
		}
	}
	for _, raw := range []string{`{"unreadOnly":false}`, `{}`} {
		value, err := DecodeInputTopicFilters([]byte(raw))
		if err != nil {
			t.Fatal(err)
		}
		encoded, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		if canonical(encoded) != canonical([]byte(raw)) {
			t.Fatalf("boolean presence changed: %s -> %s", raw, encoded)
		}
	}
	type optionalNullable struct {
		Note Optional[*string] `json:"note,omitzero"`
	}
	for _, raw := range []string{`{}`, `{"note":null}`, `{"note":""}`} {
		var value optionalNullable
		if err := json.Unmarshal([]byte(raw), &value); err != nil {
			t.Fatal(err)
		}
		encoded, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		if canonical(encoded) != canonical([]byte(raw)) {
			t.Fatalf("null/absence changed: %s -> %s", raw, encoded)
		}
	}
}
func TestSchemaIntegerLexicalEquivalence(t *testing.T) {
	for _, literal := range []string{"1", "1.0", "1e0"} {
		value, err := DecodeCalloutListRequest([]byte(`{"poolAddress":"0x1111111111111111111111111111111111111111","limit":` + literal + `}`))
		if err != nil || (!value.Limit.Present || value.Limit.Value != 1) {
			t.Fatalf("%s: %+v %v", literal, value, err)
		}
	}
	for _, bad := range []string{"1.1", "1e1000000000", "9007199254740992"} {
		var value SafeInteger
		if err := json.Unmarshal([]byte(bad), &value); err == nil {
			t.Fatalf("unsafe integer accepted: %s", bad)
		}
	}
}
func TestApplicationTimestampRejectsLeapSecondsAndInvalidDates(t *testing.T) {
	for _, date := range []string{"2026-12-31T23:59:60.000Z", "2026-02-30T00:00:00.000Z"} {
		raw := strings.Replace(fixtureSession, "2026-10-09T01:00:00.000Z", date, 1)
		if err := ValidateJSON("Session", []byte(raw)); err == nil {
			t.Fatalf("invalid date accepted: %s", date)
		}
	}
}
func TestHostnameFormatBoundaries(t *testing.T) {
	for _, name := range []string{"example.com", "example.com.", strings.Repeat("a", 63) + ".com"} {
		raw, _ := json.Marshal(name)
		if err := validateNode(json.RawMessage(`{"type":"string","format":"hostname"}`), raw, "$"); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
	for _, name := range []string{"a..b", "a.-b", strings.Repeat("a", 64) + ".com", "-a.com", "a-.com"} {
		raw, _ := json.Marshal(name)
		if err := validateNode(json.RawMessage(`{"type":"string","format":"hostname"}`), raw, "$"); err == nil {
			t.Fatalf("invalid hostname accepted: %s", name)
		}
	}
}

func TestAccountingWireCorpus(t *testing.T) {
	raw, err := os.ReadFile("../../../spec/production-system/integration/contracts/v1/accounting-cases.json")
	if err != nil {
		t.Fatal(err)
	}
	var corpus struct {
		Cases []struct {
			Id       string          `json:"id"`
			Schema   string          `json:"schema"`
			Instance json.RawMessage `json:"instance"`
			Valid    bool            `json:"expectedSchemaValidity"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(raw, &corpus); err != nil {
		t.Fatal(err)
	}
	if len(corpus.Cases) < 59 {
		t.Fatal("accounting corpus incomplete")
	}
	for _, c := range corpus.Cases {
		t.Run(c.Id, func(t *testing.T) {
			err := ValidateJSON(c.Schema, c.Instance)
			if (err == nil) != c.Valid {
				t.Fatalf("wire validity got %v want %v: %v", err == nil, c.Valid, err)
			}
		})
	}
	// Arithmetic semantic validity is intentionally tested by the domain verifier, not this wire validator.
}
func TestDiscriminatedUnionRoundtrip(t *testing.T) {
	for _, kind := range []string{"eoa_eip191", "erc1271"} {
		length := 65
		if kind == "erc1271" {
			length = 96
		}
		raw := `{"kind":"` + kind + `","wallet":"0x1111111111111111111111111111111111111111","signature":"0x` + strings.Repeat("22", length) + `"}`
		value, err := DecodeInputWalletProof([]byte(raw))
		if err != nil {
			t.Fatal(err)
		}
		encoded, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		if canonical(encoded) != canonical([]byte(raw)) {
			t.Fatalf("union changed: %s -> %s", raw, encoded)
		}
	}
	if _, err := json.Marshal(InputWalletProof{}); err == nil {
		t.Fatal("unselected union serialized")
	}
	if _, err := json.Marshal(SafeInteger(9007199254740992)); err == nil {
		t.Fatal("unsafe integer serialized")
	}
}
func TestGregorianApplicationTimestampParity(t *testing.T) {
	for _, date := range []string{"0001-01-01T00:00:00.000Z", "9999-12-31T23:59:59.999Z", "2000-02-29T00:00:00.000Z", "2024-02-29T00:00:00.000Z"} {
		raw := strings.Replace(fixtureSession, "2026-10-09T01:00:00.000Z", date, 1)
		if err := ValidateJSON("Session", []byte(raw)); err != nil {
			t.Fatalf("valid Gregorian date %s: %v", date, err)
		}
	}
	for _, date := range []string{"0000-01-01T00:00:00.000Z", "1900-02-29T00:00:00.000Z", "2100-02-29T00:00:00.000Z", "2026-04-31T00:00:00.000Z", "2026-13-01T00:00:00.000Z", "2026-10-09T24:00:00.000Z", "2026-10-09T00:00:60.000Z", "2026-10-09T00:00:00Z"} {
		raw := strings.Replace(fixtureSession, "2026-10-09T01:00:00.000Z", date, 1)
		if err := ValidateJSON("Session", []byte(raw)); err == nil {
			t.Fatalf("invalid Gregorian date %s accepted", date)
		}
	}
}
