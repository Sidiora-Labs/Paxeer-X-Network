package decode

import (
	"fmt"
	"strings"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
)

// USDLAddress is the USDL ERC20 contract.
var USDLAddress = common.HexToAddress("0x85FcD13735F4309833A503EE804ea32395851479")

// usdlTransfer is the ERC20 Transfer event, the only USDL event decoded.
const usdlTransfer = `[{"type":"event","name":"Transfer","anonymous":false,"inputs":[{"name":"from","type":"address","indexed":true},{"name":"to","type":"address","indexed":true},{"name":"value","type":"uint256","indexed":false}]}]`

func registerUSDL(r *Registry) error {
	a, err := abi.JSON(strings.NewReader(usdlTransfer))
	if err != nil {
		return fmt.Errorf("decode: USDL ABI: %w", err)
	}
	custody := []common.Address{r.opts.PerpsModule, r.opts.SpotModule}
	return r.register(USDLAddress, r.opts.PerpsModule, a, []eventDecoder{{"Transfer", func(_ *batch, f Fact, rd *reader) []Row {
		from, to, value := rd.addr("from"), rd.addr("to"), rd.u256("value")
		var rows []Row
		for _, venue := range custody {
			if from == venue {
				out := f
				out.Kind, out.Sub, out.Account = "out", int64(len(rows)), to
				rows = append(rows, USDLTransferRow{out, from, to, value, venue})
			}
		}
		for _, venue := range custody {
			if to == venue {
				in := f
				in.Kind, in.Sub, in.Account = "in", int64(len(rows)), from
				rows = append(rows, USDLTransferRow{in, from, to, value, venue})
			}
		}
		return rows
	}}})
}

// USDLTransferRow is a perps.usdl_transfers row: kind in when the custody
// address Venue receives, out when it sends; the account is the other side.
type USDLTransferRow struct {
	Fact
	FromAccount common.Address
	ToAccount   common.Address
	Value       string
	Venue       common.Address
}

func (USDLTransferRow) Table() string { return "usdl_transfers" }
