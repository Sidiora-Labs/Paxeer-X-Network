package types

const (
	// ModuleName is the module name and the name of the account that holds
	// every token's custody, escrow and fee accruals.
	ModuleName = "spot"

	// StoreKey is the primary module store key.
	StoreKey = ModuleName

	// PECORAddress is the market and swap rail.
	PECORAddress = "0x0000000000000000000000000000000000001021"

	// PECOROrdersAddress is the limit order rail.
	PECOROrdersAddress = "0x0000000000000000000000000000000000001022"

	// PECORStopOrdersAddress is the stop-limit and stop-loss order rail.
	PECORStopOrdersAddress = "0x0000000000000000000000000000000000001023"

	// PECORQuoterAddress is the read-only quoter.
	PECORQuoterAddress = "0x0000000000000000000000000000000000001024"

	// PECORVaultAddress is the liquidity vault.
	PECORVaultAddress = "0x0000000000000000000000000000000000001025"
)

var (
	ParamsKey             = []byte{0x01}
	BookPrefix            = []byte{0x02}
	EscrowPrefix          = []byte{0x03}
	FeeAccrualPrefix      = []byte{0x04}
	NextOrderIDKey        = []byte{0x05}
	LimitOrderPrefix      = []byte{0x06}
	StopLimitOrderPrefix  = []byte{0x07}
	UserOrderPrefix       = []byte{0x08}
	ActiveLimitPrefix     = []byte{0x09}
	ActiveStopLimitPrefix = []byte{0x0A}
	ActiveStopLossPrefix  = []byte{0x0B}
	TwapSamplePrefix      = []byte{0x0C}
	TwapRingPrefix        = []byte{0x0D}
	VolumePrefix          = []byte{0x0E}
	StopLossOrderPrefix   = []byte{0x0F}
	OrderMinOutputPrefix  = []byte{0x10}
	ExpiryIndexPrefix     = []byte{0x11}
	CursorPrefix          = []byte{0x12}
	FillBackoffPrefix     = []byte{0x13}
	ActiveCountPrefix     = []byte{0x14}
)
