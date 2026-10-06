package kvstore

import (
	mrand "math/rand"

	"github.com/Sidiora-Labs/Paxeer-X-Network/consensus/abci/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/consensus/crypto"
	"github.com/Sidiora-Labs/Paxeer-X-Network/consensus/crypto/ed25519"
	tmrand "github.com/Sidiora-Labs/Paxeer-X-Network/consensus/libs/rand"
)

// RandVals returns a list of cnt validators for initializing
// the application. Note that the keys are deterministically
// derived from the index in the array, while the power is
// random (Change this if not desired)
func RandVals(cnt int) []types.ValidatorUpdate {
	res := make([]types.ValidatorUpdate, cnt)
	for i := range res {
		// Random value between [0, 2^16 - 1]
		power := mrand.Uint32() & (1<<16 - 1) // nolint:gosec // G404: Use of weak random number generator
		keyBytes := tmrand.Bytes(len(crypto.PubKey{}.Bytes()))
		pubKey, err := ed25519.PublicKeyFromBytes(keyBytes)
		if err != nil {
			panic(err)
		}
		res[i] = types.ValidatorUpdate{
			PubKey: crypto.PubKeyToProto(pubKey),
			Power:  int64(power),
		}
	}
	return res
}
