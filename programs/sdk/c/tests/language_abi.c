#include "layerx/program.h"

static const uint8_t binding[] = {'b', 'i', 'n', 'd', 'i', 'n', 'g'};
static const uint8_t key[] = {'k', 'e', 'y'};
static void write_u64_le(uint8_t *out, uint64_t value)
{
    size_t index;
    for (index = 0U; index < 8U; ++index) out[index] = (uint8_t)(value >> (index * 8U));
}
static int32_t publish(int32_t code, const uint8_t *bytes, size_t length)
{
    lxp_program_status status = lxp_program_response_write(code, bytes, length);
    return status == LXP_PROGRAM_OK ? code : lxp_program_status_abi(status);
}
LXP_PROGRAM_EXPORT(LXP_PROGRAM_CALL_RESERVE_EXPORT)
int32_t layerx_reserve(int32_t length)
{
    return lxp_program_reserve_call_input_versioned(length);
}
LXP_PROGRAM_EXPORT(LXP_PROGRAM_ENTRYPOINT)
int64_t layerx_main(int64_t selector)
{
    (void)selector;
    return 0;
}
LXP_PROGRAM_EXPORT(LXP_PROGRAM_CALL_ENTRY_EXPORT)
int32_t layerx_call(int32_t pointer, int32_t input_length)
{
    const uint8_t *input;
    size_t length;
    lxp_program_status status = lxp_program_call_input_versioned(pointer, input_length, &input, &length);
    if (status != LXP_PROGRAM_OK) return lxp_program_status_abi(status);
    if (length == 0U || (input[0] == 5U ? length != 33U : length != 1U)) return LXP_PROGRAM_ERR_INVALID;
    switch (input[0]) {
    case 0U: {
        static const uint8_t topic[] = {'t', 'o', 'p', 'i', 'c'};
        status = lxp_program_storage_write_scoped(LXP_PROGRAM_STORAGE_PRINCIPAL, key, sizeof(key), binding, sizeof(binding));
        if (status == LXP_PROGRAM_OK) status = lxp_program_event_emit(topic, sizeof(topic), binding, sizeof(binding));
        break;
    }
    case 1U: {
        uint8_t market[32], record[64];
        lxp_program_oracle_observation observation;
        size_t index;
        for (index = 0U; index < sizeof(market); ++index) market[index] = 0x11U;
        status = lxp_program_oracle_read(market, &observation);
        if (status != LXP_PROGRAM_OK) return lxp_program_status_abi(status);
        write_u64_le(record, observation.price.lo);
        write_u64_le(record + 8U, observation.price.hi);
        write_u64_le(record + 16U, observation.observed_at);
        write_u64_le(record + 24U, observation.sequence);
        lxp_program_copy(record + 32U, observation.source_set_digest.bytes, 32U);
        return publish(7, record, sizeof(record));
    }
    case 2U: {
        uint8_t record[LXP_PROGRAM_WEB_RECORD_BYTES];
        lxp_program_web_answer answer;
        bool found;
        status = lxp_program_web_read(UINT64_C(0x0102030405060708), record, sizeof(record), &answer, &found);
        if (status != LXP_PROGRAM_OK) return lxp_program_status_abi(status);
        if (!found) return LXP_PROGRAM_ERR_EVIDENCE;
        return publish(7, record, LXP_PROGRAM_WEB_HEADER_BYTES + answer.response_length);
    }
    case 3U: {
        static const uint8_t reason[] = {'n', 'o'};
        status = lxp_program_refusal_write(LXP_PROGRAM_REFUSAL_REJECTED, reason, sizeof(reason));
        return status == LXP_PROGRAM_OK ? LXP_PROGRAM_V2_REFUSAL_SENTINEL : lxp_program_status_abi(status);
    }
    case 4U: {
        lxp_program_id callee;
        static uint8_t output[LXP_PROGRAM_MAX_CALL_RESPONSE_BYTES];
        static const uint8_t child[] = {6U};
        static const uint8_t grants[] = {0U, 0U};
        size_t index, written;
        int32_t code;
        for (index = 0U; index < sizeof(callee.bytes); ++index) callee.bytes[index] = 0x72U;
        status = lxp_program_call_response(callee, child, sizeof(child), grants, sizeof(grants), output, sizeof(output), &code, &written);
        if (status != LXP_PROGRAM_OK) return lxp_program_status_abi(status);
        return publish(code, output, written);
    }
    case 5U: {
        static const uint8_t seed[] = {'f', 'i', 'x', 't', 'u', 'r', 'e'};
        lxp_program_account destination;
        lxp_program_asset asset = {{9U}};
        lxp_program_copy(destination.bytes, input + 1U, sizeof(destination.bytes));
        status = lxp_program_fund_program_402(lxp_program_amount_from_parts(0U, 1U), seed, sizeof(seed), destination, asset);
        break;
    }
    case 6U: status = LXP_PROGRAM_OK; break;
    case 7U: return lxp_program_status_abi(lxp_program_storage_write_scoped(LXP_PROGRAM_STORAGE_PRINCIPAL, key, sizeof(key), binding, sizeof(binding)));
    default: return LXP_PROGRAM_ERR_INVALID;
    }
    if (status != LXP_PROGRAM_OK) return lxp_program_status_abi(status);
    return publish(7, binding, sizeof(binding));
}
