package coverage

import (
	"github.com/ethereum/go-ethereum/common"
	"math/big"
	"testing"
)

func TestActorProvenanceNeverGuessesBeneficiaryFromTransactionOrigin(t *testing.T) {
	origin := common.HexToAddress("0x1")
	router := common.HexToAddress("0x2")
	trader := common.HexToAddress("0x3")
	actors := actors(origin, &router, router, map[string]any{"sender": trader})
	if actors.TxFrom.Address != origin || actors.Trader.Address != trader || actors.Recipient.Known || actors.Payer.Known {
		t.Fatal("actor identities conflated")
	}
}
func TestOutputTransferAttributionRequiresUnambiguousNativeMovement(t *testing.T) {
	pool := common.HexToAddress("0x1")
	asset := common.HexToAddress("0x2")
	recipient := common.HexToAddress("0x3")
	fill := Effect{Kind: "core_fill", Fields: map[string]any{"pool": pool, "token": asset, "isBuy": true, "amountOut": big.NewInt(7)}}
	movement := Effect{Role: "SidioraERC20", Event: "Transfer", Identity: Identity{Emitter: asset}, Fields: map[string]any{"from": pool, "to": recipient, "value": big.NewInt(7)}}
	out := ReceiptResult{Effects: []Effect{fill, movement}}
	out.attributeTransfers()
	if !out.Effects[0].Actors.Recipient.Known || out.Effects[0].Actors.Recipient.Address != recipient {
		t.Fatal("real recipient unavailable")
	}
	out = ReceiptResult{Effects: []Effect{fill, movement, movement}}
	out.attributeTransfers()
	if out.Effects[0].Actors.Recipient.Known {
		t.Fatal("ambiguous beneficiary guessed")
	}
}
