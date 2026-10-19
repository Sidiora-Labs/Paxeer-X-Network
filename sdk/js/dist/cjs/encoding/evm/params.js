"use strict";
Object.defineProperty(exports, "__esModule", { value: true });
exports.aminoConverters = exports.registry = exports.ParamsPreV580 = exports.Params = void 0;
const wire_1 = require("@bufbuild/protobuf/wire");
exports.Params = {
    $type: "paxprotocol.paxchain.evm.Params",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.priority_normalizer !== "") {
            writer.uint32(18).string(message.priority_normalizer);
        }
        if (message.base_fee_per_gas !== "") {
            writer.uint32(26).string(message.base_fee_per_gas);
        }
        if (message.minimum_fee_per_gas !== "") {
            writer.uint32(34).string(message.minimum_fee_per_gas);
        }
        for (const v of message.whitelisted_cw_code_hashes_for_delegate_call) {
            writer.uint32(66).bytes(v);
        }
        if (message.deliver_tx_hook_wasm_gas_limit !== 0) {
            writer.uint32(72).uint64(message.deliver_tx_hook_wasm_gas_limit);
        }
        if (message.max_dynamic_base_fee_upward_adjustment !== "") {
            writer.uint32(82).string(message.max_dynamic_base_fee_upward_adjustment);
        }
        if (message.max_dynamic_base_fee_downward_adjustment !== "") {
            writer.uint32(90).string(message.max_dynamic_base_fee_downward_adjustment);
        }
        if (message.target_gas_used_per_block !== 0) {
            writer.uint32(96).uint64(message.target_gas_used_per_block);
        }
        if (message.maximum_fee_per_gas !== "") {
            writer.uint32(106).string(message.maximum_fee_per_gas);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseParams();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.priority_normalizer = reader.string();
                    continue;
                case 3:
                    if (tag !== 26) {
                        break;
                    }
                    message.base_fee_per_gas = reader.string();
                    continue;
                case 4:
                    if (tag !== 34) {
                        break;
                    }
                    message.minimum_fee_per_gas = reader.string();
                    continue;
                case 8:
                    if (tag !== 66) {
                        break;
                    }
                    message.whitelisted_cw_code_hashes_for_delegate_call.push(reader.bytes());
                    continue;
                case 9:
                    if (tag !== 72) {
                        break;
                    }
                    message.deliver_tx_hook_wasm_gas_limit = longToNumber(reader.uint64());
                    continue;
                case 10:
                    if (tag !== 82) {
                        break;
                    }
                    message.max_dynamic_base_fee_upward_adjustment = reader.string();
                    continue;
                case 11:
                    if (tag !== 90) {
                        break;
                    }
                    message.max_dynamic_base_fee_downward_adjustment = reader.string();
                    continue;
                case 12:
                    if (tag !== 96) {
                        break;
                    }
                    message.target_gas_used_per_block = longToNumber(reader.uint64());
                    continue;
                case 13:
                    if (tag !== 106) {
                        break;
                    }
                    message.maximum_fee_per_gas = reader.string();
                    continue;
            }
            if ((tag & 7) === 4 || tag === 0) {
                break;
            }
            reader.skip(tag & 7);
        }
        return message;
    },
    fromJSON(object) {
        return {
            priority_normalizer: isSet(object.priority_normalizer) ? globalThis.String(object.priority_normalizer) : "",
            base_fee_per_gas: isSet(object.base_fee_per_gas) ? globalThis.String(object.base_fee_per_gas) : "",
            minimum_fee_per_gas: isSet(object.minimum_fee_per_gas) ? globalThis.String(object.minimum_fee_per_gas) : "",
            whitelisted_cw_code_hashes_for_delegate_call: globalThis.Array.isArray(object?.whitelisted_cw_code_hashes_for_delegate_call)
                ? object.whitelisted_cw_code_hashes_for_delegate_call.map((e) => bytesFromBase64(e))
                : [],
            deliver_tx_hook_wasm_gas_limit: isSet(object.deliver_tx_hook_wasm_gas_limit) ? globalThis.Number(object.deliver_tx_hook_wasm_gas_limit) : 0,
            max_dynamic_base_fee_upward_adjustment: isSet(object.max_dynamic_base_fee_upward_adjustment)
                ? globalThis.String(object.max_dynamic_base_fee_upward_adjustment)
                : "",
            max_dynamic_base_fee_downward_adjustment: isSet(object.max_dynamic_base_fee_downward_adjustment)
                ? globalThis.String(object.max_dynamic_base_fee_downward_adjustment)
                : "",
            target_gas_used_per_block: isSet(object.target_gas_used_per_block) ? globalThis.Number(object.target_gas_used_per_block) : 0,
            maximum_fee_per_gas: isSet(object.maximum_fee_per_gas) ? globalThis.String(object.maximum_fee_per_gas) : ""
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.priority_normalizer !== "") {
            obj.priority_normalizer = message.priority_normalizer;
        }
        if (message.base_fee_per_gas !== "") {
            obj.base_fee_per_gas = message.base_fee_per_gas;
        }
        if (message.minimum_fee_per_gas !== "") {
            obj.minimum_fee_per_gas = message.minimum_fee_per_gas;
        }
        if (message.whitelisted_cw_code_hashes_for_delegate_call?.length) {
            obj.whitelisted_cw_code_hashes_for_delegate_call = message.whitelisted_cw_code_hashes_for_delegate_call.map((e) => base64FromBytes(e));
        }
        if (message.deliver_tx_hook_wasm_gas_limit !== 0) {
            obj.deliver_tx_hook_wasm_gas_limit = Math.round(message.deliver_tx_hook_wasm_gas_limit);
        }
        if (message.max_dynamic_base_fee_upward_adjustment !== "") {
            obj.max_dynamic_base_fee_upward_adjustment = message.max_dynamic_base_fee_upward_adjustment;
        }
        if (message.max_dynamic_base_fee_downward_adjustment !== "") {
            obj.max_dynamic_base_fee_downward_adjustment = message.max_dynamic_base_fee_downward_adjustment;
        }
        if (message.target_gas_used_per_block !== 0) {
            obj.target_gas_used_per_block = Math.round(message.target_gas_used_per_block);
        }
        if (message.maximum_fee_per_gas !== "") {
            obj.maximum_fee_per_gas = message.maximum_fee_per_gas;
        }
        return obj;
    },
    create(base) {
        return exports.Params.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseParams();
        message.priority_normalizer = object.priority_normalizer ?? "";
        message.base_fee_per_gas = object.base_fee_per_gas ?? "";
        message.minimum_fee_per_gas = object.minimum_fee_per_gas ?? "";
        message.whitelisted_cw_code_hashes_for_delegate_call = object.whitelisted_cw_code_hashes_for_delegate_call?.map((e) => e) || [];
        message.deliver_tx_hook_wasm_gas_limit = object.deliver_tx_hook_wasm_gas_limit ?? 0;
        message.max_dynamic_base_fee_upward_adjustment = object.max_dynamic_base_fee_upward_adjustment ?? "";
        message.max_dynamic_base_fee_downward_adjustment = object.max_dynamic_base_fee_downward_adjustment ?? "";
        message.target_gas_used_per_block = object.target_gas_used_per_block ?? 0;
        message.maximum_fee_per_gas = object.maximum_fee_per_gas ?? "";
        return message;
    }
};
exports.ParamsPreV580 = {
    $type: "paxprotocol.paxchain.evm.ParamsPreV580",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.priority_normalizer !== "") {
            writer.uint32(18).string(message.priority_normalizer);
        }
        if (message.base_fee_per_gas !== "") {
            writer.uint32(26).string(message.base_fee_per_gas);
        }
        if (message.minimum_fee_per_gas !== "") {
            writer.uint32(34).string(message.minimum_fee_per_gas);
        }
        for (const v of message.whitelisted_cw_code_hashes_for_delegate_call) {
            writer.uint32(66).bytes(v);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseParamsPreV580();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.priority_normalizer = reader.string();
                    continue;
                case 3:
                    if (tag !== 26) {
                        break;
                    }
                    message.base_fee_per_gas = reader.string();
                    continue;
                case 4:
                    if (tag !== 34) {
                        break;
                    }
                    message.minimum_fee_per_gas = reader.string();
                    continue;
                case 8:
                    if (tag !== 66) {
                        break;
                    }
                    message.whitelisted_cw_code_hashes_for_delegate_call.push(reader.bytes());
                    continue;
            }
            if ((tag & 7) === 4 || tag === 0) {
                break;
            }
            reader.skip(tag & 7);
        }
        return message;
    },
    fromJSON(object) {
        return {
            priority_normalizer: isSet(object.priority_normalizer) ? globalThis.String(object.priority_normalizer) : "",
            base_fee_per_gas: isSet(object.base_fee_per_gas) ? globalThis.String(object.base_fee_per_gas) : "",
            minimum_fee_per_gas: isSet(object.minimum_fee_per_gas) ? globalThis.String(object.minimum_fee_per_gas) : "",
            whitelisted_cw_code_hashes_for_delegate_call: globalThis.Array.isArray(object?.whitelisted_cw_code_hashes_for_delegate_call)
                ? object.whitelisted_cw_code_hashes_for_delegate_call.map((e) => bytesFromBase64(e))
                : []
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.priority_normalizer !== "") {
            obj.priority_normalizer = message.priority_normalizer;
        }
        if (message.base_fee_per_gas !== "") {
            obj.base_fee_per_gas = message.base_fee_per_gas;
        }
        if (message.minimum_fee_per_gas !== "") {
            obj.minimum_fee_per_gas = message.minimum_fee_per_gas;
        }
        if (message.whitelisted_cw_code_hashes_for_delegate_call?.length) {
            obj.whitelisted_cw_code_hashes_for_delegate_call = message.whitelisted_cw_code_hashes_for_delegate_call.map((e) => base64FromBytes(e));
        }
        return obj;
    },
    create(base) {
        return exports.ParamsPreV580.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseParamsPreV580();
        message.priority_normalizer = object.priority_normalizer ?? "";
        message.base_fee_per_gas = object.base_fee_per_gas ?? "";
        message.minimum_fee_per_gas = object.minimum_fee_per_gas ?? "";
        message.whitelisted_cw_code_hashes_for_delegate_call = object.whitelisted_cw_code_hashes_for_delegate_call?.map((e) => e) || [];
        return message;
    }
};
function createBaseParams() {
    return {
        priority_normalizer: "",
        base_fee_per_gas: "",
        minimum_fee_per_gas: "",
        whitelisted_cw_code_hashes_for_delegate_call: [],
        deliver_tx_hook_wasm_gas_limit: 0,
        max_dynamic_base_fee_upward_adjustment: "",
        max_dynamic_base_fee_downward_adjustment: "",
        target_gas_used_per_block: 0,
        maximum_fee_per_gas: ""
    };
}
function createBaseParamsPreV580() {
    return {
        priority_normalizer: "",
        base_fee_per_gas: "",
        minimum_fee_per_gas: "",
        whitelisted_cw_code_hashes_for_delegate_call: []
    };
}
function bytesFromBase64(b64) {
    if (globalThis.Buffer) {
        return Uint8Array.from(globalThis.Buffer.from(b64, "base64"));
    }
    else {
        const bin = globalThis.atob(b64);
        const arr = new Uint8Array(bin.length);
        for (let i = 0; i < bin.length; ++i) {
            arr[i] = bin.charCodeAt(i);
        }
        return arr;
    }
}
function base64FromBytes(arr) {
    if (globalThis.Buffer) {
        return globalThis.Buffer.from(arr).toString("base64");
    }
    else {
        const bin = [];
        arr.forEach((byte) => {
            bin.push(globalThis.String.fromCharCode(byte));
        });
        return globalThis.btoa(bin.join(""));
    }
}
function longToNumber(int64) {
    const num = globalThis.Number(int64.toString());
    if (num > globalThis.Number.MAX_SAFE_INTEGER) {
        throw new globalThis.Error("Value is larger than Number.MAX_SAFE_INTEGER");
    }
    if (num < globalThis.Number.MIN_SAFE_INTEGER) {
        throw new globalThis.Error("Value is smaller than Number.MIN_SAFE_INTEGER");
    }
    return num;
}
function isSet(value) {
    return value !== null && value !== undefined;
}
exports.registry = [
    ["/paxprotocol.paxchain.evm.Params", exports.Params],
    ["/paxprotocol.paxchain.evm.ParamsPreV580", exports.ParamsPreV580]
];
exports.aminoConverters = {
    "/paxprotocol.paxchain.evm.Params": {
        aminoType: "evm/Params",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/paxprotocol.paxchain.evm.ParamsPreV580": {
        aminoType: "evm/ParamsPreV580",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    }
};
