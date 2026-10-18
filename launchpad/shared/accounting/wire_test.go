package accounting_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/Sidiora-Technologies/KindleLaunch/shared/accounting"
)

func wireTuple(parts ...string) string {
	h := sha256.New()
	for _, part := range parts {
		var length [4]byte
		binary.BigEndian.PutUint32(length[:], uint32(len([]byte(part))))
		h.Write(length[:])
		h.Write([]byte(part))
	}
	return hex.EncodeToString(h.Sum(nil))
}

func wireDigest(t *testing.T, value any) accounting.Digest {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(raw)
	return accounting.Digest(hex.EncodeToString(digest[:]))
}

func wireInitialPublication(t *testing.T) accounting.PublicationV1 {
	t.Helper()
	f := accounting.FillV1{
		Version: "accounting-fill-v1",
		Position: accounting.SourcePosition{
			ChainID: "125", Height: "100", TransactionIndex: "0", LogIndex: "7",
			TransactionHash: "0x" + strings.Repeat("a", 64), BlockHash: "0x" + strings.Repeat("b", 64),
		},
		ActualEmitter: "0x" + strings.Repeat("1", 40), PoolAddress: "0x" + strings.Repeat("2", 40),
		Token:      accounting.Asset{Address: "0x" + strings.Repeat("3", 40), Decimals: 18},
		Settlement: accounting.Asset{Address: "0x" + strings.Repeat("4", 40), Decimals: 6},
		Side:       "buy", TokenAmountRaw: "332665999332665", USDLAmountRaw: "5000000000",
		PaidInputRaw: "5000000000", FeeRaw: "15000000", InputDerivationQuality: "receipt_incomplete",
		FeeDisposition: "distributed_usdl",
		Attribution:    accounting.Attribution{TransactionSender: "0x" + strings.Repeat("5", 40), Quality: "unavailable", Evidence: []accounting.EvidenceRef{}},
		Execution:      accounting.ExecutionPolicy{Quality: "unavailable", Evidence: []accounting.EvidenceRef{}},
		Provenance: accounting.Provenance{
			DeploymentID: "private-125", ABIRevision: "sidiora-v1", EmitterCodeEpoch: "epoch-1", PoolCodeEpoch: "epoch-1",
			ReceiptSHA256: strings.Repeat("c", 64), HeaderSHA256: strings.Repeat("d", 64),
		},
		BlockTimestampSeconds: "1700000000",
	}
	f.SourceFillID = wireTuple("source-fill-v1", "125", f.Provenance.DeploymentID, f.Position.TransactionHash, f.Position.LogIndex)
	digest := wireDigest(t, f)
	p := accounting.PublicationV1{
		Version: "accounting-publication-v1", Facet: "market", SourceFillID: f.SourceFillID,
		SourceFactDigest: digest, Fill: &f,
		Positions: []accounting.PartitionPosition{{
			ProjectionVersion: "market-v1", PartitionKind: "market", PartitionKey: "125:" + f.PoolAddress,
			Sequence: "1", PreviousEffectIdentity: "", SourceEventID: f.SourceFillID,
			PayloadDigest: digest, SourceOrder: f.Position,
		}},
	}
	p.PublicationID = wireTuple("publication-v1", f.SourceFillID, string(digest), "market", "market-v1", "initial", "")
	p.Positions[0].EffectIdentity = wireTuple("effect-v1", f.SourceFillID, "market", "market-v1", "", "market", p.Positions[0].PartitionKey)
	return p
}

func wireMarshal(t *testing.T, p accounting.PublicationV1) []byte {
	t.Helper()
	raw, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestPublicationV1IndependentIdentitiesAndRoundTrip(t *testing.T) {
	p := wireInitialPublication(t)
	if err := accounting.ValidatePublicationV1(p); err != nil {
		t.Fatalf("independently constructed publication rejected: %v", err)
	}
	raw := wireMarshal(t, p)
	decoded, err := accounting.DecodePublicationV1(raw)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(p, decoded) {
		t.Fatalf("wire round trip changed exact source facts: %#v", decoded)
	}
	if decoded.Fill.ActualEmitter == decoded.Fill.PoolAddress {
		t.Fatal("event emitter was collapsed into economic pool")
	}
	if decoded.Fill.Execution.FeeBPS != nil || decoded.Fill.PricedInputRaw != nil || decoded.Fill.AMMInputAfterFeeRaw != nil {
		t.Fatal("unavailable execution facts became invented values")
	}
	for _, field := range []string{`"pricedInputRaw":null`, `"ammInputAfterFeeRaw":null`, `"feeBPS":null`, `"routerCaller":null`, `"payer":null`, `"enrichment":null`} {
		if !bytes.Contains(raw, []byte(field)) {
			t.Fatalf("canonical wire omitted explicit null %s", field)
		}
	}
}

func TestPublicationV1RejectsUnknownDuplicateAndMalformedJSON(t *testing.T) {
	raw := string(wireMarshal(t, wireInitialPublication(t)))
	cases := map[string]string{
		"unknown root":              strings.Replace(raw, `{`, `{"unexpected":true,`, 1),
		"duplicate root":            strings.Replace(raw, `"facet":"market"`, `"facet":"market","facet":"market"`, 1),
		"escaped duplicate":         strings.Replace(raw, `"facet":"market"`, `"facet":"market","\u0066acet":"market"`, 1),
		"duplicate nested":          strings.Replace(raw, `"chainId":"125"`, `"chainId":"125","chainId":"125"`, 1),
		"unknown nested":            strings.Replace(raw, `"chainId":"125"`, `"chainId":"125","extra":0`, 1),
		"unknown evidence":          strings.Replace(raw, `"evidence":[]`, `"evidence":[{"kind":"receipt","artifactSHA256":"`+strings.Repeat("a", 64)+`","artifactId":"receipt-1","logIndex":null,"extra":0}]`, 1),
		"trailing object":           raw + `{}`,
		"array":                     `[` + raw + `]`,
		"null":                      `null`,
		"missing fill":              strings.Replace(raw, `"fill":`, `"discardedFill":`, 1),
		"nondecimal numeric amount": strings.Replace(raw, `"tokenAmountRaw":"332665999332665"`, `"tokenAmountRaw":332665999332665`, 1),
	}
	for name, input := range cases {
		t.Run(name, func(t *testing.T) {
			if input == raw {
				t.Fatal("malformed-wire setup did not modify input")
			}
			if _, err := accounting.DecodePublicationV1([]byte(input)); err == nil {
				t.Fatal("malformed or ambiguous wire accepted")
			}
		})
	}
}

func TestPublicationV1EnforcesOneMiBInputBound(t *testing.T) {
	raw := wireMarshal(t, wireInitialPublication(t))
	atLimit := append(append([]byte{}, raw...), bytes.Repeat([]byte(" "), (1<<20)-len(raw))...)
	if _, err := accounting.DecodePublicationV1(atLimit); err != nil {
		t.Fatalf("valid JSON at exact 1MiB limit rejected: %v", err)
	}
	if _, err := accounting.DecodePublicationV1(append(atLimit, ' ')); err == nil {
		t.Fatal("JSON beyond exact 1MiB limit accepted")
	}
}

func TestPublicationV1RejectsChangedImmutableBindings(t *testing.T) {
	cases := map[string]func(*accounting.PublicationV1){
		"publication version":  func(p *accounting.PublicationV1) { p.Version = "accounting-publication-v2" },
		"fill version":         func(p *accounting.PublicationV1) { p.Fill.Version = "accounting-fill-v2" },
		"source identity":      func(p *accounting.PublicationV1) { p.SourceFillID = strings.Repeat("a", 64) },
		"source digest":        func(p *accounting.PublicationV1) { p.SourceFactDigest = accounting.Digest(strings.Repeat("a", 64)) },
		"publication identity": func(p *accounting.PublicationV1) { p.PublicationID = strings.Repeat("a", 64) },
		"receipt provenance":   func(p *accounting.PublicationV1) { p.Fill.Provenance.ReceiptSHA256 = strings.Repeat("e", 64) },
		"amount":               func(p *accounting.PublicationV1) { p.Fill.TokenAmountRaw = "332665999332666" },
		"position payload digest": func(p *accounting.PublicationV1) {
			p.Positions[0].PayloadDigest = accounting.Digest(strings.Repeat("a", 64))
		},
		"position effect identity":   func(p *accounting.PublicationV1) { p.Positions[0].EffectIdentity = strings.Repeat("a", 64) },
		"position source event":      func(p *accounting.PublicationV1) { p.Positions[0].SourceEventID = strings.Repeat("a", 64) },
		"position source coordinate": func(p *accounting.PublicationV1) { p.Positions[0].SourceOrder.LogIndex = "8" },
		"position projection":        func(p *accounting.PublicationV1) { p.Positions[0].ProjectionVersion = "other-v1" },
		"position kind":              func(p *accounting.PublicationV1) { p.Positions[0].PartitionKind = "inventory" },
		"position pool":              func(p *accounting.PublicationV1) { p.Positions[0].PartitionKey = "125:0x" + strings.Repeat("9", 40) },
		"facet":                      func(p *accounting.PublicationV1) { p.Facet = "inventory" },
		"no body":                    func(p *accounting.PublicationV1) { p.Fill = nil },
		"two bodies":                 func(p *accounting.PublicationV1) { p.Enrichment = &accounting.EnrichmentV1{} },
		"no position":                func(p *accounting.PublicationV1) { p.Positions = nil },
		"duplicate position":         func(p *accounting.PublicationV1) { p.Positions = append(p.Positions, p.Positions[0]) },
		"zero sequence":              func(p *accounting.PublicationV1) { p.Positions[0].Sequence = "0" },
		"sequence overflow":          func(p *accounting.PublicationV1) { p.Positions[0].Sequence = "9223372036854775808" },
		"bad predecessor":            func(p *accounting.PublicationV1) { p.Positions[0].PreviousEffectIdentity = "previous" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			p := wireInitialPublication(t)
			mutate(&p)
			if err := accounting.ValidatePublicationV1(p); err == nil {
				t.Fatal("changed immutable publication binding accepted")
			}
			if _, err := accounting.DecodePublicationV1(wireMarshal(t, p)); err == nil {
				t.Fatal("decoder accepted changed immutable publication binding")
			}
		})
	}
}

func TestFillV1RejectsNonCanonicalAndOverflowInputs(t *testing.T) {
	cases := map[string]func(*accounting.FillV1){
		"leading zero":     func(f *accounting.FillV1) { f.TokenAmountRaw = "01" },
		"negative":         func(f *accounting.FillV1) { f.TokenAmountRaw = "-1" },
		"decimal fraction": func(f *accounting.FillV1) { f.TokenAmountRaw = "1.0" },
		"exponent":         func(f *accounting.FillV1) { f.TokenAmountRaw = "1e18" },
		"uint256 overflow": func(f *accounting.FillV1) {
			f.TokenAmountRaw = "115792089237316195423570985008687907853269984665640564039457584007913129639936"
		},
		"other chain":                      func(f *accounting.FillV1) { f.Position.ChainID = "126" },
		"transaction index overflow":       func(f *accounting.FillV1) { f.Position.TransactionIndex = "4294967296" },
		"log index overflow":               func(f *accounting.FillV1) { f.Position.LogIndex = "4294967296" },
		"height overflow":                  func(f *accounting.FillV1) { f.Position.Height = "9223372036854775808" },
		"timestamp millis overflow":        func(f *accounting.FillV1) { f.BlockTimestampSeconds = "9223372036854776" },
		"uppercase address":                func(f *accounting.FillV1) { f.ActualEmitter = "0x" + strings.Repeat("A", 40) },
		"short hash":                       func(f *accounting.FillV1) { f.Position.BlockHash = "0x01" },
		"wrong settlement decimals":        func(f *accounting.FillV1) { f.Settlement.Decimals = 18 },
		"unavailable fee guessed zero":     func(f *accounting.FillV1) { n := uint16(0); f.Execution.FeeBPS = &n },
		"unavailable payer guessed sender": func(f *accounting.FillV1) { f.Attribution.Payer = &f.Attribution.TransactionSender },
		"wrong buy fee disposition":        func(f *accounting.FillV1) { f.FeeDisposition = "retained_token_reserve" },
		"invalid quality":                  func(f *accounting.FillV1) { f.InputDerivationQuality = "assumed" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			p := wireInitialPublication(t)
			mutate(p.Fill)
			if err := accounting.ValidateFillV1(*p.Fill); err == nil {
				t.Fatal("noncanonical or unsupported fill accepted")
			}
		})
	}
}
