#define _XOPEN_SOURCE 700
#include <limits.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

/*
 * The determinism gate every LayerX program passes before deployment sees it.
 * It walks the produced module section by section and refuses, by name, any
 * construct that could make two honest executions disagree: an import outside
 * the frozen layerx_v1 surface, a floating-point type or instruction, a vector
 * or atomic instruction, or an instruction this gate does not recognise. The
 * gate fails closed: an opcode it cannot decode is a refusal, never a pass.
 */

enum {
    LINT_MAX_MODULE_BYTES = 1048576,
    LINT_MAX_FUNCTIONS = 4096,
    LINT_DETAIL_BYTES = 640
};

static const char RULE_MALFORMED[] = "malformed-module";
static const char RULE_MODULE_TOO_LARGE[] = "module-too-large";
static const char RULE_TOO_MANY_FUNCTIONS[] = "too-many-functions";
static const char RULE_FORBIDDEN_IMPORT[] = "forbidden-import";
static const char RULE_FLOAT_TYPE[] = "forbidden-float-type";
static const char RULE_FLOAT_INSTRUCTION[] = "forbidden-float-instruction";
static const char RULE_VECTOR[] = "forbidden-vector-type";
static const char RULE_ATOMIC[] = "forbidden-atomic-instruction";
static const char RULE_UNKNOWN_INSTRUCTION[] = "unknown-instruction";
static const char RULE_MISSING_MEMORY[] = "missing-memory-export";
static const char RULE_IMPORT_DECLARATION[] = "forbidden-import-declaration";

static unsigned selected_abi_version = 1U;
static const char *manifest_path;
static char default_manifest_path[PATH_MAX];
static const char RULE_IMPORT_SIGNATURE[] = "wrong-import-signature";

typedef struct abi_function {
    unsigned introduced;
    char module[32];
    char name[64];
    char signature[128];
} abi_function;
static abi_function abi_functions[28];
static size_t abi_function_count;

typedef struct function_type {
    uint32_t parameters;
    uint32_t results;
    uint8_t parameter_types[16];
    uint8_t result_type;
} function_type;

typedef struct lint_state {
    const uint8_t *bytes;
    size_t length;
    size_t cursor;
    const char *rule;
    char detail[LINT_DETAIL_BYTES];
    int memory_exported;
    uint32_t function_count;
    uint32_t type_count;
    function_type types[LINT_MAX_FUNCTIONS];
    uint8_t imported[28];
} lint_state;

static int fail(lint_state *state, const char *rule, const char *detail)
{
    if (state->rule == NULL) {
        state->rule = rule;
        (void)snprintf(state->detail, sizeof(state->detail), "%s", detail);
    }
    return 0;
}

static int fail_named(lint_state *state, const char *rule, const char *format,
                      const char *first, const char *second)
{
    if (state->rule == NULL) {
        state->rule = rule;
        (void)snprintf(state->detail, sizeof(state->detail), format, first,
                       second);
    }
    return 0;
}

static int read_byte(lint_state *state, uint8_t *out)
{
    if (state->cursor >= state->length)
        return fail(state, RULE_MALFORMED, "truncated section");
    *out = state->bytes[state->cursor];
    state->cursor += 1U;
    return 1;
}

static int read_u32_leb(lint_state *state, uint32_t *out)
{
    uint32_t result = 0U;
    unsigned index;
    for (index = 0U; index < 5U; ++index) {
        uint8_t byte;
        if (!read_byte(state, &byte)) return 0;
        if (index == 4U && (byte & 0xF0U) != 0U)
            return fail(state, RULE_MALFORMED, "unsigned LEB128 overflow");
        result |= (uint32_t)(byte & 0x7FU) << (index * 7U);
        if ((byte & 0x80U) == 0U) {
            *out = result;
            return 1;
        }
    }
    return fail(state, RULE_MALFORMED, "unterminated unsigned LEB128");
}

static int skip_u32_leb(lint_state *state)
{
    uint32_t discarded;
    return read_u32_leb(state, &discarded);
}

static int skip_signed_leb(lint_state *state, unsigned maximum_bytes)
{
    unsigned index;
    for (index = 0U; index < maximum_bytes; ++index) {
        uint8_t byte;
        if (!read_byte(state, &byte)) return 0;
        if ((byte & 0x80U) == 0U) return 1;
    }
    return fail(state, RULE_MALFORMED, "unterminated signed LEB128");
}

static int read_s33_leb(lint_state *state, int64_t *out)
{
    int64_t result = 0;
    unsigned shift = 0U;
    unsigned index;
    for (index = 0U; index < 5U; ++index) {
        uint8_t byte;
        if (!read_byte(state, &byte)) return 0;
        result |= (int64_t)(byte & 0x7FU) << shift;
        shift += 7U;
        if ((byte & 0x80U) == 0U) {
            if (shift < 64U && (byte & 0x40U) != 0U)
                result |= -((int64_t)1 << shift);
            *out = result;
            return 1;
        }
    }
    return fail(state, RULE_MALFORMED, "unterminated block type");
}

static int check_value_type(lint_state *state, uint8_t code)
{
    switch (code) {
    case 0x7FU:
    case 0x7EU:
    case 0x70U:
    case 0x6FU:
        return 1;
    case 0x7DU:
        return fail(state, RULE_FLOAT_TYPE, "f32 value type");
    case 0x7CU:
        return fail(state, RULE_FLOAT_TYPE, "f64 value type");
    case 0x7BU:
        return fail(state, RULE_VECTOR, "v128 value type");
    default:
        return fail(state, RULE_MALFORMED, "unknown value type");
    }
}

static int check_block_type(lint_state *state)
{
    int64_t value;
    if (!read_s33_leb(state, &value)) return 0;
    if (value >= 0) return 1;
    switch (value) {
    case -1:
    case -2:
    case -16:
    case -17:
    case -64:
        return 1;
    case -3:
        return fail(state, RULE_FLOAT_TYPE, "f32 block type");
    case -4:
        return fail(state, RULE_FLOAT_TYPE, "f64 block type");
    case -5:
        return fail(state, RULE_VECTOR, "v128 block type");
    default:
        return fail(state, RULE_MALFORMED, "unknown block type");
    }
}

static int float_opcode(uint8_t opcode)
{
    if (opcode == 0x2AU || opcode == 0x2BU) return 1;
    if (opcode == 0x38U || opcode == 0x39U) return 1;
    if (opcode == 0x43U || opcode == 0x44U) return 1;
    if (opcode >= 0x5BU && opcode <= 0x66U) return 1;
    if (opcode >= 0x8BU && opcode <= 0xA6U) return 1;
    if (opcode >= 0xA8U && opcode <= 0xABU) return 1;
    if (opcode >= 0xAEU && opcode <= 0xBFU) return 1;
    return 0;
}

static int walk_prefixed(lint_state *state)
{
    uint32_t operation;
    if (!read_u32_leb(state, &operation)) return 0;
    if (operation <= 7U)
        return fail(state, RULE_FLOAT_INSTRUCTION,
                    "saturating float to integer conversion");
    switch (operation) {
    case 8U: {
        uint8_t reserved;
        if (!skip_u32_leb(state)) return 0;
        return read_byte(state, &reserved);
    }
    case 10U: {
        uint8_t first;
        uint8_t second;
        if (!read_byte(state, &first)) return 0;
        return read_byte(state, &second);
    }
    case 11U: {
        uint8_t reserved;
        return read_byte(state, &reserved);
    }
    case 12U:
    case 14U:
        if (!skip_u32_leb(state)) return 0;
        return skip_u32_leb(state);
    case 9U:
    case 13U:
    case 15U:
    case 16U:
    case 17U:
        return skip_u32_leb(state);
    default:
        return fail(state, RULE_UNKNOWN_INSTRUCTION,
                    "unrecognised 0xFC prefixed instruction");
    }
}

static int walk_expression(lint_state *state, size_t limit)
{
    unsigned depth = 0U;
    while (state->cursor < limit) {
        uint8_t opcode;
        if (!read_byte(state, &opcode)) return 0;
        if (float_opcode(opcode))
            return fail(state, RULE_FLOAT_INSTRUCTION,
                        "floating-point instruction");
        if (opcode >= 0x28U && opcode <= 0x3EU) {
            if (!skip_u32_leb(state)) return 0;
            if (!skip_u32_leb(state)) return 0;
            continue;
        }
        if (opcode >= 0x45U && opcode <= 0xC4U) continue;
        switch (opcode) {
        case 0x00U:
        case 0x01U:
        case 0x05U:
        case 0x0FU:
        case 0x1AU:
        case 0x1BU:
        case 0xD1U:
            break;
        case 0x02U:
        case 0x03U:
        case 0x04U:
            if (!check_block_type(state)) return 0;
            depth += 1U;
            break;
        case 0x0BU:
            if (depth == 0U) return 1;
            depth -= 1U;
            break;
        case 0x0CU:
        case 0x0DU:
        case 0x10U:
        case 0x20U:
        case 0x21U:
        case 0x22U:
        case 0x23U:
        case 0x24U:
        case 0x25U:
        case 0x26U:
        case 0x3FU:
        case 0x40U:
        case 0xD2U:
            if (!skip_u32_leb(state)) return 0;
            break;
        case 0x0EU: {
            uint32_t targets;
            uint32_t index;
            if (!read_u32_leb(state, &targets)) return 0;
            for (index = 0U; index < targets; ++index)
                if (!skip_u32_leb(state)) return 0;
            if (!skip_u32_leb(state)) return 0;
            break;
        }
        case 0x11U:
            if (!skip_u32_leb(state)) return 0;
            if (!skip_u32_leb(state)) return 0;
            break;
        case 0x1CU: {
            uint32_t types;
            uint32_t index;
            if (!read_u32_leb(state, &types)) return 0;
            for (index = 0U; index < types; ++index) {
                uint8_t code;
                if (!read_byte(state, &code)) return 0;
                if (!check_value_type(state, code)) return 0;
            }
            break;
        }
        case 0x41U:
            if (!skip_signed_leb(state, 5U)) return 0;
            break;
        case 0x42U:
            if (!skip_signed_leb(state, 10U)) return 0;
            break;
        case 0xD0U: {
            uint8_t code;
            if (!read_byte(state, &code)) return 0;
            break;
        }
        case 0xFCU:
            if (!walk_prefixed(state)) return 0;
            break;
        case 0xFDU:
            return fail(state, RULE_VECTOR, "v128 vector instruction");
        case 0xFEU:
            return fail(state, RULE_ATOMIC, "shared-memory atomic instruction");
        default:
            return fail(state, RULE_UNKNOWN_INSTRUCTION,
                        "unrecognised instruction opcode");
        }
    }
    return fail(state, RULE_MALFORMED, "expression ran past its section");
}

static int read_name(lint_state *state, char *out, size_t capacity)
{
    uint32_t length;
    uint32_t index;
    if (!read_u32_leb(state, &length)) return 0;
    if ((size_t)length >= capacity)
        return fail(state, RULE_MALFORMED, "name exceeds the declared bound");
    for (index = 0U; index < length; ++index) {
        uint8_t byte;
        if (!read_byte(state, &byte)) return 0;
        if (byte == 0U) return fail(state, RULE_MALFORMED, "embedded NUL in name");
        out[index] = (char)byte;
    }
    out[length] = '\0';
    return 1;
}

static const abi_function *permitted_import(const char *module, const char *name)
{
    size_t index;
    for (index = 0U; index < abi_function_count; ++index)
        if (abi_functions[index].introduced <= selected_abi_version &&
            strcmp(module, abi_functions[index].module) == 0 &&
            strcmp(name, abi_functions[index].name) == 0)
            return &abi_functions[index];
    return NULL;
}

static int exact_signature(const function_type *type, const char *expected)
{
    char signature[128];
    size_t used = 0U;
    uint32_t index;
    if (type->parameters > 16U || type->results != 1U) return 0;
    signature[used++] = '(';
    for (index = 0U; index < type->parameters; ++index) {
        const char *name;
        if (type->parameter_types[index] == 0x7FU) name = "i32";
        else if (type->parameter_types[index] == 0x7EU) name = "i64";
        else return 0;
        if (index != 0U) signature[used++] = ',';
        memcpy(signature + used, name, 3U);
        used += 3U;
    }
    memcpy(signature + used, ")->", 3U);
    used += 3U;
    if (type->result_type == 0x7FU) memcpy(signature + used, "i32", 4U);
    else if (type->result_type == 0x7EU) memcpy(signature + used, "i64", 4U);
    else return 0;
    return strcmp(signature, expected) == 0;
}

static int check_type_section(lint_state *state, size_t limit)
{
    uint32_t count;
    uint32_t index;
    if (!read_u32_leb(state, &count)) return 0;
    if (count > LINT_MAX_FUNCTIONS) return fail(state, RULE_MALFORMED, "too many function types");
    state->type_count = count;
    for (index = 0U; index < count; ++index) {
        uint8_t form;
        uint32_t parameters;
        uint32_t results;
        uint32_t position;
        if (state->cursor > limit)
            return fail(state, RULE_MALFORMED, "type section overran");
        if (!read_byte(state, &form)) return 0;
        if (form != 0x60U)
            return fail(state, RULE_MALFORMED, "unknown type form");
        if (!read_u32_leb(state, &parameters)) return 0;
        state->types[index].parameters = parameters;
        for (position = 0U; position < parameters; ++position) {
            uint8_t code;
            if (!read_byte(state, &code)) return 0;
            if (!check_value_type(state, code)) return 0;
            if (position < 16U) state->types[index].parameter_types[position] = code;
        }
        if (!read_u32_leb(state, &results)) return 0;
        state->types[index].results = results;
        for (position = 0U; position < results; ++position) {
            uint8_t code;
            if (!read_byte(state, &code)) return 0;
            if (!check_value_type(state, code)) return 0;
            if (position == 0U) state->types[index].result_type = code;
        }
    }
    return 1;
}

static int check_import_section(lint_state *state, size_t limit)
{
    uint32_t count;
    uint32_t index;
    char module[256];
    char name[256];
    if (!read_u32_leb(state, &count)) return 0;
    for (index = 0U; index < count; ++index) {
        uint8_t kind;
        uint32_t type_index;
        const abi_function *function;
        size_t function_index;
        if (state->cursor > limit)
            return fail(state, RULE_MALFORMED, "import section overran");
        if (!read_name(state, module, sizeof(module))) return 0;
        if (!read_name(state, name, sizeof(name))) return 0;
        if (!read_byte(state, &kind)) return 0;
        function = permitted_import(module, name);
        if (kind != 0x00U || function == NULL)
            return fail_named(state, RULE_FORBIDDEN_IMPORT, "%s::%s", module,
                              name);
        if (!read_u32_leb(state, &type_index)) return 0;
        if (type_index >= state->type_count || !exact_signature(&state->types[type_index], function->signature))
            return fail_named(state, RULE_IMPORT_SIGNATURE, "%s::%s", module, name);
        function_index = (size_t)(function - abi_functions);
        if (state->imported[function_index])
            return fail_named(state, RULE_FORBIDDEN_IMPORT, "duplicate %s::%s", module, name);
        state->imported[function_index] = 1U;
    }
    return 1;
}

static int check_function_section(lint_state *state)
{
    uint32_t count;
    uint32_t index;
    if (!read_u32_leb(state, &count)) return 0;
    state->function_count = count;
    if (count > (uint32_t)LINT_MAX_FUNCTIONS)
        return fail(state, RULE_TOO_MANY_FUNCTIONS,
                    "module declares more functions than the declared limit");
    for (index = 0U; index < count; ++index)
        if (!skip_u32_leb(state)) return 0;
    return 1;
}

static int check_global_section(lint_state *state, size_t limit)
{
    uint32_t count;
    uint32_t index;
    if (!read_u32_leb(state, &count)) return 0;
    for (index = 0U; index < count; ++index) {
        uint8_t code;
        uint8_t mutability;
        if (state->cursor > limit)
            return fail(state, RULE_MALFORMED, "global section overran");
        if (!read_byte(state, &code)) return 0;
        if (!check_value_type(state, code)) return 0;
        if (!read_byte(state, &mutability)) return 0;
        if (!walk_expression(state, limit)) return 0;
    }
    return 1;
}

static int check_export_section(lint_state *state, size_t limit)
{
    uint32_t count;
    uint32_t index;
    char name[256];
    if (!read_u32_leb(state, &count)) return 0;
    for (index = 0U; index < count; ++index) {
        uint8_t kind;
        if (state->cursor > limit)
            return fail(state, RULE_MALFORMED, "export section overran");
        if (!read_name(state, name, sizeof(name))) return 0;
        if (!read_byte(state, &kind)) return 0;
        if (!skip_u32_leb(state)) return 0;
        if (kind == 0x02U && strcmp(name, "memory") == 0)
            state->memory_exported = 1;
    }
    return 1;
}

static int check_code_section(lint_state *state, size_t limit)
{
    uint32_t count;
    uint32_t index;
    if (!read_u32_leb(state, &count)) return 0;
    for (index = 0U; index < count; ++index) {
        uint32_t body_size;
        uint32_t declarations;
        uint32_t declaration;
        size_t body_end;
        if (!read_u32_leb(state, &body_size)) return 0;
        body_end = state->cursor + (size_t)body_size;
        if (body_end > limit)
            return fail(state, RULE_MALFORMED, "function body overran");
        if (!read_u32_leb(state, &declarations)) return 0;
        for (declaration = 0U; declaration < declarations; ++declaration) {
            uint8_t code;
            if (!skip_u32_leb(state)) return 0;
            if (!read_byte(state, &code)) return 0;
            if (!check_value_type(state, code)) return 0;
        }
        if (!walk_expression(state, body_end)) return 0;
        if (state->cursor != body_end)
            return fail(state, RULE_MALFORMED,
                        "function body has trailing bytes");
    }
    return 1;
}

static int check_module(lint_state *state)
{
    static const uint8_t preamble[8] = { 0x00U, 0x61U, 0x73U, 0x6DU,
                                         0x01U, 0x00U, 0x00U, 0x00U };
    if (state->length > (size_t)LINT_MAX_MODULE_BYTES)
        return fail(state, RULE_MODULE_TOO_LARGE,
                    "module exceeds the declared byte-size limit");
    if (state->length < sizeof(preamble) ||
        memcmp(state->bytes, preamble, sizeof(preamble)) != 0)
        return fail(state, RULE_MALFORMED, "missing WebAssembly preamble");
    state->cursor = sizeof(preamble);
    while (state->cursor < state->length) {
        uint8_t id;
        uint32_t size;
        size_t section_end;
        if (!read_byte(state, &id)) return 0;
        if (!read_u32_leb(state, &size)) return 0;
        section_end = state->cursor + (size_t)size;
        if (section_end > state->length)
            return fail(state, RULE_MALFORMED, "section overran the module");
        switch (id) {
        case 1U:
            if (!check_type_section(state, section_end)) return 0;
            break;
        case 2U:
            if (!check_import_section(state, section_end)) return 0;
            break;
        case 3U:
            if (!check_function_section(state)) return 0;
            break;
        case 6U:
            if (!check_global_section(state, section_end)) return 0;
            break;
        case 7U:
            if (!check_export_section(state, section_end)) return 0;
            break;
        case 10U:
            if (!check_code_section(state, section_end)) return 0;
            break;
        default:
            break;
        }
        if ((id == 1U || id == 2U || id == 3U || id == 6U || id == 7U || id == 10U)
            && state->cursor != section_end)
            return fail(state, RULE_MALFORMED, "section has trailing or overrun bytes");
        state->cursor = section_end;
    }
    if (!state->memory_exported)
        return fail(state, RULE_MISSING_MEMORY,
                    "the host reads and writes guest memory through the "
                    "\"memory\" export");
    return 1;
}

static uint8_t *read_file(const char *path, size_t *length)
{
    FILE *handle = fopen(path, "rb");
    uint8_t *bytes;
    long size;
    size_t read_bytes;
    if (handle == NULL) return NULL;
    if (fseek(handle, 0L, SEEK_END) != 0) {
        (void)fclose(handle);
        return NULL;
    }
    size = ftell(handle);
    if (size < 0L || fseek(handle, 0L, SEEK_SET) != 0) {
        (void)fclose(handle);
        return NULL;
    }
    bytes = (uint8_t *)malloc((size_t)size + 1U);
    if (bytes == NULL) {
        (void)fclose(handle);
        return NULL;
    }
    read_bytes = fread(bytes, 1U, (size_t)size, handle);
    (void)fclose(handle);
    if (read_bytes != (size_t)size) {
        free(bytes);
        return NULL;
    }
    bytes[read_bytes] = 0U;
    *length = read_bytes;
    return bytes;
}

/*
 * Source gate. A guest may only name the frozen layerx_v1 module in an import
 * attribute; naming any other module is refused before the compiler runs.
 */
static int check_source(const char *path, const char *text, lint_state *state)
{
    const char *cursor = text;
    (void)path;
    for (;;) {
        const char *marker = strstr(cursor, "import_module");
        const char *quote;
        const char *close;
        size_t length;
        char module[256];
        if (marker == NULL) return 1;
        cursor = marker + 13;
        quote = strchr(cursor, '"');
        if (quote == NULL)
            return fail(state, RULE_IMPORT_DECLARATION,
                        "import_module without a module name");
        close = strchr(quote + 1, '"');
        if (close == NULL)
            return fail(state, RULE_IMPORT_DECLARATION,
                        "unterminated import module name");
        length = (size_t)(close - quote - 1);
        if (length >= sizeof(module))
            return fail(state, RULE_IMPORT_DECLARATION,
                        "import module name exceeds the declared bound");
        memcpy(module, quote + 1, length);
        module[length] = '\0';
        if (strncmp(module, "layerx_v", 8U) != 0 || strlen(module) != 9U ||
            module[8] < '1' || module[8] > '4' || (unsigned)(module[8] - '0') > selected_abi_version)
            return fail_named(state, RULE_IMPORT_DECLARATION,
                              "%s declared outside %s", module, "selected ABI");
        {
            const char *end = strchr(close, ';');
            const char *name_marker = strstr(close, "import_name");
            if (name_marker != NULL && (end == NULL || name_marker < end)) {
                const char *name_quote = strchr(name_marker, '"');
                const char *name_close = name_quote == NULL ? NULL : strchr(name_quote + 1, '"');
                char name[256];
                if (name_close == NULL || (end != NULL && name_close > end))
                    return fail(state, RULE_IMPORT_DECLARATION, "import_name without literal name");
                length = (size_t)(name_close - name_quote - 1);
                if (length >= sizeof(name)) return fail(state, RULE_IMPORT_DECLARATION, "import name too long");
                memcpy(name, name_quote + 1, length);
                name[length] = '\0';
                if (permitted_import(module, name) == NULL)
                    return fail_named(state, RULE_IMPORT_DECLARATION, "%s::%s", module, name);
            }
        }
        cursor = close + 1;
    }
}

static int lint_path(const char *path)
{
    size_t length = 0U;
    uint8_t *bytes = read_file(path, &length);
    lint_state state;
    const char *extension;
    int passed;
    if (bytes == NULL) {
        (void)fprintf(stderr, "determinism-lint: %s: unreadable\n", path);
        return 1;
    }
    memset(&state, 0, sizeof(state));
    state.bytes = bytes;
    state.length = length;
    extension = strrchr(path, '.');
    if (extension != NULL && strcmp(extension, ".wasm") == 0)
        passed = check_module(&state);
    else
        passed = check_source(path, (const char *)bytes, &state);
    free(bytes);
    if (!passed) {
        (void)fprintf(stderr, "determinism-lint: %s: %s: %s\n", path,
                      state.rule, state.detail);
        return 1;
    }
    (void)fprintf(stdout, "determinism-lint: %s: passed\n", path);
    return 0;
}

static int select_default_manifest(void)
{
    char source[PATH_MAX];
    const char suffix[] = "/c/tools/determinism_lint.c";
    size_t length;
    size_t prefix;
    if (realpath(__FILE__, source) == NULL) return 0;
    length = strlen(source);
    if (length < sizeof(suffix) - 1U ||
        strcmp(source + length - (sizeof(suffix) - 1U), suffix) != 0) return 0;
    prefix = length - (sizeof(suffix) - 1U);
    if (prefix + sizeof("/abi-manifest.tsv") > sizeof(default_manifest_path)) return 0;
    memcpy(default_manifest_path, source, prefix);
    memcpy(default_manifest_path + prefix, "/abi-manifest.tsv", sizeof("/abi-manifest.tsv"));
    manifest_path = default_manifest_path;
    return 1;
}

static int load_manifest(void)
{
    FILE *file = fopen(manifest_path, "rb");
    char line[256];
    unsigned previous = 1U;
    if (file == NULL) return 0;
    while (fgets(line, sizeof(line), file) != NULL) {
        char *fields[4];
        char *cursor = line;
        size_t index;
        unsigned introduced;
        size_t length = strlen(line);
        if (length == 0U || line[length - 1U] != '\n' || abi_function_count >= 28U) {
            (void)fclose(file); return 0;
        }
        line[length - 1U] = '\0';
        for (index = 0U; index < 4U; ++index) {
            fields[index] = cursor;
            if (index < 3U) {
                cursor = strchr(cursor, '\t');
                if (cursor == NULL) { (void)fclose(file); return 0; }
                *cursor++ = '\0';
            } else if (strchr(cursor, '\t') != NULL) { (void)fclose(file); return 0; }
        }
        if (strlen(fields[0]) != 1U || fields[0][0] < '1' || fields[0][0] > '4') {
            (void)fclose(file); return 0;
        }
        introduced = (unsigned)(fields[0][0] - '0');
        if (introduced < previous || strlen(fields[1]) != 9U ||
            strncmp(fields[1], "layerx_v", 8U) != 0 || fields[1][8] != fields[0][0] ||
            fields[2][0] == '\0' || strlen(fields[2]) >= 64U || strlen(fields[3]) >= 128U ||
            fields[3][0] != '(' || strstr(fields[3], ")->") == NULL) {
            (void)fclose(file); return 0;
        }
        for (index = 0U; index < abi_function_count; ++index)
            if (strcmp(abi_functions[index].module, fields[1]) == 0 &&
                strcmp(abi_functions[index].name, fields[2]) == 0) { (void)fclose(file); return 0; }
        abi_functions[abi_function_count].introduced = introduced;
        (void)strcpy(abi_functions[abi_function_count].module, fields[1]);
        (void)strcpy(abi_functions[abi_function_count].name, fields[2]);
        (void)strcpy(abi_functions[abi_function_count].signature, fields[3]);
        abi_function_count++;
        previous = introduced;
    }
    if (ferror(file)) { (void)fclose(file); return 0; }
    (void)fclose(file);
    return abi_function_count == 28U && previous == 4U;
}

int main(int argc, char **argv)
{
    int failures = 0;
    int index = 1;
    while (index < argc && strncmp(argv[index], "--", 2U) == 0) {
        if (strcmp(argv[index], "--abi-version") == 0 && index + 1 < argc) {
            const char *version = argv[++index];
            if (strlen(version) != 1U || version[0] < '1' || version[0] > '4') return 2;
            selected_abi_version = (unsigned)(version[0] - '0');
        } else if (strcmp(argv[index], "--abi-manifest") == 0 && index + 1 < argc) {
            manifest_path = argv[++index];
        } else return 2;
        index++;
    }
    if (index >= argc) {
        (void)fprintf(stderr, "usage: determinism-lint [--abi-version 1..4] [--abi-manifest path] <module.wasm|source.c>...\n");
        return 2;
    }
    if ((manifest_path == NULL && !select_default_manifest()) || !load_manifest()) {
        (void)fprintf(stderr, "determinism-lint: malformed or unreadable ABI manifest\n");
        return 2;
    }
    for (; index < argc; ++index) failures += lint_path(argv[index]);
    return failures == 0 ? 0 : 1;
}
