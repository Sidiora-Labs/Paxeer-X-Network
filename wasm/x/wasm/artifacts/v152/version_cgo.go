//go:build cgo

package v152

import (
	"github.com/Sidiora-Labs/Paxeer-X-Network/wasm/x/wasm/artifacts/v152/api"
)

func libwasmvmVersionImpl() (string, error) {
	return api.LibwasmvmVersion()
}
