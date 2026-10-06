package antedecorators_test

import (
	"testing"

	tmproto "github.com/Sidiora-Labs/Paxeer-X-Network/consensus/proto/tendermint/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/node/antedecorators"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/utils"
	"github.com/stretchr/testify/require"
)

func TestTracedDecorator(t *testing.T) {
	output = ""
	anteDecorators := []sdk.AnteDecorator{
		FakeAnteDecoratorOne{},
		FakeAnteDecoratorTwo{},
		FakeAnteDecoratorThree{},
	}
	tracedDecorators := utils.Map(anteDecorators, func(d sdk.AnteDecorator) sdk.AnteDecorator {
		return antedecorators.NewTracedAnteDecorator(d, nil)
	})
	chainedHandler := sdk.ChainAnteDecorators(tracedDecorators...)
	chainedHandler(sdk.NewContext(nil, tmproto.Header{}, false), FakeTx{}, false)
	require.Equal(t, "onetwothree", output)
}
