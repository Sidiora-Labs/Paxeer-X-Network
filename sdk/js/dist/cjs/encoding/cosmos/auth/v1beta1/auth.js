"use strict";
Object.defineProperty(exports, "__esModule", { value: true });
exports.aminoConverters = exports.registry = exports.Params = exports.ModuleAccount = exports.BaseAccount = void 0;
const wire_1 = require("@bufbuild/protobuf/wire");
const any_1 = require("../../../google/protobuf/any");
exports.BaseAccount = {
    $type: "cosmos.auth.v1beta1.BaseAccount",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.address !== "") {
            writer.uint32(10).string(message.address);
        }
        if (message.pub_key !== undefined) {
            any_1.Any.encode(message.pub_key, writer.uint32(18).fork()).join();
        }
        if (message.account_number !== 0) {
            writer.uint32(24).uint64(message.account_number);
        }
        if (message.sequence !== 0) {
            writer.uint32(32).uint64(message.sequence);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseBaseAccount();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.address = reader.string();
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.pub_key = any_1.Any.decode(reader, reader.uint32());
                    continue;
                case 3:
                    if (tag !== 24) {
                        break;
                    }
                    message.account_number = longToNumber(reader.uint64());
                    continue;
                case 4:
                    if (tag !== 32) {
                        break;
                    }
                    message.sequence = longToNumber(reader.uint64());
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
            address: isSet(object.address) ? globalThis.String(object.address) : "",
            pub_key: isSet(object.pub_key) ? any_1.Any.fromJSON(object.pub_key) : undefined,
            account_number: isSet(object.account_number) ? globalThis.Number(object.account_number) : 0,
            sequence: isSet(object.sequence) ? globalThis.Number(object.sequence) : 0
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.address !== "") {
            obj.address = message.address;
        }
        if (message.pub_key !== undefined) {
            obj.pub_key = any_1.Any.toJSON(message.pub_key);
        }
        if (message.account_number !== 0) {
            obj.account_number = Math.round(message.account_number);
        }
        if (message.sequence !== 0) {
            obj.sequence = Math.round(message.sequence);
        }
        return obj;
    },
    create(base) {
        return exports.BaseAccount.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseBaseAccount();
        message.address = object.address ?? "";
        message.pub_key = object.pub_key !== undefined && object.pub_key !== null ? any_1.Any.fromPartial(object.pub_key) : undefined;
        message.account_number = object.account_number ?? 0;
        message.sequence = object.sequence ?? 0;
        return message;
    }
};
exports.ModuleAccount = {
    $type: "cosmos.auth.v1beta1.ModuleAccount",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.base_account !== undefined) {
            exports.BaseAccount.encode(message.base_account, writer.uint32(10).fork()).join();
        }
        if (message.name !== "") {
            writer.uint32(18).string(message.name);
        }
        for (const v of message.permissions) {
            writer.uint32(26).string(v);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseModuleAccount();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.base_account = exports.BaseAccount.decode(reader, reader.uint32());
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.name = reader.string();
                    continue;
                case 3:
                    if (tag !== 26) {
                        break;
                    }
                    message.permissions.push(reader.string());
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
            base_account: isSet(object.base_account) ? exports.BaseAccount.fromJSON(object.base_account) : undefined,
            name: isSet(object.name) ? globalThis.String(object.name) : "",
            permissions: globalThis.Array.isArray(object?.permissions) ? object.permissions.map((e) => globalThis.String(e)) : []
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.base_account !== undefined) {
            obj.base_account = exports.BaseAccount.toJSON(message.base_account);
        }
        if (message.name !== "") {
            obj.name = message.name;
        }
        if (message.permissions?.length) {
            obj.permissions = message.permissions;
        }
        return obj;
    },
    create(base) {
        return exports.ModuleAccount.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseModuleAccount();
        message.base_account = object.base_account !== undefined && object.base_account !== null ? exports.BaseAccount.fromPartial(object.base_account) : undefined;
        message.name = object.name ?? "";
        message.permissions = object.permissions?.map((e) => e) || [];
        return message;
    }
};
exports.Params = {
    $type: "cosmos.auth.v1beta1.Params",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.max_memo_characters !== 0) {
            writer.uint32(8).uint64(message.max_memo_characters);
        }
        if (message.tx_sig_limit !== 0) {
            writer.uint32(16).uint64(message.tx_sig_limit);
        }
        if (message.tx_size_cost_per_byte !== 0) {
            writer.uint32(24).uint64(message.tx_size_cost_per_byte);
        }
        if (message.sig_verify_cost_ed25519 !== 0) {
            writer.uint32(32).uint64(message.sig_verify_cost_ed25519);
        }
        if (message.sig_verify_cost_secp256k1 !== 0) {
            writer.uint32(40).uint64(message.sig_verify_cost_secp256k1);
        }
        if (message.disable_seqno_check !== false) {
            writer.uint32(48).bool(message.disable_seqno_check);
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
                case 1:
                    if (tag !== 8) {
                        break;
                    }
                    message.max_memo_characters = longToNumber(reader.uint64());
                    continue;
                case 2:
                    if (tag !== 16) {
                        break;
                    }
                    message.tx_sig_limit = longToNumber(reader.uint64());
                    continue;
                case 3:
                    if (tag !== 24) {
                        break;
                    }
                    message.tx_size_cost_per_byte = longToNumber(reader.uint64());
                    continue;
                case 4:
                    if (tag !== 32) {
                        break;
                    }
                    message.sig_verify_cost_ed25519 = longToNumber(reader.uint64());
                    continue;
                case 5:
                    if (tag !== 40) {
                        break;
                    }
                    message.sig_verify_cost_secp256k1 = longToNumber(reader.uint64());
                    continue;
                case 6:
                    if (tag !== 48) {
                        break;
                    }
                    message.disable_seqno_check = reader.bool();
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
            max_memo_characters: isSet(object.max_memo_characters) ? globalThis.Number(object.max_memo_characters) : 0,
            tx_sig_limit: isSet(object.tx_sig_limit) ? globalThis.Number(object.tx_sig_limit) : 0,
            tx_size_cost_per_byte: isSet(object.tx_size_cost_per_byte) ? globalThis.Number(object.tx_size_cost_per_byte) : 0,
            sig_verify_cost_ed25519: isSet(object.sig_verify_cost_ed25519) ? globalThis.Number(object.sig_verify_cost_ed25519) : 0,
            sig_verify_cost_secp256k1: isSet(object.sig_verify_cost_secp256k1) ? globalThis.Number(object.sig_verify_cost_secp256k1) : 0,
            disable_seqno_check: isSet(object.disable_seqno_check) ? globalThis.Boolean(object.disable_seqno_check) : false
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.max_memo_characters !== 0) {
            obj.max_memo_characters = Math.round(message.max_memo_characters);
        }
        if (message.tx_sig_limit !== 0) {
            obj.tx_sig_limit = Math.round(message.tx_sig_limit);
        }
        if (message.tx_size_cost_per_byte !== 0) {
            obj.tx_size_cost_per_byte = Math.round(message.tx_size_cost_per_byte);
        }
        if (message.sig_verify_cost_ed25519 !== 0) {
            obj.sig_verify_cost_ed25519 = Math.round(message.sig_verify_cost_ed25519);
        }
        if (message.sig_verify_cost_secp256k1 !== 0) {
            obj.sig_verify_cost_secp256k1 = Math.round(message.sig_verify_cost_secp256k1);
        }
        if (message.disable_seqno_check !== false) {
            obj.disable_seqno_check = message.disable_seqno_check;
        }
        return obj;
    },
    create(base) {
        return exports.Params.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseParams();
        message.max_memo_characters = object.max_memo_characters ?? 0;
        message.tx_sig_limit = object.tx_sig_limit ?? 0;
        message.tx_size_cost_per_byte = object.tx_size_cost_per_byte ?? 0;
        message.sig_verify_cost_ed25519 = object.sig_verify_cost_ed25519 ?? 0;
        message.sig_verify_cost_secp256k1 = object.sig_verify_cost_secp256k1 ?? 0;
        message.disable_seqno_check = object.disable_seqno_check ?? false;
        return message;
    }
};
function createBaseBaseAccount() {
    return { address: "", pub_key: undefined, account_number: 0, sequence: 0 };
}
function createBaseModuleAccount() {
    return { base_account: undefined, name: "", permissions: [] };
}
function createBaseParams() {
    return {
        max_memo_characters: 0,
        tx_sig_limit: 0,
        tx_size_cost_per_byte: 0,
        sig_verify_cost_ed25519: 0,
        sig_verify_cost_secp256k1: 0,
        disable_seqno_check: false
    };
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
    ["/cosmos.auth.v1beta1.BaseAccount", exports.BaseAccount],
    ["/cosmos.auth.v1beta1.ModuleAccount", exports.ModuleAccount],
    ["/cosmos.auth.v1beta1.Params", exports.Params]
];
exports.aminoConverters = {
    "/cosmos.auth.v1beta1.BaseAccount": {
        aminoType: "cosmos-sdk/BaseAccount",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.auth.v1beta1.ModuleAccount": {
        aminoType: "cosmos-sdk/ModuleAccount",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.auth.v1beta1.Params": {
        aminoType: "cosmos-sdk/Params",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    }
};
