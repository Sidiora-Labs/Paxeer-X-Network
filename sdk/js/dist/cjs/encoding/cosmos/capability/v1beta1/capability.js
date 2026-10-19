"use strict";
Object.defineProperty(exports, "__esModule", { value: true });
exports.aminoConverters = exports.registry = exports.CapabilityOwners = exports.Owner = exports.Capability = void 0;
const wire_1 = require("@bufbuild/protobuf/wire");
exports.Capability = {
    $type: "cosmos.capability.v1beta1.Capability",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.index !== 0) {
            writer.uint32(8).uint64(message.index);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseCapability();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 8) {
                        break;
                    }
                    message.index = longToNumber(reader.uint64());
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
        return { index: isSet(object.index) ? globalThis.Number(object.index) : 0 };
    },
    toJSON(message) {
        const obj = {};
        if (message.index !== 0) {
            obj.index = Math.round(message.index);
        }
        return obj;
    },
    create(base) {
        return exports.Capability.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseCapability();
        message.index = object.index ?? 0;
        return message;
    }
};
exports.Owner = {
    $type: "cosmos.capability.v1beta1.Owner",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.module !== "") {
            writer.uint32(10).string(message.module);
        }
        if (message.name !== "") {
            writer.uint32(18).string(message.name);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseOwner();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.module = reader.string();
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.name = reader.string();
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
            module: isSet(object.module) ? globalThis.String(object.module) : "",
            name: isSet(object.name) ? globalThis.String(object.name) : ""
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.module !== "") {
            obj.module = message.module;
        }
        if (message.name !== "") {
            obj.name = message.name;
        }
        return obj;
    },
    create(base) {
        return exports.Owner.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseOwner();
        message.module = object.module ?? "";
        message.name = object.name ?? "";
        return message;
    }
};
exports.CapabilityOwners = {
    $type: "cosmos.capability.v1beta1.CapabilityOwners",
    encode(message, writer = new wire_1.BinaryWriter()) {
        for (const v of message.owners) {
            exports.Owner.encode(v, writer.uint32(10).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseCapabilityOwners();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.owners.push(exports.Owner.decode(reader, reader.uint32()));
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
        return { owners: globalThis.Array.isArray(object?.owners) ? object.owners.map((e) => exports.Owner.fromJSON(e)) : [] };
    },
    toJSON(message) {
        const obj = {};
        if (message.owners?.length) {
            obj.owners = message.owners.map((e) => exports.Owner.toJSON(e));
        }
        return obj;
    },
    create(base) {
        return exports.CapabilityOwners.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseCapabilityOwners();
        message.owners = object.owners?.map((e) => exports.Owner.fromPartial(e)) || [];
        return message;
    }
};
function createBaseCapability() {
    return { index: 0 };
}
function createBaseOwner() {
    return { module: "", name: "" };
}
function createBaseCapabilityOwners() {
    return { owners: [] };
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
    ["/cosmos.capability.v1beta1.Capability", exports.Capability],
    ["/cosmos.capability.v1beta1.Owner", exports.Owner],
    ["/cosmos.capability.v1beta1.CapabilityOwners", exports.CapabilityOwners]
];
exports.aminoConverters = {
    "/cosmos.capability.v1beta1.Capability": {
        aminoType: "cosmos-sdk/Capability",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.capability.v1beta1.Owner": {
        aminoType: "cosmos-sdk/Owner",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.capability.v1beta1.CapabilityOwners": {
        aminoType: "cosmos-sdk/CapabilityOwners",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    }
};
