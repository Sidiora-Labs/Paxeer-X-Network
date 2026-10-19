"use strict";
Object.defineProperty(exports, "__esModule", { value: true });
exports.WasmMessageSubtype = exports.ResourceType = exports.AccessOperationSelectorType = exports.AccessType = void 0;
var AccessType;
(function (AccessType) {
    AccessType[AccessType["UNKNOWN"] = 0] = "UNKNOWN";
    AccessType[AccessType["READ"] = 1] = "READ";
    AccessType[AccessType["WRITE"] = 2] = "WRITE";
    AccessType[AccessType["COMMIT"] = 3] = "COMMIT";
    AccessType[AccessType["UNRECOGNIZED"] = -1] = "UNRECOGNIZED";
})(AccessType || (exports.AccessType = AccessType = {}));
var AccessOperationSelectorType;
(function (AccessOperationSelectorType) {
    AccessOperationSelectorType[AccessOperationSelectorType["NONE"] = 0] = "NONE";
    AccessOperationSelectorType[AccessOperationSelectorType["JQ"] = 1] = "JQ";
    AccessOperationSelectorType[AccessOperationSelectorType["JQ_BECH32_ADDRESS"] = 2] = "JQ_BECH32_ADDRESS";
    AccessOperationSelectorType[AccessOperationSelectorType["JQ_LENGTH_PREFIXED_ADDRESS"] = 3] = "JQ_LENGTH_PREFIXED_ADDRESS";
    AccessOperationSelectorType[AccessOperationSelectorType["SENDER_BECH32_ADDRESS"] = 4] = "SENDER_BECH32_ADDRESS";
    AccessOperationSelectorType[AccessOperationSelectorType["SENDER_LENGTH_PREFIXED_ADDRESS"] = 5] = "SENDER_LENGTH_PREFIXED_ADDRESS";
    AccessOperationSelectorType[AccessOperationSelectorType["CONTRACT_ADDRESS"] = 6] = "CONTRACT_ADDRESS";
    AccessOperationSelectorType[AccessOperationSelectorType["JQ_MESSAGE_CONDITIONAL"] = 7] = "JQ_MESSAGE_CONDITIONAL";
    AccessOperationSelectorType[AccessOperationSelectorType["CONSTANT_STRING_TO_HEX"] = 8] = "CONSTANT_STRING_TO_HEX";
    AccessOperationSelectorType[AccessOperationSelectorType["CONTRACT_REFERENCE"] = 9] = "CONTRACT_REFERENCE";
    AccessOperationSelectorType[AccessOperationSelectorType["UNRECOGNIZED"] = -1] = "UNRECOGNIZED";
})(AccessOperationSelectorType || (exports.AccessOperationSelectorType = AccessOperationSelectorType = {}));
var ResourceType;
(function (ResourceType) {
    ResourceType[ResourceType["ANY"] = 0] = "ANY";
    /** KV - child of ANY */
    ResourceType[ResourceType["KV"] = 1] = "KV";
    /** Mem - child of ANY */
    ResourceType[ResourceType["Mem"] = 2] = "Mem";
    /** KV_BANK - child of KV */
    ResourceType[ResourceType["KV_BANK"] = 4] = "KV_BANK";
    /** KV_STAKING - child of KV */
    ResourceType[ResourceType["KV_STAKING"] = 5] = "KV_STAKING";
    /** KV_WASM - child of KV */
    ResourceType[ResourceType["KV_WASM"] = 6] = "KV_WASM";
    /** KV_ORACLE - child of KV */
    ResourceType[ResourceType["KV_ORACLE"] = 7] = "KV_ORACLE";
    /** KV_EPOCH - child of KV */
    ResourceType[ResourceType["KV_EPOCH"] = 9] = "KV_EPOCH";
    /** KV_TOKENFACTORY - child of KV */
    ResourceType[ResourceType["KV_TOKENFACTORY"] = 10] = "KV_TOKENFACTORY";
    /** KV_ORACLE_VOTE_TARGETS - child of KV_ORACLE */
    ResourceType[ResourceType["KV_ORACLE_VOTE_TARGETS"] = 11] = "KV_ORACLE_VOTE_TARGETS";
    /** KV_ORACLE_AGGREGATE_VOTES - child of KV_ORACLE */
    ResourceType[ResourceType["KV_ORACLE_AGGREGATE_VOTES"] = 12] = "KV_ORACLE_AGGREGATE_VOTES";
    /** KV_ORACLE_FEEDERS - child of KV_ORACLE */
    ResourceType[ResourceType["KV_ORACLE_FEEDERS"] = 13] = "KV_ORACLE_FEEDERS";
    /** KV_STAKING_DELEGATION - child of KV_STAKING */
    ResourceType[ResourceType["KV_STAKING_DELEGATION"] = 14] = "KV_STAKING_DELEGATION";
    /** KV_STAKING_VALIDATOR - child of KV_STAKING */
    ResourceType[ResourceType["KV_STAKING_VALIDATOR"] = 15] = "KV_STAKING_VALIDATOR";
    /** KV_AUTH - child of KV */
    ResourceType[ResourceType["KV_AUTH"] = 16] = "KV_AUTH";
    /** KV_AUTH_ADDRESS_STORE - child of KV */
    ResourceType[ResourceType["KV_AUTH_ADDRESS_STORE"] = 17] = "KV_AUTH_ADDRESS_STORE";
    /** KV_BANK_SUPPLY - child of KV_BANK */
    ResourceType[ResourceType["KV_BANK_SUPPLY"] = 18] = "KV_BANK_SUPPLY";
    /** KV_BANK_DENOM - child of KV_BANK */
    ResourceType[ResourceType["KV_BANK_DENOM"] = 19] = "KV_BANK_DENOM";
    /** KV_BANK_BALANCES - child of KV_BANK */
    ResourceType[ResourceType["KV_BANK_BALANCES"] = 20] = "KV_BANK_BALANCES";
    /** KV_TOKENFACTORY_DENOM - child of KV_TOKENFACTORY */
    ResourceType[ResourceType["KV_TOKENFACTORY_DENOM"] = 21] = "KV_TOKENFACTORY_DENOM";
    /** KV_TOKENFACTORY_METADATA - child of KV_TOKENFACTORY */
    ResourceType[ResourceType["KV_TOKENFACTORY_METADATA"] = 22] = "KV_TOKENFACTORY_METADATA";
    /** KV_TOKENFACTORY_ADMIN - child of KV_TOKENFACTORY */
    ResourceType[ResourceType["KV_TOKENFACTORY_ADMIN"] = 23] = "KV_TOKENFACTORY_ADMIN";
    /** KV_TOKENFACTORY_CREATOR - child of KV_TOKENFACTORY */
    ResourceType[ResourceType["KV_TOKENFACTORY_CREATOR"] = 24] = "KV_TOKENFACTORY_CREATOR";
    /** KV_ORACLE_EXCHANGE_RATE - child of KV_ORACLE */
    ResourceType[ResourceType["KV_ORACLE_EXCHANGE_RATE"] = 25] = "KV_ORACLE_EXCHANGE_RATE";
    /** KV_ORACLE_VOTE_PENALTY_COUNTER - child of KV_ORACLE */
    ResourceType[ResourceType["KV_ORACLE_VOTE_PENALTY_COUNTER"] = 26] = "KV_ORACLE_VOTE_PENALTY_COUNTER";
    /** KV_ORACLE_PRICE_SNAPSHOT - child of KV_ORACLE */
    ResourceType[ResourceType["KV_ORACLE_PRICE_SNAPSHOT"] = 27] = "KV_ORACLE_PRICE_SNAPSHOT";
    /** KV_STAKING_VALIDATION_POWER - child of KV_STAKING */
    ResourceType[ResourceType["KV_STAKING_VALIDATION_POWER"] = 28] = "KV_STAKING_VALIDATION_POWER";
    /** KV_STAKING_TOTAL_POWER - child of KV_STAKING */
    ResourceType[ResourceType["KV_STAKING_TOTAL_POWER"] = 29] = "KV_STAKING_TOTAL_POWER";
    /** KV_STAKING_VALIDATORS_CON_ADDR - child of KV_STAKING */
    ResourceType[ResourceType["KV_STAKING_VALIDATORS_CON_ADDR"] = 30] = "KV_STAKING_VALIDATORS_CON_ADDR";
    /** KV_STAKING_UNBONDING_DELEGATION - child of KV_STAKING */
    ResourceType[ResourceType["KV_STAKING_UNBONDING_DELEGATION"] = 31] = "KV_STAKING_UNBONDING_DELEGATION";
    /** KV_STAKING_UNBONDING_DELEGATION_VAL - child of KV_STAKING */
    ResourceType[ResourceType["KV_STAKING_UNBONDING_DELEGATION_VAL"] = 32] = "KV_STAKING_UNBONDING_DELEGATION_VAL";
    /** KV_STAKING_REDELEGATION - child of KV_STAKING */
    ResourceType[ResourceType["KV_STAKING_REDELEGATION"] = 33] = "KV_STAKING_REDELEGATION";
    /** KV_STAKING_REDELEGATION_VAL_SRC - child of KV_STAKING */
    ResourceType[ResourceType["KV_STAKING_REDELEGATION_VAL_SRC"] = 34] = "KV_STAKING_REDELEGATION_VAL_SRC";
    /** KV_STAKING_REDELEGATION_VAL_DST - child of KV_STAKING */
    ResourceType[ResourceType["KV_STAKING_REDELEGATION_VAL_DST"] = 35] = "KV_STAKING_REDELEGATION_VAL_DST";
    /** KV_STAKING_REDELEGATION_QUEUE - child of KV_STAKING */
    ResourceType[ResourceType["KV_STAKING_REDELEGATION_QUEUE"] = 36] = "KV_STAKING_REDELEGATION_QUEUE";
    /** KV_STAKING_VALIDATOR_QUEUE - child of KV_STAKING */
    ResourceType[ResourceType["KV_STAKING_VALIDATOR_QUEUE"] = 37] = "KV_STAKING_VALIDATOR_QUEUE";
    /** KV_STAKING_HISTORICAL_INFO - child of KV_STAKING */
    ResourceType[ResourceType["KV_STAKING_HISTORICAL_INFO"] = 38] = "KV_STAKING_HISTORICAL_INFO";
    /** KV_STAKING_UNBONDING - child of KV_STAKING */
    ResourceType[ResourceType["KV_STAKING_UNBONDING"] = 39] = "KV_STAKING_UNBONDING";
    /** KV_STAKING_VALIDATORS_BY_POWER - child of KV_STAKING */
    ResourceType[ResourceType["KV_STAKING_VALIDATORS_BY_POWER"] = 41] = "KV_STAKING_VALIDATORS_BY_POWER";
    /** KV_DISTRIBUTION - child of KV */
    ResourceType[ResourceType["KV_DISTRIBUTION"] = 40] = "KV_DISTRIBUTION";
    /** KV_DISTRIBUTION_FEE_POOL - child of KV_DISTRIBUTION */
    ResourceType[ResourceType["KV_DISTRIBUTION_FEE_POOL"] = 42] = "KV_DISTRIBUTION_FEE_POOL";
    /** KV_DISTRIBUTION_PROPOSER_KEY - child of KV_DISTRIBUTION */
    ResourceType[ResourceType["KV_DISTRIBUTION_PROPOSER_KEY"] = 43] = "KV_DISTRIBUTION_PROPOSER_KEY";
    /** KV_DISTRIBUTION_OUTSTANDING_REWARDS - child of KV_DISTRIBUTION */
    ResourceType[ResourceType["KV_DISTRIBUTION_OUTSTANDING_REWARDS"] = 44] = "KV_DISTRIBUTION_OUTSTANDING_REWARDS";
    /** KV_DISTRIBUTION_DELEGATOR_WITHDRAW_ADDR - child of KV_DISTRIBUTION */
    ResourceType[ResourceType["KV_DISTRIBUTION_DELEGATOR_WITHDRAW_ADDR"] = 45] = "KV_DISTRIBUTION_DELEGATOR_WITHDRAW_ADDR";
    /** KV_DISTRIBUTION_DELEGATOR_STARTING_INFO - child of KV_DISTRIBUTION */
    ResourceType[ResourceType["KV_DISTRIBUTION_DELEGATOR_STARTING_INFO"] = 46] = "KV_DISTRIBUTION_DELEGATOR_STARTING_INFO";
    /** KV_DISTRIBUTION_VAL_HISTORICAL_REWARDS - child of KV_DISTRIBUTION */
    ResourceType[ResourceType["KV_DISTRIBUTION_VAL_HISTORICAL_REWARDS"] = 47] = "KV_DISTRIBUTION_VAL_HISTORICAL_REWARDS";
    /** KV_DISTRIBUTION_VAL_CURRENT_REWARDS - child of KV_DISTRIBUTION */
    ResourceType[ResourceType["KV_DISTRIBUTION_VAL_CURRENT_REWARDS"] = 48] = "KV_DISTRIBUTION_VAL_CURRENT_REWARDS";
    /** KV_DISTRIBUTION_VAL_ACCUM_COMMISSION - child of KV_DISTRIBUTION */
    ResourceType[ResourceType["KV_DISTRIBUTION_VAL_ACCUM_COMMISSION"] = 49] = "KV_DISTRIBUTION_VAL_ACCUM_COMMISSION";
    /** KV_DISTRIBUTION_SLASH_EVENT - child of KV_DISTRIBUTION */
    ResourceType[ResourceType["KV_DISTRIBUTION_SLASH_EVENT"] = 50] = "KV_DISTRIBUTION_SLASH_EVENT";
    /** KV_ACCESSCONTROL - child of KV */
    ResourceType[ResourceType["KV_ACCESSCONTROL"] = 71] = "KV_ACCESSCONTROL";
    /** KV_ACCESSCONTROL_WASM_DEPENDENCY_MAPPING - child of KV_ACCESSCONTROL */
    ResourceType[ResourceType["KV_ACCESSCONTROL_WASM_DEPENDENCY_MAPPING"] = 72] = "KV_ACCESSCONTROL_WASM_DEPENDENCY_MAPPING";
    /** KV_WASM_CODE - child of KV_WASM */
    ResourceType[ResourceType["KV_WASM_CODE"] = 73] = "KV_WASM_CODE";
    /** KV_WASM_CONTRACT_ADDRESS - child of KV_WASM */
    ResourceType[ResourceType["KV_WASM_CONTRACT_ADDRESS"] = 74] = "KV_WASM_CONTRACT_ADDRESS";
    /** KV_WASM_CONTRACT_STORE - child of KV_WASM */
    ResourceType[ResourceType["KV_WASM_CONTRACT_STORE"] = 75] = "KV_WASM_CONTRACT_STORE";
    /** KV_WASM_SEQUENCE_KEY - child of KV_WASM */
    ResourceType[ResourceType["KV_WASM_SEQUENCE_KEY"] = 76] = "KV_WASM_SEQUENCE_KEY";
    /** KV_WASM_CONTRACT_CODE_HISTORY - child of KV_WASM */
    ResourceType[ResourceType["KV_WASM_CONTRACT_CODE_HISTORY"] = 77] = "KV_WASM_CONTRACT_CODE_HISTORY";
    /** KV_WASM_CONTRACT_BY_CODE_ID - child of KV_WASM */
    ResourceType[ResourceType["KV_WASM_CONTRACT_BY_CODE_ID"] = 78] = "KV_WASM_CONTRACT_BY_CODE_ID";
    /** KV_WASM_PINNED_CODE_INDEX - child of KV_WASM */
    ResourceType[ResourceType["KV_WASM_PINNED_CODE_INDEX"] = 79] = "KV_WASM_PINNED_CODE_INDEX";
    /** KV_AUTH_GLOBAL_ACCOUNT_NUMBER - child of KV_AUTH */
    ResourceType[ResourceType["KV_AUTH_GLOBAL_ACCOUNT_NUMBER"] = 80] = "KV_AUTH_GLOBAL_ACCOUNT_NUMBER";
    /** KV_AUTHZ - child of KV */
    ResourceType[ResourceType["KV_AUTHZ"] = 81] = "KV_AUTHZ";
    /** KV_FEEGRANT - child of KV */
    ResourceType[ResourceType["KV_FEEGRANT"] = 82] = "KV_FEEGRANT";
    /** KV_FEEGRANT_ALLOWANCE - child of KV_FEEGRANT */
    ResourceType[ResourceType["KV_FEEGRANT_ALLOWANCE"] = 83] = "KV_FEEGRANT_ALLOWANCE";
    /** KV_SLASHING - child of KV */
    ResourceType[ResourceType["KV_SLASHING"] = 84] = "KV_SLASHING";
    /** KV_SLASHING_VAL_SIGNING_INFO - child of KV_SLASHING */
    ResourceType[ResourceType["KV_SLASHING_VAL_SIGNING_INFO"] = 85] = "KV_SLASHING_VAL_SIGNING_INFO";
    /** KV_SLASHING_ADDR_PUBKEY_RELATION_KEY - child of KV_SLASHING */
    ResourceType[ResourceType["KV_SLASHING_ADDR_PUBKEY_RELATION_KEY"] = 86] = "KV_SLASHING_ADDR_PUBKEY_RELATION_KEY";
    /** KV_BANK_DEFERRED - child of KV */
    ResourceType[ResourceType["KV_BANK_DEFERRED"] = 93] = "KV_BANK_DEFERRED";
    /** KV_BANK_DEFERRED_MODULE_TX_INDEX - child of KV_BANK_DEFERRED */
    ResourceType[ResourceType["KV_BANK_DEFERRED_MODULE_TX_INDEX"] = 95] = "KV_BANK_DEFERRED_MODULE_TX_INDEX";
    /** KV_EVM - child of KV */
    ResourceType[ResourceType["KV_EVM"] = 96] = "KV_EVM";
    /** KV_EVM_BALANCE - child of KV_EVM; deprecated */
    ResourceType[ResourceType["KV_EVM_BALANCE"] = 97] = "KV_EVM_BALANCE";
    /** KV_EVM_TRANSIENT - child of KV_EVM */
    ResourceType[ResourceType["KV_EVM_TRANSIENT"] = 98] = "KV_EVM_TRANSIENT";
    /** KV_EVM_ACCOUNT_TRANSIENT - child of KV_EVM */
    ResourceType[ResourceType["KV_EVM_ACCOUNT_TRANSIENT"] = 99] = "KV_EVM_ACCOUNT_TRANSIENT";
    /** KV_EVM_MODULE_TRANSIENT - child of KV_EVM */
    ResourceType[ResourceType["KV_EVM_MODULE_TRANSIENT"] = 100] = "KV_EVM_MODULE_TRANSIENT";
    /** KV_EVM_NONCE - child of KV_EVM */
    ResourceType[ResourceType["KV_EVM_NONCE"] = 101] = "KV_EVM_NONCE";
    /** KV_EVM_RECEIPT - child of KV_EVM */
    ResourceType[ResourceType["KV_EVM_RECEIPT"] = 102] = "KV_EVM_RECEIPT";
    /** KV_EVM_S2E - child of KV_EVM */
    ResourceType[ResourceType["KV_EVM_S2E"] = 103] = "KV_EVM_S2E";
    /** KV_EVM_E2S - child of KV_EVM */
    ResourceType[ResourceType["KV_EVM_E2S"] = 104] = "KV_EVM_E2S";
    /** KV_EVM_CODE_HASH - child of KV_EVM */
    ResourceType[ResourceType["KV_EVM_CODE_HASH"] = 105] = "KV_EVM_CODE_HASH";
    /** KV_EVM_CODE - child of KV_EVM */
    ResourceType[ResourceType["KV_EVM_CODE"] = 106] = "KV_EVM_CODE";
    /** KV_EVM_CODE_SIZE - child of KV_EVM */
    ResourceType[ResourceType["KV_EVM_CODE_SIZE"] = 107] = "KV_EVM_CODE_SIZE";
    /** KV_BANK_WEI_BALANCE - child of KV_BANK */
    ResourceType[ResourceType["KV_BANK_WEI_BALANCE"] = 108] = "KV_BANK_WEI_BALANCE";
    /** KV_CT - child of KV */
    ResourceType[ResourceType["KV_CT"] = 111] = "KV_CT";
    /** KV_CT_ACCOUNT - child of KV_CT */
    ResourceType[ResourceType["KV_CT_ACCOUNT"] = 112] = "KV_CT_ACCOUNT";
    ResourceType[ResourceType["UNRECOGNIZED"] = -1] = "UNRECOGNIZED";
})(ResourceType || (exports.ResourceType = ResourceType = {}));
var WasmMessageSubtype;
(function (WasmMessageSubtype) {
    WasmMessageSubtype[WasmMessageSubtype["QUERY"] = 0] = "QUERY";
    WasmMessageSubtype[WasmMessageSubtype["EXECUTE"] = 1] = "EXECUTE";
    WasmMessageSubtype[WasmMessageSubtype["UNRECOGNIZED"] = -1] = "UNRECOGNIZED";
})(WasmMessageSubtype || (exports.WasmMessageSubtype = WasmMessageSubtype = {}));
