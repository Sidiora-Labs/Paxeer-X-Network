//go:build cgo

package lxphost

/*
#include <stdlib.h>
#include <string.h>
#include "layerx/lxp_hash.h"
#include "layerx/lxp_module_ctx.h"
#include "layerx/programs.h"
#include "layerx/lx_asset.h"
#include "layerx/lxp_governance.h"

typedef struct lxphost_read_state {
    lxp_kernel kernel;
    lxp_state_store store;
    lxp_state_journal journal;
    uint64_t parameters;
} lxphost_read_state;

static lxphost_read_state *lxphost_read_new(uint64_t sequence, uint64_t epoch,
                                            int32_t *status)
{
    lxphost_read_state *state = calloc(1U, sizeof(*state));
    if (state == NULL) {
        *status = LXP_ERR_ARENA_EXHAUSTED;
        return NULL;
    }
    *status = lxp_state_store_init(&state->store, sequence);
    if (*status != LXP_OK) {
        free(state);
        return NULL;
    }
    state->parameters = 1U;
    *status = lxp_kernel_create(&state->kernel, &state->store, &state->journal,
                                &state->parameters, epoch);
    if (*status != LXP_OK) {
        (void)lxp_state_store_destroy(&state->store);
        free(state);
        return NULL;
    }
    return state;
}

static int32_t lxphost_read_free(lxphost_read_state *state)
{
    int32_t status = lxp_state_store_destroy(&state->store);
    free(state);
    return status;
}

static int32_t lxphost_read_kv(lxphost_read_state *state, uint16_t module,
                               const uint8_t *key, size_t key_length,
                               const uint8_t *value, size_t value_length)
{
    lxp_module_kv_entry *entry;
    if (module == 0U || key == NULL || key_length == 0U ||
        key_length > LXP_MODULE_MAX_KEY_BYTES ||
        (value == NULL && value_length != 0U) ||
        value_length > LXP_MODULE_MAX_VALUE_BYTES)
        return LXP_FATAL_INVARIANT;
    if (state->kernel.module_kv_count >= LXP_KERNEL_MAX_MODULE_KV)
        return LXP_ERR_ARENA_EXHAUSTED;
    entry = &state->kernel.module_kv[state->kernel.module_kv_count++];
    entry->module_id = module;
    entry->key_length = (uint16_t)key_length;
    entry->value_length = (uint32_t)value_length;
    memcpy(entry->key, key, key_length);
    if (value_length != 0U) memcpy(entry->value, value, value_length);
    return LXP_OK;
}

static int32_t lxphost_read_replay_load(lxphost_read_state *state,
                                        const uint8_t hash[32],
                                        const uint8_t *bytes, size_t length)
{
    lxp_idempotency_key_state *entry;
    int32_t status;
    if (state->store.idempotency_count >= LXP_STATE_MAX_IDEMPOTENCY)
        return LXP_ERR_ARENA_EXHAUSTED;
    status = lxp_idempotency_reserve(&state->store);
    if (status != LXP_OK) return status;
    entry = &state->store.idempotency[state->store.idempotency_count];
    status = lxp_idempotency_snapshot_decode(entry, bytes, length);
    if (status != LXP_OK) return status;
    memcpy(entry->key_hash, hash, 32U);
    ++state->store.idempotency_count;
    return LXP_OK;
}

static int32_t lxphost_read_asset(lxphost_read_state *state,
                                  const uint8_t id[32], lx_asset_record *out)
{
    lx_asset_record *record = NULL;
    lx_asset_registry *assets = calloc(1U, sizeof(*assets));
    int32_t status;
    if (assets == NULL) return LXP_ERR_ARENA_EXHAUSTED;
    status = lx_asset_committed_records(&state->kernel, assets->assets,
                                         LX_ASSET_REGISTRY_CAPACITY,
                                         &assets->count);
    if (status == LXP_OK) status = lx_asset_lookup(assets, id, &record);
    if (status == LXP_OK) *out = *record;
    free(assets);
    return status;
}

static int32_t lxphost_read_total(lxphost_read_state *state,
                                  const uint8_t id[32], lxp_u128 *out)
{
    lx_asset_registry *assets = calloc(1U, sizeof(*assets));
    int32_t status;
    if (assets == NULL) return LXP_ERR_ARENA_EXHAUSTED;
    status = lx_asset_committed_records(&state->kernel, assets->assets,
                                         LX_ASSET_REGISTRY_CAPACITY,
                                         &assets->count);
    if (status == LXP_OK)
        status = lx_asset_total_units(assets, state->store.accounts, id, out);
    free(assets);
    return status;
}

static int32_t lxphost_read_account(lxphost_read_state *state,
                                    const uint8_t *name, size_t name_length,
                                    const uint8_t id[32], lx_account *out)
{
    lx_account *account = NULL;
    int32_t status = lx_account_lookup(state->store.accounts, name,
                                        name_length, id, &account);
    if (status == LXP_OK) *out = *account;
    return status;
}

static int32_t lxphost_read_owner(lxphost_read_state *state,
                                  const uint8_t id[32], uint8_t owner[32])
{
    size_t slot = 0U;
    int32_t status = lx_account_registry_index_lookup(state->store.accounts,
                                                      id, &slot);
    if (status != LXP_OK) return status;
    return lx_account_owner_did(&state->store.accounts->accounts[slot], owner);
}

static int32_t lxphost_read_receipt(lxphost_read_state *state,
                                    const uint8_t *did, size_t did_length,
                                    const uint8_t key[32], lxp_receipt *receipt,
                                    const uint8_t **encoded, size_t *length)
{
    int32_t status = lxp_idempotency_canonical_lookup(&state->store, did,
                                                      did_length, key,
                                                      encoded, length);
    if (status == LXP_OK) return LXP_ERR_UNKNOWN_FIELD;
    if (status != LXP_ERR_IDEMPOTENT_REPLAY) return status;
    return lxp_receipt_decode(*encoded, *length, true, receipt);
}
*/
import "C"

import (
	"encoding/binary"
	"math"
	"math/big"
	"runtime"
	"unsafe"

	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
)

type AssetRecord struct {
	AssetID          [32]byte
	Symbol           string
	Name             string
	Decimals         uint8
	SupplyCap        sdk.Int
	IssuerDID        [32]byte
	Salt             [32]byte
	IssuerKind       uint8
	CustodyKind      uint8
	CustodyReference []byte
	Paused           bool
	TotalUnits       sdk.Int
}

type AccountRecord struct {
	ID                [32]byte
	Name              string
	Kind              uint8
	Balance           sdk.Int
	AssetID           [32]byte
	HasAsset          bool
	NextSequence      uint64
	CreatedAtSequence uint64
	Frozen            bool
	HasOpenReference  bool
	AuthorityKey      [32]byte
	HasAuthorityKey   bool
}

type PayerGrantState struct {
	Grant           []byte
	DrawnTotal      sdk.Int
	DrawnThisPeriod sdk.Int
	WindowStart     uint64
	RevokedAt       uint64
	Revoked         bool
	InvoiceSettled  bool
}

func readAmount(amount C.struct_lxp_u128) sdk.Int {
	value := new(big.Int).SetUint64(uint64(amount.hi))
	value.Lsh(value, 64)
	value.Or(value, new(big.Int).SetUint64(uint64(amount.lo)))
	return sdk.NewIntFromBigInt(value)
}

func (h *Host) withRead(ctx sdk.Context, read func(*C.lxphost_read_state) error) (err error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.node == nil {
		return codeNonCanonical
	}
	branch := &branchCtx{store: ctx.KVStore(h.cfg.StoreKey)}
	last, err := branch.lastSequence()
	if err != nil {
		return err
	}
	if last == math.MaxUint64 {
		return Code(C.LXP_ERR_SEQUENCE_EXHAUSTED)
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	var status C.int32_t
	state := C.lxphost_read_new(C.uint64_t(last+1), C.uint64_t(h.cfg.Epoch), &status)
	if state == nil {
		return Code(status)
	}
	defer func() {
		if releaseErr := result(C.lxphost_read_free(state)); releaseErr != nil {
			err = releaseErr
		}
	}()
	if err := loadReadState(branch.store, state); err != nil {
		return err
	}
	return read(state)
}

func loadReadState(store sdk.KVStore, state *C.lxphost_read_state) error {
	for _, prefix := range []byte{moduleKVPrefix, idempotencyPrefix} {
		iterator := sdk.KVStorePrefixIterator(store, []byte{prefix})
		var err error
		for ; iterator.Valid(); iterator.Next() {
			key, value := iterator.Key(), iterator.Value()
			if prefix == moduleKVPrefix {
				if len(key) < 4 {
					err = codeFatalInvariant
					break
				}
				err = result(C.lxphost_read_kv(state, C.uint16_t(binary.BigEndian.Uint16(key[1:3])),
					bytesPtr(key[3:]), C.size_t(len(key)-3), bytesPtr(value), C.size_t(len(value))))
			} else {
				if len(key) != 33 {
					err = codeFatalInvariant
					break
				}
				err = result(C.lxphost_read_replay_load(state, bytesPtr(key[1:]),
					bytesPtr(value), C.size_t(len(value))))
			}
			if err != nil {
				break
			}
		}
		closeErr := iterator.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return codeFatalInvariant
		}
	}
	return nil
}

func (h *Host) ReadAsset(ctx sdk.Context, id [32]byte) (AssetRecord, error) {
	var out AssetRecord
	err := h.withRead(ctx, func(state *C.lxphost_read_state) error {
		var record C.lx_asset_record
		if err := result(C.lxphost_read_asset(state, bytes32(&id), &record)); err != nil {
			return err
		}
		if record.symbol_length > C.LX_ASSET_SYMBOL_MAX || record.name_length > C.LX_ASSET_NAME_MAX ||
			record.custody_reference_length > C.LX_ASSET_CUSTODY_REFERENCE_MAX {
			return codeNonCanonical
		}
		out = AssetRecord{
			AssetID:          *(*[32]byte)(unsafe.Pointer(&record.asset_id[0])),
			Symbol:           C.GoStringN(&record.symbol[0], C.int(record.symbol_length)),
			Name:             string(C.GoBytes(unsafe.Pointer(&record.name[0]), C.int(record.name_length))),
			Decimals:         uint8(record.decimals),
			SupplyCap:        readAmount(record.supply_cap),
			IssuerDID:        *(*[32]byte)(unsafe.Pointer(&record.issuer_did32[0])),
			Salt:             *(*[32]byte)(unsafe.Pointer(&record.salt[0])),
			IssuerKind:       uint8(record.issuer_kind),
			CustodyKind:      uint8(record.custody_kind),
			CustodyReference: C.GoBytes(unsafe.Pointer(&record.custody_reference[0]), C.int(record.custody_reference_length)),
			Paused:           bool(record.paused),
			TotalUnits:       readAmount(record.total_units),
		}
		return nil
	})
	if err != nil {
		return AssetRecord{}, err
	}
	return out, nil
}

func (h *Host) ReadBalance(ctx sdk.Context, accountID, assetID [32]byte) (sdk.Int, error) {
	out := sdk.ZeroInt()
	err := h.withRead(ctx, func(state *C.lxphost_read_state) error {
		var amount C.struct_lxp_u128
		if err := result(C.lx_asset_balance_get(state.store.accounts, bytes32(&accountID), bytes32(&assetID), &amount)); err != nil {
			return err
		}
		out = readAmount(amount)
		return nil
	})
	if err != nil {
		return sdk.ZeroInt(), err
	}
	return out, nil
}

func (h *Host) ReadTotalUnits(ctx sdk.Context, assetID [32]byte) (sdk.Int, error) {
	out := sdk.ZeroInt()
	err := h.withRead(ctx, func(state *C.lxphost_read_state) error {
		var amount C.struct_lxp_u128
		if err := result(C.lxphost_read_total(state, bytes32(&assetID), &amount)); err != nil {
			return err
		}
		out = readAmount(amount)
		return nil
	})
	if err != nil {
		return sdk.ZeroInt(), err
	}
	return out, nil
}

func (h *Host) ReadAccountOwner(ctx sdk.Context, accountID [32]byte) ([32]byte, error) {
	var owner [32]byte
	err := h.withRead(ctx, func(state *C.lxphost_read_state) error {
		return result(C.lxphost_read_owner(state, bytes32(&accountID), bytes32(&owner)))
	})
	if err != nil {
		return [32]byte{}, err
	}
	return owner, nil
}

func (h *Host) ReadAccount(ctx sdk.Context, name string, accountID [32]byte) (AccountRecord, error) {
	var out AccountRecord
	err := h.withRead(ctx, func(state *C.lxphost_read_state) error {
		var account C.lx_account
		nameBytes := []byte(name)
		if err := result(C.lxphost_read_account(state, bytesPtr(nameBytes), C.size_t(len(nameBytes)), bytes32(&accountID), &account)); err != nil {
			return err
		}
		if account.name_length > C.LX_ACCOUNT_NAME_MAX {
			return codeNonCanonical
		}
		out = AccountRecord{
			ID:                *(*[32]byte)(unsafe.Pointer(&account.id[0])),
			Name:              string(C.GoBytes(unsafe.Pointer(&account.name[0]), C.int(account.name_length))),
			Kind:              uint8(account.kind),
			Balance:           readAmount(account.balance),
			AssetID:           *(*[32]byte)(unsafe.Pointer(&account.asset_id[0])),
			HasAsset:          bool(account.has_asset),
			NextSequence:      uint64(account.next_sequence),
			CreatedAtSequence: uint64(account.created_at_sequence),
			Frozen:            bool(account.frozen),
			HasOpenReference:  bool(account.has_open_reference),
			AuthorityKey:      *(*[32]byte)(unsafe.Pointer(&account.authority_key[0])),
			HasAuthorityKey:   bool(account.has_authority_key),
		}
		return nil
	})
	if err != nil {
		return AccountRecord{}, err
	}
	return out, nil
}

func (h *Host) ReadSessionKey(ctx sdk.Context, grantID [32]byte) ([]byte, error) {
	var summary [209]byte
	err := h.withRead(ctx, func(state *C.lxphost_read_state) error {
		return result(C.lxp_governance_session_key_read(&state.kernel, bytes32(&grantID), bytesPtr(summary[:])))
	})
	if err != nil {
		return nil, err
	}
	return summary[:], nil
}

func (h *Host) ReadPayerGrant(ctx sdk.Context, grantID [32]byte) (PayerGrantState, error) {
	var out PayerGrantState
	err := h.withRead(ctx, func(state *C.lxphost_read_state) error {
		var grant C.lxp_grant_state
		if err := result(C.lx_asset_payer_grant_read(&state.kernel, bytes32(&grantID), &grant)); err != nil {
			return err
		}
		var encoded [1024]byte
		var length C.size_t
		if err := result(C.lxp_payer_grant_encode(&grant.grant, bytesPtr(encoded[:]), C.size_t(len(encoded)), &length)); err != nil {
			return err
		}
		if length > C.size_t(len(encoded)) {
			return codeFatalInvariant
		}
		out = PayerGrantState{
			Grant:           append([]byte(nil), encoded[:int(length)]...),
			DrawnTotal:      readAmount(grant.drawn_total),
			DrawnThisPeriod: readAmount(grant.drawn_this_period),
			WindowStart:     uint64(grant.window_start),
			RevokedAt:       uint64(grant.revoked_at_sequence),
			Revoked:         bool(grant.revoked),
			InvoiceSettled:  bool(grant.invoice_settled),
		}
		return nil
	})
	if err != nil {
		return PayerGrantState{}, err
	}
	return out, nil
}

func (h *Host) ReadIdempotentReceipt(ctx sdk.Context, actorDID []byte, key [32]byte) (Receipt, error) {
	var out Receipt
	err := h.withRead(ctx, func(state *C.lxphost_read_state) error {
		var receipt C.struct_lxp_receipt
		var encoded *C.uint8_t
		var length C.size_t
		if err := result(C.lxphost_read_receipt(state, bytesPtr(actorDID), C.size_t(len(actorDID)), bytes32(&key),
			&receipt, &encoded, &length)); err != nil {
			return err
		}
		if length > C.LXP_STATE_MAX_RECEIPT_BYTES {
			return codeFatalInvariant
		}
		out = Receipt{
			ResultCode:         Code(receipt.result_code),
			GlobalSequence:     uint64(receipt.global_sequence),
			Timestamp:          uint64(receipt.timestamp),
			PreviousStateRoot:  *(*[32]byte)(unsafe.Pointer(&receipt.previous_state_root[0])),
			ResultingStateRoot: *(*[32]byte)(unsafe.Pointer(&receipt.resulting_state_root[0])),
			Encoded:            C.GoBytes(unsafe.Pointer(encoded), C.int(length)),
		}
		return nil
	})
	if err != nil {
		return Receipt{}, err
	}
	return out, nil
}
