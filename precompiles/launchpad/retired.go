package launchpad

import (
	"errors"

	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	"github.com/ethereum/go-ethereum/common"
)

// retiredSelector is the selector of the chain-defined error Retired(address).
var retiredSelector = [4]byte{0xda, 0x9d, 0x2e, 0x31}

// retiredRuntime returns the fixed runtime installed at a retired generation 1
// address: for any calldata, value or caller it reverts with
// Retired(replacement), the selector followed by the left-padded address.
func retiredRuntime(replacement common.Address) []byte {
	code := make([]byte, 0, 40)
	code = append(code, 0x63) // PUSH4 selector
	code = append(code, retiredSelector[:]...)
	code = append(code,
		0x60, 0xe0, // PUSH1 224
		0x1b,       // SHL
		0x60, 0x00, // PUSH1 0
		0x52, // MSTORE: memory[0:4] = selector
		0x73, // PUSH20 replacement
	)
	code = append(code, replacement.Bytes()...)
	return append(code,
		0x60, 0x04, // PUSH1 4
		0x52,       // MSTORE: memory[4:36] = replacement word
		0x60, 0x24, // PUSH1 36
		0x60, 0x00, // PUSH1 0
		0xfd, // REVERT memory[0:36]
	)
}

// InstallRetired writes the retirement runtime naming replacement at retired
// with SetCode; storage, balance and nonce at retired are left as they are.
func InstallRetired(ctx sdk.Context, k interface {
	SetCode(ctx sdk.Context, addr common.Address, code []byte)
}, retired, replacement common.Address,
) error {
	if replacement == (common.Address{}) {
		return errors.New("retired address needs a non-zero replacement")
	}
	if retired == replacement {
		return errors.New("retired address cannot be its own replacement")
	}
	k.SetCode(ctx, retired, retiredRuntime(replacement))
	return nil
}
