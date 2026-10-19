"use strict";
Object.defineProperty(exports, "__esModule", { value: true });
exports.aminoConverters = exports.registry = exports.LastValidatorPower = exports.GenesisState = void 0;
const wire_1 = require("@bufbuild/protobuf/wire");
const staking_1 = require("./staking");
exports.GenesisState = {
    $type: "cosmos.staking.v1beta1.GenesisState",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.params !== undefined) {
            staking_1.Params.encode(message.params, writer.uint32(10).fork()).join();
        }
        if (message.last_total_power.length !== 0) {
            writer.uint32(18).bytes(message.last_total_power);
        }
        for (const v of message.last_validator_powers) {
            exports.LastValidatorPower.encode(v, writer.uint32(26).fork()).join();
        }
        for (const v of message.validators) {
            staking_1.Validator.encode(v, writer.uint32(34).fork()).join();
        }
        for (const v of message.delegations) {
            staking_1.Delegation.encode(v, writer.uint32(42).fork()).join();
        }
        for (const v of message.unbonding_delegations) {
            staking_1.UnbondingDelegation.encode(v, writer.uint32(50).fork()).join();
        }
        for (const v of message.redelegations) {
            staking_1.Redelegation.encode(v, writer.uint32(58).fork()).join();
        }
        if (message.exported !== false) {
            writer.uint32(64).bool(message.exported);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseGenesisState();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.params = staking_1.Params.decode(reader, reader.uint32());
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.last_total_power = reader.bytes();
                    continue;
                case 3:
                    if (tag !== 26) {
                        break;
                    }
                    message.last_validator_powers.push(exports.LastValidatorPower.decode(reader, reader.uint32()));
                    continue;
                case 4:
                    if (tag !== 34) {
                        break;
                    }
                    message.validators.push(staking_1.Validator.decode(reader, reader.uint32()));
                    continue;
                case 5:
                    if (tag !== 42) {
                        break;
                    }
                    message.delegations.push(staking_1.Delegation.decode(reader, reader.uint32()));
                    continue;
                case 6:
                    if (tag !== 50) {
                        break;
                    }
                    message.unbonding_delegations.push(staking_1.UnbondingDelegation.decode(reader, reader.uint32()));
                    continue;
                case 7:
                    if (tag !== 58) {
                        break;
                    }
                    message.redelegations.push(staking_1.Redelegation.decode(reader, reader.uint32()));
                    continue;
                case 8:
                    if (tag !== 64) {
                        break;
                    }
                    message.exported = reader.bool();
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
            params: isSet(object.params) ? staking_1.Params.fromJSON(object.params) : undefined,
            last_total_power: isSet(object.last_total_power) ? bytesFromBase64(object.last_total_power) : new Uint8Array(0),
            last_validator_powers: globalThis.Array.isArray(object?.last_validator_powers)
                ? object.last_validator_powers.map((e) => exports.LastValidatorPower.fromJSON(e))
                : [],
            validators: globalThis.Array.isArray(object?.validators) ? object.validators.map((e) => staking_1.Validator.fromJSON(e)) : [],
            delegations: globalThis.Array.isArray(object?.delegations) ? object.delegations.map((e) => staking_1.Delegation.fromJSON(e)) : [],
            unbonding_delegations: globalThis.Array.isArray(object?.unbonding_delegations)
                ? object.unbonding_delegations.map((e) => staking_1.UnbondingDelegation.fromJSON(e))
                : [],
            redelegations: globalThis.Array.isArray(object?.redelegations) ? object.redelegations.map((e) => staking_1.Redelegation.fromJSON(e)) : [],
            exported: isSet(object.exported) ? globalThis.Boolean(object.exported) : false
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.params !== undefined) {
            obj.params = staking_1.Params.toJSON(message.params);
        }
        if (message.last_total_power.length !== 0) {
            obj.last_total_power = base64FromBytes(message.last_total_power);
        }
        if (message.last_validator_powers?.length) {
            obj.last_validator_powers = message.last_validator_powers.map((e) => exports.LastValidatorPower.toJSON(e));
        }
        if (message.validators?.length) {
            obj.validators = message.validators.map((e) => staking_1.Validator.toJSON(e));
        }
        if (message.delegations?.length) {
            obj.delegations = message.delegations.map((e) => staking_1.Delegation.toJSON(e));
        }
        if (message.unbonding_delegations?.length) {
            obj.unbonding_delegations = message.unbonding_delegations.map((e) => staking_1.UnbondingDelegation.toJSON(e));
        }
        if (message.redelegations?.length) {
            obj.redelegations = message.redelegations.map((e) => staking_1.Redelegation.toJSON(e));
        }
        if (message.exported !== false) {
            obj.exported = message.exported;
        }
        return obj;
    },
    create(base) {
        return exports.GenesisState.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseGenesisState();
        message.params = object.params !== undefined && object.params !== null ? staking_1.Params.fromPartial(object.params) : undefined;
        message.last_total_power = object.last_total_power ?? new Uint8Array(0);
        message.last_validator_powers = object.last_validator_powers?.map((e) => exports.LastValidatorPower.fromPartial(e)) || [];
        message.validators = object.validators?.map((e) => staking_1.Validator.fromPartial(e)) || [];
        message.delegations = object.delegations?.map((e) => staking_1.Delegation.fromPartial(e)) || [];
        message.unbonding_delegations = object.unbonding_delegations?.map((e) => staking_1.UnbondingDelegation.fromPartial(e)) || [];
        message.redelegations = object.redelegations?.map((e) => staking_1.Redelegation.fromPartial(e)) || [];
        message.exported = object.exported ?? false;
        return message;
    }
};
exports.LastValidatorPower = {
    $type: "cosmos.staking.v1beta1.LastValidatorPower",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.address !== "") {
            writer.uint32(10).string(message.address);
        }
        if (message.power !== 0) {
            writer.uint32(16).int64(message.power);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseLastValidatorPower();
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
                    if (tag !== 16) {
                        break;
                    }
                    message.power = longToNumber(reader.int64());
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
            power: isSet(object.power) ? globalThis.Number(object.power) : 0
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.address !== "") {
            obj.address = message.address;
        }
        if (message.power !== 0) {
            obj.power = Math.round(message.power);
        }
        return obj;
    },
    create(base) {
        return exports.LastValidatorPower.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseLastValidatorPower();
        message.address = object.address ?? "";
        message.power = object.power ?? 0;
        return message;
    }
};
function createBaseGenesisState() {
    return {
        params: undefined,
        last_total_power: new Uint8Array(0),
        last_validator_powers: [],
        validators: [],
        delegations: [],
        unbonding_delegations: [],
        redelegations: [],
        exported: false
    };
}
function createBaseLastValidatorPower() {
    return { address: "", power: 0 };
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
    ["/cosmos.staking.v1beta1.GenesisState", exports.GenesisState],
    ["/cosmos.staking.v1beta1.LastValidatorPower", exports.LastValidatorPower]
];
exports.aminoConverters = {
    "/cosmos.staking.v1beta1.GenesisState": {
        aminoType: "cosmos-sdk/GenesisState",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.staking.v1beta1.LastValidatorPower": {
        aminoType: "cosmos-sdk/LastValidatorPower",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    }
};
