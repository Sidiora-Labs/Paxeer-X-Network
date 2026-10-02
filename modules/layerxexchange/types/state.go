package types

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"errors"

	"github.com/sidiora-labs/paxeer-network/layerxproof/codec"
	"github.com/sidiora-labs/paxeer-network/layerxproof/verify"
)

// LayerX perps state lives in native module 6. Its keys are the prefixes of
// src/modules/perps followed by the 32-byte identifiers.
const (
	PerpsModuleID   uint16 = 6
	AccountModuleID uint16 = 0

	SideBuy  uint8 = 1
	SideSell uint8 = 2

	// Time in force is carried to LayerX unchanged.
	TimeInForceGoodTillCancelled uint8 = 0
	TimeInForceImmediateOrCancel uint8 = 1
	TimeInForceFillOrKill        uint8 = 2
	TimeInForcePostOnly          uint8 = 3

	// MaxWitnessBytes bounds every caller-supplied state proof.
	MaxWitnessBytes = 64 * 1024
)

func perpsKey(prefix string, ids ...[32]byte) []byte {
	out := []byte(prefix)
	for _, id := range ids {
		out = append(out, id[:]...)
	}
	return out
}

// MarketStateKey is "market:" || market_id.
func MarketStateKey(marketID [32]byte) []byte { return perpsKey("market:", marketID) }

// OrderStateKey is "order:" || market_id || order_id.
func OrderStateKey(marketID, orderID [32]byte) []byte { return perpsKey("order:", marketID, orderID) }

// PositionStateKey is "position:" || market_id || position_id.
func PositionStateKey(marketID, positionID [32]byte) []byte {
	return perpsKey("position:", marketID, positionID)
}

const (
	GovernanceModuleID uint16 = 7
	MaxNativeGenesisBytes = 262144
)

func GenesisManifestStateKey() []byte { return []byte("genesis/manifest/v1") }

type genesisReader struct {
	data []byte
	offset int
	bad bool
}

func (r *genesisReader) take(n int) []byte {
	if r.bad || n < 0 || n > len(r.data)-r.offset {
		r.bad = true
		return nil
	}
	out := r.data[r.offset:r.offset+n]
	r.offset += n
	return out
}

func (r *genesisReader) number(n int) uint64 {
	var v uint64
	for _, b := range r.take(n) { v = v<<8 | uint64(b) }
	return v
}

func (r *genesisReader) blob(exact, maximum int) []byte {
	n := r.number(4)
	if n > uint64(maximum) || (exact >= 0 && n != uint64(exact)) {
		r.bad = true
		return nil
	}
	return r.take(int(n))
}

func (r *genesisReader) count(minimum, maximum uint64) uint64 {
	n := r.number(4)
	if n < minimum || n > maximum { r.bad = true; return 0 }
	return n
}

func genesisZero(value []byte) bool {
	for _, b := range value { if b != 0 { return false } }
	return true
}

func genesisKey(name string) []byte {
	key := make([]byte, 32)
	copy(key, name)
	return key
}

func genesisOrdered(module uint64, key []byte, previousModule uint64, previousKey []byte) bool {
	return module > 0 && module <= 11 && (previousKey == nil || module > previousModule ||
		(module == previousModule && bytes.Compare(previousKey, key) < 0))
}

func NativeGenesisCapability(encoded []byte) ([32]byte, bool, error) {
	invalid := errors.New("non-canonical native genesis capability")
	if len(encoded) == 0 || len(encoded) > MaxNativeGenesisBytes { return [32]byte{}, false, invalid }
	r := genesisReader{data: encoded}
	version := r.number(2)
	if r.number(2) != 0x4701 || version < 1 || version > 3 || r.number(2) != version {
		return [32]byte{}, false, invalid
	}
	if r.number(4) == 0 || r.number(8) == 0 { return [32]byte{}, false, invalid }
	var previousModule uint64
	var previousKey []byte
	var tif, oracle, perps bool
	for i, n := uint64(0), r.count(1, 64); i < n && !r.bad; i++ {
		module, key, value := r.number(2), r.blob(32, 32), r.blob(32, 32)
		if r.bad { break }
		if !genesisOrdered(module, key, previousModule, previousKey) || genesisZero(key) { r.bad = true; break }
		previousModule, previousKey = module, key
		for _, parameter := range []string{"perps-order-tif", "perps-oracle-transport", "module-enable:perps"} {
			if !bytes.HasPrefix(key, []byte(parameter)) { continue }
			if !bytes.Equal(key, genesisKey(parameter)) || module != uint64(GovernanceModuleID) ||
				!genesisZero(value[:31]) || value[31] > 1 { r.bad = true; break }
			switch parameter {
			case "perps-order-tif":
				if version != 3 || value[31] != 1 { r.bad = true }; tif = value[31] == 1
			case "perps-oracle-transport":
				if version != 3 || value[31] != 1 { r.bad = true }; oracle = value[31] == 1
			case "module-enable:perps": perps = value[31] == 1
			}
		}
	}
	previousKey = nil
	for i, n := uint64(0), r.count(1, 32); i < n && !r.bad; i++ {
		id, key, bond := r.blob(32, 32), r.blob(33, 33), r.take(16)
		if genesisZero(id) || genesisZero(key) || !genesisZero(bond) ||
			(previousKey != nil && bytes.Compare(previousKey, id) >= 0) { r.bad = true }
		previousKey = id
	}
	previousKey = nil
	var asset []byte
	seen := map[uint64]bool{}
	for i, n := uint64(0), r.count(3, 256); i < n && !r.bad; i++ {
		id, accountAsset, balance := r.blob(32, 32), r.blob(32, 32), r.take(16)
		locked, kind, parent := r.number(1), r.number(2), r.blob(32, 32)
		name := map[uint64]string{9: "system:insurance", 10: "system:fees", 11: "system:paxeer-reserve", 12: "system:paxeer-withdrawals"}[kind]
		derived, err := codec.DeriveAccountID([]byte(name))
		if err != nil || !bytes.Equal(id, derived[:]) || genesisZero(accountAsset) ||
			!genesisZero(balance) || locked != 0 || !genesisZero(parent) || seen[kind] ||
			(asset != nil && !bytes.Equal(asset, accountAsset)) ||
			(previousKey != nil && bytes.Compare(previousKey, id) >= 0) { r.bad = true }
		seen[kind], asset, previousKey = true, accountAsset, id
	}
	if !seen[10] || !seen[11] || !seen[12] { r.bad = true }
	previousModule, previousKey = 0, nil
	for i, n := uint64(0), r.count(0, 128); i < n && !r.bad; i++ {
		module, key, value := r.number(2), r.blob(32, 32), r.blob(-1, 256)
		if !genesisOrdered(module, key, previousModule, previousKey) || len(value) == 0 { r.bad = true }
		previousModule, previousKey = module, key
	}
	contentEnd := r.offset
	stateRoot, receiptRoot, publicKey := r.blob(32, 32), r.blob(32, 32), r.blob(32, 32)
	signatureStart := r.offset
	signature := r.blob(64, 64)
	if r.bad || r.offset != len(encoded) || genesisZero(stateRoot) || genesisZero(receiptRoot) ||
		(tif && (!oracle || !perps)) || (oracle && !perps) { return [32]byte{}, false, invalid }
	var pk [32]byte
	var sig [64]byte
	copy(pk[:], publicKey)
	copy(sig[:], signature)
	if err := verify.Ed25519(pk, sig, encoded[:signatureStart]); err != nil { return [32]byte{}, false, err }
	h := sha256.New()
	h.Write([]byte("LXP/v1/genesis-manifest\x00"))
	h.Write(encoded[4:contentEnd])
	h.Write(publicKey)
	var commitment [32]byte
	copy(commitment[:], h.Sum(nil))
	preimage := binary.BigEndian.AppendUint32(nil, binary.BigEndian.Uint32(encoded[6:10]))
	preimage = append(preimage, stateRoot...)
	receiptDigest := sha256.Sum256(append([]byte("LXP/v1/genesis-receipt-root\x00"), preimage...))
	if !bytes.Equal(receiptRoot, receiptDigest[:]) { return [32]byte{}, false, invalid }
	return commitment, tif, nil
}
