package types

import (
	"fmt"
	"math/big"
	"strings"

	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	"github.com/ethereum/go-ethereum/common"
)

// MaxProtocolFeeBps is ProtocolConfig.setProtocolFeeBps's bound (50%).
const MaxProtocolFeeBps = 5_000

// MaxFeeWeight is ProtocolConfig.setFeeWeights's bound on the decay rate and
// the volatility weight.
const MaxFeeWeight = 10_000

// maxUint112 is ProtocolConfig.setVirtualDefaults's bound on each virtual
// reserve default.
var maxUint112 = new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 112), big.NewInt(1))

// Params are KindleLaunch's ProtocolConfig fields. QuoteToken is the USDL
// ERC20 contract address; QuoteDenom is the bank denom that played USDL.
// Params change only through UpdateParams, which accepts the governance module
// account alone.
type Params struct {
	QuoteDenom          string  `json:"quote_denom"`
	VirtualQuoteDefault sdk.Int `json:"virtual_quote_default"`
	VirtualTokenDefault sdk.Int `json:"virtual_token_default"`
	MinFeeBps           uint64  `json:"min_fee_bps"`
	MaxFeeBps           uint64  `json:"max_fee_bps"`
	BaseFeeBps          uint64  `json:"base_fee_bps"`
	ProtocolFeeBps      uint64  `json:"protocol_fee_bps"`
	FeeDecayRate        uint64  `json:"fee_decay_rate"`
	VolatilityWeight    uint64  `json:"volatility_weight"`
	ConcentrationWeight uint64  `json:"concentration_weight"`
	CreationFee         sdk.Int `json:"creation_fee"`
	QuoteToken          string  `json:"quote_token"`
}

// DefaultQuoteDenom is the bank base denom of USDL on Paxeer.
const DefaultQuoteDenom = "uusdl"

// DefaultQuoteToken is the USDL ERC20 contract address.
const DefaultQuoteToken = "0x85FcD13735F4309833A503EE804ea32395851479"

// DefaultParams are ProtocolConfig.initialize's values.
func DefaultParams() Params {
	return Params{
		QuoteDenom:          DefaultQuoteDenom,
		VirtualQuoteDefault: sdk.NewInt(10_000_000_000),
		VirtualTokenDefault: sdk.NewInt(1_000_000_000_000_000),
		MinFeeBps:           10,
		MaxFeeBps:           300,
		BaseFeeBps:          30,
		ProtocolFeeBps:      1_000,
		FeeDecayRate:        500,
		VolatilityWeight:    100,
		ConcentrationWeight: 0,
		CreationFee:         sdk.NewInt(1_000_000),
		QuoteToken:          DefaultQuoteToken,
	}
}

func (p Params) Validate() error {
	if err := sdk.ValidateDenom(p.QuoteDenom); err != nil {
		return fmt.Errorf("%w: quote denom: %v", ErrInvalidParams, err)
	}
	if len(p.QuoteToken) != 42 || !strings.HasPrefix(p.QuoteToken, "0x") || !common.IsHexAddress(p.QuoteToken) {
		return fmt.Errorf("%w: quote token %q is not a 0x-prefixed 20-byte hex address", ErrInvalidParams, p.QuoteToken)
	}
	if common.HexToAddress(p.QuoteToken) == (common.Address{}) {
		return fmt.Errorf("%w: quote token is the zero address", ErrInvalidParams)
	}
	for _, value := range []sdk.Int{p.VirtualQuoteDefault, p.VirtualTokenDefault, p.CreationFee} {
		if value.IsNil() || value.IsNegative() || fits(value.BigInt()) != nil {
			return fmt.Errorf("%w: reserve defaults and creation fee must be uint256 integers", ErrInvalidParams)
		}
	}
	if !p.VirtualQuoteDefault.IsPositive() || !p.VirtualTokenDefault.IsPositive() {
		return fmt.Errorf("%w: virtual reserves must be positive", ErrInvalidParams)
	}
	if p.VirtualQuoteDefault.BigInt().Cmp(maxUint112) > 0 || p.VirtualTokenDefault.BigInt().Cmp(maxUint112) > 0 {
		return fmt.Errorf("%w: virtual reserves must fit uint112", ErrInvalidParams)
	}
	if p.MinFeeBps > p.MaxFeeBps || p.MaxFeeBps >= BpsDenominator {
		return fmt.Errorf("%w: fee bounds must satisfy min <= max < 10000", ErrInvalidParams)
	}
	if p.BaseFeeBps < p.MinFeeBps || p.BaseFeeBps > p.MaxFeeBps {
		return fmt.Errorf("%w: base fee must satisfy min <= base <= max", ErrInvalidParams)
	}
	if p.ProtocolFeeBps > MaxProtocolFeeBps {
		return fmt.Errorf("%w: protocol fee above 5000 bps", ErrInvalidParams)
	}
	if p.FeeDecayRate > MaxFeeWeight || p.VolatilityWeight > MaxFeeWeight {
		return fmt.Errorf("%w: fee decay rate and volatility weight must be at most 10000", ErrInvalidParams)
	}
	if p.ConcentrationWeight != 0 {
		return fmt.Errorf("%w: concentration weight must be 0", ErrInvalidParams)
	}
	return nil
}

// FeeInputs returns the ProtocolConfig side of FeeLib.calculateDynamicFee.
func (p Params) FeeInputs(poolAgeSeconds, volatility *big.Int) FeeInputs {
	return FeeInputs{
		BaseFee:             new(big.Int).SetUint64(p.BaseFeeBps),
		MinFee:              new(big.Int).SetUint64(p.MinFeeBps),
		MaxFee:              new(big.Int).SetUint64(p.MaxFeeBps),
		FeeDecayRate:        new(big.Int).SetUint64(p.FeeDecayRate),
		VolatilityWeight:    new(big.Int).SetUint64(p.VolatilityWeight),
		ConcentrationWeight: new(big.Int).SetUint64(p.ConcentrationWeight),
		PoolAgeSeconds:      poolAgeSeconds,
		Volatility:          volatility,
		// Concentration is 0 on chain, as in SidioraPool._calculateFee.
		TopHolderBps: new(big.Int),
	}
}
