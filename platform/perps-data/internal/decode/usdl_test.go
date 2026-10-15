package decode_test

import (
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"

	"github.com/Sidiora-Labs/Paxeer-X-Network/platform/perps-data/internal/decode"
)

const erc20Transfer = `[{"type":"event","name":"Transfer","anonymous":false,"inputs":[{"name":"from","type":"address","indexed":true},{"name":"to","type":"address","indexed":true},{"name":"value","type":"uint256","indexed":false}]}]`

func TestUSDLDecoder(t *testing.T) {
	ev := event(t, func() (abi.ABI, error) { return abi.JSON(strings.NewReader(erc20Transfer)) }, "Transfer")
	usdl := decode.USDLAddress
	row := func(index, sub int64, kind string, account, from, to, venue common.Address, value string) decode.USDLTransferRow {
		f := fact(usdl, index, kind, account)
		f.Sub = sub
		return decode.USDLTransferRow{Fact: f, FromAccount: from, ToAccount: to, Value: value, Venue: venue}
	}

	t.Run("Transfer", func(t *testing.T) {
		in := pack(t, ev, usdl, 0, xAlice, perpsModule, n(100))
		out := pack(t, ev, usdl, 1, spotModule, xBob, n(40))
		wantRows(t, decodeRows(t, registry(t, nil), in, out),
			row(0, 0, "in", xAlice, xAlice, perpsModule, perpsModule, "100"),
			row(1, 0, "out", xBob, spotModule, xBob, spotModule, "40"))
	})
	t.Run("venue_filter_and_tvl", func(t *testing.T) {
		// A transfer between users touches no custody address and is not stored.
		res := decodeRows(t, registry(t, nil), pack(t, ev, usdl, 0, xAlice, xBob, n(7)))
		if len(res.Decoded) != 0 || len(res.Raw) != 0 {
			t.Fatalf("user transfer: %+v", res)
		}
		// A transfer between the venues is an out of one and an in of the other.
		res = decodeRows(t, registry(t, nil), pack(t, ev, usdl, 0, perpsModule, spotModule, n(9)))
		wantRows(t, res,
			row(0, 0, "out", spotModule, perpsModule, spotModule, perpsModule, "9"),
			row(0, 1, "in", perpsModule, perpsModule, spotModule, spotModule, "9"))
		// TVL of a venue is its ins less its outs.
		res = decodeRows(t, registry(t, nil),
			pack(t, ev, usdl, 0, xAlice, perpsModule, n(100)),
			pack(t, ev, usdl, 1, xBob, perpsModule, n(50)),
			pack(t, ev, usdl, 2, perpsModule, xAlice, n(30)))
		tvl := int64(0)
		for _, d := range res.Decoded {
			for _, r := range d.Rows {
				u := r.(decode.USDLTransferRow)
				if u.Venue != perpsModule {
					t.Fatalf("venue %s", u.Venue.Hex())
				}
				v := map[string]int64{"100": 100, "50": 50, "30": 30}[u.Value]
				if u.Kind == "in" {
					tvl += v
				} else {
					tvl -= v
				}
			}
		}
		if tvl != 120 {
			t.Fatalf("perps venue tvl %d, want 120", tvl)
		}
	})
}
