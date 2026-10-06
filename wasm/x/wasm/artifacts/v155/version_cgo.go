//go:build cgo

package v155

import (
	"github.com/Sidiora-Labs/Paxeer-X-Network/wasm/x/wasm/artifacts/v155/api"
)

func libwasmvmVersionImpl() (string, error) {
	return api.LibwasmvmVersion()
}
