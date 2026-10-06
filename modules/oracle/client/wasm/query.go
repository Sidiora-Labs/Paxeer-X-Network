package wasm

import (
	oraclekeeper "github.com/Sidiora-Labs/Paxeer-X-Network/modules/oracle/keeper"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/oracle/types"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
)

type OracleWasmQueryHandler struct {
	oracleKeeper oraclekeeper.Keeper
}

func NewOracleWasmQueryHandler(keeper *oraclekeeper.Keeper) *OracleWasmQueryHandler {
	return &OracleWasmQueryHandler{
		oracleKeeper: *keeper,
	}
}

func (handler OracleWasmQueryHandler) GetExchangeRates(ctx sdk.Context) (*types.QueryExchangeRatesResponse, error) {
	querier := oraclekeeper.NewQuerier(handler.oracleKeeper)
	c := sdk.WrapSDKContext(ctx)
	return querier.ExchangeRates(c, &types.QueryExchangeRatesRequest{})
}

func (handler OracleWasmQueryHandler) GetOracleTwaps(ctx sdk.Context, req *types.QueryTwapsRequest) (*types.QueryTwapsResponse, error) {
	querier := oraclekeeper.NewQuerier(handler.oracleKeeper)
	c := sdk.WrapSDKContext(ctx)
	return querier.Twaps(c, req)
}
