//go:build cgo

package lxphost

import (
	"bytes"
	"encoding/binary"
	"sort"

	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
)

const (
	moduleKVPrefix         byte = 0x01
	lastSequenceKey        byte = 0x02
	identitySequencePrefix byte = 0x03
	idempotencyPrefix      byte = 0x04
)

type sequenceContextKey struct{}

func WithSequence(ctx sdk.Context, sequence uint64) sdk.Context {
	return ctx.WithValue(sequenceContextKey{}, sequence)
}

type branchCtx struct {
	store       sdk.KVStore
	sequence    uint64
	timestampMs uint64
	batchNumber uint64
	loaded      map[string][]byte
	idempotency int
}

func newBranchCtx(ctx sdk.Context, key sdk.StoreKey) (*branchCtx, error) {
	sequence, ok := ctx.Value(sequenceContextKey{}).(uint64)
	if !ok || sequence == 0 {
		return nil, codeNonCanonical
	}
	timestamp := ctx.BlockTime().UnixMilli()
	height := ctx.BlockHeight()
	if timestamp < 0 || height <= 0 {
		return nil, codeNonCanonical
	}
	branch := &branchCtx{
		store:       ctx.KVStore(key),
		sequence:    sequence,
		timestampMs: uint64(timestamp),
		batchNumber: uint64(height),
		loaded:      map[string][]byte{},
	}
	last, err := branch.lastSequence()
	if err != nil {
		return nil, err
	}
	if sequence <= last {
		return nil, codeSequenceReused
	}
	return branch, nil
}

func (b *branchCtx) lastSequence() (uint64, error) {
	value := b.store.Get([]byte{lastSequenceKey})
	if value == nil {
		return 0, nil
	}
	if len(value) != 8 {
		return 0, codeFatalInvariant
	}
	return binary.BigEndian.Uint64(value), nil
}

func (b *branchCtx) identitySequence(did [32]byte) (uint64, error) {
	value := b.store.Get(identitySequenceKey(did))
	if value == nil {
		return 0, nil
	}
	if len(value) != 8 {
		return 0, codeFatalInvariant
	}
	return binary.BigEndian.Uint64(value), nil
}

func (b *branchCtx) load(h *Host) error {
	iterator := sdk.KVStorePrefixIterator(b.store, []byte{moduleKVPrefix})
	for ; iterator.Valid(); iterator.Next() {
		key := iterator.Key()
		if len(key) < 4 {
			iterator.Close()
			return codeFatalInvariant
		}
		value := append([]byte(nil), iterator.Value()...)
		if err := h.loadModuleKV(binary.BigEndian.Uint16(key[1:3]), key[3:], value); err != nil {
			iterator.Close()
			return err
		}
		b.loaded[string(key)] = value
	}
	if err := iterator.Close(); err != nil {
		return codeFatalInvariant
	}
	iterator = sdk.KVStorePrefixIterator(b.store, []byte{idempotencyPrefix})
	for ; iterator.Valid(); iterator.Next() {
		key := iterator.Key()
		if len(key) != 33 {
			iterator.Close()
			return codeFatalInvariant
		}
		var hash [32]byte
		copy(hash[:], key[1:])
		if err := h.loadIdempotency(hash, iterator.Value()); err != nil {
			iterator.Close()
			return err
		}
		b.idempotency++
	}
	if err := iterator.Close(); err != nil {
		return codeFatalInvariant
	}
	return nil
}

func (b *branchCtx) commit(h *Host, did [32]byte) error {
	entries, err := h.moduleKV()
	if err != nil {
		return err
	}
	replays, err := h.idempotencyFrom(b.idempotency)
	if err != nil {
		return err
	}
	next, err := h.actorSequence(did)
	if err != nil {
		return err
	}
	staged := make(map[string][]byte, len(entries))
	for _, entry := range entries {
		key := moduleKVKey(entry.module, entry.key)
		if _, duplicate := staged[string(key)]; duplicate {
			return codeFatalInvariant
		}
		staged[string(key)] = entry.value
	}
	keys := make([]string, 0, len(staged)+len(b.loaded))
	for key := range staged {
		keys = append(keys, key)
	}
	for key := range b.loaded {
		if _, kept := staged[key]; !kept {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	for _, key := range keys {
		value, kept := staged[key]
		previous, existed := b.loaded[key]
		switch {
		case !kept:
			b.store.Delete([]byte(key))
		case !existed || !bytes.Equal(previous, value):
			b.store.Set([]byte(key), value)
		}
	}
	for _, replay := range replays {
		b.store.Set(append([]byte{idempotencyPrefix}, replay.hash[:]...), replay.entry)
	}
	sequence := make([]byte, 8)
	binary.BigEndian.PutUint64(sequence, next)
	b.store.Set(identitySequenceKey(did), sequence)
	last := make([]byte, 8)
	binary.BigEndian.PutUint64(last, b.sequence)
	b.store.Set([]byte{lastSequenceKey}, last)
	return nil
}

func moduleKVKey(module uint16, key []byte) []byte {
	out := make([]byte, 3, 3+len(key))
	out[0] = moduleKVPrefix
	binary.BigEndian.PutUint16(out[1:3], module)
	return append(out, key...)
}

func identitySequenceKey(did [32]byte) []byte {
	return append([]byte{identitySequencePrefix}, did[:]...)
}
