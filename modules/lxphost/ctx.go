//go:build cgo

package lxphost

/*
#include <string.h>
#include "layerx/lxp_ledger.h"
#include "layerx/lxp_snapshot.h"

enum { LXPHOST_ACCOUNT_RECORD_BYTES = 256 + LX_ACCOUNT_NAME_MAX };

static int32_t lxphost_account_restore(void *registry, const uint8_t id[32],
                                       const uint8_t *bytes, size_t length)
{
    lx_account account;
    int32_t status;
    if (registry == NULL || id == NULL) return LXP_FATAL_INVARIANT;
    status = lx_account_record_decode(bytes, length, &account);
    if (status != LXP_OK) return status;
    if (memcmp(account.id, id, 32U) != 0) return LXP_FATAL_INVARIANT;
    return lx_account_registry_slot_insert((lx_account_registry *)registry,
                                           &account, NULL);
}

static int32_t lxphost_account_index_rebuild(void *registry)
{
    if (registry == NULL) return LXP_FATAL_INVARIANT;
    return lx_account_registry_index_rebuild((lx_account_registry *)registry);
}

static size_t lxphost_account_count(const void *registry)
{
    return registry == NULL ? 0U :
        ((const lx_account_registry *)registry)->count;
}

static int32_t lxphost_account_encode(const void *registry, size_t index,
                                      uint8_t id[32], uint8_t *bytes,
                                      size_t capacity, size_t *length)
{
    const lx_account_registry *accounts = (const lx_account_registry *)registry;
    if (accounts == NULL || accounts->count > (size_t)LX_ACCOUNT_REGISTRY_CAPACITY ||
        index >= accounts->count || accounts->accounts == NULL)
        return LXP_FATAL_INVARIANT;
    (void)memcpy(id, accounts->accounts[index].id, 32U);
    return lx_account_record_encode(&accounts->accounts[index], bytes, capacity,
                                    length);
}
*/
import "C"

import (
	"bytes"
	"encoding/binary"
	"sort"
	"unsafe"

	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
)

const (
	moduleKVPrefix         byte = 0x01
	lastSequenceKey        byte = 0x02
	identitySequencePrefix byte = 0x03
	idempotencyPrefix      byte = 0x04
	accountPrefix          byte = 0x06
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
	rows, err := restoreAccounts(b.store, h.accountRegistry())
	if err != nil {
		return err
	}
	for key, value := range rows {
		b.loaded[key] = value
	}
	return nil
}

// loadCommittedAccounts restores every persisted kernel account row of the
// store into the account registry, passed as an opaque pointer, and rebuilds
// its index.
func loadCommittedAccounts(store sdk.KVStore, registry unsafe.Pointer) error {
	_, err := restoreAccounts(store, registry)
	return err
}

func restoreAccounts(store sdk.KVStore, registry unsafe.Pointer) (map[string][]byte, error) {
	rows := map[string][]byte{}
	iterator := sdk.KVStorePrefixIterator(store, []byte{accountPrefix})
	for ; iterator.Valid(); iterator.Next() {
		key := append([]byte(nil), iterator.Key()...)
		if len(key) != 33 {
			iterator.Close()
			return nil, codeFatalInvariant
		}
		value := append([]byte(nil), iterator.Value()...)
		if err := result(C.lxphost_account_restore(registry, bytesPtr(key[1:]), bytesPtr(value), C.size_t(len(value)))); err != nil {
			iterator.Close()
			return nil, err
		}
		rows[string(key)] = value
	}
	if err := iterator.Close(); err != nil {
		return nil, codeFatalInvariant
	}
	if err := result(C.lxphost_account_index_rebuild(registry)); err != nil {
		return nil, err
	}
	return rows, nil
}

func committedAccounts(registry unsafe.Pointer) (map[string][]byte, error) {
	count := int(C.lxphost_account_count(registry))
	rows := make(map[string][]byte, count)
	buffer := make([]byte, C.LXPHOST_ACCOUNT_RECORD_BYTES)
	for i := 0; i < count; i++ {
		var id [32]byte
		var length C.size_t
		if err := result(C.lxphost_account_encode(registry, C.size_t(i), bytes32(&id),
			bytesPtr(buffer), C.size_t(len(buffer)), &length)); err != nil {
			return nil, err
		}
		key := accountKey(id)
		if _, duplicate := rows[string(key)]; duplicate {
			return nil, codeFatalInvariant
		}
		rows[string(key)] = append([]byte(nil), buffer[:length]...)
	}
	return rows, nil
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
	accounts, err := committedAccounts(h.accountRegistry())
	if err != nil {
		return err
	}
	staged := make(map[string][]byte, len(entries)+len(accounts))
	for key, value := range accounts {
		staged[key] = value
	}
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

func accountKey(id [32]byte) []byte {
	return append([]byte{accountPrefix}, id[:]...)
}

func identitySequenceKey(did [32]byte) []byte {
	return append([]byte{identitySequencePrefix}, did[:]...)
}
