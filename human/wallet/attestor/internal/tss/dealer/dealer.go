package dealer

import (
	"crypto/ed25519"
	"crypto/sha512"
	"errors"
	"fmt"
	"math/big"
	"sort"

	"github.com/getamis/alice/crypto/birkhoffinterpolation"
	pt "github.com/getamis/alice/crypto/ecpointgrouplaw"
	"github.com/getamis/alice/crypto/elliptic"
	"github.com/getamis/alice/crypto/utils"
)

const (
	Participants        = 5
	Threshold    uint32 = 3
)

type Curve uint8

const (
	Secp256k1 Curve = iota + 1
	Ed25519
)

var (
	ErrUnknownCurve      = errors.New("dealer: unknown curve")
	ErrParticipants      = errors.New("dealer: five distinct non-empty participant ids are required")
	ErrScalarRange       = errors.New("dealer: private scalar out of range")
	ErrSeedLength        = errors.New("dealer: ed25519 seed must be 32 bytes")
	ErrPointCurve        = errors.New("dealer: point is not on the ed25519 curve")
	ErrIdentityPoint     = errors.New("dealer: point is the identity")
	ErrBundleIncomplete  = errors.New("dealer: share bundle is incomplete")
	ErrShareMismatch     = errors.New("dealer: share does not match its partial public key")
	ErrPublicKeyMismatch = errors.New("dealer: partial public keys do not interpolate to the public key")
)

func (c Curve) Elliptic() (elliptic.Curve, error) {
	switch c {
	case Secp256k1:
		return elliptic.Secp256k1(), nil
	case Ed25519:
		return elliptic.Ed25519(), nil
	}
	return nil, ErrUnknownCurve
}

func (c Curve) String() string {
	switch c {
	case Secp256k1:
		return "secp256k1"
	case Ed25519:
		return "ed25519"
	}
	return fmt.Sprintf("curve(%d)", uint8(c))
}

type ShareBundle struct {
	Curve             Curve
	ParticipantID     string
	Share             *big.Int
	PublicKey         *pt.ECPoint
	PartialPublicKeys map[string]*pt.ECPoint
	Bks               map[string]*birkhoffinterpolation.BkParameter
	Threshold         uint32
}

func (b ShareBundle) ParticipantIDs() []string {
	ids := make([]string, 0, len(b.Bks))
	for id := range b.Bks {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func (b ShareBundle) ValidatePublicData() error {
	curve, err := b.Curve.Elliptic()
	if err != nil {
		return err
	}
	if b.PublicKey == nil || b.PublicKey.GetCurve() != curve || len(b.Bks) < int(b.Threshold) || len(b.PartialPublicKeys) != len(b.Bks) {
		return ErrBundleIncomplete
	}
	ids := b.ParticipantIDs()
	bks := make(birkhoffinterpolation.BkParameters, len(ids))
	sgs := make([]*pt.ECPoint, len(ids))
	for i, id := range ids {
		point, ok := b.PartialPublicKeys[id]
		if !ok || point == nil || point.GetCurve() != curve || b.Bks[id] == nil {
			return ErrBundleIncomplete
		}
		bks[i] = b.Bks[id]
		sgs[i] = point
	}
	if err := bks.ValidatePublicKey(sgs, b.Threshold, b.PublicKey); err != nil {
		return fmt.Errorf("%w: %v", ErrPublicKeyMismatch, err)
	}
	return nil
}

func (b ShareBundle) Validate() error {
	if err := b.ValidatePublicData(); err != nil {
		return err
	}
	curve, _ := b.Curve.Elliptic()
	own, ok := b.PartialPublicKeys[b.ParticipantID]
	if !ok || b.Share == nil {
		return ErrBundleIncomplete
	}
	if b.Share.Sign() <= 0 || b.Share.Cmp(curve.Params().N) >= 0 {
		return ErrShareMismatch
	}
	if !pt.ScalarBaseMult(curve, b.Share).Equal(own) {
		return ErrShareMismatch
	}
	return nil
}

func Split(curve Curve, secret *big.Int, ids []string) ([]ShareBundle, *pt.ECPoint, error) {
	defer Wipe(secret)
	ec, err := curve.Elliptic()
	if err != nil {
		return nil, nil, err
	}
	if err := checkIDs(ids); err != nil {
		return nil, nil, err
	}
	order := ec.Params().N
	if secret == nil || secret.Sign() <= 0 || secret.Cmp(order) >= 0 {
		return nil, nil, ErrScalarRange
	}

	coefficients := make([]*big.Int, Threshold)
	defer func() {
		for _, c := range coefficients {
			Wipe(c)
		}
	}()
	coefficients[0] = new(big.Int).Set(secret)
	for i := 1; i < len(coefficients); i++ {
		c, err := utils.RandomPositiveInt(order)
		if err != nil {
			return nil, nil, err
		}
		coefficients[i] = c
	}

	publicKey := pt.ScalarBaseMult(ec, coefficients[0])
	bks := make(map[string]*birkhoffinterpolation.BkParameter, len(ids))
	shares := make(map[string]*big.Int, len(ids))
	partials := make(map[string]*pt.ECPoint, len(ids))
	for i, id := range ids {
		x := big.NewInt(int64(i + 1))
		bks[id] = birkhoffinterpolation.NewBkParameter(x, 0)
		share := evaluate(coefficients, x, order)
		if share.Sign() == 0 {
			for _, s := range shares {
				Wipe(s)
			}
			Wipe(share)
			return nil, nil, ErrScalarRange
		}
		shares[id] = share
		partials[id] = pt.ScalarBaseMult(ec, share)
	}

	bundles := make([]ShareBundle, len(ids))
	for i, id := range ids {
		bundles[i] = ShareBundle{
			Curve:             curve,
			ParticipantID:     id,
			Share:             shares[id],
			PublicKey:         publicKey.Copy(),
			PartialPublicKeys: copyPoints(partials),
			Bks:               copyBks(bks),
			Threshold:         Threshold,
		}
	}
	return bundles, publicKey, nil
}

func Ed25519ScalarFromSeed(seed []byte) (*big.Int, error) {
	if len(seed) != ed25519.SeedSize {
		return nil, ErrSeedLength
	}
	digest := sha512.Sum512(seed)
	defer func() {
		for i := range digest {
			digest[i] = 0
		}
	}()
	digest[0] &= 248
	digest[31] &= 127
	digest[31] |= 64
	be := make([]byte, 32)
	defer func() {
		for i := range be {
			be[i] = 0
		}
	}()
	for i := 0; i < 32; i++ {
		be[i] = digest[31-i]
	}
	scalar := new(big.Int).SetBytes(be)
	scalar.Mod(scalar, elliptic.Ed25519().Params().N)
	return scalar, nil
}

func EncodeEd25519(p *pt.ECPoint) ([]byte, error) {
	if p == nil || p.GetCurve() != elliptic.Ed25519() {
		return nil, ErrPointCurve
	}
	if p.IsIdentity() {
		return nil, ErrIdentityPoint
	}
	be := p.GetY().FillBytes(make([]byte, 32))
	out := make([]byte, 32)
	for i := 0; i < 32; i++ {
		out[i] = be[31-i]
	}
	if p.GetX().Bit(0) == 1 {
		out[31] |= 0x80
	}
	return out, nil
}

func Wipe(x *big.Int) {
	if x == nil {
		return
	}
	words := x.Bits()
	for i := range words {
		words[i] = 0
	}
	x.SetInt64(0)
}

func evaluate(coefficients []*big.Int, x, order *big.Int) *big.Int {
	result := new(big.Int).Set(coefficients[len(coefficients)-1])
	product := new(big.Int)
	defer Wipe(product)
	for i := len(coefficients) - 2; i >= 0; i-- {
		product.Mul(result, x)
		Wipe(result)
		result.Add(product, coefficients[i])
		result.Mod(result, order)
	}
	return result
}

func checkIDs(ids []string) error {
	if len(ids) != Participants {
		return ErrParticipants
	}
	seen := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		if id == "" {
			return ErrParticipants
		}
		if _, dup := seen[id]; dup {
			return ErrParticipants
		}
		seen[id] = struct{}{}
	}
	return nil
}

func copyPoints(in map[string]*pt.ECPoint) map[string]*pt.ECPoint {
	out := make(map[string]*pt.ECPoint, len(in))
	for k, v := range in {
		out[k] = v.Copy()
	}
	return out
}

func copyBks(in map[string]*birkhoffinterpolation.BkParameter) map[string]*birkhoffinterpolation.BkParameter {
	out := make(map[string]*birkhoffinterpolation.BkParameter, len(in))
	for k, v := range in {
		out[k] = birkhoffinterpolation.NewBkParameter(v.GetX(), v.GetRank())
	}
	return out
}

func (b ShareBundle) Clone() ShareBundle {
	c := b
	if b.Share != nil {
		c.Share = new(big.Int).Set(b.Share)
	}
	if b.PublicKey != nil {
		c.PublicKey = b.PublicKey.Copy()
	}
	c.PartialPublicKeys = copyPoints(b.PartialPublicKeys)
	c.Bks = copyBks(b.Bks)
	return c
}
