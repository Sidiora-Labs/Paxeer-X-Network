package broker

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"math"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"
)

func testFixtures(t *testing.T) map[string]json.RawMessage {
	t.Helper()
	raw, err := os.ReadFile("frozen_stream_fixtures_test.json")
	if err != nil {
		t.Fatal(err)
	}
	var f map[string]json.RawMessage
	if json.Unmarshal(raw, &f) != nil {
		t.Fatal("fixtures")
	}
	return f
}
func validTestFrame(t *testing.T) Frame {
	t.Helper()
	var f Frame
	if json.Unmarshal(testFixtures(t)["CandleEvent"], &f) != nil {
		t.Fatal("fixture")
	}
	return f
}
func TestFrozenFamiliesAndSchemaNegatives(t *testing.T) {
	fixtures := testFixtures(t)
	for family, raw := range fixtures {
		t.Run(family, func(t *testing.T) {
			if _, err := ParseFrame("1", raw); err != nil {
				t.Fatal(err)
			}
		})
	}
	mutations := map[string]func(map[string]any){
		"arbitrary topic":     func(x map[string]any) { x["topic"] = "anything" },
		"bad address":         func(x map[string]any) { x["topic"] = "market.candles.0x123.1m" },
		"bad interval":        func(x map[string]any) { x["topic"] = strings.Replace(x["topic"].(string), ".1m", ".2m", 1) },
		"empty payload":       func(x map[string]any) { x["payload"] = map[string]any{} },
		"scalar payload":      func(x map[string]any) { x["payload"] = 3 },
		"array payload":       func(x map[string]any) { x["payload"] = []any{} },
		"numeric raw":         func(x map[string]any) { x["payload"].(map[string]any)["volumeUsdlRaw"] = 12 },
		"malformed watermark": func(x map[string]any) { x["watermark"] = map[string]any{} },
		"wrong chain":         func(x map[string]any) { x["watermark"].(map[string]any)["chainId"] = "1" },
		"wrong position chain": func(x map[string]any) {
			x["payload"].(map[string]any)["firstPosition"].(map[string]any)["chainId"] = "1"
		},
		"wrong family": func(x map[string]any) {
			x["topic"] = strings.Replace(x["topic"].(string), "candles", "trades", 1)
			x["topic"] = strings.TrimSuffix(x["topic"].(string), ".1m")
		},
		"wrong pool":             func(x map[string]any) { x["payload"].(map[string]any)["poolAddress"] = "0x" + strings.Repeat("2", 40) },
		"wrong interval binding": func(x map[string]any) { x["payload"].(map[string]any)["interval"] = "5m" },
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			var x map[string]any
			json.Unmarshal(fixtures["CandleEvent"], &x)
			mutate(x)
			raw, _ := json.Marshal(x)
			if _, err := ParseFrame("1", raw); err == nil {
				t.Fatal("invalid accepted")
			}
			d := &boundaryDelivery{raw: raw}
			c := &boundaryConsumer{d: d}
			called := false
			err := ConsumeDurable(context.Background(), c, "1", func(context.Context, Frame, string, uint64) error { called = true; return nil })
			if err == nil || called || d.acked || !d.retried {
				t.Fatal("invalid reached processing/ack", err)
			}
		})
	}
	var x map[string]any
	json.Unmarshal(fixtures["CandleEvent"], &x)
	x["watermark"] = nil
	raw, _ := json.Marshal(x)
	if _, err := ParseFrame("1", raw); err != nil {
		t.Fatal("nullable watermark", err)
	}
}
func TestFrozenTopicAndTimestampPatterns(t *testing.T) {
	s := streamSchemas["CandleEvent"]["properties"].(map[string]any)
	for key, value := range map[string]string{"topic": "market.candles.0x" + strings.Repeat("1", 40) + ".1m", "occurredAt": "2026-10-10T00:00:00.000Z"} {
		p := s[key].(map[string]any)["pattern"].(string)
		fixed := regexp.MustCompile(p)
		if !fixed.MatchString(value) || fixed.MatchString(strings.ReplaceAll(value, ".", "X")) {
			t.Fatal("correction widened")
		}
	}
}
func TestCursorScopeAndIntegerBoundaries(t *testing.T) {
	now := time.Unix(1800000000, 0)
	key := []byte(strings.Repeat("k", 32))
	good := Cursor{CursorScope{"125", "alice", "3", "epoch", "1", "filter"}, map[string]string{"p": "1"}, now.Unix(), now.Add(time.Hour).Unix()}
	cases := []Cursor{good, good, good, good, good}
	cases[0].Scope.Subject = ""
	cases[1].Scope.PermissionEpoch = ""
	cases[2].IssuedAt = math.MinInt64
	cases[2].ExpiresAt = math.MaxInt64
	cases[3].IssuedAt = -1
	cases[4].ExpiresAt = math.MaxInt64
	for i, c := range cases {
		if _, err := SignCursor(c, key, now); err == nil {
			t.Fatal(i)
		}
		raw, _ := json.Marshal(c)
		body := base64.RawURLEncoding.EncodeToString(raw)
		mac := hmac.New(sha256.New, key)
		mac.Write([]byte(body))
		token := body + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
		if _, err := VerifyCursor(token, key, c.Scope, now); err == nil {
			t.Fatal("signed malformed cursor verified", i)
		}
	}
}
