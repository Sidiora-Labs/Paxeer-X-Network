package types

import (
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
)

const (
	// AccountAddressPrefix is the bech32 prefix of every account of the
	// network (node/const.go:5).
	AccountAddressPrefix = "pax"
	// MaxObservationsPerMessage bounds the observations of one MsgSubmitPrices.
	MaxObservationsPerMessage = 64
	// MaxPriceDigits bounds the decimal digits of a price string.
	MaxPriceDigits = 39
)

var (
	_ sdk.Msg = &MsgSubmitPrices{}
	_ sdk.Msg = &MsgSetParams{}
)

// accountFromBech32 decodes a pax account address.
func accountFromBech32(address string) (sdk.AccAddress, error) {
	bz, err := sdk.GetFromBech32(address, AccountAddressPrefix)
	if err != nil {
		return nil, err
	}
	if err := sdk.VerifyAddressFormat(bz); err != nil {
		return nil, err
	}
	return sdk.AccAddress(bz), nil
}

// singleSigner returns the one signer of a message. An address that is not a
// pax account yields no signer, and ValidateBasic refuses it before any
// signature is checked.
func singleSigner(address string) []sdk.AccAddress {
	account, err := accountFromBech32(address)
	if err != nil {
		return []sdk.AccAddress{}
	}
	return []sdk.AccAddress{account}
}

func (m MsgSubmitPrices) GetSigners() []sdk.AccAddress { return singleSigner(m.Feeder) }

func (m MsgSetParams) GetSigners() []sdk.AccAddress { return singleSigner(m.Authority) }

func (m MsgSubmitPrices) ValidateBasic() error {
	if _, err := accountFromBech32(m.Feeder); err != nil {
		return ErrInvalidObservation.Wrapf("feeder: %v", err)
	}
	if len(m.Observations) == 0 {
		return ErrInvalidObservation.Wrap("no observations")
	}
	if len(m.Observations) > MaxObservationsPerMessage {
		return ErrTooManyObservations.Wrapf("%d observations, at most %d", len(m.Observations), MaxObservationsPerMessage)
	}
	seen := make(map[uint64]struct{}, len(m.Observations))
	for i, observation := range m.Observations {
		if _, ok := seen[observation.MarketId]; ok {
			return ErrDuplicateMarket.Wrapf("market %d", observation.MarketId)
		}
		seen[observation.MarketId] = struct{}{}
		if observation.Sequence == 0 {
			return ErrInvalidObservation.Wrapf("observation %d: zero sequence", i)
		}
		if observation.SourceTimestampMs == 0 {
			return ErrInvalidObservation.Wrapf("observation %d: zero source timestamp", i)
		}
		if !isCanonicalPrice(observation.IndexPrice) {
			return ErrInvalidObservation.Wrapf("observation %d: index price %q", i, observation.IndexPrice)
		}
		if !isCanonicalPrice(observation.MarkPrice) {
			return ErrInvalidObservation.Wrapf("observation %d: mark price %q", i, observation.MarkPrice)
		}
	}
	return nil
}

// ValidateBasic checks the authority; the parameter set itself is checked by
// Params.Validate when the message is executed.
func (m MsgSetParams) ValidateBasic() error {
	if _, err := accountFromBech32(m.Authority); err != nil {
		return ErrUnauthorized.Wrapf("authority: %v", err)
	}
	return nil
}

// isCanonicalPrice reports whether price is a non-zero decimal integer of 1 to
// 39 digits without a leading zero.
func isCanonicalPrice(price string) bool {
	if len(price) == 0 || len(price) > MaxPriceDigits || price[0] == '0' {
		return false
	}
	for i := 0; i < len(price); i++ {
		if price[i] < '0' || price[i] > '9' {
			return false
		}
	}
	return true
}
