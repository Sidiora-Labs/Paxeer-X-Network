"use strict";
Object.defineProperty(exports, "__esModule", { value: true });
exports.aminoConverters = exports.registry = exports.GenesisState = exports.GenesisOwners = void 0;
const wire_1 = require("@bufbuild/protobuf/wire");
const capability_1 = require("./capability");
exports.GenesisOwners = {
    $type: "cosmos.capability.v1beta1.GenesisOwners",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.index !== 0) {
            writer.uint32(8).uint64(message.index);
        }
        if (message.index_owners !== undefined) {
            capability_1.CapabilityOwners.encode(message.index_owners, writer.uint32(18).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseGenesisOwners();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 8) {
                        break;
                    }
                    message.index = longToNumber(reader.uint64());
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.index_owners = capability_1.CapabilityOwners.decode(reader, reader.uint32());
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
            index: isSet(object.index) ? globalThis.Number(object.index) : 0,
            index_owners: isSet(object.index_owners) ? capability_1.CapabilityOwners.fromJSON(object.index_owners) : undefined
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.index !== 0) {
            obj.index = Math.round(message.index);
        }
        if (message.index_owners !== undefined) {
            obj.index_owners = capability_1.CapabilityOwners.toJSON(message.index_owners);
        }
        return obj;
    },
    create(base) {
        return exports.GenesisOwners.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseGenesisOwners();
        message.index = object.index ?? 0;
        message.index_owners = object.index_owners !== undefined && object.index_owners !== null ? capability_1.CapabilityOwners.fromPartial(object.index_owners) : undefined;
        return message;
    }
};
exports.GenesisState = {
    $type: "cosmos.capability.v1beta1.GenesisState",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.index !== 0) {
            writer.uint32(8).uint64(message.index);
        }
        for (const v of message.owners) {
            exports.GenesisOwners.encode(v, writer.uint32(18).fork()).join();
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
                    if (tag !== 8) {
                        break;
                    }
                    message.index = longToNumber(reader.uint64());
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.owners.push(exports.GenesisOwners.decode(reader, reader.uint32()));
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
            index: isSet(object.index) ? globalThis.Number(object.index) : 0,
            owners: globalThis.Array.isArray(object?.owners) ? object.owners.map((e) => exports.GenesisOwners.fromJSON(e)) : []
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.index !== 0) {
            obj.index = Math.round(message.index);
        }
        if (message.owners?.length) {
            obj.owners = message.owners.map((e) => exports.GenesisOwners.toJSON(e));
        }
        return obj;
    },
    create(base) {
        return exports.GenesisState.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseGenesisState();
        message.index = object.index ?? 0;
        message.owners = object.owners?.map((e) => exports.GenesisOwners.fromPartial(e)) || [];
        return message;
    }
};
function createBaseGenesisOwners() {
    return { index: 0, index_owners: undefined };
}
function createBaseGenesisState() {
    return { index: 0, owners: [] };
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
    ["/cosmos.capability.v1beta1.GenesisOwners", exports.GenesisOwners],
    ["/cosmos.capability.v1beta1.GenesisState", exports.GenesisState]
];
exports.aminoConverters = {
    "/cosmos.capability.v1beta1.GenesisOwners": {
        aminoType: "cosmos-sdk/GenesisOwners",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.capability.v1beta1.GenesisState": {
        aminoType: "cosmos-sdk/GenesisState",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    }
};
