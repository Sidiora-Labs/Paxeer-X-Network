//go:build cgo

package lxphost

/*
#cgo CFLAGS: -I${SRCDIR}/../../include -I${SRCDIR}/../../build/generated
#cgo LDFLAGS: -L${SRCDIR}/../../build -L${SRCDIR}/../../programs/target/debug -llayerx -llayerx_programs_sandbox -llayerx -lssl -lcrypto -pthread -ldl -lm
#include <stdlib.h>
#include <string.h>
#include "layerx/lxp_hash.h"
#include "layerx/lxp_module_ctx.h"
#include "layerx/programs.h"

enum { LXPHOST_ARENA_BYTES = 4194304 };

typedef struct lxphost_envelope {
    lxp_activity activity;
    uint8_t did[LXP_MAX_DID_LENGTH];
    uint8_t authority[32];
    uint8_t signature[128];
    uint8_t payload[LXP_MAX_PAYLOAD_BYTES];
} lxphost_envelope;

typedef struct lxphost_node {
    lxp_kernel kernel;
    lxp_state_store store;
    lxp_state_journal journal;
    lxp_identity_store identities;
    lxp_fee_params fees;
    lxp_authority_resolved authority;
    lxp_kernel_execution execution;
    lxp_receipt receipt;
    lxp_arena arena;
    lxphost_envelope envelope;
    uint64_t parameters;
    uint64_t epoch;
    int store_ready;
    uint8_t arena_bytes[LXPHOST_ARENA_BYTES];
} lxphost_node;

typedef struct lxphost_kv_view {
    uint16_t module_id;
    const uint8_t *key;
    size_t key_length;
    const uint8_t *value;
    size_t value_length;
} lxphost_kv_view;

typedef struct lxphost_outcome {
    int32_t result_code;
    uint64_t global_sequence;
    uint64_t timestamp;
    uint8_t previous_state_root[32];
    uint8_t resulting_state_root[32];
    const uint8_t *encoded;
    size_t encoded_length;
} lxphost_outcome;

static void lxphost_release_blobs(lxp_kernel *kernel)
{
    size_t i;
    for (i = 0U; i < kernel->blob_count && i < (size_t)LXP_KERNEL_MAX_BLOBS; ++i) {
        free(kernel->blobs[i].bytes);
        kernel->blobs[i].bytes = NULL;
    }
    kernel->blob_count = 0U;
}

static lxphost_node *lxphost_node_new(uint64_t epoch, int32_t *status)
{
    lxphost_node *node = (lxphost_node *)calloc(1U, sizeof(*node));
    if (node == NULL) {
        *status = LXP_ERR_ARENA_EXHAUSTED;
        return NULL;
    }
    node->epoch = epoch;
    node->parameters = 1U;
    *status = lxp_kernel_create(&node->kernel, &node->store, &node->journal,
                                &node->parameters, epoch);
    if (*status != LXP_OK) {
        free(node);
        return NULL;
    }
    return node;
}

static void lxphost_node_free(lxphost_node *node)
{
    if (node == NULL) return;
    lxphost_release_blobs(&node->kernel);
    if (node->store_ready != 0) (void)lxp_state_store_destroy(&node->store);
    free(node);
}

static lxphost_envelope *lxphost_node_envelope(lxphost_node *node)
{
    return &node->envelope;
}

static int32_t lxphost_reset(lxphost_node *node, uint64_t first_sequence)
{
    uint64_t epoch = node->epoch;
    int32_t status;
    lxphost_release_blobs(&node->kernel);
    if (node->store_ready != 0) {
        node->store_ready = 0;
        status = lxp_state_store_destroy(&node->store);
        if (status != LXP_OK) return status;
    }
    (void)memset(node, 0, sizeof(*node));
    node->epoch = epoch;
    node->parameters = 1U;
    node->fees.version = 1U;
    node->fees.multiplier_basis_points = 10000U;
    status = lxp_state_store_init(&node->store, first_sequence);
    if (status != LXP_OK) return status;
    node->store_ready = 1;
    status = lxp_idempotency_reserve(&node->store);
    if (status == LXP_OK)
        status = lxp_arena_init(&node->arena, node->arena_bytes,
                                sizeof(node->arena_bytes));
    if (status == LXP_OK)
        status = lxp_kernel_create(&node->kernel, &node->store, &node->journal,
                                   &node->parameters, epoch);
    if (status == LXP_OK)
        status = lxp_kernel_register_module(&node->kernel,
                                            lxp_governance_module_iface());
    if (status == LXP_OK)
        status = lxp_kernel_register_module(&node->kernel,
                                            programs_module_registration());
    return status;
}

static int32_t lxphost_envelope_write(
    lxphost_envelope *envelope, uint16_t protocol_version, uint32_t network_id,
    uint32_t activity_type, const uint8_t *did, size_t did_length,
    const uint8_t authority[32], uint64_t account_sequence,
    uint64_t not_before, uint64_t not_after,
    const uint8_t idempotency_key[32], const uint8_t *payload,
    size_t payload_length, const uint8_t *signature, size_t signature_length)
{
    lxp_activity *activity = &envelope->activity;
    if (did == NULL || did_length == 0U || did_length > sizeof(envelope->did) ||
        (payload == NULL && payload_length != 0U) ||
        payload_length > sizeof(envelope->payload) ||
        (signature == NULL && signature_length != 0U) ||
        signature_length > sizeof(envelope->signature))
        return LXP_ERR_MALFORMED_ENVELOPE;
    (void)memset(envelope, 0, sizeof(*envelope));
    (void)memcpy(envelope->did, did, did_length);
    (void)memcpy(envelope->authority, authority, 32U);
    if (payload_length != 0U)
        (void)memcpy(envelope->payload, payload, payload_length);
    if (signature_length != 0U)
        (void)memcpy(envelope->signature, signature, signature_length);
    activity->protocol_version = protocol_version;
    activity->network_id = network_id;
    activity->activity_type = activity_type;
    activity->actor_did = (lxp_byte_span){envelope->did, did_length};
    activity->authority = (lxp_byte_span){envelope->authority, 32U};
    activity->account_sequence = account_sequence;
    activity->timestamp_bound = (lxp_timestamp_bound){not_before, not_after};
    (void)memcpy(activity->idempotency_key, idempotency_key, 32U);
    activity->payload = (lxp_byte_span){envelope->payload, payload_length};
    activity->signature = (lxp_byte_span){envelope->signature, signature_length};
    return lxp_hash_payload(envelope->payload, payload_length,
                            activity->payload_hash);
}

static int32_t lxphost_envelope_preimage(const lxphost_envelope *envelope,
                                         uint8_t preimage[32])
{
    return lxp_activity_signing_preimage(&envelope->activity, preimage);
}

static void lxphost_authority_write(lxphost_node *node, const uint8_t actor[32],
                                    const uint8_t principal[32],
                                    const uint8_t verified_key[32])
{
    (void)memset(&node->authority, 0, sizeof(node->authority));
    (void)memcpy(node->authority.actor, actor, 32U);
    (void)memcpy(node->authority.principal, principal, 32U);
    (void)memcpy(node->authority.verified_key, verified_key, 32U);
    node->authority.kind = LXP_AUTHORITY_OWNER;
}

static int32_t lxphost_load_kv(lxphost_node *node, uint16_t module_id,
                               const uint8_t *key, size_t key_length,
                               const uint8_t *value, size_t value_length)
{
    lxp_module_kv_entry *entry;
    if (module_id == 0U || key == NULL || key_length == 0U ||
        key_length > (size_t)LXP_MODULE_MAX_KEY_BYTES ||
        (value == NULL && value_length != 0U) ||
        value_length > (size_t)LXP_MODULE_MAX_VALUE_BYTES)
        return LXP_FATAL_INVARIANT;
    if (node->kernel.module_kv_count >= (size_t)LXP_KERNEL_MAX_MODULE_KV)
        return LXP_ERR_ARENA_EXHAUSTED;
    entry = &node->kernel.module_kv[node->kernel.module_kv_count++];
    entry->module_id = module_id;
    entry->key_length = (uint16_t)key_length;
    entry->value_length = (uint32_t)value_length;
    (void)memcpy(entry->key, key, key_length);
    if (value_length != 0U) (void)memcpy(entry->value, value, value_length);
    return LXP_OK;
}

static size_t lxphost_kv_count(const lxphost_node *node)
{
    return node->kernel.module_kv_count;
}

static int32_t lxphost_kv_read(const lxphost_node *node, size_t index,
                               lxphost_kv_view *view)
{
    const lxp_module_kv_entry *entry;
    if (node->kernel.module_kv_count > (size_t)LXP_KERNEL_MAX_MODULE_KV ||
        index >= node->kernel.module_kv_count)
        return LXP_FATAL_INVARIANT;
    entry = &node->kernel.module_kv[index];
    if (entry->module_id == 0U || entry->key_length == 0U ||
        entry->key_length > LXP_MODULE_MAX_KEY_BYTES ||
        entry->value_length > LXP_MODULE_MAX_VALUE_BYTES)
        return LXP_FATAL_INVARIANT;
    view->module_id = entry->module_id;
    view->key = entry->key;
    view->key_length = entry->key_length;
    view->value = entry->value;
    view->value_length = entry->value_length;
    return LXP_OK;
}

static int32_t lxphost_load_idempotency(lxphost_node *node,
                                        const uint8_t key_hash[32],
                                        const uint8_t *bytes, size_t length)
{
    lxp_idempotency_key_state *entry;
    int32_t status;
    if (node->store.idempotency == NULL)
        return LXP_FATAL_INVARIANT;
    if (node->store.idempotency_count >= (size_t)LXP_STATE_MAX_IDEMPOTENCY)
        return LXP_ERR_ARENA_EXHAUSTED;
    entry = &node->store.idempotency[node->store.idempotency_count];
    status = lxp_idempotency_snapshot_decode(entry, bytes, length);
    if (status != LXP_OK) return status;
    (void)memcpy(entry->key_hash, key_hash, 32U);
    ++node->store.idempotency_count;
    return LXP_OK;
}

static size_t lxphost_idempotency_count(const lxphost_node *node)
{
    return node->store.idempotency_count;
}

static int32_t lxphost_idempotency_read(const lxphost_node *node, size_t index,
                                        uint8_t key_hash[32], uint8_t *bytes,
                                        size_t capacity, size_t *length)
{
    const lxp_idempotency_key_state *entry;
    if (node->store.idempotency == NULL ||
        node->store.idempotency_count > (size_t)LXP_STATE_MAX_IDEMPOTENCY ||
        index >= node->store.idempotency_count)
        return LXP_FATAL_INVARIANT;
    entry = &node->store.idempotency[index];
    (void)memcpy(key_hash, entry->key_hash, 32U);
    return lxp_idempotency_snapshot_encode(entry, bytes, capacity, length);
}

static lxp_identity *lxphost_identity_find(lxphost_node *node,
                                           const uint8_t did_id[32])
{
    size_t i;
    for (i = 0U; i < node->identities.count &&
                 i < (size_t)LXP_IDENTITY_STORE_CAPACITY; ++i)
        if (memcmp(node->identities.identities[i].did_id, did_id, 32U) == 0)
            return &node->identities.identities[i];
    return NULL;
}

static int32_t lxphost_bind_actor(lxphost_node *node, uint8_t did_id[32])
{
    const lxp_activity *activity = &node->envelope.activity;
    lxp_identity *identity;
    int32_t status = lxp_governance_identities_restore(&node->kernel,
                                                       &node->identities);
    if (status == LXP_OK)
        status = lxp_did_id_derive(activity->actor_did.bytes,
                                   activity->actor_did.length, did_id);
    if (status != LXP_OK) return status;
    if (lxphost_identity_find(node, did_id) != NULL) return LXP_OK;
    return lxp_identity_register(&node->identities, activity->actor_did.bytes,
                                 activity->actor_did.length,
                                 node->authority.verified_key, &identity);
}

static int32_t lxphost_actor_sequence_set(lxphost_node *node,
                                          const uint8_t did_id[32],
                                          uint64_t sequence)
{
    lxp_identity *identity = lxphost_identity_find(node, did_id);
    if (identity == NULL) return LXP_ERR_UNKNOWN_DID;
    identity->next_sequence = sequence;
    return LXP_OK;
}

static int32_t lxphost_actor_sequence_get(lxphost_node *node,
                                          const uint8_t did_id[32],
                                          uint64_t *sequence)
{
    lxp_identity *identity = lxphost_identity_find(node, did_id);
    if (identity == NULL) return LXP_FATAL_INVARIANT;
    *sequence = identity->next_sequence;
    return LXP_OK;
}

static int32_t lxphost_execute(lxphost_node *node, uint32_t network_id,
                               uint64_t batch_number, uint64_t timestamp_ms,
                               uint64_t timestamp_window, uint64_t sequence,
                               uint32_t parameter_version, uint64_t gas_limit)
{
    const lxp_module_registration *registration;
    lxp_kernel_execution *execution = &node->execution;
    int32_t status = lxp_kernel_module_for_activity(
        &node->kernel, node->envelope.activity.activity_type,
        node->kernel.epoch, &registration);
    if (status == LXP_OK)
        status = lxp_state_root(&node->kernel, node->kernel.current_state_root);
    if (status != LXP_OK) return status;
    (void)memset(execution, 0, sizeof(*execution));
    execution->network_id = network_id;
    execution->batch_number = batch_number;
    execution->batch_timestamp_ms = timestamp_ms;
    execution->maximum_timestamp_window = timestamp_window;
    execution->epoch = node->kernel.epoch;
    execution->global_sequence = sequence;
    execution->recorded_module_version = registration->abi_version;
    execution->parameter_version = parameter_version;
    execution->signature_valid = true;
    execution->identities = &node->identities;
    execution->authority = &node->authority;
    execution->fee_parameters = &node->fees;
    execution->gas_limit = gas_limit;
    execution->arena = &node->arena;
    return lxp_kernel_execute_activity(&node->kernel, &node->envelope.activity,
                                       execution, &node->receipt);
}

static int32_t lxphost_outcome_read(lxphost_node *node, lxphost_outcome *outcome)
{
    lxp_byte_span encoded;
    int32_t status;
    if (node->kernel.publication_poisoned || node->journal.open ||
        node->store.count != 0U || node->store.accounts != NULL ||
        node->kernel.blob_count != 0U)
        return LXP_FATAL_INVARIANT;
    status = lxp_arena_reset(&node->arena, 0U);
    if (status == LXP_OK)
        status = lxp_receipt_encode(&node->receipt, false, &node->arena, &encoded);
    if (status != LXP_OK) return status;
    outcome->result_code = node->receipt.result_code;
    outcome->global_sequence = node->receipt.global_sequence;
    outcome->timestamp = node->receipt.timestamp;
    (void)memcpy(outcome->previous_state_root,
                 node->receipt.previous_state_root, 32U);
    (void)memcpy(outcome->resulting_state_root,
                 node->receipt.resulting_state_root, 32U);
    outcome->encoded = encoded.bytes;
    outcome->encoded_length = encoded.length;
    return LXP_OK;
}
*/
import "C"

import (
	"runtime"
	"sync"
	"unsafe"

	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
)

type Code int32

func (c Code) Error() string {
	return C.GoString(C.lxp_result_name(C.int32_t(c)))
}

const (
	codeNonCanonical   = Code(C.LXP_ERR_NON_CANONICAL)
	codeSequenceReused = Code(C.LXP_ERR_SEQUENCE_REUSED)
	codeFatalInvariant = Code(C.LXP_FATAL_INVARIANT)
)

type Config struct {
	StoreKey          sdk.StoreKey
	NetworkID         uint32
	Epoch             uint64
	ParameterVersion  uint32
	TimestampWindowMs uint64
	GasLimit          uint64
}

type Activity struct {
	ProtocolVersion uint16
	Type            uint32
	ActorDID        []byte
	AuthorityKey    [32]byte
	AccountSequence uint64
	NotBefore       uint64
	NotAfter        uint64
	IdempotencyKey  [32]byte
	Payload         []byte
	Signature       []byte
}

type KernelAuthority struct {
	Actor       [32]byte
	Principal   [32]byte
	VerifiedKey [32]byte
}

type Receipt struct {
	ResultCode         Code
	GlobalSequence     uint64
	Timestamp          uint64
	PreviousStateRoot  [32]byte
	ResultingStateRoot [32]byte
	Encoded            []byte
}

type Host struct {
	mu   sync.Mutex
	cfg  Config
	node *C.lxphost_node
}

type kvEntry struct {
	module uint16
	key    []byte
	value  []byte
}

type idempotencyEntry struct {
	hash  [32]byte
	entry []byte
}

func NewHost(cfg Config) (*Host, error) {
	if cfg.StoreKey == nil || cfg.NetworkID == 0 || cfg.ParameterVersion == 0 || cfg.GasLimit == 0 {
		return nil, codeNonCanonical
	}
	var status C.int32_t
	node := C.lxphost_node_new(C.uint64_t(cfg.Epoch), &status)
	if node == nil {
		return nil, Code(status)
	}
	return &Host{cfg: cfg, node: node}, nil
}

func (h *Host) Close() {
	h.mu.Lock()
	defer h.mu.Unlock()
	C.lxphost_node_free(h.node)
	h.node = nil
}

func (h *Host) Execute(ctx sdk.Context, activity Activity, authority KernelAuthority) (Receipt, error) {
	branch, err := newBranchCtx(ctx, h.cfg.StoreKey)
	if err != nil {
		return Receipt{}, err
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.node == nil {
		return Receipt{}, codeNonCanonical
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if err := result(C.lxphost_reset(h.node, C.uint64_t(branch.sequence))); err != nil {
		return Receipt{}, err
	}
	if err := writeEnvelope(C.lxphost_node_envelope(h.node), h.cfg.NetworkID, activity); err != nil {
		return Receipt{}, err
	}
	C.lxphost_authority_write(h.node, bytes32(&authority.Actor), bytes32(&authority.Principal), bytes32(&authority.VerifiedKey))
	if err := branch.load(h); err != nil {
		return Receipt{}, err
	}
	did, err := h.bindActor()
	if err != nil {
		return Receipt{}, err
	}
	next, err := branch.identitySequence(did)
	if err != nil {
		return Receipt{}, err
	}
	if err := result(C.lxphost_actor_sequence_set(h.node, bytes32(&did), C.uint64_t(next))); err != nil {
		return Receipt{}, err
	}
	if err := result(C.lxphost_execute(h.node, C.uint32_t(h.cfg.NetworkID), C.uint64_t(branch.batchNumber),
		C.uint64_t(branch.timestampMs), C.uint64_t(h.cfg.TimestampWindowMs), C.uint64_t(branch.sequence),
		C.uint32_t(h.cfg.ParameterVersion), C.uint64_t(h.cfg.GasLimit))); err != nil {
		return Receipt{}, err
	}
	var outcome C.lxphost_outcome
	if err := result(C.lxphost_outcome_read(h.node, &outcome)); err != nil {
		return Receipt{}, err
	}
	receipt := Receipt{
		ResultCode:         Code(outcome.result_code),
		GlobalSequence:     uint64(outcome.global_sequence),
		Timestamp:          uint64(outcome.timestamp),
		PreviousStateRoot:  *(*[32]byte)(unsafe.Pointer(&outcome.previous_state_root[0])),
		ResultingStateRoot: *(*[32]byte)(unsafe.Pointer(&outcome.resulting_state_root[0])),
		Encoded:            C.GoBytes(unsafe.Pointer(outcome.encoded), C.int(outcome.encoded_length)),
	}
	if err := branch.commit(h, did); err != nil {
		return Receipt{}, err
	}
	return receipt, nil
}

func (h *Host) SigningHash(activity Activity) ([32]byte, error) {
	var hash [32]byte
	envelope := (*C.lxphost_envelope)(C.calloc(1, C.sizeof_lxphost_envelope))
	if envelope == nil {
		return hash, Code(C.LXP_ERR_ARENA_EXHAUSTED)
	}
	defer C.free(unsafe.Pointer(envelope))
	if err := writeEnvelope(envelope, h.cfg.NetworkID, activity); err != nil {
		return hash, err
	}
	err := result(C.lxphost_envelope_preimage(envelope, bytes32(&hash)))
	return hash, err
}

func DIDID(did []byte) ([32]byte, error) {
	var id [32]byte
	err := result(C.lxp_did_id_derive(bytesPtr(did), C.size_t(len(did)), bytes32(&id)))
	return id, err
}

func writeEnvelope(envelope *C.lxphost_envelope, networkID uint32, a Activity) error {
	return result(C.lxphost_envelope_write(envelope, C.uint16_t(a.ProtocolVersion), C.uint32_t(networkID),
		C.uint32_t(a.Type), bytesPtr(a.ActorDID), C.size_t(len(a.ActorDID)), bytes32(&a.AuthorityKey),
		C.uint64_t(a.AccountSequence), C.uint64_t(a.NotBefore), C.uint64_t(a.NotAfter),
		bytes32(&a.IdempotencyKey), bytesPtr(a.Payload), C.size_t(len(a.Payload)),
		bytesPtr(a.Signature), C.size_t(len(a.Signature))))
}

func (h *Host) bindActor() ([32]byte, error) {
	var did [32]byte
	err := result(C.lxphost_bind_actor(h.node, bytes32(&did)))
	return did, err
}

func (h *Host) actorSequence(did [32]byte) (uint64, error) {
	var sequence C.uint64_t
	err := result(C.lxphost_actor_sequence_get(h.node, bytes32(&did), &sequence))
	return uint64(sequence), err
}

func (h *Host) loadModuleKV(module uint16, key, value []byte) error {
	return result(C.lxphost_load_kv(h.node, C.uint16_t(module), bytesPtr(key), C.size_t(len(key)),
		bytesPtr(value), C.size_t(len(value))))
}

func (h *Host) loadIdempotency(hash [32]byte, entry []byte) error {
	return result(C.lxphost_load_idempotency(h.node, bytes32(&hash), bytesPtr(entry), C.size_t(len(entry))))
}

func (h *Host) moduleKV() ([]kvEntry, error) {
	count := int(C.lxphost_kv_count(h.node))
	entries := make([]kvEntry, 0, count)
	for i := 0; i < count; i++ {
		var view C.lxphost_kv_view
		if err := result(C.lxphost_kv_read(h.node, C.size_t(i), &view)); err != nil {
			return nil, err
		}
		entries = append(entries, kvEntry{
			module: uint16(view.module_id),
			key:    C.GoBytes(unsafe.Pointer(view.key), C.int(view.key_length)),
			value:  C.GoBytes(unsafe.Pointer(view.value), C.int(view.value_length)),
		})
	}
	return entries, nil
}

func (h *Host) idempotencyFrom(start int) ([]idempotencyEntry, error) {
	count := int(C.lxphost_idempotency_count(h.node))
	if count < start {
		return nil, codeFatalInvariant
	}
	buffer := make([]byte, C.LXP_STATE_MAX_REPLAY_ENTRY_BYTES)
	entries := make([]idempotencyEntry, 0, count-start)
	for i := start; i < count; i++ {
		var entry idempotencyEntry
		var length C.size_t
		if err := result(C.lxphost_idempotency_read(h.node, C.size_t(i), bytes32(&entry.hash),
			bytesPtr(buffer), C.size_t(len(buffer)), &length)); err != nil {
			return nil, err
		}
		entry.entry = append([]byte(nil), buffer[:length]...)
		entries = append(entries, entry)
	}
	return entries, nil
}

func result(status C.int32_t) error {
	if status == C.LXP_OK {
		return nil
	}
	return Code(status)
}

func bytesPtr(b []byte) *C.uint8_t {
	if len(b) == 0 {
		return nil
	}
	return (*C.uint8_t)(unsafe.Pointer(&b[0]))
}

func bytes32(b *[32]byte) *C.uint8_t {
	return (*C.uint8_t)(unsafe.Pointer(&b[0]))
}
