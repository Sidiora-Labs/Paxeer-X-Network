package keeper

import (
	"math/big"

	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	"github.com/ethereum/go-ethereum/common"
)

// CustodyToken exposes custodyToken to the external test package.
type CustodyToken = custodyToken

// TokenOf exposes tokenOf to the external test package.
func (k *Keeper) TokenOf(ctx sdk.Context, token common.Address) (CustodyToken, error) {
	return k.tokenOf(ctx, token)
}

// PullTokens exposes pullTokens to the external test package.
func (k *Keeper) PullTokens(ctx sdk.Context, rail, token, from common.Address, amount sdk.Int) (sdk.Context, sdk.Int, error) {
	return k.pullTokens(ctx, rail, token, from, amount)
}

// PushTokens exposes pushTokens to the external test package.
func (k *Keeper) PushTokens(ctx sdk.Context, token, to common.Address, amount sdk.Int) (sdk.Context, error) {
	return k.pushTokens(ctx, token, to, amount)
}

// PullNative exposes pullNative to the external test package.
func (k *Keeper) PullNative(ctx sdk.Context, rail common.Address, value *big.Int) (sdk.Context, sdk.Int, error) {
	return k.pullNative(ctx, rail, value)
}

// RunOf exposes runOf to the external test package.
func RunOf(ctx sdk.Context) *Run { return runOf(ctx) }

// Resume exposes resume to the external test package.
func (r *Run) Resume(ctx sdk.Context) (sdk.Context, error) { return r.resume(ctx) }
