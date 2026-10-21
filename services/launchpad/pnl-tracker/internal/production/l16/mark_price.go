package l16

import (
	"bytes"
	"context"
	"errors"
	"math/big"
	"strconv"
	"strings"
	"time"

	"github.com/Sidiora-Technologies/KindleLaunch/core/pnl-tracker/internal/production/l17"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/accounting"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/delivery"
	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/jackc/pgx/v5"
)

// usdlAddress is the chain-125 settlement asset every launched pool quotes in
// (6 decimals); l17.Derive admits no other quote asset.
const usdlAddress = "0x85fcd13735f4309833a503ee804ea32395851479"

var effectiveReservesSelector = crypto.Keccak256([]byte("getEffectiveReserves()"))[:4]
var poolStateUpdatedTopic = crypto.Keccak256Hash([]byte("PoolStateUpdated(bytes32,uint256,uint256,uint256,uint256,uint256,uint256)"))
var decimalsSelector = crypto.Keccak256([]byte("decimals()"))[:4]

// markPriceTx returns the frozen post_swap_spot / effective_reserves mark for
// token as of the accepted head: the pool's effective reserves
// (virtualUsdlReserve + realUsdlBalance, tokenReserve) read by an EIP-1898
// pinned eth_call at the block of the token's latest canonical accounting fill
// at or below head.
//
// A pinned read returns end-of-block state, which is the post-swap state of
// that fill only when nothing touches the pool's reserves later in the same
// block. When a later token or USDL Transfer involving the pool, or a later
// PoolStateUpdated reserve sync for the pool, follows the fill in that block the mark
// is not provable and nil is returned (the position stays "unpriced"). A token
// with no fill at or below head also has no mark. Errors are dependency or
// integrity failures and fail the fold.
func (a *BaselineAuthority) markPriceTx(ctx context.Context, tx pgx.Tx, token string, head int64, headHash string) (*l17.Price, error) {
	var raw []byte
	var digest string
	err := tx.QueryRow(ctx, `SELECT payload,source_fact_digest FROM indexer.indexer_accounting_fills_v1 WHERE payload->'token'->>'address'=$1 AND (payload->'position'->>'height')::bigint<=$2 ORDER BY (payload->'position'->>'height')::bigint DESC,(payload->'position'->>'transactionIndex')::bigint DESC,(payload->'position'->>'logIndex')::bigint DESC LIMIT 1`, token, head).Scan(&raw, &digest)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	fill, err := accounting.DecodeFillV1(raw)
	if err != nil {
		return nil, err
	}
	actual, err := accounting.FillDigest(fill)
	if err != nil || string(actual) != digest {
		return nil, delivery.ErrIntegrity
	}
	position := fill.Position
	height, err := strconv.ParseInt(position.Height, 10, 64)
	if err != nil || height <= 0 || height > head || position.ChainID != "125" || fill.Token.Address != token || !hash.MatchString(position.BlockHash) || !hash.MatchString(position.TransactionHash) || !address.MatchString(fill.PoolAddress) {
		return nil, delivery.ErrIntegrity
	}
	if fill.Settlement.Address != usdlAddress || fill.Settlement.Decimals != 6 {
		return nil, nil
	}
	txIndex, err := strconv.ParseUint(position.TransactionIndex, 10, 32)
	if err != nil {
		return nil, delivery.ErrIntegrity
	}
	logIndex, err := strconv.ParseUint(position.LogIndex, 10, 32)
	if err != nil {
		return nil, delivery.ErrIntegrity
	}
	blockHash := common.HexToHash(position.BlockHash)
	pool := common.HexToAddress(fill.PoolAddress)
	clean, err := a.reservesSettledAfter(ctx, blockHash, pool, fill, uint(txIndex), uint(logIndex))
	if err != nil || !clean {
		return nil, err
	}
	reserves, err := a.rpc.CallContractAtHash(ctx, ethereum.CallMsg{To: &pool, Data: effectiveReservesSelector}, blockHash)
	if err != nil {
		return nil, err
	}
	if len(reserves) != 64 {
		return nil, delivery.ErrIntegrity
	}
	effectiveUSDL := new(big.Int).SetBytes(reserves[:32])
	tokenReserve := new(big.Int).SetBytes(reserves[32:])
	if effectiveUSDL.Sign() == 0 || tokenReserve.Sign() == 0 {
		return nil, nil
	}
	contract := common.HexToAddress(token)
	decimalRaw, err := a.rpc.CallContractAtHash(ctx, ethereum.CallMsg{To: &contract, Data: decimalsSelector}, common.HexToHash(headHash))
	if err != nil {
		return nil, err
	}
	if len(decimalRaw) != 32 || new(big.Int).SetBytes(decimalRaw).Cmp(big.NewInt(255)) > 0 {
		return nil, delivery.ErrIntegrity
	}
	decimals := int(new(big.Int).SetBytes(decimalRaw).Int64())
	seconds, err := strconv.ParseInt(fill.BlockTimestampSeconds, 10, 64)
	if err != nil || seconds <= 0 {
		return nil, delivery.ErrIntegrity
	}
	return SpotMark(token, decimals, effectiveUSDL, tokenReserve, fill.SourceFillID, l17.ChainPosition{ChainID: "125", Height: position.Height, TransactionIndex: position.TransactionIndex, LogIndex: position.LogIndex, TransactionHash: position.TransactionHash, BlockHash: position.BlockHash}, time.Unix(seconds, 0).UTC(), fill.PoolAddress)
}

// SpotMark builds the frozen IF_PRICE post-swap spot mark from pinned
// effective reserves: human USDL per WHOLE token, as a reduced fraction
// (effectiveUSDL * 10^decimals) / (tokenReserve * 10^6).
func SpotMark(token string, decimals int, effectiveUSDL, tokenReserve *big.Int, sourceEventID string, position l17.ChainPosition, observed time.Time, pool string) (*l17.Price, error) {
	if !address.MatchString(token) || decimals < 0 || decimals > 255 || effectiveUSDL == nil || tokenReserve == nil || effectiveUSDL.Sign() <= 0 || tokenReserve.Sign() <= 0 || sourceEventID == "" {
		return nil, delivery.ErrIntegrity
	}
	numerator := new(big.Int).Mul(effectiveUSDL, new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(decimals)), nil))
	denominator := new(big.Int).Mul(tokenReserve, big.NewInt(1000000))
	divisor := new(big.Int).GCD(nil, nil, numerator, denominator)
	numerator.Quo(numerator, divisor)
	denominator.Quo(denominator, divisor)
	chain, quoteAddress, currency, quoteDecimals := "125", usdlAddress, "USDL", 6
	base := strings.ToLower(token)
	reference := "pool:" + strings.ToLower(pool)
	baseDecimals := decimals
	event := sourceEventID
	at := position
	return &l17.Price{
		Numerator:   numerator.String(),
		Denominator: denominator.String(),
		BaseAsset:   l17.AssetRef{Kind: "erc20", ChainID: &chain, Address: &base, CurrencyCode: nil, Decimals: &baseDecimals},
		QuoteAsset:  l17.AssetRef{Kind: "erc20", ChainID: &chain, Address: &quoteAddress, CurrencyCode: &currency, Decimals: &quoteDecimals},
		Source:      l17.PriceSource{Kind: "post_swap_spot", AmountBasis: "effective_reserves", SourceEventID: &event, Position: &at, ProviderID: nil, ObservedAt: observed.UTC().Format("2006-01-02T15:04:05.000Z"), SourceReference: &reference},
	}, nil
}

// reservesSettledAfter reports whether no log after (txIndex, logIndex) in the
// fill's block can have changed the pool's reserves: no token or USDL Transfer
// to or from the pool and no PoolStateUpdated (syncReserves) for the pool.
func (a *BaselineAuthority) reservesSettledAfter(ctx context.Context, blockHash common.Hash, pool common.Address, fill accounting.FillV1, txIndex, logIndex uint) (bool, error) {
	addresses := []common.Address{common.HexToAddress(fill.Token.Address), common.HexToAddress(fill.Settlement.Address)}
	emitter := strings.ToLower(fill.ActualEmitter)
	emitterQueried := address.MatchString(emitter) && common.HexToAddress(emitter) != pool
	if emitterQueried {
		addresses = append(addresses, common.HexToAddress(emitter))
	}
	logs, err := a.rpc.FilterLogs(ctx, ethereum.FilterQuery{BlockHash: &blockHash, Addresses: addresses})
	if err != nil {
		return false, err
	}
	poolWord := common.LeftPadBytes(pool.Bytes(), 32)
	found := false
	for _, log := range logs {
		if log.BlockHash != blockHash {
			return false, delivery.ErrIntegrity
		}
		if log.TxIndex == txIndex && log.Index == logIndex {
			found = true
			continue
		}
		if log.TxIndex < txIndex || log.TxIndex == txIndex && log.Index < logIndex {
			continue
		}
		touches := false
		switch {
		case emitterQueried && log.Address == common.HexToAddress(emitter):
			// Informational emitter events (Swap, router trade records)
			// change nothing; only a reserve sync rewrites the reserves
			// without moving tokens.
			touches = len(log.Topics) >= 2 && log.Topics[0] == poolStateUpdatedTopic && bytes.Equal(log.Topics[1].Bytes(), poolWord)
		case len(log.Topics) == 3 && log.Topics[0] == transferTopic:
			touches = bytes.Equal(log.Topics[1].Bytes(), poolWord) || bytes.Equal(log.Topics[2].Bytes(), poolWord)
		}
		if touches {
			return false, nil
		}
	}
	if emitterQueried && !found {
		return false, delivery.ErrIntegrity
	}
	return true, nil
}
