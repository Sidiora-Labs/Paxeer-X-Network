#ifndef BALANCE_READ_GUESTS_H
#define BALANCE_READ_GUESTS_H

#include <stdbool.h>
#include <stddef.h>
#include <stdint.h>
#include <string.h>

enum balance_guest_kind {
    BALANCE_GUEST_READ = 0,
    BALANCE_GUEST_READ32 = 1,
    BALANCE_GUEST_SPEND = 2,
    BALANCE_GUEST_NESTED = 3,
    BALANCE_GUEST_BASELINE = 4,
    BALANCE_GUEST_COUNT = 5
};

enum {
    BALANCE_GUEST_INPUT = 1024,
    BALANCE_GUEST_OUTPUT = 32768,
    BALANCE_GUEST_QUERY_BYTES = 88,
    BALANCE_GUEST_RESULT_BYTES = 20,
    BALANCE_GUEST_MAX_BYTES = 8192
};

typedef struct balance_guest_writer {
    uint8_t *bytes;
    size_t length;
    size_t capacity;
    bool failed;
} balance_guest_writer;

static void balance_guest_bytes(balance_guest_writer *w, const void *bytes,
                                size_t length)
{
    if (w->failed || length > w->capacity - w->length) {
        w->failed = true;
        return;
    }
    (void)memcpy(w->bytes + w->length, bytes, length);
    w->length += length;
}

static void balance_guest_byte(balance_guest_writer *w, uint8_t byte)
{
    balance_guest_bytes(w, &byte, 1U);
}

static void balance_guest_u32(balance_guest_writer *w, uint32_t value)
{
    do {
        uint8_t byte = (uint8_t)(value & 0x7fU);
        value >>= 7U;
        balance_guest_byte(w, (uint8_t)(byte | (value != 0U ? 0x80U : 0U)));
    } while (value != 0U);
}

static void balance_guest_i32(balance_guest_writer *w, int32_t value)
{
    bool more;
    do {
        uint8_t byte = (uint8_t)((uint32_t)value & 0x7fU);
        int32_t next = value / 128;
        if (value < 0 && value % 128 != 0) --next;
        more = !((next == 0 && (byte & 0x40U) == 0U) ||
                 (next == -1 && (byte & 0x40U) != 0U));
        balance_guest_byte(w, (uint8_t)(byte | (more ? 0x80U : 0U)));
        value = next;
    } while (more);
}

static void balance_guest_const(balance_guest_writer *w, int32_t value)
{
    balance_guest_byte(w, 0x41U);
    balance_guest_i32(w, value);
}

static void balance_guest_name(balance_guest_writer *w, const char *name)
{
    size_t length = strlen(name);
    balance_guest_u32(w, (uint32_t)length);
    balance_guest_bytes(w, name, length);
}

static void balance_guest_section(balance_guest_writer *out, uint8_t id,
                                  const balance_guest_writer *section)
{
    if (section->failed) {
        out->failed = true;
        return;
    }
    balance_guest_byte(out, id);
    balance_guest_u32(out, (uint32_t)section->length);
    balance_guest_bytes(out, section->bytes, section->length);
}

static void balance_guest_type(balance_guest_writer *w, unsigned wide,
                               unsigned narrow, uint8_t result)
{
    balance_guest_byte(w, 0x60U);
    balance_guest_u32(w, wide + narrow);
    for (unsigned i = 0U; i < wide; ++i) balance_guest_byte(w, 0x7eU);
    for (unsigned i = 0U; i < narrow; ++i) balance_guest_byte(w, 0x7fU);
    balance_guest_byte(w, 1U);
    balance_guest_byte(w, result);
}

static void balance_guest_import(balance_guest_writer *w, const char *module,
                                 const char *name, unsigned type)
{
    balance_guest_name(w, module);
    balance_guest_name(w, name);
    balance_guest_byte(w, 0U);
    balance_guest_u32(w, type);
}

static void balance_guest_input_pointer(balance_guest_writer *w, int32_t offset)
{
    balance_guest_byte(w, 0x20U);
    balance_guest_byte(w, 0U);
    balance_guest_const(w, offset);
    balance_guest_byte(w, 0x6aU);
}

static void balance_guest_input_load(balance_guest_writer *w, uint32_t offset)
{
    balance_guest_byte(w, 0x20U);
    balance_guest_byte(w, 0U);
    balance_guest_byte(w, 0x28U);
    balance_guest_byte(w, 2U);
    balance_guest_u32(w, offset);
}

static void balance_guest_call(balance_guest_writer *w, unsigned index)
{
    balance_guest_byte(w, 0x10U);
    balance_guest_u32(w, index);
}

static void balance_guest_store_status(balance_guest_writer *w)
{
    balance_guest_byte(w, 0x36U);
    balance_guest_byte(w, 2U);
    balance_guest_byte(w, 0U);
}

static size_t balance_guest_generate(uint8_t *bytes, size_t capacity,
                                     unsigned kind)
{
    static const uint8_t header[] = {0U, 0x61U, 0x73U, 0x6dU, 1U, 0U, 0U, 0U};
    static const uint8_t functions[] = {2U, 2U, 3U};
    static const uint8_t memory[] = {1U, 1U, 1U, 1U};
    uint8_t section_bytes[4096];
    uint8_t body_bytes[4096];
    uint8_t sentinels[32U * BALANCE_GUEST_RESULT_BYTES];
    balance_guest_writer out = {bytes, 0U, capacity, false};
    balance_guest_writer section = {section_bytes, 0U, sizeof(section_bytes), false};
    balance_guest_writer body = {body_bytes, 0U, sizeof(body_bytes), false};
    unsigned imports;
    unsigned response_index;
    unsigned results = kind == BALANCE_GUEST_READ32 ? 32U : 1U;
    unsigned response_bytes = results * BALANCE_GUEST_RESULT_BYTES;
    unsigned minimum_input = results * BALANCE_GUEST_QUERY_BYTES;
    if (bytes == NULL || kind >= BALANCE_GUEST_COUNT) return 0U;
    if (kind == BALANCE_GUEST_SPEND) {
        imports = 4U;
        response_index = 3U;
        response_bytes = 3U * BALANCE_GUEST_RESULT_BYTES;
        minimum_input = 128U;
    } else if (kind == BALANCE_GUEST_BASELINE) {
        imports = 1U;
        response_index = 0U;
        minimum_input = 0U;
    } else {
        imports = 2U;
        response_index = 1U;
        if (kind == BALANCE_GUEST_NESTED) {
            response_bytes = 28U;
            minimum_input = 128U;
        }
    }
    balance_guest_bytes(&out, header, sizeof(header));
    balance_guest_byte(&section, 8U);
    balance_guest_type(&section, 0U, 6U, 0x7fU);
    balance_guest_type(&section, 0U, 3U, 0x7fU);
    balance_guest_type(&section, 0U, 1U, 0x7fU);
    balance_guest_type(&section, 0U, 2U, 0x7fU);
    balance_guest_type(&section, 2U, 4U, 0x7fU);
    balance_guest_type(&section, 2U, 8U, 0x7fU);
    balance_guest_type(&section, 2U, 6U, 0x7fU);
    balance_guest_type(&section, 0U, 8U, 0x7eU);
    balance_guest_section(&out, 1U, &section);
    section.length = 0U;
    balance_guest_u32(&section, imports);
    if (kind == BALANCE_GUEST_SPEND) {
        balance_guest_import(&section, "layerx_v1", "transfer_402", 4U);
        balance_guest_import(&section, "layerx_v2", "transfer_program_402", 5U);
        balance_guest_import(&section, "layerx_v2", "fund_program_402", 6U);
    } else if (kind == BALANCE_GUEST_NESTED) {
        balance_guest_import(&section, "layerx_v2", "program_call_response", 7U);
    } else if (kind != BALANCE_GUEST_BASELINE) {
        balance_guest_import(&section, "layerx_v2", "balance_read", 0U);
    }
    balance_guest_import(&section, "layerx_v2", "response_write", 1U);
    balance_guest_section(&out, 2U, &section);
    section.length = 0U;
    balance_guest_bytes(&section, functions, sizeof(functions));
    balance_guest_section(&out, 3U, &section);
    section.length = 0U;
    balance_guest_bytes(&section, memory, sizeof(memory));
    balance_guest_section(&out, 5U, &section);
    section.length = 0U;
    balance_guest_byte(&section, 3U);
    balance_guest_name(&section, "layerx_reserve");
    balance_guest_byte(&section, 0U);
    balance_guest_u32(&section, imports);
    balance_guest_name(&section, "layerx_call");
    balance_guest_byte(&section, 0U);
    balance_guest_u32(&section, imports + 1U);
    balance_guest_name(&section, "memory");
    balance_guest_byte(&section, 2U);
    balance_guest_byte(&section, 0U);
    balance_guest_section(&out, 7U, &section);
    section.length = 0U;
    balance_guest_byte(&section, 2U);
    balance_guest_byte(&body, 0U);
    balance_guest_byte(&body, 0x20U);
    balance_guest_byte(&body, 0U);
    balance_guest_const(&body, BALANCE_GUEST_OUTPUT - BALANCE_GUEST_INPUT);
    balance_guest_byte(&body, 0x4bU);
    balance_guest_byte(&body, 0x04U);
    balance_guest_byte(&body, 0x40U);
    balance_guest_const(&body, -1);
    balance_guest_byte(&body, 0x0fU);
    balance_guest_byte(&body, 0x0bU);
    balance_guest_const(&body, BALANCE_GUEST_INPUT);
    balance_guest_byte(&body, 0x0bU);
    balance_guest_u32(&section, (uint32_t)body.length);
    balance_guest_bytes(&section, body.bytes, body.length);
    body.length = 0U;
    balance_guest_byte(&body, 0U);
    balance_guest_byte(&body, 0x20U);
    balance_guest_byte(&body, 1U);
    balance_guest_const(&body, (int32_t)minimum_input);
    balance_guest_byte(&body, 0x49U);
    balance_guest_byte(&body, 0x04U);
    balance_guest_byte(&body, 0x40U);
    balance_guest_const(&body, -3);
    balance_guest_byte(&body, 0x0fU);
    balance_guest_byte(&body, 0x0bU);
    if (kind == BALANCE_GUEST_READ || kind == BALANCE_GUEST_READ32) {
        for (unsigned i = 0U; i < results; ++i) {
            balance_guest_const(&body, BALANCE_GUEST_OUTPUT +
                                (int32_t)(i * BALANCE_GUEST_RESULT_BYTES));
            for (unsigned argument = 0U; argument < 6U; ++argument)
                balance_guest_input_load(&body, i * BALANCE_GUEST_QUERY_BYTES +
                                         argument * 4U);
            balance_guest_call(&body, 0U);
            balance_guest_store_status(&body);
        }
    } else if (kind == BALANCE_GUEST_SPEND) {
        for (unsigned i = 0U; i < 3U; ++i) {
            balance_guest_const(&body, BALANCE_GUEST_OUTPUT +
                                (int32_t)(i * BALANCE_GUEST_RESULT_BYTES));
            balance_guest_byte(&body, 0x42U);
            balance_guest_byte(&body, 0U);
            balance_guest_byte(&body, 0x42U);
            balance_guest_byte(&body, 1U);
            if (i != 0U) {
                balance_guest_input_pointer(&body, 0);
                balance_guest_const(&body, 1);
                balance_guest_input_pointer(&body, 32);
                balance_guest_const(&body, 32);
            }
            balance_guest_input_pointer(&body, 64);
            balance_guest_const(&body, 32);
            if (i != 2U) {
                balance_guest_input_pointer(&body, 96);
                balance_guest_const(&body, 32);
            }
            balance_guest_call(&body, i);
            balance_guest_store_status(&body);
        }
    } else if (kind == BALANCE_GUEST_NESTED) {
        balance_guest_const(&body, BALANCE_GUEST_OUTPUT);
        balance_guest_input_pointer(&body, 0);
        balance_guest_const(&body, 32);
        balance_guest_input_pointer(&body, 40);
        balance_guest_input_load(&body, 32U);
        balance_guest_input_pointer(&body, 128);
        balance_guest_input_load(&body, 36U);
        balance_guest_const(&body, BALANCE_GUEST_OUTPUT + 8);
        balance_guest_const(&body, BALANCE_GUEST_RESULT_BYTES);
        balance_guest_call(&body, 0U);
        balance_guest_byte(&body, 0x37U);
        balance_guest_byte(&body, 3U);
        balance_guest_byte(&body, 0U);
    }
    balance_guest_const(&body, 0);
    balance_guest_const(&body, BALANCE_GUEST_OUTPUT);
    balance_guest_const(&body, (int32_t)response_bytes);
    balance_guest_call(&body, response_index);
    balance_guest_byte(&body, 0x0bU);
    if (body.failed) return 0U;
    balance_guest_u32(&section, (uint32_t)body.length);
    balance_guest_bytes(&section, body.bytes, body.length);
    balance_guest_section(&out, 10U, &section);
    section.length = 0U;
    balance_guest_byte(&section, 1U);
    balance_guest_byte(&section, 0U);
    balance_guest_const(&section, BALANCE_GUEST_OUTPUT);
    balance_guest_byte(&section, 0x0bU);
    (void)memset(sentinels, 0xa5, sizeof(sentinels));
    balance_guest_u32(&section, (uint32_t)sizeof(sentinels));
    balance_guest_bytes(&section, sentinels, sizeof(sentinels));
    balance_guest_section(&out, 11U, &section);
    return out.failed ? 0U : out.length;
}

#endif
