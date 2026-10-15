package types

import (
	"fmt"

	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	authtypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/auth/types"
	govtypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/gov/types"
	"github.com/ethereum/go-ethereum/common"
)

const (
	// BpsDenominator is one whole in basis points.
	BpsDenominator = 10_000

	// MaxFeeBps bounds swapFeeBps + tier1FeeBps + tier2FeeBps (PECOR MAX_FEE_BPS).
	MaxFeeBps = 200

	// MaxOrderFeeBps bounds every order fee and the order fee stack
	// (PECOROrders setPricingConfig).
	MaxOrderFeeBps = 200

	// MaxImpactBps bounds both impact scalars and every impact deduction
	// (PECOR MAX_IMPACT_BPS).
	MaxImpactBps = 500

	// DefaultMaxUtilizationBps is the utilization cap of a token without its
	// own cap.
	DefaultMaxUtilizationBps = 10_000

	// OpeningTokenMaxUtilizationBps is the utilization cap of the opening
	// tokens.
	OpeningTokenMaxUtilizationBps = 9_000

	// MaxTokenDecimals is the PECORVault decimals bound of a registered token.
	MaxTokenDecimals = 36

	// USDLAddress is the USDL ERC20 contract, the only stable token.
	USDLAddress = "0x85FcD13735F4309833A503EE804ea32395851479"

	// OpeningTokenDecimals is the pinned decimals of USDL, PAX and SID.
	OpeningTokenDecimals = 6

	// PAXMarketID and SIDMarketID are the pricefeed markets of the opening
	// non-stable tokens.
	PAXMarketID = 11
	SIDMarketID = 12
)

// ParamsError is a refusal of Params.Validate, returned as a module error of
// MsgUpdateParams and never as EVM revert data.
type ParamsError string

func (e ParamsError) Error() string { return string(e) }

const (
	ErrParamsFeeTooHigh             ParamsError = "FeeTooHigh()"
	ErrParamsTier2BelowTier1        ParamsError = "Tier2BelowTier1()"
	ErrParamsScalarTooHigh          ParamsError = "ScalarTooHigh()"
	ErrParamsFeeExceedsMax          ParamsError = "FeeExceedsMax()"
	ErrParamsInvalidTWAPPeriod      ParamsError = "InvalidTWAPPeriod()"
	ErrParamsZeroAddress            ParamsError = "ZeroAddress()"
	ErrParamsTokenAlreadyRegistered ParamsError = "TokenAlreadyRegistered()"
	ErrParamsUnsupportedToken       ParamsError = "UnsupportedToken()"
	ErrParamsInvalid                ParamsError = "invalid spot params"
)

// TokenKind is the custody kind of a registered token.
type TokenKind uint8

const (
	// TokenKindUnspecified is the zero value and is never valid.
	TokenKindUnspecified TokenKind = iota
	// TokenKindERC20 is an ERC20 balance of the spot module EVM address.
	TokenKindERC20
	// TokenKindNative is a bank balance of the spot module account, addressed
	// by its native pointer.
	TokenKindNative
)

// TokenParams is one registered token of the vault.
type TokenParams struct {
	Address           common.Address `json:"address"`
	Kind              TokenKind      `json:"kind"`
	IsStablecoin      bool           `json:"is_stablecoin"`
	Decimals          uint32         `json:"decimals"`
	MarketID          uint64         `json:"market_id"`
	Paused            bool           `json:"paused"`
	MaxUtilizationBps uint64         `json:"max_utilization_bps"`
}

// Params are the PECOR engine, order engine and vault settings. They change
// only through MsgUpdateParams, which accepts the governance module account
// alone.
type Params struct {
	SwapFeeBps                uint64           `json:"swap_fee_bps"`
	Tier1FeeBps               uint64           `json:"tier1_fee_bps"`
	Tier2FeeBps               uint64           `json:"tier2_fee_bps"`
	PriceImpactEnabled        bool             `json:"price_impact_enabled"`
	PriceImpactScalarBps      uint64           `json:"price_impact_scalar_bps"`
	OrderFeeBps               uint64           `json:"order_fee_bps"`
	OrderTier1FeeBps          uint64           `json:"order_tier1_fee_bps"`
	OrderTier2FeeBps          uint64           `json:"order_tier2_fee_bps"`
	OrderPriceImpactEnabled   bool             `json:"order_price_impact_enabled"`
	OrderPriceImpactScalarBps uint64           `json:"order_price_impact_scalar_bps"`
	FeeCollector              common.Address   `json:"fee_collector"`
	SwapPaused                bool             `json:"swap_paused"`
	OrdersPaused              bool             `json:"orders_paused"`
	KeeperOpen                bool             `json:"keeper_open"`
	Keepers                   []common.Address `json:"keepers"`
	FillQualityTwapPeriod     uint64           `json:"fill_quality_twap_period"`
	TwapSanityEnabled         bool             `json:"twap_sanity_enabled"`
	TwapSanityPeriod          uint64           `json:"twap_sanity_period"`
	MaxTwapDeviationBps       uint64           `json:"max_twap_deviation_bps"`
	TwapMaxPeriod             uint64           `json:"twap_max_period"`
	MinOrderNotional          sdk.Int          `json:"min_order_notional"`
	MaxActiveOrdersPerUser    uint64           `json:"max_active_orders_per_user"`
	MaxExpiriesPerBlock       uint64           `json:"max_expiries_per_block"`
	ActivationScanPerBlock    uint64           `json:"activation_scan_per_block"`
	FillScanPerBlock          uint64           `json:"fill_scan_per_block"`
	MaxFillsPerBlock          uint64           `json:"max_fills_per_block"`
	MaxFillBackoffBlocks      uint64           `json:"max_fill_backoff_blocks"`
	Tokens                    []TokenParams    `json:"tokens"`
}

// GovernanceEVMAddress is the EVM address of the governance module account.
func GovernanceEVMAddress() common.Address {
	return common.BytesToAddress(authtypes.NewModuleAddress(govtypes.ModuleName))
}

// DefaultParams are the spot parameter sheet values with no registered
// token; InitV6120 registers the opening tokens.
func DefaultParams() Params {
	return Params{
		SwapFeeBps:                7,
		Tier1FeeBps:               0,
		Tier2FeeBps:               0,
		PriceImpactEnabled:        true,
		PriceImpactScalarBps:      100,
		OrderFeeBps:               4,
		OrderTier1FeeBps:          0,
		OrderTier2FeeBps:          0,
		OrderPriceImpactEnabled:   true,
		OrderPriceImpactScalarBps: 100,
		FeeCollector:              GovernanceEVMAddress(),
		SwapPaused:                false,
		OrdersPaused:              false,
		KeeperOpen:                true,
		Keepers:                   []common.Address{},
		FillQualityTwapPeriod:     0,
		TwapSanityEnabled:         false,
		TwapSanityPeriod:          300,
		MaxTwapDeviationBps:       200,
		TwapMaxPeriod:             86_400,
		MinOrderNotional:          sdk.NewIntWithDecimal(10, 18),
		MaxActiveOrdersPerUser:    100,
		MaxExpiriesPerBlock:       200,
		ActivationScanPerBlock:    500,
		FillScanPerBlock:          500,
		MaxFillsPerBlock:          200,
		MaxFillBackoffBlocks:      256,
		Tokens:                    []TokenParams{},
	}
}

// OpeningTokens are USDL, PAX and SID in registration order, given the
// native pointers of uhpx and the SID denom.
func OpeningTokens(paxPointer, sidPointer common.Address) []TokenParams {
	return []TokenParams{
		{
			Address:           common.HexToAddress(USDLAddress),
			Kind:              TokenKindERC20,
			IsStablecoin:      true,
			Decimals:          OpeningTokenDecimals,
			MaxUtilizationBps: OpeningTokenMaxUtilizationBps,
		},
		{
			Address:           paxPointer,
			Kind:              TokenKindNative,
			Decimals:          OpeningTokenDecimals,
			MarketID:          PAXMarketID,
			MaxUtilizationBps: OpeningTokenMaxUtilizationBps,
		},
		{
			Address:           sidPointer,
			Kind:              TokenKindNative,
			Decimals:          OpeningTokenDecimals,
			MarketID:          SIDMarketID,
			MaxUtilizationBps: OpeningTokenMaxUtilizationBps,
		},
	}
}

func (p Params) Validate() error {
	// PECOR setTieredFees and setSwapFee; each term is bounded first so the
	// sum cannot wrap.
	if p.Tier2FeeBps < p.Tier1FeeBps {
		return fmt.Errorf("%w: tier2FeeBps %d below tier1FeeBps %d", ErrParamsTier2BelowTier1, p.Tier2FeeBps, p.Tier1FeeBps)
	}
	if p.SwapFeeBps > MaxFeeBps || p.Tier1FeeBps > MaxFeeBps || p.Tier2FeeBps > MaxFeeBps ||
		p.SwapFeeBps+p.Tier1FeeBps+p.Tier2FeeBps > MaxFeeBps {
		return fmt.Errorf("%w: swap fee stack above %d bps", ErrParamsFeeTooHigh, MaxFeeBps)
	}
	if p.PriceImpactScalarBps > MaxImpactBps {
		return fmt.Errorf("%w: priceImpactScalarBps above %d", ErrParamsScalarTooHigh, MaxImpactBps)
	}
	// PECOROrders setPricingConfig.
	if p.OrderTier2FeeBps < p.OrderTier1FeeBps {
		return fmt.Errorf("%w: orderTier2FeeBps %d below orderTier1FeeBps %d", ErrParamsTier2BelowTier1, p.OrderTier2FeeBps, p.OrderTier1FeeBps)
	}
	if p.OrderFeeBps > MaxOrderFeeBps || p.OrderTier1FeeBps > MaxOrderFeeBps || p.OrderTier2FeeBps > MaxOrderFeeBps ||
		p.OrderFeeBps+p.OrderTier1FeeBps+p.OrderTier2FeeBps > MaxOrderFeeBps {
		return fmt.Errorf("%w: order fee stack above %d bps", ErrParamsFeeExceedsMax, MaxOrderFeeBps)
	}
	if p.OrderPriceImpactScalarBps > MaxImpactBps {
		return fmt.Errorf("%w: orderPriceImpactScalarBps above %d", ErrParamsScalarTooHigh, MaxImpactBps)
	}
	if p.FillQualityTwapPeriod > p.TwapMaxPeriod || p.TwapSanityPeriod > p.TwapMaxPeriod {
		return fmt.Errorf("%w: TWAP period above twapMaxPeriod %d", ErrParamsInvalidTWAPPeriod, p.TwapMaxPeriod)
	}
	if p.FeeCollector == (common.Address{}) {
		return fmt.Errorf("%w: feeCollector", ErrParamsZeroAddress)
	}
	for i, keeper := range p.Keepers {
		if keeper == (common.Address{}) {
			return fmt.Errorf("%w: keeper %d", ErrParamsZeroAddress, i)
		}
	}
	if p.MinOrderNotional.IsNil() || p.MinOrderNotional.IsNegative() || p.MinOrderNotional.BigInt().BitLen() > 256 {
		return fmt.Errorf("%w: minOrderNotional must be a uint256 integer", ErrParamsInvalid)
	}
	if p.MaxActiveOrdersPerUser == 0 {
		return fmt.Errorf("%w: zero maxActiveOrdersPerUser", ErrParamsInvalid)
	}
	if p.MaxExpiriesPerBlock == 0 || p.ActivationScanPerBlock == 0 || p.FillScanPerBlock == 0 || p.MaxFillsPerBlock == 0 {
		return fmt.Errorf("%w: zero EndBlock budget", ErrParamsInvalid)
	}
	if p.MaxFillBackoffBlocks == 0 {
		return fmt.Errorf("%w: zero maxFillBackoffBlocks", ErrParamsInvalid)
	}
	seen := make(map[common.Address]struct{}, len(p.Tokens))
	for _, token := range p.Tokens {
		if token.Address == (common.Address{}) {
			return fmt.Errorf("%w: token address", ErrParamsZeroAddress)
		}
		if _, ok := seen[token.Address]; ok {
			return fmt.Errorf("%w: %s", ErrParamsTokenAlreadyRegistered, token.Address.Hex())
		}
		seen[token.Address] = struct{}{}
		if token.Kind != TokenKindERC20 && token.Kind != TokenKindNative {
			return fmt.Errorf("%w: token %s has no kind", ErrParamsInvalid, token.Address.Hex())
		}
		if token.Decimals > MaxTokenDecimals {
			return fmt.Errorf("%w: token %s decimals above %d", ErrParamsUnsupportedToken, token.Address.Hex(), MaxTokenDecimals)
		}
		if token.MaxUtilizationBps == 0 || token.MaxUtilizationBps > BpsDenominator {
			return fmt.Errorf("%w: token %s maxUtilizationBps must be in [1, %d]", ErrParamsInvalid, token.Address.Hex(), BpsDenominator)
		}
	}
	return nil
}
