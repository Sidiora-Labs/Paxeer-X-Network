package capabilities

import (
	"bytes"
	"context"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
)

func TestPreflightCalldataUsesActualCreationSignatureAndSpenderContext(t *testing.T) {
	encoded, err := encodeCall("createMarket(string,string,uint8,address)", "Real Market", "REAL", uint8(0), common.Address{})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(encoded[:4], common.FromHex("0x57c0d7fd")) || !bytes.Contains(encoded, []byte("Real Market")) || !bytes.Contains(encoded, []byte("REAL")) {
		t.Fatal("creation calldata differs from reviewed Router/Factory ABI")
	}
	for _, signature := range []string{"", "createMarket", "createMarket("} {
		if _, err := encodeCall(signature); err == nil {
			t.Fatal("invalid signature accepted")
		}
	}
	quote, err := encodeCall("quoteExactInput(address,uint256,bool)", common.HexToAddress("0x1111111111111111111111111111111111111111"), big.NewInt(1000000), true)
	if err != nil || len(quote) != 100 || !bytes.Equal(quote[:4], common.FromHex("0x4d9f71e4")) {
		t.Fatal("quote ABI drift")
	}
}

func TestDisplayProvenanceCannotAuthorizeSettlement(t *testing.T) {
	now := time.Now().UTC()
	price := PriceProvenance{QuoteAsset: "USD", ProviderKind: "reviewed-provider", ObservedAt: now, ConfidenceBPS: 9500, DisplayOnly: true, Value: "1000000"}
	if err := price.Validate(now, time.Minute); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*PriceProvenance){func(p *PriceProvenance) { p.DisplayOnly = false }, func(p *PriceProvenance) { p.Stale = true }, func(p *PriceProvenance) { p.ObservedAt = now.Add(-2 * time.Minute) }, func(p *PriceProvenance) { p.ConfidenceBPS = 10001 }, func(p *PriceProvenance) { p.Value = "-1" }} {
		bad := price
		mutate(&bad)
		if err := bad.Validate(now, time.Minute); err == nil {
			t.Fatal("unusable display provenance accepted")
		}
	}
	request := PreflightRequest{Family: Trading, AmountIn: "1000000", IsBuy: true}
	b, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"price", "oracle", "display"} {
		if strings.Contains(strings.ToLower(string(b)), name) {
			t.Fatal("display value entered settlement preflight")
		}
	}
}

func TestPinnedQuoteContextRejectsMalformedInputsBeforeRPC(t *testing.T) {
	for _, amount := range []string{"0", "-1", "01", strings.Repeat("9", 78)} {
		if _, err := ReadPinnedQuote(context.Background(), nil, common.Address{}, common.Address{}, common.Address{}, amount, true, common.Hash{}); err == nil {
			t.Fatal("unbound quote accepted")
		}
	}
	if _, err := NewPinnedRPCReader(nil); err == nil {
		t.Fatal("missing RPC client accepted")
	}
	if _, err := (*PreflightEngine)(nil).Revalidate(context.Background(), Preflight{}, PreflightRequest{}); err == nil {
		t.Fatal("missing preflight engine accepted")
	}
}

func TestPreflightHandlerRejectsDuplicateUnknownAndNestedFields(t *testing.T) {
	for _, body := range []string{
		`{"family":"creation","family":"creation"}`,
		`{"family":"creation","trader":null}`,
		`{"family":"creation","trader":{"address":"0x1"}}`,
		`{"family":"creation","privateRPC":"secret"}`,
		`{"family":"creation"}{}`,
		strings.Repeat(" ", 16<<10+1),
	} {
		recorder := httptest.NewRecorder()
		(*PreflightEngine)(nil).Handler(Creation).ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body)))
		if recorder.Code != http.StatusBadRequest || strings.Contains(recorder.Body.String(), "secret") {
			t.Fatalf("unsafe request accepted: %d %s", recorder.Code, recorder.Body.String())
		}
	}
	if !uniquePreflightRequest([]byte(`{"family":"creation","isBuy":false,"feeStrategy":0}`)) {
		t.Fatal("valid primitive request rejected")
	}
}
