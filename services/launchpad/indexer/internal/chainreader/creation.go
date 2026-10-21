package chainreader

import (
	"fmt"
	"github.com/Sidiora-Technologies/KindleLaunch/core/indexer/internal/decode"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"math/big"
)

// CreationIdentity is receipt evidence, not a projection or current-state
// attestation. Pool registry pinned-getter corroboration belongs to the caller.
type CreationIdentity struct {
	Token   common.Address
	Pool    common.Address
	Creator common.Address
	Optical common.Address
	NFTID   *big.Int
	Raw     decode.RawIdentity
}

// CreationFromReceipt resolves the native factory event in the exact successful
// receipt. It never scans recent creator logs or promotes emitter mirrors into
// a second creation. The supplied registry must authorize the factory epoch.
func CreationFromReceipt(registry *decode.Registry, receipt *ethtypes.Receipt, expectedTx common.Hash) (*CreationIdentity, error) {
	if registry == nil || receipt == nil || expectedTx == (common.Hash{}) || receipt.BlockNumber == nil || receipt.BlockNumber.Sign() < 0 || receipt.BlockNumber.BitLen() > 64 || receipt.BlockHash == (common.Hash{}) || receipt.TxHash != expectedTx || receipt.Status != ethtypes.ReceiptStatusSuccessful {
		return nil, fmt.Errorf("successful exact creation receipt required")
	}
	if len(receipt.Logs) > decode.MaxRawBatchLogs {
		return nil, fmt.Errorf("receipt exceeds log count cap")
	}
	aggregate := 0
	for _, l := range receipt.Logs {
		if l == nil {
			return nil, fmt.Errorf("nil receipt log")
		}
		if len(l.Topics) > 4 || len(l.Data) > decode.MaxEnvelopeBytes || len(l.Topics) > (decode.MaxEnvelopeBytes-len(l.Data))/32 {
			return nil, fmt.Errorf("receipt log exceeds envelope cap")
		}
		size := len(l.Data) + 32*len(l.Topics)
		if size > decode.MaxRawBatchBytes-aggregate {
			return nil, fmt.Errorf("receipt exceeds aggregate byte cap")
		}
		aggregate += size
	}
	logs := make([]ethtypes.Log, 0, len(receipt.Logs))
	for _, l := range receipt.Logs {
		if l == nil || l.Removed {
			return nil, fmt.Errorf("nil receipt log")
		}
		if l.TxHash != receipt.TxHash || l.BlockHash != receipt.BlockHash || l.BlockNumber != receipt.BlockNumber.Uint64() || l.TxIndex != receipt.TransactionIndex {
			return nil, fmt.Errorf("receipt log identity mismatch")
		}
		logs = append(logs, *l)
	}
	unique, err := decode.DeduplicateRaw(logs)
	if err != nil {
		return nil, err
	}
	var result *CreationIdentity
	for _, l := range unique {
		if len(l.Topics) == 0 || l.Topics[0] != crypto.Keccak256Hash([]byte("MarketCreated(address,address,address,uint256,address)")) {
			continue
		}
		e, err := registry.Decode(l)
		if err != nil {
			return nil, err
		}
		if e.Event == nil || e.EmitterRole != "SidioraFactory" || e.EventName != "MarketCreated" {
			continue
		}
		if result != nil {
			return nil, fmt.Errorf("ambiguous multiple native creations")
		}
		a := e.Event.Args
		address := func(field string) (common.Address, error) {
			s, ok := a[field].(string)
			if !ok || !common.IsHexAddress(s) {
				return common.Address{}, fmt.Errorf("invalid creation %s", field)
			}
			return common.HexToAddress(s), nil
		}
		result = &CreationIdentity{Raw: e.Raw.Identity}
		if result.Token, err = address("token"); err != nil {
			return nil, err
		}
		if result.Pool, err = address("pool"); err != nil {
			return nil, err
		}
		if result.Creator, err = address("creator"); err != nil {
			return nil, err
		}
		if result.Optical, err = address("optical"); err != nil {
			return nil, err
		}
		s, ok := a["nftId"].(string)
		if !ok {
			return nil, fmt.Errorf("invalid nft id")
		}
		result.NFTID, ok = new(big.Int).SetString(s, 10)
		if !ok || result.Token == (common.Address{}) || result.Pool == (common.Address{}) || result.Creator == (common.Address{}) || result.NFTID.Sign() <= 0 {
			return nil, fmt.Errorf("invalid native creation identity")
		}
	}
	if result == nil {
		return nil, fmt.Errorf("native factory creation absent")
	}
	return result, nil
}
