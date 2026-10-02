typedef struct deployment_leaf {
    uint8_t key[LXP_MODULE_MAX_KEY_BYTES + 1U];
    size_t key_length;
    const uint8_t *value;
    size_t value_length;
} deployment_leaf;

static int deployment_key_compare(const deployment_leaf *left,
                                   const deployment_leaf *right)
{
    size_t common = left->key_length < right->key_length ?
                    left->key_length : right->key_length;
    int order = memcmp(left->key, right->key, common);
    return order != 0 ? order : left->key_length < right->key_length ? -1 :
           left->key_length != right->key_length;
}

static lxp_result deployment_frame(evidence_writer *writer,
                                   const uint8_t *bytes, size_t length)
{
    lxp_result status;
    if (length > UINT32_MAX) return LXP_ERR_LENGTH_LIMIT;
    status = writer_u32(writer, (uint32_t)length);
    return status == LXP_OK ? writer_bytes(writer, bytes, length) : status;
}

static lxp_result deployment_merkle(evidence_writer *writer,
                                    const lxp_merkle_proof *proof)
{
    lxp_result status = writer_u32(writer, (uint32_t)(10U + 32U * proof->depth));
    if (status == LXP_OK) status = writer_u8(writer, 1U);
    if (status == LXP_OK) status = write_merkle_proof(writer, proof);
    return status;
}

static lxp_result deployment_witness(evidence_writer *writer,
                                     const lxp_kernel *kernel,
                                     const deployment_leaf *leaf,
                                     const uint8_t programs_root[32])
{
    uint8_t root[32];
    lxp_state_proof proof;
    lxp_result status = lxp_state_subtree_proof(kernel, 9U, leaf->key,
        leaf->key_length, root, &proof);
    if (status == LXP_OK && memcmp(root, programs_root, 32U) != 0)
        status = LXP_ERR_ROOT_MISMATCH;
    if (status == LXP_OK)
        status = deployment_frame(writer, leaf->key, leaf->key_length);
    if (status == LXP_OK)
        status = deployment_frame(writer, leaf->value, leaf->value_length);
    if (status == LXP_OK) status = write_state_proof(writer, &proof);
    return status;
}

lxp_result lxp_daemon_deployment_encode(
    const lxp_kernel *kernel, const lxp_daemon_activity_evidence *evidence,
    const lxp_daemon_receipt_authority_store *receipts,
    uint32_t network_id, lxp_arena *arena, lxp_byte_span *encoded)
{
    static const uint8_t domain[] = "LayerX/programs/deployment-proof/v1";
    static const uint8_t maintained_domain[] = "LayerX/programs/deployment-proof/v2";
    lxp_daemon_receipt_evidence retained, maintenance;
    lxp_batch_header header;
    bool maintained = false;
    deployment_leaf record = {{0}, 40U, NULL, 0U};
    deployment_leaf target = {{0}, 43U, NULL, 0U};
    deployment_leaf lower = {{0}, 0U, NULL, 0U};
    deployment_leaf upper = {{0}, 0U, NULL, 0U};
    deployment_leaf present = {{0}, 0U, NULL, 0U};
    lxp_activity activity;
    lxp_receipt receipt;
    lxp_state_proof root_proof;
    uint8_t root[32], programs_root[32];
    size_t index, capacity;
    void *allocation;
    evidence_writer writer;
    lxp_result status;
    if (kernel == NULL || evidence == NULL || arena == NULL || encoded == NULL)
        return LXP_ERR_NON_CANONICAL;
    status = verify_activity_evidence(evidence, network_id, arena);
    if (status == LXP_OK)
        status = lxp_activity_decode(evidence->canonical_activity.bytes,
            evidence->canonical_activity.length, &activity);
    if (status == LXP_OK)
        status = lxp_receipt_decode(evidence->canonical_receipt.bytes,
            evidence->canonical_receipt.length, true, &receipt);
    if (status != LXP_OK) return status;
    if ((activity.activity_type != UINT32_C(0x00090001) &&
         activity.activity_type != UINT32_C(0x00090002)) ||
        activity.payload.length < 32U || receipt.result_code != LXP_OK)
        return LXP_ERR_UNKNOWN_ACTIVITY;
    status = lxp_state_root_proof(kernel, 9U, root, &root_proof);
    if (status != LXP_OK) return status;
    status = lxp_batch_header_decode(evidence->signed_header.canonical_header.bytes,
        evidence->signed_header.canonical_header.length, &header);
    if (status != LXP_OK) return status;
    if (header.protocol_version == 3U) {
        if (receipts == NULL) return LXP_ERR_UNKNOWN_FIELD;
        status = lxp_daemon_receipt_authority_lookup(receipts,
            evidence->receipt_digest, arena, &retained);
        if (status == LXP_OK)
            status = lxp_daemon_receipt_authority_batch_maintenance(receipts,
                &retained, arena, &maintenance, &maintained);
        if (status != LXP_OK) return status;
        if (!maintained) return LXP_ERR_UNKNOWN_FIELD;
    }
    if (memcmp(root, maintained ? header.resulting_state_root :
               receipt.resulting_state_root, 32U) != 0)
        return LXP_ERR_PROJECTION_STALE;
    status = lxp_state_subtree_root(kernel, 9U, programs_root);
    if (status != LXP_OK) return status;
    (void)memcpy(record.key, "program", 8U);
    (void)memcpy(record.key + 8U, activity.payload.bytes, 32U);
    (void)memcpy(target.key, "wind-down\0s", 11U);
    (void)memcpy(target.key + 11U, activity.payload.bytes, 32U);
    for (index = 0U; index < kernel->module_kv_count + kernel->blob_count; ++index) {
        deployment_leaf leaf = {{0}, 0U, NULL, 0U};
        int order;
        if (index < kernel->module_kv_count) {
            const lxp_module_kv_entry *entry = &kernel->module_kv[index];
            if (entry->module_id != 9U) continue;
            leaf.key_length = entry->key_length;
            (void)memcpy(leaf.key, entry->key, leaf.key_length);
            leaf.value = entry->value;
            leaf.value_length = entry->value_length;
        } else {
            const lxp_module_blob *blob = &kernel->blobs[index - kernel->module_kv_count];
            if (blob->module_id != 9U) continue;
            leaf.key_length = sizeof(leaf.key);
            leaf.key[0] = 0xffU;
            (void)memcpy(leaf.key + sizeof(leaf.key) - 32U, blob->key, 32U);
            leaf.value = blob->bytes;
            leaf.value_length = blob->length;
        }
        if (deployment_key_compare(&leaf, &record) == 0) record = leaf;
        order = deployment_key_compare(&leaf, &target);
        if (order == 0) present = leaf;
        if (order < 0 && (lower.key_length == 0U ||
            deployment_key_compare(&lower, &leaf) < 0)) lower = leaf;
        if (order > 0 && (upper.key_length == 0U ||
            deployment_key_compare(&leaf, &upper) < 0)) upper = leaf;
    }
    if (record.value == NULL) return LXP_ERR_UNKNOWN_FIELD;
    capacity = sizeof(domain) + evidence->canonical_activity.length +
        evidence->canonical_receipt.length + evidence->signed_header.canonical_header.length +
        record.value_length + lower.value_length + upper.value_length +
        present.value_length + (maintained ? maintenance.canonical_receipt.length : 0U) + 8192U;
    if (capacity > 40U * 1024U * 1024U) return LXP_ERR_LENGTH_LIMIT;
    status = lxp_arena_alloc(arena, capacity, _Alignof(uint64_t), &allocation);
    if (status != LXP_OK) return status;
    writer = (evidence_writer){allocation, capacity, 0U};
    status = writer_bytes(&writer, maintained ? maintained_domain : domain, sizeof(domain));
    if (status == LXP_OK) status = deployment_frame(&writer,
        evidence->canonical_activity.bytes, evidence->canonical_activity.length);
    if (status == LXP_OK) status = deployment_merkle(&writer, &evidence->activity_proof);
    if (status == LXP_OK) status = deployment_frame(&writer,
        evidence->canonical_receipt.bytes, evidence->canonical_receipt.length);
    if (status == LXP_OK) status = deployment_merkle(&writer, &evidence->receipt_proof);
    if (status == LXP_OK) status = deployment_frame(&writer,
        evidence->signed_header.canonical_header.bytes,
        evidence->signed_header.canonical_header.length);
    if (status == LXP_OK) status = writer_bytes(&writer, evidence->signed_header.signature, 64U);
    if (status == LXP_OK) status = writer_bytes(&writer, programs_root, 32U);
    if (status == LXP_OK) status = write_state_proof(&writer, &root_proof);
    if (status == LXP_OK) status = deployment_witness(&writer, kernel, &record, programs_root);
    if (status == LXP_OK) status = writer_u8(&writer, present.key_length != 0U ? 1U : 0U);
    if (present.key_length != 0U) {
        if (status == LXP_OK) status = deployment_witness(&writer, kernel, &present, programs_root);
    } else {
        if (status == LXP_OK) status = writer_u8(&writer, lower.key_length != 0U ? 1U : 0U);
        if (status == LXP_OK && lower.key_length != 0U)
            status = deployment_witness(&writer, kernel, &lower, programs_root);
        if (status == LXP_OK) status = writer_u8(&writer, upper.key_length != 0U ? 1U : 0U);
        if (status == LXP_OK && upper.key_length != 0U)
            status = deployment_witness(&writer, kernel, &upper, programs_root);
    }
    if (status == LXP_OK && maintained)
        status = deployment_frame(&writer, maintenance.canonical_receipt.bytes,
            maintenance.canonical_receipt.length);
    if (status == LXP_OK && maintained)
        status = deployment_merkle(&writer, &maintenance.receipt_proof);
    if (status == LXP_OK) *encoded = (lxp_byte_span){writer.bytes, writer.cursor};
    return status;
}


lxp_result lxp_daemon_program_state_encode(
    const lxp_kernel *kernel,
    const lxp_daemon_receipt_authority_store *receipts,
    uint32_t network_id, const uint8_t program_id[32],
    uint64_t sequence, const uint8_t receipt_digest[32],
    const uint8_t expected_root[32], lxp_arena *arena, lxp_byte_span *encoded)
{
    static const uint8_t domain[] = "LayerX/programs/state-proof/v1";
    lxp_daemon_receipt_evidence evidence;
    lxp_batch_header header;
    lxp_receipt receipt;
    lxp_programs_occupancy_receipt maintenance;
    deployment_leaf record = {{0}, 40U, NULL, 0U};
    deployment_leaf interface = {{0}, 42U, NULL, 0U};
    deployment_leaf target = {{0}, 43U, NULL, 0U};
    deployment_leaf lower = {{0}, 0U, NULL, 0U};
    deployment_leaf upper = {{0}, 0U, NULL, 0U};
    deployment_leaf present = {{0}, 0U, NULL, 0U};
    lxp_state_proof root_proof;
    uint8_t root[32], programs_root[32], digest[32];
    const uint8_t *receipt_root;
    size_t index, capacity;
    void *allocation;
    evidence_writer writer;
    lxp_result status;
    if (kernel == NULL || receipts == NULL || program_id == NULL ||
        receipt_digest == NULL || expected_root == NULL || arena == NULL ||
        encoded == NULL || sequence == 0U || lxp_ct_is_zero(program_id, 32U))
        return LXP_ERR_NON_CANONICAL;
    status = lxp_daemon_receipt_authority_lookup(receipts, receipt_digest,
        arena, &evidence);
    if (status != LXP_OK) return status;
    status = lxp_batch_header_decode(evidence.canonical_header.bytes,
        evidence.canonical_header.length, &header);
    if (status != LXP_OK) return status;
    if (header.network_id != network_id || evidence.global_sequence != sequence)
        return LXP_ERR_CONTEXT_MISMATCH;
    if (evidence.format_version == 3U) {
        status = lxp_batch_maintenance_occupancy_decode(evidence.canonical_receipt.bytes,
            evidence.canonical_receipt.length, &maintenance);
        if (status != LXP_OK) return status;
        receipt_root = maintenance.resulting_state_root;
        if (header.protocol_version != 3U || maintenance.global_sequence != sequence ||
            header.last_sequence != sequence)
            return LXP_ERR_CONTEXT_MISMATCH;
    } else {
        status = lxp_receipt_decode(evidence.canonical_receipt.bytes,
            evidence.canonical_receipt.length, true, &receipt);
        if (status != LXP_OK) return status;
        receipt_root = receipt.resulting_state_root;
        if (header.protocol_version == 3U || receipt.global_sequence != sequence ||
            header.last_sequence != sequence)
            return LXP_ERR_PROJECTION_STALE;
    }
    status = lxp_state_root_proof(kernel, 9U, root, &root_proof);
    if (status != LXP_OK) return status;
    if (lxp_ct_memcmp(root, expected_root, 32U) != 0 ||
        lxp_ct_memcmp(root, receipt_root, 32U) != 0 ||
        lxp_ct_memcmp(root, header.resulting_state_root, 32U) != 0 ||
        lxp_ct_memcmp(root, kernel->current_state_root, 32U) != 0)
        return LXP_ERR_PROJECTION_STALE;
    status = lxp_state_subtree_root(kernel, 9U, programs_root);
    if (status != LXP_OK) return status;
    (void)memcpy(record.key, "program", 8U);
    (void)memcpy(record.key + 8U, program_id, 32U);
    (void)memcpy(interface.key, "interface", 10U);
    (void)memcpy(interface.key + 10U, program_id, 32U);
    (void)memcpy(target.key, "wind-down\0s", 11U);
    (void)memcpy(target.key + 11U, program_id, 32U);
    for (index = 0U; index < kernel->module_kv_count + kernel->blob_count; ++index) {
        deployment_leaf leaf = {{0}, 0U, NULL, 0U};
        int order;
        if (index < kernel->module_kv_count) {
            const lxp_module_kv_entry *entry = &kernel->module_kv[index];
            if (entry->module_id != 9U) continue;
            leaf.key_length = entry->key_length;
            (void)memcpy(leaf.key, entry->key, leaf.key_length);
            leaf.value = entry->value;
            leaf.value_length = entry->value_length;
        } else {
            const lxp_module_blob *blob = &kernel->blobs[index - kernel->module_kv_count];
            if (blob->module_id != 9U) continue;
            leaf.key_length = sizeof(leaf.key);
            leaf.key[0] = 0xffU;
            (void)memcpy(leaf.key + sizeof(leaf.key) - 32U, blob->key, 32U);
            leaf.value = blob->bytes;
            leaf.value_length = blob->length;
        }
        if (deployment_key_compare(&leaf, &record) == 0) record = leaf;
        if (deployment_key_compare(&leaf, &interface) == 0) interface = leaf;
        order = deployment_key_compare(&leaf, &target);
        if (order == 0) present = leaf;
        if (order < 0 && (lower.key_length == 0U ||
            deployment_key_compare(&lower, &leaf) < 0)) lower = leaf;
        if (order > 0 && (upper.key_length == 0U ||
            deployment_key_compare(&leaf, &upper) < 0)) upper = leaf;
    }
    if (record.value == NULL || interface.value == NULL)
        return LXP_ERR_UNKNOWN_FIELD;
    if (record.value_length != 71U || interface.value_length < 134U ||
        interface.value_length > 1024U ||
        ((uint32_t)interface.value[68U] << 24U | (uint32_t)interface.value[69U] << 16U | (uint32_t)interface.value[70U] << 8U | interface.value[71U]) != interface.value_length - 72U ||
        lxp_ct_memcmp(interface.value, program_id, 32U) != 0 ||
        lxp_ct_memcmp(interface.value + 32U, record.value + 67U, 4U) != 0 ||
        lxp_ct_memcmp(interface.value + 100U, record.value + 33U, 34U) != 0)
        return LXP_ERR_CONTEXT_MISMATCH;
    status = lxp_hash_sha256(interface.value + 72U, interface.value_length - 72U, digest);
    if (status != LXP_OK) return status;
    if (lxp_ct_memcmp(digest, interface.value + 36U, 32U) != 0)
        return LXP_ERR_ROOT_MISMATCH;
    capacity = sizeof(domain) + evidence.canonical_receipt.length +
        evidence.canonical_header.length + record.value_length + interface.value_length +
        lower.value_length + upper.value_length + present.value_length + 8192U;
    if (capacity > 40U * 1024U * 1024U) return LXP_ERR_LENGTH_LIMIT;
    status = lxp_arena_alloc(arena, capacity, _Alignof(uint64_t), &allocation);
    if (status != LXP_OK) return status;
    writer = (evidence_writer){allocation, capacity, 0U};
    status = writer_bytes(&writer, domain, sizeof(domain));
    if (status == LXP_OK) status = writer_u8(&writer, evidence.format_version == 3U ? 1U : 0U);
    if (status == LXP_OK) status = deployment_frame(&writer,
        evidence.canonical_receipt.bytes, evidence.canonical_receipt.length);
    if (status == LXP_OK) status = deployment_merkle(&writer, &evidence.receipt_proof);
    if (status == LXP_OK) status = deployment_frame(&writer,
        evidence.canonical_header.bytes, evidence.canonical_header.length);
    if (status == LXP_OK) status = writer_bytes(&writer, evidence.header_signature, 64U);
    if (status == LXP_OK) status = writer_bytes(&writer, programs_root, 32U);
    if (status == LXP_OK) status = write_state_proof(&writer, &root_proof);
    if (status == LXP_OK) status = deployment_witness(&writer, kernel, &record, programs_root);
    if (status == LXP_OK) status = writer_u8(&writer, present.key_length != 0U ? 1U : 0U);
    if (present.key_length != 0U) {
        if (status == LXP_OK) status = deployment_witness(&writer, kernel, &present, programs_root);
    } else {
        if (status == LXP_OK) status = writer_u8(&writer, lower.key_length != 0U ? 1U : 0U);
        if (status == LXP_OK && lower.key_length != 0U)
            status = deployment_witness(&writer, kernel, &lower, programs_root);
        if (status == LXP_OK) status = writer_u8(&writer, upper.key_length != 0U ? 1U : 0U);
        if (status == LXP_OK && upper.key_length != 0U)
            status = deployment_witness(&writer, kernel, &upper, programs_root);
    }
    if (status == LXP_OK) status = deployment_witness(&writer, kernel, &interface, programs_root);
    if (status == LXP_OK) *encoded = (lxp_byte_span){writer.bytes, writer.cursor};
    return status;
}
