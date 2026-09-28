package ecdsa

import (
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"sort"

	"github.com/ethereum/go-ethereum/common"
	gethcrypto "github.com/ethereum/go-ethereum/crypto"
	"github.com/getamis/alice/crypto/birkhoffinterpolation"
	pt "github.com/getamis/alice/crypto/ecpointgrouplaw"
	"github.com/getamis/alice/crypto/elliptic"
	"github.com/getamis/alice/crypto/homo/paillier"
	paillierzkproof "github.com/getamis/alice/crypto/zkproof/paillier"
)

const (
	KeyShareVersion = 1
	CurveName       = "secp256k1"
)

var (
	ErrUnsupportedShareVersion = errors.New("ecdsa: unsupported key share version")
	ErrUnsupportedCurve        = errors.New("ecdsa: unsupported curve")
	ErrInvalidKeyShare         = errors.New("ecdsa: invalid key share")
)

type KeyShare struct {
	SelfID            string
	Threshold         uint32
	SSID              []byte
	Rid               []byte
	PublicKey         *pt.ECPoint
	Share             *big.Int
	PartialPublicKeys map[string]*pt.ECPoint
	Bks               map[string]*birkhoffinterpolation.BkParameter
	PaillierKey       *paillier.Paillier
	Pedersen          map[string]*paillierzkproof.PederssenOpenParameter
}

type encodedPoint struct {
	X string `json:"x"`
	Y string `json:"y"`
}

type encodedBk struct {
	X    string `json:"x"`
	Rank uint32 `json:"rank"`
}

type encodedPedersen struct {
	N string `json:"n"`
	S string `json:"s"`
	T string `json:"t"`
}

type encodedPaillier struct {
	P string `json:"p"`
	Q string `json:"q"`
}

type encodedKeyShare struct {
	Version           uint32                     `json:"version"`
	Curve             string                     `json:"curve"`
	SelfID            string                     `json:"self_id"`
	Threshold         uint32                     `json:"threshold"`
	SSID              []byte                     `json:"ssid"`
	Rid               []byte                     `json:"rid"`
	PublicKey         encodedPoint               `json:"public_key"`
	Share             string                     `json:"share"`
	PartialPublicKeys map[string]encodedPoint    `json:"partial_public_keys"`
	Bks               map[string]encodedBk       `json:"bks"`
	Paillier          encodedPaillier            `json:"paillier"`
	Pedersen          map[string]encodedPedersen `json:"pedersen"`
}

func (k *KeyShare) ParticipantIDs() []string {
	ids := make([]string, 0, len(k.Bks))
	for id := range k.Bks {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func (k *KeyShare) PublicKeyBytes() []byte {
	return uncompressedPoint(k.PublicKey)
}

func (k *KeyShare) Address() (common.Address, error) {
	pub, err := gethcrypto.UnmarshalPubkey(k.PublicKeyBytes())
	if err != nil {
		return common.Address{}, fmt.Errorf("%w: public key: %v", ErrInvalidKeyShare, err)
	}
	return gethcrypto.PubkeyToAddress(*pub), nil
}

func (k *KeyShare) Validate() error {
	if k == nil {
		return fmt.Errorf("%w: nil", ErrInvalidKeyShare)
	}
	if k.SelfID == "" {
		return fmt.Errorf("%w: empty self id", ErrInvalidKeyShare)
	}
	if len(k.SSID) == 0 || len(k.Rid) == 0 {
		return fmt.Errorf("%w: empty ssid or rid", ErrInvalidKeyShare)
	}
	if k.PublicKey == nil || k.PublicKey.IsIdentity() {
		return fmt.Errorf("%w: public key", ErrInvalidKeyShare)
	}
	if k.PaillierKey == nil {
		return fmt.Errorf("%w: missing paillier key", ErrInvalidKeyShare)
	}
	curve := elliptic.Secp256k1()
	n := curve.Params().N
	if k.Share == nil || k.Share.Sign() <= 0 || k.Share.Cmp(n) >= 0 {
		return fmt.Errorf("%w: share out of range", ErrInvalidKeyShare)
	}
	if k.Threshold < 2 || uint32(len(k.Bks)) < k.Threshold {
		return fmt.Errorf("%w: threshold %d with %d participants", ErrInvalidKeyShare, k.Threshold, len(k.Bks))
	}
	if len(k.PartialPublicKeys) != len(k.Bks) || len(k.Pedersen) != len(k.Bks) {
		return fmt.Errorf("%w: participant sets differ", ErrInvalidKeyShare)
	}
	if _, ok := k.Bks[k.SelfID]; !ok {
		return fmt.Errorf("%w: self id not a participant", ErrInvalidKeyShare)
	}
	ids := k.ParticipantIDs()
	bks := make(birkhoffinterpolation.BkParameters, 0, len(ids))
	points := make([]*pt.ECPoint, 0, len(ids))
	for _, id := range ids {
		bk := k.Bks[id]
		point, ok := k.PartialPublicKeys[id]
		if !ok || point == nil || bk == nil {
			return fmt.Errorf("%w: participant %q incomplete", ErrInvalidKeyShare, id)
		}
		if ped, ok := k.Pedersen[id]; !ok || ped == nil {
			return fmt.Errorf("%w: participant %q has no pedersen parameters", ErrInvalidKeyShare, id)
		}
		bks = append(bks, bk)
		points = append(points, point)
	}
	if !pt.ScalarBaseMult(curve, k.Share).Equal(k.PartialPublicKeys[k.SelfID]) {
		return fmt.Errorf("%w: share does not match its partial public key", ErrInvalidKeyShare)
	}
	if err := bks.ValidatePublicKey(points, k.Threshold, k.PublicKey); err != nil {
		return fmt.Errorf("%w: partial public keys: %v", ErrInvalidKeyShare, err)
	}
	return nil
}

func (k *KeyShare) Marshal() ([]byte, error) {
	if err := k.Validate(); err != nil {
		return nil, err
	}
	p, q := k.PaillierKey.GetPQ()
	enc := encodedKeyShare{
		Version:           KeyShareVersion,
		Curve:             CurveName,
		SelfID:            k.SelfID,
		Threshold:         k.Threshold,
		SSID:              k.SSID,
		Rid:               k.Rid,
		PublicKey:         encodePoint(k.PublicKey),
		Share:             encodeInt(k.Share),
		PartialPublicKeys: make(map[string]encodedPoint, len(k.PartialPublicKeys)),
		Bks:               make(map[string]encodedBk, len(k.Bks)),
		Paillier:          encodedPaillier{P: encodeInt(p), Q: encodeInt(q)},
		Pedersen:          make(map[string]encodedPedersen, len(k.Pedersen)),
	}
	for id, point := range k.PartialPublicKeys {
		enc.PartialPublicKeys[id] = encodePoint(point)
	}
	for id, bk := range k.Bks {
		enc.Bks[id] = encodedBk{X: encodeInt(bk.GetX()), Rank: bk.GetRank()}
	}
	for id, ped := range k.Pedersen {
		enc.Pedersen[id] = encodedPedersen{N: encodeInt(ped.GetN()), S: encodeInt(ped.GetS()), T: encodeInt(ped.GetT())}
	}
	return json.Marshal(enc)
}

func LoadKeyShare(data []byte) (*KeyShare, error) {
	var enc encodedKeyShare
	if err := json.Unmarshal(data, &enc); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidKeyShare, err)
	}
	if enc.Version != KeyShareVersion {
		return nil, fmt.Errorf("%w: %d", ErrUnsupportedShareVersion, enc.Version)
	}
	if enc.Curve != CurveName {
		return nil, fmt.Errorf("%w: %q", ErrUnsupportedCurve, enc.Curve)
	}
	curve := elliptic.Secp256k1()
	pub, err := decodePoint(curve, enc.PublicKey)
	if err != nil {
		return nil, err
	}
	share, err := decodeInt(enc.Share)
	if err != nil {
		return nil, err
	}
	p, err := decodeInt(enc.Paillier.P)
	if err != nil {
		return nil, err
	}
	q, err := decodeInt(enc.Paillier.Q)
	if err != nil {
		return nil, err
	}
	paillierKey, err := paillier.NewPaillierWithGivenPrimes(p, q)
	if err != nil {
		return nil, fmt.Errorf("%w: paillier key: %v", ErrInvalidKeyShare, err)
	}
	k := &KeyShare{
		SelfID:            enc.SelfID,
		Threshold:         enc.Threshold,
		SSID:              enc.SSID,
		Rid:               enc.Rid,
		PublicKey:         pub,
		Share:             share,
		PartialPublicKeys: make(map[string]*pt.ECPoint, len(enc.PartialPublicKeys)),
		Bks:               make(map[string]*birkhoffinterpolation.BkParameter, len(enc.Bks)),
		PaillierKey:       paillierKey,
		Pedersen:          make(map[string]*paillierzkproof.PederssenOpenParameter, len(enc.Pedersen)),
	}
	for id, ep := range enc.PartialPublicKeys {
		point, err := decodePoint(curve, ep)
		if err != nil {
			return nil, err
		}
		k.PartialPublicKeys[id] = point
	}
	for id, eb := range enc.Bks {
		x, err := decodeInt(eb.X)
		if err != nil {
			return nil, err
		}
		k.Bks[id] = birkhoffinterpolation.NewBkParameter(x, eb.Rank)
	}
	for id, ep := range enc.Pedersen {
		n, err := decodeInt(ep.N)
		if err != nil {
			return nil, err
		}
		s, err := decodeInt(ep.S)
		if err != nil {
			return nil, err
		}
		t, err := decodeInt(ep.T)
		if err != nil {
			return nil, err
		}
		k.Pedersen[id] = paillierzkproof.NewPedersenOpenParameter(n, s, t)
	}
	if err := k.Validate(); err != nil {
		return nil, err
	}
	return k, nil
}

func encodeInt(v *big.Int) string {
	return v.Text(16)
}

func decodeInt(s string) (*big.Int, error) {
	v, ok := new(big.Int).SetString(s, 16)
	if !ok || v.Sign() < 0 {
		return nil, fmt.Errorf("%w: malformed integer", ErrInvalidKeyShare)
	}
	return v, nil
}

func encodePoint(p *pt.ECPoint) encodedPoint {
	return encodedPoint{X: encodeInt(p.GetX()), Y: encodeInt(p.GetY())}
}

func decodePoint(curve elliptic.Curve, ep encodedPoint) (*pt.ECPoint, error) {
	x, err := decodeInt(ep.X)
	if err != nil {
		return nil, err
	}
	y, err := decodeInt(ep.Y)
	if err != nil {
		return nil, err
	}
	point, err := pt.NewECPoint(curve, x, y)
	if err != nil {
		return nil, fmt.Errorf("%w: point: %v", ErrInvalidKeyShare, err)
	}
	if point.IsIdentity() {
		return nil, fmt.Errorf("%w: identity point", ErrInvalidKeyShare)
	}
	return point, nil
}

func uncompressedPoint(p *pt.ECPoint) []byte {
	out := make([]byte, 65)
	out[0] = 4
	p.GetX().FillBytes(out[1:33])
	p.GetY().FillBytes(out[33:65])
	return out
}
