package pricefeed

import (
	"encoding/hex"
	"errors"
	"math/big"

	pricefeedkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/modules/pricefeed/keeper"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/pricefeed/types"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
)

type PriceView struct {
	IndexPrice  *big.Int
	MarkPrice   *big.Int
	TimestampMs uint64
	Height      int64
	Sources     uint32
	Fresh       bool
}

type MarketView struct {
	MarketId        uint64
	Symbol          string
	ChainflowSymbol string
	MinPrice        *big.Int
	MaxPrice        *big.Int
	Enabled         bool
}

type ObservationView struct {
	Sequence          uint64
	IndexPrice        *big.Int
	MarkPrice         *big.Int
	SourceTimestampMs uint64
	Height            int64
}

type ParamsView struct {
	StalenessWindowMs            uint64
	MaxClockSkewMs               uint64
	MinSources                   uint32
	MaxDivergenceBps             uint32
	MaxDeviationBps              uint32
	MaxObservationsPerSubmission uint32
	MaxSubmissionBytes           uint32
	MarkEmaTauMs                 uint64
	MaxMarkBasisBps              uint32
	MaxPriceAgeMs                uint64
}

func marketID(value *big.Int) (uint64, error) {
	if value == nil || !value.IsUint64() {
		return 0, types.ErrUnknownMarket.Wrap("market id is outside uint64")
	}
	return value.Uint64(), nil
}

func priceView(price pricefeedkeeper.Price) PriceView {
	return PriceView{IndexPrice: price.Index.BigInt(), MarkPrice: price.Mark.BigInt(), TimestampMs: price.TimestampMs,
		Height: price.Height, Sources: price.Sources, Fresh: price.Fresh}
}

func (p PrecompileExecutor) getPrice(ctx sdk.Context, method *abi.Method, args []interface{}) ([]byte, error) {
	id, err := marketID(args[0].(*big.Int))
	if err != nil {
		return nil, err
	}
	price, err := p.keeper.GetPrice(ctx, id)
	if err != nil {
		return nil, err
	}
	return method.Outputs.Pack(priceView(price))
}

func (p PrecompileExecutor) getPriceBySymbol(ctx sdk.Context, method *abi.Method, args []interface{}) ([]byte, error) {
	price, err := p.keeper.GetPriceBySymbol(ctx, args[0].(string))
	if err != nil {
		return nil, err
	}
	return method.Outputs.Pack(priceView(price))
}

func (p PrecompileExecutor) getPrices(ctx sdk.Context, method *abi.Method, args []interface{}) ([]byte, error) {
	ids := args[0].([]*big.Int)
	if len(ids) > 64 {
		return nil, errors.New("LXP_PRICEFEED_BATCH_TOO_LARGE: getPrices takes at most 64 market ids")
	}
	prices := make([]PriceView, len(ids))
	for i, value := range ids {
		id, err := marketID(value)
		if err != nil {
			return nil, err
		}
		price, err := p.keeper.GetPrice(ctx, id)
		if err != nil {
			return nil, err
		}
		prices[i] = priceView(price)
	}
	return method.Outputs.Pack(prices)
}

func (p PrecompileExecutor) market(ctx sdk.Context, id uint64) (types.Market, error) {
	for _, market := range p.keeper.GetParams(ctx).Markets {
		if market.ID == id {
			return market, nil
		}
	}
	return types.Market{}, types.ErrUnknownMarket.Wrapf("market id %d", id)
}

func (p PrecompileExecutor) getMarket(ctx sdk.Context, method *abi.Method, args []interface{}) ([]byte, error) {
	id, err := marketID(args[0].(*big.Int))
	if err != nil {
		return nil, err
	}
	market, err := p.market(ctx, id)
	if err != nil {
		return nil, err
	}
	return method.Outputs.Pack(MarketView{MarketId: market.ID, Symbol: market.Symbol, ChainflowSymbol: market.ChainflowSymbol,
		MinPrice: market.MinPrice.BigInt(), MaxPrice: market.MaxPrice.BigInt(), Enabled: market.Enabled})
}

func (p PrecompileExecutor) marketIdOf(ctx sdk.Context, method *abi.Method, args []interface{}) ([]byte, error) {
	symbol := args[0].(string)
	for _, market := range p.keeper.GetParams(ctx).Markets {
		if market.Symbol == symbol {
			return method.Outputs.Pack(new(big.Int).SetUint64(market.ID))
		}
	}
	return nil, types.ErrUnknownMarket.Wrapf("market symbol %q", symbol)
}

func (p PrecompileExecutor) getObservation(ctx sdk.Context, method *abi.Method, args []interface{}) ([]byte, error) {
	id, err := marketID(args[0].(*big.Int))
	if err != nil {
		return nil, err
	}
	if _, err := p.market(ctx, id); err != nil {
		return nil, err
	}
	operator := sdk.ValAddress(args[1].(common.Address).Bytes())
	observation, found := p.keeper.GetObservation(ctx, id, operator)
	view := ObservationView{IndexPrice: new(big.Int), MarkPrice: new(big.Int)}
	if found {
		view = ObservationView{Sequence: observation.Sequence, IndexPrice: observation.IndexPrice.BigInt(),
			MarkPrice: observation.MarkPrice.BigInt(), SourceTimestampMs: observation.SourceTimestampMs, Height: observation.Height}
	}
	return method.Outputs.Pack(view)
}

func (p PrecompileExecutor) getFeedKey(ctx sdk.Context, method *abi.Method, args []interface{}) ([]byte, error) {
	key, found := p.keeper.GetFeedKey(ctx, sdk.ValAddress(args[0].(common.Address).Bytes()))
	if !found {
		return method.Outputs.Pack([]byte{}, common.Address{})
	}
	pubKey, err := hex.DecodeString(key.PubKey)
	if err != nil {
		return nil, types.ErrInvalidKey.Wrap(err.Error())
	}
	account, err := sdk.AccAddressFromBech32(key.FeedAccount)
	if err != nil {
		return nil, types.ErrInvalidKey.Wrap(err.Error())
	}
	return method.Outputs.Pack(pubKey, common.BytesToAddress(account))
}

func (p PrecompileExecutor) getParams(ctx sdk.Context, method *abi.Method) ([]byte, error) {
	params := p.keeper.GetParams(ctx)
	return method.Outputs.Pack(ParamsView{StalenessWindowMs: params.StalenessWindowMs, MaxClockSkewMs: params.MaxClockSkewMs,
		MinSources: params.MinSources, MaxDivergenceBps: params.MaxDivergenceBps, MaxDeviationBps: params.MaxDeviationBps,
		MaxObservationsPerSubmission: params.MaxObservationsPerSubmission, MaxSubmissionBytes: params.MaxSubmissionBytes,
		MarkEmaTauMs: params.MarkEmaTauMs, MaxMarkBasisBps: params.MaxMarkBasisBps, MaxPriceAgeMs: params.MaxPriceAgeMs})
}
