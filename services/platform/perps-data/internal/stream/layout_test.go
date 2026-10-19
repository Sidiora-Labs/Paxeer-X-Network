package stream

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/nats-io/nats.go/jetstream"
)

func TestStreamLayout(t *testing.T) {
	t.Run("config", func(t *testing.T) {
		got := Config(125)
		want := jetstream.StreamConfig{
			Name:       "PERPS_V1",
			Subjects:   []string{"perps.125.>"},
			Storage:    jetstream.FileStorage,
			Retention:  jetstream.LimitsPolicy,
			Replicas:   3,
			MaxAge:     72 * time.Hour,
			Discard:    jetstream.DiscardNew,
			DenyDelete: true,
			DenyPurge:  true,
			Duplicates: 2 * time.Minute,
			MaxMsgSize: 1048576,
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("Config(125) = %+v, want %+v", got, want)
		}
		if s := Config(7).Subjects; !reflect.DeepEqual(s, []string{"perps.7.>"}) {
			t.Fatalf("Config(7).Subjects = %v", s)
		}
		if !ValidFilter(got.Subjects[0]) {
			t.Fatalf("stream subject %q is not a valid filter", got.Subjects[0])
		}
	})

	t.Run("market_subjects", func(t *testing.T) {
		want := []string{"perps.125.1.fill", "perps.125.1.order", "perps.125.1.funding", "perps.125.1.index", "perps.125.1.liq", "perps.125.1.mark", "perps.125.1.oi", "perps.125.1.market"}
		for i, kind := range []string{"fill", "order", "funding", "index", "liq", "mark", "oi", "market"} {
			got, err := MarketSubject(125, 1, kind)
			if err != nil || got != want[i] {
				t.Fatalf("MarketSubject(125, 1, %s) = %q, %v; want %q", kind, got, err, want[i])
			}
			if !subjectName.MatchString(got) {
				t.Fatalf("%q does not match the subject pattern", got)
			}
		}
		if got, err := MarketSubject(125, 17, "mark"); err != nil || got != "perps.125.17.mark" {
			t.Fatalf("MarketSubject(125, 17, mark) = %q, %v", got, err)
		}
	})

	t.Run("user_subjects", func(t *testing.T) {
		account := common.HexToAddress("0x00000000000000000000000000000000000000A1")
		for _, kind := range []string{"position", "order", "fill", "margin", "funding", "ledger"} {
			want := "perps.125.user.0x00000000000000000000000000000000000000a1." + kind
			got, err := UserSubject(125, account, kind)
			if err != nil || got != want {
				t.Fatalf("UserSubject(125, account, %s) = %q, %v; want %q", kind, got, err, want)
			}
		}
		mixed := common.HexToAddress("0xAbCdEf0123456789aBcDeF0123456789AbCdEf01")
		if got, err := UserSubject(125, mixed, "margin"); err != nil || got != "perps.125.user.0xabcdef0123456789abcdef0123456789abcdef01.margin" {
			t.Fatalf("UserSubject(125, mixed, margin) = %q, %v", got, err)
		}
	})

	t.Run("chain_subjects", func(t *testing.T) {
		for _, kind := range []string{"block", "tx", "rewind"} {
			got, err := ChainSubject(125, kind)
			if err != nil || got != "perps.125.chain."+kind {
				t.Fatalf("ChainSubject(125, %s) = %q, %v", kind, got, err)
			}
		}
		if got, err := Subject(125, FamilyChain, "x", "block"); err == nil || got != "" {
			t.Fatalf("chain subject with an entity = %q, %v; want refusal", got, err)
		}
	})

	t.Run("spot_subjects", func(t *testing.T) {
		token := common.HexToAddress("0x00000000000000000000000000000000000000b2")
		for _, kind := range []string{"swap", "order", "vault"} {
			want := "perps.125.spot.0x00000000000000000000000000000000000000b2." + kind
			got, err := SpotSubject(125, token, kind)
			if err != nil || got != want {
				t.Fatalf("SpotSubject(125, token, %s) = %q, %v; want %q", kind, got, err, want)
			}
		}
	})

	t.Run("unknown_kind", func(t *testing.T) {
		account := common.HexToAddress("0x00000000000000000000000000000000000000a1")
		cases := []struct {
			name, want string
			build      func() (string, error)
		}{
			{"market oracle", `stream: unknown market kind "oracle"`, func() (string, error) { return MarketSubject(125, 1, "oracle") }},
			{"market position", `stream: unknown market kind "position"`, func() (string, error) { return MarketSubject(125, 1, "position") }},
			{"user mark", `stream: unknown user kind "mark"`, func() (string, error) { return UserSubject(125, account, "mark") }},
			{"chain fill", `stream: unknown chain kind "fill"`, func() (string, error) { return ChainSubject(125, "fill") }},
			{"spot fill", `stream: unknown spot kind "fill"`, func() (string, error) { return SpotSubject(125, account, "fill") }},
			{"family", `stream: unknown subject family "vault"`, func() (string, error) { return Subject(125, "vault", "x", "swap") }},
		}
		for _, c := range cases {
			got, err := c.build()
			if got != "" || err == nil || err.Error() != c.want {
				t.Fatalf("%s = %q, %v; want error %q", c.name, got, err, c.want)
			}
		}
		if got, err := Subject(125, FamilyUser, "bad token", "fill"); got != "" || err == nil || !strings.Contains(err.Error(), "invalid subject") {
			t.Fatalf("Subject with a bad entity = %q, %v", got, err)
		}
	})

	t.Run("filters", func(t *testing.T) {
		if !ValidFilter("perps.125.*.fill") || !Matches("perps.125.*.fill", "perps.125.3.fill") {
			t.Fatal("perps.125.*.fill must be valid and match perps.125.3.fill")
		}
		if Matches("perps.125.*.fill", "perps.125.user.x.fill") {
			t.Fatal("perps.125.*.fill must not match perps.125.user.x.fill")
		}
		if !Matches("perps.125.>", "perps.125.chain.rewind") || Matches("perps.125.>", "perps.125") {
			t.Fatal("perps.125.> must match one or more trailing tokens only")
		}
		if !Matches("perps.125.user.*.position", "perps.125.user.0x00000000000000000000000000000000000000a1.position") {
			t.Fatal("user wildcard filter must match a user subject")
		}
		for _, bad := range []string{"", "perps.>.fill", "perps..fill", "perps.125.a b", "perps.12*", strings.Repeat("a", 257)} {
			if ValidFilter(bad) {
				t.Fatalf("ValidFilter(%q) = true", bad)
			}
		}
	})
}
