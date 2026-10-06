//go:build cgo

package cosmwasm

import (
	"github.com/Sidiora-Labs/Paxeer-X-Network/wasm-runtime/internal/api"
)

func libwasmvmVersionImpl() (string, error) {
	return api.LibwasmvmVersion()
}
