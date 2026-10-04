#include "reference-custody-fixture-base.inc"

#define CUSTODY_CHECK(c) do { if (!(c)) { fprintf(stderr, "Reference custody line %d\n", __LINE__); return 1; } } while (0)

static const uint8_t custody_other_seed[32] = {0x58U};
static const uint8_t custody_other_did[] = "did:lxp:reference-vault-other";
static const uint8_t custody_seed[] = "reference/custody";
static const uint8_t custody_second_seed[] = "reference/equal-condition";
static lxp_verified_receipt_index custody_index;
static lxp_identity *custody_other_identity;
static lx_account *custody_other_account;
static uint8_t custody_other_key[32];
static unsigned int custody_nonce;
static const char *custody_evidence_directory;

static int custody_write(const char *name, const uint8_t *bytes, size_t length)
{
    char path[4096];
    int count = snprintf(path, sizeof(path), "%s/%s", custody_evidence_directory, name);
    CUSTODY_CHECK(count > 0 && (size_t)count < sizeof(path));
    int fd = open(path, O_WRONLY | O_CREAT | O_EXCL | O_NOFOLLOW, 0600);
    CUSTODY_CHECK(fd >= 0);
    size_t offset = 0U;
    while (offset < length) {
        ssize_t written = write(fd, bytes + offset, length - offset);
        if (written < 0 && errno == EINTR) continue;
        CUSTODY_CHECK(written > 0);
        offset += (size_t)written;
    }
    CUSTODY_CHECK(fsync(fd) == 0 && close(fd) == 0);
    return 0;
}

static int custody_execute(spend_fixture *f, uint32_t type,
    const uint8_t *payload, size_t length, bool other, lxp_result expected)
{
    CUSTODY_CHECK(custody_nonce < 255U &&
        lxp_state_root(&f->kernel, f->kernel.current_state_root) == LXP_OK &&
        spend_sign(f, type, payload, length, (uint8_t)++custody_nonce) == 0);
    if (other) {
        uint8_t digest[32];
        f->activity.actor_did = (lxp_byte_span){custody_other_did, sizeof(custody_other_did) - 1U};
        f->activity.authority = (lxp_byte_span){custody_other_key, 32U};
        f->activity.account_sequence = custody_other_identity->next_sequence;
        CUSTODY_CHECK(lxp_activity_signing_preimage(&f->activity, digest) == LXP_OK);
        EVP_PKEY *key = EVP_PKEY_new_raw_private_key(EVP_PKEY_ED25519, NULL, custody_other_seed, 32U);
        EVP_MD_CTX *context = EVP_MD_CTX_new();
        size_t signature_length = 64U;
        int signed_ok = key != NULL && context != NULL &&
            EVP_DigestSignInit(context, NULL, NULL, NULL, key) == 1 &&
            EVP_DigestSign(context, f->signature, &signature_length, digest, sizeof(digest)) == 1;
        EVP_MD_CTX_free(context);
        EVP_PKEY_free(key);
        CUSTODY_CHECK(signed_ok && signature_length == 64U &&
            lxp_activity_verify_signature(&f->activity) == LXP_OK &&
            lxp_authority_resolve_activity(&f->kernel, custody_other_identity, &f->activity,
                lxp_identity_key_valid(custody_other_identity, custody_other_key, 10U, f->state.next_sequence),
                true, 10U, 100U, f->state.next_sequence, &f->grant, &f->authority) == LXP_OK);
        f->execution.fee_balance = custody_other_account->balance;
    }
    f->execution.verified_receipts = &custody_index;
    CUSTODY_CHECK(lxp_kernel_execute_activity(&f->kernel, &f->activity,
        &f->execution, &f->receipt) == LXP_OK && f->receipt.result_code == expected);
    uint8_t public_key[32], root[32];
    CUSTODY_CHECK(executed_public_key(executed_sequencer_seed, public_key) == 0 &&
        lxp_receipt_verify(&f->receipt, public_key, &f->arena) == LXP_OK &&
        lxp_state_root(&f->kernel, root) == LXP_OK &&
        memcmp(root, f->receipt.resulting_state_root, 32U) == 0);
    lxp_byte_span encoded;
    char name[64];
    CUSTODY_CHECK(lxp_receipt_encode(&f->receipt, true, &f->arena, &encoded) == LXP_OK);
    int count = snprintf(name, sizeof(name), "receipt-%u.bin", custody_nonce);
    CUSTODY_CHECK(count > 0 && (size_t)count < sizeof(name) &&
        custody_write(name, encoded.bytes, encoded.length) == 0);
    if (type == LX_PROGRAMS_CALL) {
        CUSTODY_CHECK(f->receipt.program_outcome.present &&
            f->receipt.program_outcome.terminal_kind ==
                (expected == LXP_OK ? LXP_PROGRAM_TERMINAL_SUCCESS : LXP_PROGRAM_TERMINAL_FAILURE));
        if (expected != LXP_OK)
            CUSTODY_CHECK(f->receipt.effects.count == 0U &&
                lxp_ct_is_zero(f->receipt.program_outcome.transfer_root, 32U));
    }
    return 0;
}

static int custody_deploy(spend_fixture *f, const char *path,
    const uint8_t program[32], uint8_t code_hash[32])
{
    struct stat information;
    int fd = open(path, O_RDONLY | O_NOFOLLOW | O_CLOEXEC);
    CUSTODY_CHECK(fd >= 0 && fstat(fd, &information) == 0 &&
        S_ISREG(information.st_mode) && information.st_nlink == 1 &&
        information.st_size > 0 && (uint64_t)information.st_size <= LXP_MAX_ACTIVITY_BYTES - 104U);
    size_t length = (size_t)information.st_size, offset = 0U;
    uint8_t *wasm = malloc(length), *payload = malloc(length + 104U);
    CUSTODY_CHECK(wasm != NULL && payload != NULL);
    while (offset < length) {
        ssize_t consumed = read(fd, wasm + offset, length - offset);
        if (consumed < 0 && errno == EINTR) continue;
        CUSTODY_CHECK(consumed > 0);
        offset += (size_t)consumed;
    }
    CUSTODY_CHECK(close(fd) == 0);
    size_t payload_length = program_spend_deploy_payload(payload, program,
        f->identity->did_id, wasm, length, code_hash);
    CUSTODY_CHECK(custody_execute(f, LX_PROGRAMS_DEPLOY, payload, payload_length, false, LXP_OK) == 0);
    free(payload);
    free(wasm);
    return 0;
}

static int custody_register(spend_fixture *f, const uint8_t program[32],
    const uint8_t *seed, size_t seed_length, uint8_t account_id[32])
{
    uint8_t payload[73U + LX_PROGRAMS_ACCOUNT_MAX_SEED_BYTES];
    CUSTODY_CHECK(seed_length <= LX_PROGRAMS_ACCOUNT_MAX_SEED_BYTES);
    memcpy(payload, program, 32U);
    memcpy(payload + 32U, "LXPA1", 5U);
    memcpy(payload + 37U, f->asset.asset_id, 32U);
    write_u32(payload + 69U, (uint32_t)seed_length);
    memcpy(payload + 73U, seed, seed_length);
    CUSTODY_CHECK(custody_execute(f, LX_PROGRAMS_ACCOUNT, payload, 73U + seed_length, false, LXP_OK) == 0 &&
        lxp_programs_account_derive(program, seed, seed_length, account_id) == LXP_OK);
    lx_account *account = program_spend_account(&f->accounts, account_id);
    CUSTODY_CHECK(account != NULL && lxp_u128_is_zero(account->balance));
    return 0;
}

static size_t custody_capabilities(uint8_t *out, const spend_fixture *f,
    const uint8_t program[32], const uint8_t *seed, size_t seed_length,
    const uint8_t account[32], const uint8_t destination[32], const uint8_t *condition)
{
    size_t offset = 2U;
    write_u16(out, condition == NULL ? 7U : 8U);
    out[offset++] = 1U; out[offset++] = 2U; out[offset++] = 3U;
    out[offset++] = 5U;
    memcpy(out + offset, f->asset.asset_id, 32U); offset += 32U;
    memcpy(out + offset, account, 32U); offset += 32U;
    write_u64(out + offset, 0U); write_u64(out + offset + 8U, 100U); offset += 16U;
    if (condition != NULL) {
        out[offset++] = 6U;
        memcpy(out + offset, condition, 32U); offset += 32U;
    }
    out[offset++] = 7U; out[offset++] = 8U;
    uint8_t payment[512];
    size_t payment_length = program_spend_capabilities(payment, program, seed,
        seed_length, account, f->asset.asset_id, destination, 100U);
    memcpy(out + offset, payment + 2U, payment_length - 2U);
    return offset + payment_length - 2U;
}

static int custody_call(spend_fixture *f, const uint8_t program[32],
    const uint8_t *seed, size_t seed_length, const uint8_t account[32],
    const uint8_t destination[32], const uint8_t *condition,
    const uint8_t *input, size_t input_length, bool other, lxp_result expected)
{
    static const uint8_t absent_access[] = "LayerX/programs/access-declaration/v1\0";
    uint8_t grants[1024], payload[4096];
    size_t grant_length = custody_capabilities(grants, f, program, seed, seed_length,
        account, destination, condition);
    size_t payload_length = call_payload_with_data(payload, program, grants, grant_length,
        absent_access, sizeof(absent_access), input, input_length);
    write_u16(payload + 32U, LX_PROGRAMS_ACCOUNT_ABI_VERSION);
    return custody_execute(f, LX_PROGRAMS_CALL, payload, payload_length, other, expected);
}

static size_t custody_vault_input(uint8_t *out, uint8_t operation,
    const uint8_t account[32], const uint8_t asset[32], const uint8_t destination[32], uint64_t amount)
{
    size_t offset = 0U;
    out[offset++] = 1U; out[offset++] = operation;
    write_u16(out + offset, sizeof(custody_seed) - 1U); offset += 2U;
    memcpy(out + offset, custody_seed, sizeof(custody_seed) - 1U); offset += sizeof(custody_seed) - 1U;
    memcpy(out + offset, account, 32U); offset += 32U;
    memcpy(out + offset, asset, 32U); offset += 32U;
    memcpy(out + offset, destination, 32U); offset += 32U;
    write_u64(out + offset, 0U); write_u64(out + offset + 8U, amount);
    return offset + 16U;
}

static void custody_hex(const uint8_t bytes[32], char text[65])
{
    static const char alphabet[] = "0123456789abcdef";
    for (size_t i = 0U; i < 32U; ++i) {
        text[i * 2U] = alphabet[bytes[i] >> 4U];
        text[i * 2U + 1U] = alphabet[bytes[i] & 15U];
    }
    text[64] = '\0';
}

int main(int argc, char **argv)
{
    if (argc != 4) return 78;
    custody_evidence_directory = argv[3];
    struct stat directory;
    CUSTODY_CHECK(lstat(custody_evidence_directory, &directory) == 0 &&
        S_ISDIR(directory.st_mode) && directory.st_uid == geteuid() &&
        (directory.st_mode & 0777U) == 0700U);
    spend_fixture *f = calloc(1U, sizeof(*f));
    CUSTODY_CHECK(f != NULL && spend_init(f) == 0 &&
        lxp_verified_receipt_index_init(&custody_index) == LXP_OK);
    static const uint8_t other_name[] = "agent:did:lxp:reference-vault-other:main";
    uint8_t other_id[32];
    CUSTODY_CHECK(executed_public_key(custody_other_seed, custody_other_key) == 0 &&
        lxp_identity_register(&f->identities, custody_other_did, sizeof(custody_other_did) - 1U,
            custody_other_key, &custody_other_identity) == LXP_OK &&
        lx_account_id_from_string(other_name, sizeof(other_name) - 1U, other_id) == LXP_OK &&
        lx_account_open(&f->accounts, other_name, sizeof(other_name) - 1U, other_id,
            1U, LX_ACCOUNT_OPEN_GENESIS, NULL, &custody_other_account) == LXP_OK &&
        lxp_ledger_bootstrap_balance(custody_other_account, f->asset.asset_id,
            (lxp_u128){0U, 1000000000U}, 0U) == LXP_OK);
    uint8_t escrow_hash[32], vault_hash[32], escrow_account[32], vault_account[32];
    uint8_t release_digest[32], refund_digest[32], public_key[32];
    CUSTODY_CHECK(custody_deploy(f, argv[1], f->owner, escrow_hash) == 0 &&
        lxp_receipt_digest(&f->receipt, &f->arena, release_digest) == LXP_OK &&
        executed_public_key(executed_sequencer_seed, public_key) == 0 &&
        lxp_verified_receipt_index_add(&custody_index, &f->receipt, public_key, &f->arena) == LXP_OK &&
        custody_deploy(f, argv[2], f->child, vault_hash) == 0 &&
        lxp_receipt_digest(&f->receipt, &f->arena, refund_digest) == LXP_OK &&
        lxp_verified_receipt_index_add(&custody_index, &f->receipt, public_key, &f->arena) == LXP_OK &&
        memcmp(release_digest, refund_digest, 32U) != 0 &&
        custody_register(f, f->owner, custody_seed, sizeof(custody_seed) - 1U, escrow_account) == 0 &&
        custody_register(f, f->child, custody_seed, sizeof(custody_seed) - 1U, vault_account) == 0);
    uint8_t input[512];
    size_t length = custody_vault_input(input, 1U, escrow_account, f->asset.asset_id, f->payee->id, 20U);
    size_t destination_offset = 4U + sizeof(custody_seed) - 1U + 64U;
    memmove(input + destination_offset + 64U, input + destination_offset + 32U, 16U);
    memcpy(input + destination_offset + 32U, f->actor->id, 32U);
    length += 32U;
    memcpy(input + length, release_digest, 32U); length += 32U;
    memcpy(input + length, refund_digest, 32U); length += 32U;
    CUSTODY_CHECK(custody_call(f, f->owner, custody_seed, sizeof(custody_seed) - 1U,
        escrow_account, f->payee->id, NULL, input, length, false, LXP_OK) == 0);
    lx_account *escrow = program_spend_account(&f->accounts, escrow_account);
    CUSTODY_CHECK(escrow != NULL && escrow->balance.hi == 0U && escrow->balance.lo == 20U &&
        custody_call(f, f->owner, custody_seed, sizeof(custody_seed) - 1U,
            escrow_account, f->payee->id, NULL, input, length, false, LXP_ERR_PROGRAM_REFUSED) == 0 &&
        escrow->balance.lo == 20U);
    for (uint8_t operation = 2U; operation <= 3U; ++operation) {
        input[0] = 1U; input[1] = operation;
        memcpy(input + 2U, escrow_account, 32U);
        const uint8_t *condition = operation == 2U ? release_digest : refund_digest;
        memcpy(input + 34U, condition, 32U);
        CUSTODY_CHECK(custody_call(f, f->owner, custody_seed, sizeof(custody_seed) - 1U,
            escrow_account, operation == 2U ? f->payee->id : f->actor->id, condition,
            input, 66U, false, LXP_ERR_PROGRAM_REFUSED) == 0 && escrow->balance.lo == 20U);
    }
    uint8_t second_account[32];
    CUSTODY_CHECK(custody_register(f, f->owner, custody_second_seed,
        sizeof(custody_second_seed) - 1U, second_account) == 0);
    size_t offset = 0U;
    input[offset++] = 1U; input[offset++] = 1U;
    write_u16(input + offset, sizeof(custody_second_seed) - 1U); offset += 2U;
    memcpy(input + offset, custody_second_seed, sizeof(custody_second_seed) - 1U); offset += sizeof(custody_second_seed) - 1U;
    memcpy(input + offset, second_account, 32U); offset += 32U;
    memcpy(input + offset, f->asset.asset_id, 32U); offset += 32U;
    memcpy(input + offset, f->payee->id, 32U); offset += 32U;
    memcpy(input + offset, f->actor->id, 32U); offset += 32U;
    write_u64(input + offset, 0U); write_u64(input + offset + 8U, 20U); offset += 16U;
    memcpy(input + offset, release_digest, 32U); offset += 32U;
    memcpy(input + offset, release_digest, 32U); offset += 32U;
    CUSTODY_CHECK(custody_call(f, f->owner, custody_second_seed, sizeof(custody_second_seed) - 1U,
        second_account, f->payee->id, NULL, input, offset, false, LXP_ERR_PROGRAM_REFUSED) == 0 &&
        lxp_u128_is_zero(program_spend_account(&f->accounts, second_account)->balance));
    for (unsigned int participant = 0U; participant < 2U; ++participant) {
        length = custody_vault_input(input, 1U, vault_account, f->asset.asset_id, vault_account,
            participant == 0U ? 30U : 20U);
        CUSTODY_CHECK(custody_call(f, f->child, custody_seed, sizeof(custody_seed) - 1U,
            vault_account, f->payee->id, NULL, input, length, participant != 0U, LXP_OK) == 0);
    }
    lx_account *vault = program_spend_account(&f->accounts, vault_account);
    CUSTODY_CHECK(vault != NULL && vault->balance.lo == 50U);
    length = custody_vault_input(input, 2U, vault_account, f->asset.asset_id, f->payee->id, 21U);
    CUSTODY_CHECK(custody_call(f, f->child, custody_seed, sizeof(custody_seed) - 1U,
        vault_account, f->payee->id, NULL, input, length, true, LXP_ERR_PROGRAM_REFUSED) == 0 &&
        vault->balance.lo == 50U && f->payee->balance.lo == 0U);
    for (unsigned int participant = 0U; participant < 2U; ++participant) {
        length = custody_vault_input(input, 2U, vault_account, f->asset.asset_id, f->payee->id,
            participant == 0U ? 30U : 20U);
        CUSTODY_CHECK(custody_call(f, f->child, custody_seed, sizeof(custody_seed) - 1U,
            vault_account, f->payee->id, NULL, input, length, participant != 0U, LXP_OK) == 0);
    }
    CUSTODY_CHECK(lxp_u128_is_zero(vault->balance) && f->payee->balance.lo == 50U);
    length = custody_vault_input(input, 2U, vault_account, f->asset.asset_id, f->payee->id, 1U);
    CUSTODY_CHECK(custody_call(f, f->child, custody_seed, sizeof(custody_seed) - 1U,
        vault_account, f->payee->id, NULL, input, length, false, LXP_ERR_PROGRAM_REFUSED) == 0 &&
        lxp_u128_is_zero(vault->balance) && f->payee->balance.lo == 50U);
    char escrow_text[65], vault_text[65];
    custody_hex(escrow_hash, escrow_text); custody_hex(vault_hash, vault_text);
    CUSTODY_CHECK(printf("{\"schema\":\"paxeer-x.reference-custody-native.v1\",\"cases\":["
        "{\"name\":\"escrow-open-real-funding\",\"passed\":true},"
        "{\"name\":\"escrow-duplicate-open\",\"passed\":true},"
        "{\"name\":\"escrow-bad-condition-rollback\",\"passed\":true},"
        "{\"name\":\"escrow-equal-condition-refusal\",\"passed\":true},"
        "{\"name\":\"vault-two-principal-pool\",\"passed\":true},"
        "{\"name\":\"vault-principal-isolation\",\"passed\":true},"
        "{\"name\":\"vault-full-withdrawals\",\"passed\":true},"
        "{\"name\":\"vault-overwithdraw-rollback\",\"passed\":true}],"
        "\"programs\":{\"escrow\":{\"code_hash\":\"%s\"},\"vault\":{\"code_hash\":\"%s\"}},"
        "\"missing\":[\"escrow-successful-release\",\"escrow-successful-refund\","
        "\"escrow-duplicate-settlement\",\"authenticated-source-verification\"]}\n",
        escrow_text, vault_text) > 0);
    while (f->kernel.blob_count != 0U) free(f->kernel.blobs[--f->kernel.blob_count].bytes);
    CUSTODY_CHECK(lxp_state_store_destroy(&f->state) == LXP_OK);
    lx_account_registry_release(&f->accounts);
    free(f);
    return 78;
}
