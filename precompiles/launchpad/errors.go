package launchpad

import (
	"bytes"
	"errors"
	"fmt"
	"math/big"

	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/launchpad/types"
	pcommon "github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/common"
	sdkerrors "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types/errors"
)

type customRevert struct {
	err  *sdkerrors.Error
	data []byte
}

func (e *customRevert) Error() string           { return e.err.Error() }
func (e *customRevert) Unwrap() error           { return e.err }
func (e *customRevert) Cause() error            { return e.err }
func (e *customRevert) Revert() ([]byte, error) { return bytes.Clone(e.data), nil }

func newCustomRevert(err *sdkerrors.Error, values ...interface{}) (*customRevert, error) {
	contract, ok := types.ContractErrorOf(err)
	if !ok {
		return nil, sdkerrors.Wrap(types.ErrInvalidParams, "error has no contract mapping")
	}
	for _, value := range values {
		if value == nil {
			return nil, sdkerrors.Wrap(types.ErrInvalidParams, "custom error argument is nil")
		}
		if number, ok := value.(*big.Int); ok && (number == nil || number.Sign() < 0 || number.BitLen() > 256) {
			return nil, sdkerrors.Wrap(types.ErrInvalidParams, "custom error argument is not uint256")
		}
	}
	data, packErr := contract.Revert(values...)
	if packErr != nil {
		return nil, packErr
	}
	return &customRevert{err: err, data: data}, nil
}

func revertData(err error) ([]byte, error) {
	if err == nil {
		return nil, nil
	}
	data, packErr, found := contractRevertData(err, err)
	if packErr != nil {
		return nil, fmt.Errorf("launchpad revert encoding: %w", packErr)
	}
	if found {
		return data, nil
	}
	return pcommon.RevertReason(err), nil
}

func contractRevertData(err, original error) ([]byte, error, bool) {
	if err == nil {
		return nil, nil, false
	}
	if carrier, ok := err.(interface{ Revert() ([]byte, error) }); ok {
		data, packErr := carrier.Revert()
		return bytes.Clone(data), packErr, true
	}
	switch wrapped := err.(type) {
	case interface{ Unwrap() []error }:
		for _, cause := range wrapped.Unwrap() {
			if data, packErr, found := contractRevertData(cause, original); found {
				return data, packErr, true
			}
		}
		return nil, nil, false
	case interface{ Unwrap() error }:
		return contractRevertData(wrapped.Unwrap(), original)
	case interface{ Cause() error }:
		return contractRevertData(wrapped.Cause(), original)
	}
	if contract, ok := types.ContractErrorOf(err); ok {
		data, packErr := contract.Revert()
		return data, packErr, true
	}
	for _, keeperOnly := range types.KeeperOnlyErrors {
		if errors.Is(err, keeperOnly) {
			return pcommon.RevertReason(original), nil, true
		}
	}
	return nil, nil, false
}
