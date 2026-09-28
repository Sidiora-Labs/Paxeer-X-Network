package eddsa

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"sort"

	"filippo.io/edwards25519"
	"filippo.io/edwards25519/field"
	"github.com/getamis/alice/crypto/birkhoffinterpolation"
	pt "github.com/getamis/alice/crypto/ecpointgrouplaw"
	"github.com/getamis/alice/crypto/elliptic"
)

const (
	EncodingVersion uint32 = 1
	curveName              = "ed25519"
	scalarSize             = 32
)

var (
	ErrEncoding      = errors.New("eddsa: malformed key share encoding")
	ErrVersion       = errors.New("eddsa: unsupported key share encoding version")
	ErrInconsistent  = errors.New("eddsa: key share is inconsistent with its public data")
	ErrIdentityPoint = errors.New("eddsa: point is the identity")
)

type KeyShare struct {
	id             string
	threshold      uint32
	publicKey      *pt.ECPoint
	publicKeyBytes [32]byte
	share          *big.Int
	bks            map[string]*birkhoffinterpolation.BkParameter
	ys             map[string]*pt.ECPoint
}

type partyV1 struct {
	ID          string `json:"id"`
	X           []byte `json:"x"`
	Rank        uint32 `json:"rank"`
	PublicShare []byte `json:"public_share"`
}

type keyShareV1 struct {
	Version   uint32    `json:"version"`
	Curve     string    `json:"curve"`
	ID        string    `json:"id"`
	Threshold uint32    `json:"threshold"`
	PublicKey []byte    `json:"public_key"`
	Share     []byte    `json:"share"`
	Parties   []partyV1 `json:"parties"`
}

func newKeyShare(id string, threshold uint32, publicKey *pt.ECPoint, share *big.Int, bks map[string]*birkhoffinterpolation.BkParameter, ys map[string]*pt.ECPoint) (*KeyShare, error) {
	k := &KeyShare{
		id:        id,
		threshold: threshold,
		publicKey: publicKey.Copy(),
		share:     new(big.Int).Set(share),
		bks:       make(map[string]*birkhoffinterpolation.BkParameter, len(bks)),
		ys:        make(map[string]*pt.ECPoint, len(ys)),
	}
	for pid, bk := range bks {
		k.bks[pid] = birkhoffinterpolation.NewBkParameter(new(big.Int).Set(bk.GetX()), bk.GetRank())
	}
	for pid, y := range ys {
		k.ys[pid] = y.Copy()
	}
	if err := k.validate(); err != nil {
		return nil, err
	}
	encoded, err := encodePoint(k.publicKey)
	if err != nil {
		return nil, err
	}
	k.publicKeyBytes = encoded
	return k, nil
}

func (k *KeyShare) validate() error {
	curve := elliptic.Ed25519()
	n := curve.Params().N
	if k.id == "" || k.threshold < 2 {
		return ErrInconsistent
	}
	if len(k.bks) != len(k.ys) || uint32(len(k.bks)) < k.threshold {
		return ErrInconsistent
	}
	if k.publicKey == nil || k.publicKey.GetCurve() != curve || k.publicKey.IsIdentity() {
		return ErrInconsistent
	}
	if k.share.Sign() <= 0 || k.share.Cmp(n) >= 0 {
		return ErrInconsistent
	}
	selfY, ok := k.ys[k.id]
	if !ok {
		return ErrInconsistent
	}
	if _, ok := k.bks[k.id]; !ok {
		return ErrInconsistent
	}
	if !pt.ScalarBaseMult(curve, k.share).Equal(selfY) {
		return ErrInconsistent
	}
	ids := k.Participants()
	bks := make(birkhoffinterpolation.BkParameters, 0, len(ids))
	ys := make([]*pt.ECPoint, 0, len(ids))
	for _, pid := range ids {
		bk := k.bks[pid]
		y, ok := k.ys[pid]
		if bk == nil || !ok || y == nil || y.GetCurve() != curve || y.IsIdentity() {
			return ErrInconsistent
		}
		bks = append(bks, bk)
		ys = append(ys, y)
	}
	if err := bks.ValidateThresholdScheme(k.threshold, n); err != nil {
		return fmt.Errorf("%w: %v", ErrInconsistent, err)
	}
	if err := bks.ValidatePublicKey(ys, k.threshold, k.publicKey); err != nil {
		return fmt.Errorf("%w: %v", ErrInconsistent, err)
	}
	return nil
}

func (k *KeyShare) ID() string {
	return k.id
}

func (k *KeyShare) Threshold() uint32 {
	return k.threshold
}

func (k *KeyShare) Participants() []string {
	ids := make([]string, 0, len(k.bks))
	for pid := range k.bks {
		ids = append(ids, pid)
	}
	sort.Strings(ids)
	return ids
}

func (k *KeyShare) PublicKeyBytes() [32]byte {
	return k.publicKeyBytes
}

func (k *KeyShare) String() string {
	return fmt.Sprintf("eddsa.KeyShare{id=%s threshold=%d participants=%v}", k.id, k.threshold, k.Participants())
}

func (k *KeyShare) GoString() string {
	return k.String()
}

func (k *KeyShare) Marshal() ([]byte, error) {
	publicKey := k.publicKeyBytes
	record := keyShareV1{
		Version:   EncodingVersion,
		Curve:     curveName,
		ID:        k.id,
		Threshold: k.threshold,
		PublicKey: publicKey[:],
		Share:     encodeScalar(k.share),
	}
	for _, pid := range k.Participants() {
		y, err := encodePoint(k.ys[pid])
		if err != nil {
			return nil, err
		}
		bk := k.bks[pid]
		record.Parties = append(record.Parties, partyV1{
			ID:          pid,
			X:           encodeScalar(bk.GetX()),
			Rank:        bk.GetRank(),
			PublicShare: y[:],
		})
	}
	return json.Marshal(record)
}

func Load(encoded []byte) (*KeyShare, error) {
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	var record keyShareV1
	if err := decoder.Decode(&record); err != nil {
		return nil, ErrEncoding
	}
	if decoder.More() {
		return nil, ErrEncoding
	}
	if record.Version != EncodingVersion {
		return nil, ErrVersion
	}
	if record.Curve != curveName {
		return nil, ErrEncoding
	}
	publicKey, err := decodePointSlice(record.PublicKey)
	if err != nil {
		return nil, err
	}
	share, err := decodeScalar(record.Share)
	if err != nil {
		return nil, err
	}
	bks := make(map[string]*birkhoffinterpolation.BkParameter, len(record.Parties))
	ys := make(map[string]*pt.ECPoint, len(record.Parties))
	for i, party := range record.Parties {
		if party.ID == "" {
			return nil, ErrEncoding
		}
		if i > 0 && record.Parties[i-1].ID >= party.ID {
			return nil, ErrEncoding
		}
		x, err := decodeScalar(party.X)
		if err != nil {
			return nil, err
		}
		y, err := decodePointSlice(party.PublicShare)
		if err != nil {
			return nil, err
		}
		bks[party.ID] = birkhoffinterpolation.NewBkParameter(x, party.Rank)
		ys[party.ID] = y
	}
	return newKeyShare(record.ID, record.Threshold, publicKey, share, bks, ys)
}

func encodeScalar(v *big.Int) []byte {
	out := make([]byte, scalarSize)
	v.FillBytes(out)
	reverse(out)
	return out
}

func decodeScalar(encoded []byte) (*big.Int, error) {
	if len(encoded) != scalarSize {
		return nil, ErrEncoding
	}
	be := make([]byte, scalarSize)
	copy(be, encoded)
	reverse(be)
	v := new(big.Int).SetBytes(be)
	if v.Cmp(elliptic.Ed25519().Params().N) >= 0 {
		return nil, ErrEncoding
	}
	return v, nil
}

func encodePoint(p *pt.ECPoint) ([32]byte, error) {
	var out [32]byte
	if p == nil || p.GetCurve() != elliptic.Ed25519() {
		return out, ErrEncoding
	}
	if p.IsIdentity() {
		return out, ErrIdentityPoint
	}
	p.GetY().FillBytes(out[:])
	reverse(out[:])
	if p.GetX().Bit(0) == 1 {
		out[31] |= 0x80
	}
	return out, nil
}

func decodePointSlice(encoded []byte) (*pt.ECPoint, error) {
	if len(encoded) != 32 {
		return nil, ErrEncoding
	}
	var fixed [32]byte
	copy(fixed[:], encoded)
	return decodePoint(fixed)
}

func decodePoint(encoded [32]byte) (*pt.ECPoint, error) {
	point, err := new(edwards25519.Point).SetBytes(encoded[:])
	if err != nil {
		return nil, ErrEncoding
	}
	if !bytes.Equal(point.Bytes(), encoded[:]) {
		return nil, ErrEncoding
	}
	if point.Equal(edwards25519.NewIdentityPoint()) == 1 {
		return nil, ErrIdentityPoint
	}
	X, Y, Z, _ := point.ExtendedCoordinates()
	zInv := new(field.Element).Invert(Z)
	x := new(field.Element).Multiply(X, zInv)
	y := new(field.Element).Multiply(Y, zInv)
	result, err := pt.NewECPoint(elliptic.Ed25519(), fieldToInt(x), fieldToInt(y))
	if err != nil {
		return nil, ErrEncoding
	}
	return result, nil
}

func fieldToInt(v *field.Element) *big.Int {
	le := v.Bytes()
	reverse(le)
	return new(big.Int).SetBytes(le)
}

func reverse(b []byte) {
	for i, j := 0, len(b)-1; i < j; i, j = i+1, j-1 {
		b[i], b[j] = b[j], b[i]
	}
}
