package l16

import (
	"context"
	"encoding/json"
	"math/big"
	"strconv"
	"strings"
	"time"

	"github.com/Sidiora-Technologies/KindleLaunch/shared/accounting"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/delivery"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/jackc/pgx/v5"
)

type EconomicRecord struct {
	BlockHash     string `json:"blockHash"`
	TokenDecimals *int   `json:"tokenDecimals"`
	TimedLeg
	SourceFillID string            `json:"sourceFillId,omitempty"`
	SourceDigest string            `json:"sourceDigest"`
	Payer        string            `json:"payer,omitempty"`
	Recipient    string            `json:"recipient,omitempty"`
	FeeAsset     *accounting.Asset `json:"feeAsset,omitempty"`
	FeeRaw       string            `json:"feeRaw,omitempty"`
	GasPAXRaw    string            `json:"gasPaxRaw,omitempty"`
	Allocation   *Disposal         `json:"allocation,omitempty"`
	State        Inventory         `json:"state"`
}

type receiptSettlement struct {
	fill                  accounting.FillV1
	tokenMovement         movement
	payer, recipient, gas string
	digest                string
}

func (a *BaselineAuthority) receiptSettlement(ctx context.Context, tx pgx.Tx, fill accounting.FillV1) (receiptSettlement, error) {
	var result receiptSettlement
	if err := accounting.ValidateFillV1(fill); err != nil {
		return result, err
	}
	var count int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM indexer.indexer_accounting_fills_v1 WHERE payload->'position'->>'transactionHash'=$1`, fill.Position.TransactionHash).Scan(&count); err != nil {
		return result, err
	}
	if count != 1 {
		return result, ErrCostAllocationUnavailable
	}
	receipt, err := a.rpc.TransactionReceipt(ctx, common.HexToHash(fill.Position.TransactionHash))
	if err != nil {
		return result, err
	}
	if receipt == nil || receipt.Status != types.ReceiptStatusSuccessful || receipt.BlockNumber == nil || receipt.BlockNumber.String() != fill.Position.Height || strings.ToLower(receipt.BlockHash.Hex()) != fill.Position.BlockHash || strings.ToLower(receipt.TxHash.Hex()) != fill.Position.TransactionHash || strconv.FormatUint(uint64(receipt.TransactionIndex), 10) != fill.Position.TransactionIndex {
		return result, delivery.ErrIntegrity
	}
	raw, err := json.Marshal(receipt)
	if err != nil {
		return result, err
	}
	if delivery.Hash(raw) != fill.Provenance.ReceiptSHA256 {
		return result, delivery.ErrIntegrity
	}
	header, err := a.rpc.HeaderByHash(ctx, receipt.BlockHash)
	if err != nil {
		return result, err
	}
	if header == nil || strconv.FormatUint(header.Time, 10) != fill.BlockTimestampSeconds {
		return result, delivery.ErrIntegrity
	}
	raw, err = json.Marshal(header)
	if err != nil {
		return result, err
	}
	if delivery.Hash(raw) != fill.Provenance.HeaderSHA256 {
		return result, delivery.ErrIntegrity
	}
	var tokenMoves, settlements, quoteMoves []movement
	for _, log := range receipt.Logs {
		if log == nil {
			return result, delivery.ErrIntegrity
		}
		emitter := strings.ToLower(log.Address.Hex())
		if emitter != fill.Token.Address && emitter != fill.Settlement.Address {
			continue
		}
		if len(log.Topics) == 0 || log.Topics[0] != transferTopic {
			continue
		}
		m, e := decodeTransfer(*log)
		if e != nil {
			return result, e
		}
		if m.TransactionHash != fill.Position.TransactionHash || m.BlockHash != fill.Position.BlockHash || strconv.FormatInt(m.Height, 10) != fill.Position.Height || strconv.FormatUint(uint64(m.TransactionIndex), 10) != fill.Position.TransactionIndex {
			return result, delivery.ErrIntegrity
		}
		if emitter == fill.Settlement.Address {
			quoteMoves = append(quoteMoves, m)
		}
		if emitter == fill.Token.Address && m.Amount == fill.TokenAmountRaw && ((fill.Side == "buy" && m.From == fill.PoolAddress) || (fill.Side == "sell" && m.To == fill.PoolAddress)) {
			tokenMoves = append(tokenMoves, m)
		}
		if emitter == fill.Settlement.Address && m.Amount == fill.USDLAmountRaw && ((fill.Side == "buy" && m.To == fill.PoolAddress) || (fill.Side == "sell" && m.From == fill.PoolAddress)) {
			settlements = append(settlements, m)
		}
	}
	if len(tokenMoves) != 1 || len(settlements) != 1 {
		return result, ErrCostAllocationUnavailable
	}
	payer, recipient := settlements[0].From, tokenMoves[0].To
	if fill.Side == "sell" {
		payer, recipient = tokenMoves[0].From, settlements[0].To
	}
	if payer == zeroAddress || recipient == zeroAddress {
		return result, ErrCostAllocationUnavailable
	}
	actual := new(big.Int)
	for _, m := range quoteMoves {
		if fill.Side == "buy" && m.From == payer && m.To != payer || fill.Side == "sell" && m.To == recipient && m.From != recipient {
			v, e := Uint256(m.Amount)
			if e != nil {
				return result, e
			}
			actual.Add(actual, v)
		}
	}
	if actual.String() != fill.USDLAmountRaw {
		return result, ErrCostAllocationUnavailable
	}
	if receipt.EffectiveGasPrice == nil || receipt.EffectiveGasPrice.Sign() < 0 {
		return result, delivery.ErrIntegrity
	}
	result = receiptSettlement{fill: fill, tokenMovement: tokenMoves[0], payer: payer, recipient: recipient, gas: new(big.Int).Mul(new(big.Int).SetUint64(receipt.GasUsed), receipt.EffectiveGasPrice).String(), digest: fill.Provenance.ReceiptSHA256}
	return result, nil
}

const zeroAddress = "0x0000000000000000000000000000000000000000"

func normalizeMovement(wallet, token string, m movement, settlement *receiptSettlement, at time.Time, digest string) (EconomicRecord, bool, error) {
	var out EconomicRecord
	if m.From == m.To || m.Amount == "0" || m.From != wallet && m.To != wallet {
		return out, false, nil
	}
	leg := Leg{Identity: Identity{Chain: "125", Emitter: token, Transaction: m.TransactionHash, Log: m.LogIndex}, Height: uint64(m.Height), TransactionIndex: m.TransactionIndex, Wallet: wallet, Token: token, Quantity: m.Amount, SettlementReference: digest}
	if m.To == wallet {
		leg.Kind = "incoming"
		if m.From == zeroAddress {
			leg.Kind = "mint"
		}
	} else {
		leg.Kind = "outgoing"
		if m.To == zeroAddress {
			leg.Kind = "burn"
		}
	}
	out = EconomicRecord{BlockHash: m.BlockHash, TimedLeg: TimedLeg{Leg: leg, CommittedAt: at.UTC()}, SourceDigest: digest}
	if settlement != nil {
		s := settlement
		if s.tokenMovement != m {
			return EconomicRecord{}, false, delivery.ErrIntegrity
		}
		out.SourceFillID = s.fill.SourceFillID
		out.SourceDigest = s.digest
		out.Payer = s.payer
		out.Recipient = s.recipient
		out.GasPAXRaw = s.gas
		out.FeeRaw = s.fill.FeeRaw
		asset := s.fill.Settlement
		if s.fill.Side == "sell" {
			asset = s.fill.Token
		}
		out.FeeAsset = &asset
		out.Leg.Payer = s.payer
		out.Leg.Recipient = s.recipient
		out.Leg.Sender = s.fill.Attribution.TransactionSender
		out.Leg.SettlementReference = s.digest
		amount := s.fill.USDLAmountRaw
		if s.fill.Side == "buy" && s.recipient == wallet && s.payer == wallet {
			out.Leg.Kind = "buy"
			out.Leg.ActualUSDL = &amount
		}
		if s.fill.Side == "sell" && s.payer == wallet {
			if s.recipient != wallet {
				return EconomicRecord{}, false, ErrCostAllocationUnavailable
			}
			out.Leg.Kind = "sell"
			out.Leg.ActualUSDL = &amount
		}
	}
	if err := out.Leg.Validate(); err != nil {
		return EconomicRecord{}, false, err
	}
	return out, true, nil
}
