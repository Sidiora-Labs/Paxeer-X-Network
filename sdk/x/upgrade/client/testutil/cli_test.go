package testutil

import (
	"testing"

	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/testutil/network"
	"github.com/stretchr/testify/suite"
)

func TestIntegrationTestSuite(t *testing.T) {
	cfg := network.DefaultConfig(t)
	cfg.NumValidators = 1
	suite.Run(t, NewIntegrationTestSuite(cfg))
}
