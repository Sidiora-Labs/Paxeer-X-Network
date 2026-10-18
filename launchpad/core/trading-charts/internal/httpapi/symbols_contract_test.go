package httpapi

import (
	"net/http"
	"reflect"
	"strings"
	"testing"
)

func TestUDFSymbolsRejectInvalidIdentity(t *testing.T) {
	for _, query := range []string{
		"",
		"symbol=ABC",
		"symbol=" + udfPool + "&chainId=1",
		"symbol=" + udfPool + "&chainId=",
		"symbol=" + udfPool + "&chainId=125&chainId=125",
		"symbol=" + udfPool + "&symbol=" + udfPool,
		"symbol=" + udfPool + "&chainId=%zz",
		"symbol=" + udfPool + "&chainId=125;ignored=1",
		"symbol=" + udfPool + "&resolution=1M",
		"symbol=" + udfPool + "&unrecognized%=1",
		"symbol=" + udfPool + "&padding=" + strings.Repeat("x", 16385),
	} {
		rec, body := doGET(t, http.HandlerFunc(udfSymbols), "/symbols?"+query)
		if rec.Code != 422 || body["code"] != "FIELD_INVALID" || body["retryable"] != false {
			t.Fatalf("query=%q status=%d body=%+v", query, rec.Code, body)
		}
	}
}

func TestUDFSymbolsCanonicalIdentityAndResolutionParity(t *testing.T) {
	mixed := "0x000000000000000000000000000000000000eEeE"
	rec, symbol := doGET(t, http.HandlerFunc(udfSymbols), "/symbols?symbol="+mixed+"&chainId=125")
	if rec.Code != 200 || symbol["name"] != udfPool || symbol["ticker"] != udfPool || symbol["full_name"] != udfPool+"/USDL" {
		t.Fatalf("symbol identity: %d %+v", rec.Code, symbol)
	}
	_, config := doGET(t, http.HandlerFunc(udfConfig), "/config")
	if !reflect.DeepEqual(symbol["supported_resolutions"], config["supported_resolutions"]) {
		t.Fatal("symbol and config resolution vocabulary differ")
	}
	want := []interface{}{"1", "5", "15", "60", "240", "1D", "D", "1W", "W"}
	if !reflect.DeepEqual(symbol["supported_resolutions"], want) {
		t.Fatal("unexpected supported resolutions", symbol["supported_resolutions"])
	}
	for _, resolution := range want {
		if _, err := ResolutionSeconds(resolution.(string)); err != nil {
			t.Fatal("advertised unsupported resolution", resolution, err)
		}
	}
	history, err := parseHistory(map[string][]string{"symbol": {mixed}, "chainId": {"125"}, "resolution": {"1"}, "from": {"0"}, "to": {"120"}})
	if err != nil || history.Symbol != symbol["ticker"] {
		t.Fatal("symbol and history identify different pools", history, err)
	}
}
