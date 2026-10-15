package types

import (
	"github.com/ethereum/go-ethereum/common"

	evmtypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/types"
	pricefeedkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/modules/pricefeed/keeper"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
)

// EVMKeeper runs the nested USDL ERC20 calls of the collateral ledger and
// reads and writes the synthetic receipts that carry the venue's logs. The
// module declares no bank keeper and holds no bank coins: USDL is the ERC20
// of Params.CollateralToken.
type EVMKeeper interface {
	CallEVM(ctx sdk.Context, from common.Address, to *common.Address, val *sdk.Int, data []byte) ([]byte, error)
	StaticCallEVM(ctx sdk.Context, from sdk.AccAddress, to *common.Address, data []byte) ([]byte, error)
	SetTransientReceipt(ctx sdk.Context, txHash common.Hash, receipt *evmtypes.Receipt) error
	GetTransientReceipt(ctx sdk.Context, txHash common.Hash, txIndex uint64) (*evmtypes.Receipt, error)
}

// PricefeedKeeper serves the index and mark of a market with their timestamp
// and freshness; the venue refuses trading and liquidation while Fresh is
// false.
type PricefeedKeeper interface {
	GetPrice(ctx sdk.Context, marketId uint64) (pricefeedkeeper.Price, error)
}
