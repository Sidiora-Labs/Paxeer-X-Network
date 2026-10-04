#ifndef LAYERX_PROGRAM_HOST_H
#define LAYERX_PROGRAM_HOST_H

#include <stdint.h>

/*
 * The complete version-one host surface. These seven declarations are the only
 * imports a LayerX program may carry; the deterministic runtime refuses every
 * other module or name at validation time, so a clock, a socket, a thread or a
 * source of randomness cannot be reached even by accident.
 */
#define LXP_PROGRAM_IMPORT(function_name) \
    __attribute__((import_module("layerx_v1"), import_name(function_name)))

LXP_PROGRAM_IMPORT("storage_read")
extern int32_t lxp_program_host_storage_read(int32_t key_pointer,
                                             int32_t key_length,
                                             int32_t output_pointer,
                                             int32_t output_capacity);

LXP_PROGRAM_IMPORT("storage_write")
extern int32_t lxp_program_host_storage_write(int32_t key_pointer,
                                              int32_t key_length,
                                              int32_t value_pointer,
                                              int32_t value_length);

LXP_PROGRAM_IMPORT("storage_delete")
extern int32_t lxp_program_host_storage_delete(int32_t key_pointer,
                                               int32_t key_length);

LXP_PROGRAM_IMPORT("event_emit")
extern int32_t lxp_program_host_event_emit(int32_t topic_pointer,
                                           int32_t topic_length,
                                           int32_t data_pointer,
                                           int32_t data_length);

LXP_PROGRAM_IMPORT("program_call")
extern int32_t lxp_program_host_program_call(int32_t program_pointer,
                                             int32_t program_length,
                                             int32_t input_pointer,
                                             int32_t input_length,
                                             int32_t capabilities_pointer,
                                             int32_t capabilities_length);

LXP_PROGRAM_IMPORT("transfer_402")
extern int32_t lxp_program_host_transfer_402(int64_t amount_high,
                                             int64_t amount_low,
                                             int32_t asset_pointer,
                                             int32_t asset_length,
                                             int32_t recipient_pointer,
                                             int32_t recipient_length);

LXP_PROGRAM_IMPORT("receipt_read")
extern int32_t lxp_program_host_receipt_read(int32_t digest_pointer,
                                             int32_t digest_length,
                                             int32_t output_pointer,
                                             int32_t output_capacity);

#if defined(__wasm__)
#define LXP_PROGRAM_IMPORT_VERSIONED(module_name, function_name) \
    __attribute__((import_module(module_name), import_name(function_name)))
#else
#define LXP_PROGRAM_IMPORT_VERSIONED(module_name, function_name)
#endif

LXP_PROGRAM_IMPORT_VERSIONED("layerx_v2", "response_write")
extern int32_t lxp_program_host_response_write(int32_t code, int32_t pointer, int32_t length);

LXP_PROGRAM_IMPORT_VERSIONED("layerx_v2", "program_call_response")
extern int64_t lxp_program_host_program_call_response(int32_t program_pointer, int32_t program_length, int32_t input_pointer, int32_t input_length, int32_t capabilities_pointer, int32_t capabilities_length, int32_t output_pointer, int32_t output_capacity);

LXP_PROGRAM_IMPORT_VERSIONED("layerx_v2", "refusal_write")
extern int32_t lxp_program_host_refusal_write(int32_t class, int32_t reason_pointer, int32_t reason_length);

LXP_PROGRAM_IMPORT_VERSIONED("layerx_v2", "storage_read_scoped")
extern int32_t lxp_program_host_storage_read_scoped(int32_t selector, int32_t key_pointer, int32_t key_length, int32_t output_pointer, int32_t output_capacity);

LXP_PROGRAM_IMPORT_VERSIONED("layerx_v2", "storage_write_scoped")
extern int32_t lxp_program_host_storage_write_scoped(int32_t selector, int32_t key_pointer, int32_t key_length, int32_t value_pointer, int32_t value_length);

LXP_PROGRAM_IMPORT_VERSIONED("layerx_v2", "storage_delete_scoped")
extern int32_t lxp_program_host_storage_delete_scoped(int32_t selector, int32_t key_pointer, int32_t key_length);

LXP_PROGRAM_IMPORT_VERSIONED("layerx_v2", "storage_drop_scoped")
extern int32_t lxp_program_host_storage_drop_scoped(int32_t selector);

LXP_PROGRAM_IMPORT_VERSIONED("layerx_v2", "storage_scan_scoped")
extern int32_t lxp_program_host_storage_scan_scoped(int32_t selector, int32_t prefix_pointer, int32_t prefix_length, int32_t cursor_pointer, int32_t cursor_length, int32_t max_entries, int32_t max_bytes, int32_t output_pointer, int32_t output_capacity);

LXP_PROGRAM_IMPORT_VERSIONED("layerx_v2", "transfer_program_402")
extern int32_t lxp_program_host_transfer_program_402(int64_t amount_high, int64_t amount_low, int32_t seed_pointer, int32_t seed_length, int32_t source_pointer, int32_t source_length, int32_t asset_pointer, int32_t asset_length, int32_t recipient_pointer, int32_t recipient_length);

LXP_PROGRAM_IMPORT_VERSIONED("layerx_v2", "fund_program_402")
extern int32_t lxp_program_host_fund_program_402(int64_t amount_high, int64_t amount_low, int32_t seed_pointer, int32_t seed_length, int32_t destination_pointer, int32_t destination_length, int32_t asset_pointer, int32_t asset_length);

LXP_PROGRAM_IMPORT_VERSIONED("layerx_v2", "context_read")
extern int32_t lxp_program_host_context_read(int32_t field, int32_t output_pointer, int32_t output_capacity);

LXP_PROGRAM_IMPORT_VERSIONED("layerx_v2", "balance_read")
extern int32_t lxp_program_host_balance_read(int32_t account_pointer, int32_t account_length, int32_t asset_pointer, int32_t asset_length, int32_t output_pointer, int32_t output_capacity);

LXP_PROGRAM_IMPORT_VERSIONED("layerx_v2", "hash")
extern int32_t lxp_program_host_hash(int32_t algorithm, int32_t input_pointer, int32_t input_length, int32_t output_pointer);

LXP_PROGRAM_IMPORT_VERSIONED("layerx_v2", "signature_verify")
extern int32_t lxp_program_host_signature_verify(int32_t algorithm, int32_t message_pointer, int32_t message_length, int32_t public_key_pointer, int32_t public_key_length, int32_t signature_pointer, int32_t signature_length);

LXP_PROGRAM_IMPORT_VERSIONED("layerx_v2", "signature_recover")
extern int32_t lxp_program_host_signature_recover(int32_t message_pointer, int32_t message_length, int32_t signature_pointer, int32_t signature_length, int32_t recovery_id, int32_t output_pointer, int32_t output_capacity);

LXP_PROGRAM_IMPORT_VERSIONED("layerx_v2", "bigint_mul_256")
extern int32_t lxp_program_host_bigint_mul_256(int32_t left_pointer, int32_t left_length, int32_t right_pointer, int32_t right_length, int32_t output_pointer, int32_t output_capacity);

LXP_PROGRAM_IMPORT_VERSIONED("layerx_v2", "bigint_div_256")
extern int32_t lxp_program_host_bigint_div_256(int32_t left_pointer, int32_t left_length, int32_t right_pointer, int32_t right_length, int32_t output_pointer, int32_t output_capacity);

LXP_PROGRAM_IMPORT_VERSIONED("layerx_v2", "bigint_rem_256")
extern int32_t lxp_program_host_bigint_rem_256(int32_t left_pointer, int32_t left_length, int32_t right_pointer, int32_t right_length, int32_t output_pointer, int32_t output_capacity);

LXP_PROGRAM_IMPORT_VERSIONED("layerx_v2", "bigint_modexp_256")
extern int32_t lxp_program_host_bigint_modexp_256(int32_t base_pointer, int32_t base_length, int32_t exponent_pointer, int32_t exponent_length, int32_t modulus_pointer, int32_t modulus_length, int32_t output_pointer, int32_t output_capacity);

LXP_PROGRAM_IMPORT_VERSIONED("layerx_v3", "oracle_read")
extern int32_t lxp_program_host_oracle_read(int32_t market_pointer, int32_t market_length, int32_t output_pointer, int32_t output_capacity);

LXP_PROGRAM_IMPORT_VERSIONED("layerx_v4", "web_read")
extern int32_t lxp_program_host_web_read(int32_t request_pointer, int32_t request_length, int32_t output_pointer, int32_t output_capacity);


#endif
