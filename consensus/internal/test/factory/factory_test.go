package factory

import (
	"testing"

	"github.com/Sidiora-Labs/Paxeer-X-Network/consensus/types"
)

func TestMakeHeader(t *testing.T) {
	MakeHeader(&types.Header{})
}

func TestRandomNodeID(t *testing.T) {
	RandomNodeID(t)
}
