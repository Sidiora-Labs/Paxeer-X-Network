package layerxexchange

import (
	"errors"

	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/layerxexchange/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/lxphost"
	pcommon "github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/common"
)

func KernelCodeText(code lxphost.Code) string {
	return pcommon.LXPCodeName(int32(code))
}

func RevertError(err error) error {
	if err == nil {
		return nil
	}
	code := types.ErrorCode(err)
	if code == "" {
		var result lxphost.Code
		if !errors.As(err, &result) {
			return err
		}
		code = KernelCodeText(result)
	}
	return revertError{cause: err, code: code}
}

type revertError struct {
	cause error
	code  string
}

func (e revertError) Error() string {
	return e.code
}

func (e revertError) Unwrap() error {
	return e.cause
}

func (e revertError) Cause() error {
	return e.cause
}
