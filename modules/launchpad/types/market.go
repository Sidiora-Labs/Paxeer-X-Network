package types

import (
	"fmt"
	"math/big"
	"strings"
	"unicode/utf8"

	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	"github.com/ethereum/go-ethereum/common"
)

// FeeStrategy is the fee-rights holder's choice of what accumulated buy fees
// do, as the SidioraNFT strategy byte.
type FeeStrategy uint8

const (
	FeeStrategyClaim FeeStrategy = iota
	FeeStrategyBurn
	FeeStrategyAirdrop
	FeeStrategyLpRewards
)

func (s FeeStrategy) Validate() error {
	if s > FeeStrategyLpRewards {
		return fmt.Errorf("%w: %d", ErrInvalidStrategy, s)
	}
	return nil
}

const (
	MaxNameLength   = 64
	MaxSymbolLength = 16
)

// ValidateNameSymbol bounds the ERC20 metadata of a launched token.
func ValidateNameSymbol(name, symbol string) error {
	for _, field := range [][2]string{{"name", name}, {"symbol", symbol}} {
		value := field[1]
		if strings.TrimSpace(value) == "" || value != strings.TrimSpace(value) || !utf8.ValidString(value) {
			return fmt.Errorf("%w: %s must be non-blank UTF-8 without surrounding space", ErrInvalidMarket, field[0])
		}
		for _, r := range value {
			if r < 0x20 || r == 0x7f {
				return fmt.Errorf("%w: %s contains a control character", ErrInvalidMarket, field[0])
			}
		}
	}
	if len(name) > MaxNameLength || len(symbol) > MaxSymbolLength {
		return fmt.Errorf("%w: name is at most %d bytes and symbol at most %d", ErrInvalidMarket, MaxNameLength, MaxSymbolLength)
	}
	return nil
}

// Generation is the Launch-x contract generation a market came from.
// GenerationNative markets were created by the module and carry no pool
// contract fields; generation 1 and 2 markets are SidioraPool records.
type Generation uint8

const (
	GenerationNative Generation = iota
	Generation1
	Generation2
)

// Market is one launched token and its virtual-reserve curve: SidioraPool's
// state plus the FeeAccumulator and SidioraNFT fields that belong to it.
// Addresses are bech32 bank accounts; Pointer is the ERC20 pointer's hex
// address.
type Market struct {
	Denom                string      `json:"denom"`
	Index                uint64      `json:"index"`
	Name                 string      `json:"name"`
	Symbol               string      `json:"symbol"`
	Pointer              string      `json:"pointer"`
	Creator              string      `json:"creator"`
	Guardian             string      `json:"guardian"`
	FeeRightsHolder      string      `json:"fee_rights_holder"`
	FeeStrategy          FeeStrategy `json:"fee_strategy"`
	Paused               bool        `json:"paused"`
	TotalSupply          sdk.Int     `json:"total_supply"`
	VirtualQuoteReserve  sdk.Int     `json:"virtual_quote_reserve"`
	RealQuoteBalance     sdk.Int     `json:"real_quote_balance"`
	TokenReserve         sdk.Int     `json:"token_reserve"`
	CreationTime         int64       `json:"creation_time"`
	CumulativeVolume     sdk.Int     `json:"cumulative_volume"`
	PriceSnapshots       []sdk.Int   `json:"price_snapshots"`
	SnapshotHeights      []uint64    `json:"snapshot_heights"`
	SnapshotIndex        uint64      `json:"snapshot_index"`
	SnapshotCount        uint64      `json:"snapshot_count"`
	AccumulatedQuoteFees sdk.Int     `json:"accumulated_quote_fees"`
	AccumulatedTokenFees sdk.Int     `json:"accumulated_token_fees"`
	AccumulatedFees      sdk.Int     `json:"accumulated_fees"`
	AirdropEpoch         uint64      `json:"airdrop_epoch"`
	AirdropBalance       sdk.Int     `json:"airdrop_balance"`

	Generation                Generation `json:"generation"`
	PoolAddress               string     `json:"pool_address"`
	TokenAddress              string     `json:"token_address"`
	Optical                   string     `json:"optical"`
	Factory                   string     `json:"factory"`
	FeeAccumulator            string     `json:"fee_accumulator"`
	ProtocolConfig            string     `json:"protocol_config"`
	EventEmitter              string     `json:"event_emitter"`
	PendingGuardian           string     `json:"pending_guardian"`
	LastSnapshotTimestamp     int64      `json:"last_snapshot_timestamp"`
	CumulativeBuyQuoteVolume  sdk.Int    `json:"cumulative_buy_quote_volume"`
	CumulativeSellQuoteVolume sdk.Int    `json:"cumulative_sell_quote_volume"`
	FeeRightCollection        string     `json:"fee_right_collection"`
	FeeRightID                uint64     `json:"fee_right_id"`
}

// Snapshots returns the ring as a fixed array.
func (m Market) Snapshots() [SnapshotSlots]*big.Int {
	var out [SnapshotSlots]*big.Int
	for i := range out {
		out[i] = new(big.Int)
		if i < len(m.PriceSnapshots) && !m.PriceSnapshots[i].IsNil() {
			out[i] = m.PriceSnapshots[i].BigInt()
		}
	}
	return out
}

// EffectiveQuote is virtualQuoteReserve + realQuoteBalance.
func (m Market) EffectiveQuote() *big.Int {
	return new(big.Int).Add(m.VirtualQuoteReserve.BigInt(), m.RealQuoteBalance.BigInt())
}

// Price is SidioraPool._currentPrice: 0 without a token reserve.
func (m Market) Price() (*big.Int, error) {
	if m.TokenReserve.IsZero() {
		return new(big.Int), nil
	}
	return GetPrice(m.EffectiveQuote(), m.TokenReserve.BigInt())
}

func nonNegative(name string, value sdk.Int) error {
	if value.IsNil() || value.IsNegative() {
		return fmt.Errorf("%w: %s must be a non-negative integer", ErrInvalidMarket, name)
	}
	return fits(value.BigInt())
}

func (m Market) Validate() error {
	if err := sdk.ValidateDenom(m.Denom); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidMarket, err)
	}
	if err := ValidateNameSymbol(m.Name, m.Symbol); err != nil {
		return err
	}
	if m.Index == 0 {
		return fmt.Errorf("%w: index starts at 1", ErrInvalidMarket)
	}
	for _, addr := range []string{m.Creator, m.Guardian, m.FeeRightsHolder} {
		if _, err := sdk.AccAddressFromBech32(addr); err != nil {
			return fmt.Errorf("%w: account %q: %v", ErrInvalidMarket, addr, err)
		}
	}
	if err := m.FeeStrategy.Validate(); err != nil {
		return err
	}
	amounts := []sdk.Int{m.TotalSupply, m.VirtualQuoteReserve, m.RealQuoteBalance, m.TokenReserve, m.CumulativeVolume,
		m.AccumulatedQuoteFees, m.AccumulatedTokenFees, m.AccumulatedFees, m.AirdropBalance}
	for _, value := range amounts {
		if err := nonNegative("amount", value); err != nil {
			return err
		}
	}
	if len(m.PriceSnapshots) != SnapshotSlots || m.SnapshotIndex >= SnapshotSlots || m.SnapshotCount > SnapshotSlots {
		return fmt.Errorf("%w: snapshot ring", ErrInvalidMarket)
	}
	if len(m.SnapshotHeights) != 0 && len(m.SnapshotHeights) != SnapshotSlots {
		return fmt.Errorf("%w: snapshot heights", ErrInvalidMarket)
	}
	for _, price := range m.PriceSnapshots {
		if err := nonNegative("price snapshot", price); err != nil {
			return err
		}
	}
	if m.TokenReserve.GT(m.TotalSupply) {
		return fmt.Errorf("%w: token reserve above total supply", ErrInvalidMarket)
	}
	return m.validatePool()
}

// contractAddress requires an EIP-55 checksummed 20-byte hex address, nonzero
// unless allowZero.
func contractAddress(name, value string, allowZero bool) error {
	if !strings.HasPrefix(value, "0x") || !common.IsHexAddress(value) || common.HexToAddress(value).Hex() != value {
		return fmt.Errorf("%w: %s %q must be a checksummed hex address", ErrInvalidMarket, name, value)
	}
	if !allowZero && common.HexToAddress(value) == (common.Address{}) {
		return fmt.Errorf("%w: %s is the zero address", ErrInvalidMarket, name)
	}
	return nil
}

// validatePool checks the SidioraPool and fee-right fields: unset on a native
// market, complete on a generation 1 or 2 market.
func (m Market) validatePool() error {
	addresses := []struct {
		name, value string
		allowZero   bool
	}{
		{"pool address", m.PoolAddress, false},
		{"token address", m.TokenAddress, false},
		{"optical", m.Optical, true},
		{"factory", m.Factory, false},
		{"fee accumulator", m.FeeAccumulator, false},
		{"protocol config", m.ProtocolConfig, false},
		{"event emitter", m.EventEmitter, false},
		{"fee right collection", m.FeeRightCollection, false},
	}
	switch m.Generation {
	case GenerationNative:
		for _, addr := range addresses {
			if addr.value != "" {
				return fmt.Errorf("%w: native market sets %s", ErrInvalidMarket, addr.name)
			}
		}
		if m.PendingGuardian != "" || m.LastSnapshotTimestamp != 0 || !m.CumulativeBuyQuoteVolume.IsNil() ||
			!m.CumulativeSellQuoteVolume.IsNil() || m.FeeRightID != 0 {
			return fmt.Errorf("%w: native market sets pool fields", ErrInvalidMarket)
		}
		return nil
	case Generation1, Generation2:
	default:
		return fmt.Errorf("%w: generation %d", ErrInvalidMarket, m.Generation)
	}
	for _, addr := range addresses {
		if err := contractAddress(addr.name, addr.value, addr.allowZero); err != nil {
			return err
		}
	}
	if m.PendingGuardian != "" {
		if _, err := sdk.AccAddressFromBech32(m.PendingGuardian); err != nil {
			return fmt.Errorf("%w: pending guardian %q: %v", ErrInvalidMarket, m.PendingGuardian, err)
		}
	}
	if m.LastSnapshotTimestamp < 0 {
		return fmt.Errorf("%w: last snapshot timestamp is negative", ErrInvalidMarket)
	}
	for _, value := range []sdk.Int{m.CumulativeBuyQuoteVolume, m.CumulativeSellQuoteVolume} {
		if err := nonNegative("directional volume", value); err != nil {
			return err
		}
	}
	if m.FeeRightID == 0 {
		return fmt.Errorf("%w: fee right ids start at 1", ErrInvalidMarket)
	}
	return nil
}
