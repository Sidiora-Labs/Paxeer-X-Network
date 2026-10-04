#include "terminal-v5-language-fixture.inc"

#define V5_CHECK(c) do { if (!(c)) { fprintf(stderr, "terminal v5 fixture line %d\n", __LINE__); return 1; } } while (0)

static int v5_hex(const char *name, const uint8_t *bytes, size_t length)
{
    return executed_hex_field(name, bytes, length);
}

static int v5_execute(language_fixture *f, uint32_t type, size_t length,
    uint8_t marker, lxp_receipt *receipt, lxp_activity *activity, uint8_t signature[64])
{
    uint8_t public_key[32];
    fill_activity(activity, type, f->payload, length, language_did,
        sizeof(language_did) - 1U, f->primary_key);
    activity->protocol_version = LXP_PROTOCOL_VERSION_STATE_COMMITMENT;
    activity->account_sequence = f->identity->next_sequence;
    activity->idempotency_key[31] = marker;
    activity->fee_limit = (lxp_u128){0U, 67108864U};
    activity->signature = (lxp_byte_span){signature, 64U};
    V5_CHECK(lifecycle_vector_signature(activity, public_key, signature) == 0 &&
        memcmp(public_key, f->primary_key, 32U) == 0 &&
        lxp_activity_verify_signature(activity) == LXP_OK);
    f->execution.fee_balance = f->actor->balance;
    f->execution.global_sequence = f->state.next_sequence;
    V5_CHECK(lxp_arena_reset(&f->arena, 0U) == LXP_OK &&
        execute_artifact_fixture_activity(&f->kernel, activity, &f->execution, receipt) == LXP_OK &&
        executed_public_key(executed_sequencer_seed, public_key) == 0 &&
        lxp_receipt_verify(receipt, public_key, &f->arena) == LXP_OK);
    return 0;
}

static int v5_case(uint16_t abi, const char *outcome, unsigned int variant)
{
    language_fixture *f = calloc(1U, sizeof(*f));
    uint8_t wasm[2048], hash[32], signature[64], key[32], destination[32];
    lxp_activity activity;
    lxp_receipt receipt;
    lx_account *recipient = NULL;
    size_t wasm_length, length;
    static const uint8_t success[] = {0x41U, 0U, 0x0bU};
    static const uint8_t recipient_name[] = "agent:did:lxp:terminal-v5-recipient:main";
    V5_CHECK(f != NULL && language_setup(f) == 0);
    if (variant == 3U) {
        V5_CHECK(lx_account_id_from_string(recipient_name, sizeof(recipient_name) - 1U,
            destination) == LXP_OK &&
            lx_account_open(&f->accounts, recipient_name, sizeof(recipient_name) - 1U,
                destination, 1U, LX_ACCOUNT_OPEN_GENESIS, NULL, &recipient) == LXP_OK);
        wasm_length = counter_transfer_module(wasm, f->fee_asset, destination);
    } else if (variant == 0U) wasm_length = candidate_module(wasm, success, sizeof(success));
    else wasm_length = staged_terminal_module(wasm, variant == 2U);
    length = program_spend_deploy_payload(f->payload, f->program_id,
        f->authority.principal, wasm, wasm_length, hash);
    write_u16(f->payload + 32U, abi);
    V5_CHECK(v5_execute(f, LX_PROGRAMS_DEPLOY, length, 1U, &receipt, &activity, signature) == 0 &&
        receipt.result_code == LXP_OK);
    if (variant == 3U) {
        uint8_t capabilities[83] = {0U, 1U, 5U};
        memcpy(capabilities + 3U, f->fee_asset, 32U);
        memcpy(capabilities + 35U, destination, 32U);
        capabilities[82U] = 1U;
        length = call_payload_with_capabilities(f->payload, f->program_id,
            capabilities, sizeof(capabilities));
    } else if (variant == 0U) length = call_payload(f->payload, f->program_id);
    else length = staged_call_payload(f->payload, f->program_id);
    write_u16(f->payload + 32U, abi);
    V5_CHECK(v5_execute(f, LX_PROGRAMS_CALL, length, 2U, &receipt, &activity, signature) == 0 &&
        receipt.program_outcome.present && receipt.program_outcome.abi_version == abi &&
        receipt.program_outcome.encoding_version == 4U &&
        lxp_program_outcome_validate_for_protocol(&receipt.program_outcome,
            LXP_PROTOCOL_VERSION_STATE_COMMITMENT) == LXP_OK);
    if (variant == 0U || variant == 3U) V5_CHECK(receipt.result_code == LXP_OK &&
        receipt.program_outcome.terminal_kind == LXP_PROGRAM_TERMINAL_SUCCESS);
    else V5_CHECK(receipt.result_code == (variant == 2U ? LXP_ERR_GAS_EXHAUSTED : LXP_ERR_PROGRAM_REFUSED) &&
        receipt.program_outcome.terminal_kind == (variant == 2U ? LXP_PROGRAM_TERMINAL_RESOURCE : LXP_PROGRAM_TERMINAL_FAILURE) &&
        receipt.effects.count == 0U && lxp_ct_is_zero(receipt.program_outcome.transfer_root, 32U));
    if (variant == 3U) V5_CHECK(recipient != NULL && recipient->balance.hi == 0U &&
        recipient->balance.lo == 1U && !lxp_ct_is_zero(receipt.program_outcome.transfer_root, 32U));
    lxp_byte_span canonical, signed_activity;
    uint8_t id[32];
    V5_CHECK(lxp_receipt_encode(&receipt, true, &f->arena, &canonical) == LXP_OK &&
        lxp_activity_encode(&activity, &f->arena, &signed_activity) == LXP_OK &&
        lxp_activity_id(signed_activity.bytes, signed_activity.length, id) == LXP_OK &&
        memcmp(id, receipt.activity_id, 32U) == 0 &&
        executed_public_key(executed_sequencer_seed, key) == 0);
    V5_CHECK(printf("{\"name\":\"abi%u-%s\",\"guest_abi\":%u,\"outcome\":\"%s\",",
        (unsigned)abi, outcome, (unsigned)abi, variant == 3U ? "success" : outcome) > 0 &&
        v5_hex("canonical_receipt_hex", canonical.bytes, canonical.length) == 0 &&
        v5_hex("signed_activity_hex", signed_activity.bytes, signed_activity.length) == 0 &&
        v5_hex("native_call_payload_hex", activity.payload.bytes, activity.payload.length) == 0 &&
        v5_hex("program_id_hex", f->program_id, 32U) == 0 &&
        v5_hex("sequencer_public_key_hex", key, 32U) == 0 &&
        v5_hex("terminal_payload_hex", receipt.program_outcome.terminal_payload.bytes,
            receipt.program_outcome.terminal_payload.length) == 0 &&
        v5_hex("call_graph_hex", receipt.program_outcome.call_graph_payload.bytes,
            receipt.program_outcome.call_graph_payload.length) == 0 &&
        printf("\"fee_limit\":\"67108864\",\"authorized_batch\":{") > 0 &&
        v5_hex("batch_id_hex", receipt.batch_id, 32U) == 0 &&
        v5_hex("asset_hex", receipt.asset, 32U) == 0 &&
        v5_hex("previous_state_root_hex", receipt.previous_state_root, 32U) == 0 &&
        v5_hex("resulting_state_root_hex", receipt.resulting_state_root, 32U) == 0 &&
        printf("\"sequencer_public_key_hex\":\"") > 0 && lifecycle_vector_hex(key, 32U) == 0 &&
        printf("\"}}") > 0);
    while (f->kernel.blob_count != 0U) free(f->kernel.blobs[--f->kernel.blob_count].bytes);
    V5_CHECK(lxp_state_store_destroy(&f->state) == LXP_OK);
    lx_account_registry_release(&f->accounts);
    free(f);
    return 0;
}

int main(int argc, char **argv)
{
    static const char *names[] = {"success", "failure", "resource", "settlement-success"};
    const char *revision = getenv("PAXEER_X_MAINLINE");
    uint8_t key[32];
    if (argc != 2 || strcmp(argv[1], "--dump-corpus") != 0 || revision == NULL ||
        strlen(revision) != 40U || strspn(revision, "0123456789abcdef") != 40U) return 78;
    V5_CHECK(executed_public_key(executed_sequencer_seed, key) == 0 &&
        printf("{\"source_revision\":\"%s\",\"trusted_sequencer_public_key_hex\":\"", revision) > 0 &&
        lifecycle_vector_hex(key, 32U) == 0 && printf("\",\"cases\":[") > 0);
    for (uint16_t abi = 3U; abi <= 4U; ++abi) {
        for (unsigned int variant = 0U; variant < 4U; ++variant) {
            if (abi != 3U || variant != 0U) V5_CHECK(printf(",") > 0);
            V5_CHECK(v5_case(abi, names[variant], variant) == 0);
        }
    }
    V5_CHECK(printf("],\"missing\":[\"abi3-callback\",\"abi4-callback\",\"abi3-settlement\",\"abi4-settlement\"]}\n") > 0 &&
        fflush(stdout) == 0);
    return 78;
}
