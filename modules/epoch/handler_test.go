package epoch_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/testutil/testdata"

	tmproto "github.com/Sidiora-Labs/Paxeer-X-Network/consensus/proto/tendermint/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/epoch"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/epoch/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/node"
)

func TestNewHandler(t *testing.T) {
	app := app.Setup(t, false, false, false) // Your setup function here
	handler := epoch.NewHandler(app.EpochKeeper)

	// Test unrecognized message type
	testMsg := testdata.NewTestMsg()
	_, err := handler(app.BaseApp.NewContext(false, tmproto.Header{}), testMsg)
	require.Error(t, err)

	expectedErrMsg := fmt.Sprintf("unrecognized %s message type", types.ModuleName)
	require.ErrorContains(t, err, expectedErrMsg)
}
