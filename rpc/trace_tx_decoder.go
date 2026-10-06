package evmrpc

import (
	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/client"
	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/codec"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	authtx "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/auth/tx"
)

type traceTxConfig struct {
	client.TxConfig
	decoder sdk.TxDecoder
}

func (c traceTxConfig) TxDecoder() sdk.TxDecoder {
	return c.decoder
}

type protoCodecProvider interface {
	ProtoCodec() codec.ProtoCodecMarshaler
}

func traceCompatTxConfig(txConfig client.TxConfig, v65ActiveAtHeight bool) client.TxConfig {
	if v65ActiveAtHeight {
		return txConfig
	}
	provider, ok := txConfig.(protoCodecProvider)
	if !ok {
		return txConfig
	}
	return traceTxConfig{
		TxConfig: txConfig,
		decoder:  authtx.DefaultTxDecoderWithoutBodyBloatRejection(provider.ProtoCodec()),
	}
}

func traceCompatTxConfigProvider(txConfigProvider func(int64) client.TxConfig, isV65ActiveAtHeight func(int64) bool) func(int64) client.TxConfig {
	return func(height int64) client.TxConfig {
		return traceCompatTxConfig(txConfigProvider(height), isV65ActiveAtHeight(height))
	}
}

func traceCompatTxDecoder(txConfig client.TxConfig, v65ActiveAtHeight bool) sdk.TxDecoder {
	return traceCompatTxConfig(txConfig, v65ActiveAtHeight).TxDecoder()
}
