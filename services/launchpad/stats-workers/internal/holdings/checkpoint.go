package holdings

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"hash"
	"strings"
)

// CheckpointDigest consumes positive balances in normalized address order from
// a repeatable-read/archive watermark. Framing is unambiguous fixed addresses
// followed by canonical decimal amounts. Pool context cannot alter ownership.
type CheckpointDigest struct {
	h    hash.Hash
	last string
}

func NewCheckpointDigest(token, supply, height string) (*CheckpointDigest, error) {
	token, err := Address(token)
	if err != nil {
		return nil, err
	}
	if _, err = UInt(supply); err != nil {
		return nil, err
	}
	if _, err = UInt(height); err != nil {
		return nil, err
	}
	h := sha256.New()
	h.Write([]byte("holdings_v1\n125\n" + token + "\n" + supply + "\n" + height + "\n"))
	return &CheckpointDigest{h: h}, nil
}
func (d *CheckpointDigest) Add(address, raw string) error {
	a, err := Address(address)
	if err != nil {
		return err
	}
	v, err := UInt(raw)
	if err != nil {
		return err
	}
	if v.Sign() == 0 || a == Zero || strings.Compare(a, d.last) <= 0 {
		return errors.New("checkpoint rows must be unique positive and address sorted")
	}
	d.h.Write([]byte(a + ":" + raw + "\n"))
	d.last = a
	return nil
}
func (d *CheckpointDigest) Hex() string { return hex.EncodeToString(d.h.Sum(nil)) }
