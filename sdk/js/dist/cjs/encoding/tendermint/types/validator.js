"use strict";
Object.defineProperty(exports, "__esModule", { value: true });
exports.aminoConverters = exports.registry = exports.SimpleValidator = exports.Validator = exports.ValidatorSet = void 0;
const wire_1 = require("@bufbuild/protobuf/wire");
const keys_1 = require("../crypto/keys");
exports.ValidatorSet = {
    $type: "tendermint.types.ValidatorSet",
    encode(message, writer = new wire_1.BinaryWriter()) {
        for (const v of message.validators) {
            exports.Validator.encode(v, writer.uint32(10).fork()).join();
        }
        if (message.proposer !== undefined) {
            exports.Validator.encode(message.proposer, writer.uint32(18).fork()).join();
        }
        if (message.total_voting_power !== 0) {
            writer.uint32(24).int64(message.total_voting_power);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseValidatorSet();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.validators.push(exports.Validator.decode(reader, reader.uint32()));
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.proposer = exports.Validator.decode(reader, reader.uint32());
                    continue;
                case 3:
                    if (tag !== 24) {
                        break;
                    }
                    message.total_voting_power = longToNumber(reader.int64());
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
            validators: globalThis.Array.isArray(object?.validators) ? object.validators.map((e) => exports.Validator.fromJSON(e)) : [],
            proposer: isSet(object.proposer) ? exports.Validator.fromJSON(object.proposer) : undefined,
            total_voting_power: isSet(object.total_voting_power) ? globalThis.Number(object.total_voting_power) : 0
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.validators?.length) {
            obj.validators = message.validators.map((e) => exports.Validator.toJSON(e));
        }
        if (message.proposer !== undefined) {
            obj.proposer = exports.Validator.toJSON(message.proposer);
        }
        if (message.total_voting_power !== 0) {
            obj.total_voting_power = Math.round(message.total_voting_power);
        }
        return obj;
    },
    create(base) {
        return exports.ValidatorSet.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseValidatorSet();
        message.validators = object.validators?.map((e) => exports.Validator.fromPartial(e)) || [];
        message.proposer = object.proposer !== undefined && object.proposer !== null ? exports.Validator.fromPartial(object.proposer) : undefined;
        message.total_voting_power = object.total_voting_power ?? 0;
        return message;
    }
};
exports.Validator = {
    $type: "tendermint.types.Validator",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.address.length !== 0) {
            writer.uint32(10).bytes(message.address);
        }
        if (message.pub_key !== undefined) {
            keys_1.PublicKey.encode(message.pub_key, writer.uint32(18).fork()).join();
        }
        if (message.voting_power !== 0) {
            writer.uint32(24).int64(message.voting_power);
        }
        if (message.proposer_priority !== 0) {
            writer.uint32(32).int64(message.proposer_priority);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseValidator();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.address = reader.bytes();
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.pub_key = keys_1.PublicKey.decode(reader, reader.uint32());
                    continue;
                case 3:
                    if (tag !== 24) {
                        break;
                    }
                    message.voting_power = longToNumber(reader.int64());
                    continue;
                case 4:
                    if (tag !== 32) {
                        break;
                    }
                    message.proposer_priority = longToNumber(reader.int64());
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
            address: isSet(object.address) ? bytesFromBase64(object.address) : new Uint8Array(0),
            pub_key: isSet(object.pub_key) ? keys_1.PublicKey.fromJSON(object.pub_key) : undefined,
            voting_power: isSet(object.voting_power) ? globalThis.Number(object.voting_power) : 0,
            proposer_priority: isSet(object.proposer_priority) ? globalThis.Number(object.proposer_priority) : 0
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.address.length !== 0) {
            obj.address = base64FromBytes(message.address);
        }
        if (message.pub_key !== undefined) {
            obj.pub_key = keys_1.PublicKey.toJSON(message.pub_key);
        }
        if (message.voting_power !== 0) {
            obj.voting_power = Math.round(message.voting_power);
        }
        if (message.proposer_priority !== 0) {
            obj.proposer_priority = Math.round(message.proposer_priority);
        }
        return obj;
    },
    create(base) {
        return exports.Validator.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseValidator();
        message.address = object.address ?? new Uint8Array(0);
        message.pub_key = object.pub_key !== undefined && object.pub_key !== null ? keys_1.PublicKey.fromPartial(object.pub_key) : undefined;
        message.voting_power = object.voting_power ?? 0;
        message.proposer_priority = object.proposer_priority ?? 0;
        return message;
    }
};
exports.SimpleValidator = {
    $type: "tendermint.types.SimpleValidator",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.pub_key !== undefined) {
            keys_1.PublicKey.encode(message.pub_key, writer.uint32(10).fork()).join();
        }
        if (message.voting_power !== 0) {
            writer.uint32(16).int64(message.voting_power);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseSimpleValidator();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.pub_key = keys_1.PublicKey.decode(reader, reader.uint32());
                    continue;
                case 2:
                    if (tag !== 16) {
                        break;
                    }
                    message.voting_power = longToNumber(reader.int64());
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
            pub_key: isSet(object.pub_key) ? keys_1.PublicKey.fromJSON(object.pub_key) : undefined,
            voting_power: isSet(object.voting_power) ? globalThis.Number(object.voting_power) : 0
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.pub_key !== undefined) {
            obj.pub_key = keys_1.PublicKey.toJSON(message.pub_key);
        }
        if (message.voting_power !== 0) {
            obj.voting_power = Math.round(message.voting_power);
        }
        return obj;
    },
    create(base) {
        return exports.SimpleValidator.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseSimpleValidator();
        message.pub_key = object.pub_key !== undefined && object.pub_key !== null ? keys_1.PublicKey.fromPartial(object.pub_key) : undefined;
        message.voting_power = object.voting_power ?? 0;
        return message;
    }
};
function createBaseValidatorSet() {
    return { validators: [], proposer: undefined, total_voting_power: 0 };
}
function createBaseValidator() {
    return { address: new Uint8Array(0), pub_key: undefined, voting_power: 0, proposer_priority: 0 };
}
function createBaseSimpleValidator() {
    return { pub_key: undefined, voting_power: 0 };
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
    ["/tendermint.types.ValidatorSet", exports.ValidatorSet],
    ["/tendermint.types.Validator", exports.Validator],
    ["/tendermint.types.SimpleValidator", exports.SimpleValidator]
];
exports.aminoConverters = {
    "/tendermint.types.ValidatorSet": {
        aminoType: "tendermint.types.ValidatorSet",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/tendermint.types.Validator": {
        aminoType: "tendermint.types.Validator",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/tendermint.types.SimpleValidator": {
        aminoType: "tendermint.types.SimpleValidator",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    }
};
