//go:build norace
// +build norace

package testutil

import (
	"testing"

	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/testutil/network"

	"github.com/stretchr/testify/suite"
)

func TestIntegrationTestSuite(t *testing.T) {
	cfg := network.DefaultConfig()
	cfg.NumValidators = 3
	suite.Run(t, NewIntegrationTestSuite(cfg))
}
