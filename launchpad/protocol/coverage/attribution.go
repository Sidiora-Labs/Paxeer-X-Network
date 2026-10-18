package coverage

import (
	"github.com/ethereum/go-ethereum/common"
	"math/big"
)

type Actor struct {
	Address    common.Address
	Provenance string
	Known      bool
}
type ActorSet struct{ TxFrom, TxTo, Executor, Trader, Payer, Recipient Actor }

func actors(from common.Address, to *common.Address, executor common.Address, fields map[string]any) ActorSet {
	a := ActorSet{TxFrom: Actor{from, "signed_transaction", true}, Executor: Actor{executor, "registered_event_origin", true}}
	if to != nil {
		a.TxTo = Actor{*to, "signed_transaction", true}
	}
	for _, k := range []string{"sender", "user", "buyer", "seller"} {
		if v, ok := fields[k].(common.Address); ok {
			a.Trader = Actor{v, "native_event:" + k, true}
			break
		}
	}
	if v, ok := fields["recipient"].(common.Address); ok {
		a.Recipient = Actor{v, "native_event:recipient", true}
	}
	return a
}
func (out *ReceiptResult) attributeTransfers() {
	for i, e := range out.Effects {
		if e.Kind != "core_fill" {
			continue
		}
		buy, ok := e.Fields["isBuy"].(bool)
		if !ok {
			continue
		}
		asset, _ := e.Fields["token"].(common.Address)
		if !buy {
			asset, _ = e.Fields["settlement"].(common.Address)
		}
		pool, _ := e.Fields["pool"].(common.Address)
		amount, _ := e.Fields["amountOut"].(*big.Int)
		recipients := []common.Address{}
		for _, movement := range out.Effects {
			if movement.Role == "SidioraERC20" && movement.Event == "Transfer" && movement.Identity.Emitter == asset && movement.Fields["from"] == pool && equalNumber(movement.Fields["value"], amount) {
				recipient, ok := movement.Fields["to"].(common.Address)
				if ok {
					recipients = append(recipients, recipient)
				}
			}
		}
		if len(recipients) == 1 {
			out.Effects[i].Actors.Recipient = Actor{recipients[0], "native_transfer", true}
		} else {
			out.Diagnostics = append(out.Diagnostics, "unattributed_recipient")
		}
	}
}
