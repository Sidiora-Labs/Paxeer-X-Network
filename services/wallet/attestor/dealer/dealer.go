package dealer

import (
	"math/big"

	pt "github.com/getamis/alice/crypto/ecpointgrouplaw"

	tss "github.com/Sidiora-Labs/Paxeer-X-Network/human/wallet/attestor/internal/tss/dealer"
)

type Curve = tss.Curve

type ShareBundle = tss.ShareBundle

const (
	Secp256k1 = tss.Secp256k1
	Ed25519   = tss.Ed25519
)

func Split(curve Curve, secret *big.Int, ids []string) ([]ShareBundle, *pt.ECPoint, error) {
	return tss.Split(curve, secret, ids)
}
