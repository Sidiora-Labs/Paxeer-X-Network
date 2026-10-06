package types_test

import (
	"github.com/Sidiora-Labs/Paxeer-X-Network/node"
)

var (
	a        = app.SetupWithDefaultHome(false, false, false)
	ecdc     = app.MakeEncodingConfig()
	appCodec = ecdc.Marshaler
)
