"use strict";
Object.defineProperty(exports, "__esModule", { value: true });
exports.aminoConverters = exports.registry = exports.MissedBlock = exports.ValidatorMissedBlocks = exports.SigningInfoLegacyMissedHeights = exports.SigningInfo = exports.GenesisStateLegacyV43 = exports.GenesisStateLegacyMissingHeights = exports.GenesisState = void 0;
const wire_1 = require("@bufbuild/protobuf/wire");
const slashing_1 = require("./slashing");
exports.GenesisState = {
    $type: "cosmos.slashing.v1beta1.GenesisState",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.params !== undefined) {
            slashing_1.Params.encode(message.params, writer.uint32(10).fork()).join();
        }
        for (const v of message.signing_infos) {
            exports.SigningInfo.encode(v, writer.uint32(18).fork()).join();
        }
        for (const v of message.missed_blocks) {
            slashing_1.ValidatorMissedBlockArray.encode(v, writer.uint32(26).fork()).join();
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
                    message.params = slashing_1.Params.decode(reader, reader.uint32());
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.signing_infos.push(exports.SigningInfo.decode(reader, reader.uint32()));
                    continue;
                case 3:
                    if (tag !== 26) {
                        break;
                    }
                    message.missed_blocks.push(slashing_1.ValidatorMissedBlockArray.decode(reader, reader.uint32()));
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
            params: isSet(object.params) ? slashing_1.Params.fromJSON(object.params) : undefined,
            signing_infos: globalThis.Array.isArray(object?.signing_infos) ? object.signing_infos.map((e) => exports.SigningInfo.fromJSON(e)) : [],
            missed_blocks: globalThis.Array.isArray(object?.missed_blocks) ? object.missed_blocks.map((e) => slashing_1.ValidatorMissedBlockArray.fromJSON(e)) : []
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.params !== undefined) {
            obj.params = slashing_1.Params.toJSON(message.params);
        }
        if (message.signing_infos?.length) {
            obj.signing_infos = message.signing_infos.map((e) => exports.SigningInfo.toJSON(e));
        }
        if (message.missed_blocks?.length) {
            obj.missed_blocks = message.missed_blocks.map((e) => slashing_1.ValidatorMissedBlockArray.toJSON(e));
        }
        return obj;
    },
    create(base) {
        return exports.GenesisState.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseGenesisState();
        message.params = object.params !== undefined && object.params !== null ? slashing_1.Params.fromPartial(object.params) : undefined;
        message.signing_infos = object.signing_infos?.map((e) => exports.SigningInfo.fromPartial(e)) || [];
        message.missed_blocks = object.missed_blocks?.map((e) => slashing_1.ValidatorMissedBlockArray.fromPartial(e)) || [];
        return message;
    }
};
exports.GenesisStateLegacyMissingHeights = {
    $type: "cosmos.slashing.v1beta1.GenesisStateLegacyMissingHeights",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.params !== undefined) {
            slashing_1.Params.encode(message.params, writer.uint32(10).fork()).join();
        }
        for (const v of message.signing_infos) {
            exports.SigningInfo.encode(v, writer.uint32(18).fork()).join();
        }
        for (const v of message.missed_blocks) {
            slashing_1.ValidatorMissedBlockArrayLegacyMissedHeights.encode(v, writer.uint32(26).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseGenesisStateLegacyMissingHeights();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.params = slashing_1.Params.decode(reader, reader.uint32());
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.signing_infos.push(exports.SigningInfo.decode(reader, reader.uint32()));
                    continue;
                case 3:
                    if (tag !== 26) {
                        break;
                    }
                    message.missed_blocks.push(slashing_1.ValidatorMissedBlockArrayLegacyMissedHeights.decode(reader, reader.uint32()));
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
            params: isSet(object.params) ? slashing_1.Params.fromJSON(object.params) : undefined,
            signing_infos: globalThis.Array.isArray(object?.signing_infos) ? object.signing_infos.map((e) => exports.SigningInfo.fromJSON(e)) : [],
            missed_blocks: globalThis.Array.isArray(object?.missed_blocks)
                ? object.missed_blocks.map((e) => slashing_1.ValidatorMissedBlockArrayLegacyMissedHeights.fromJSON(e))
                : []
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.params !== undefined) {
            obj.params = slashing_1.Params.toJSON(message.params);
        }
        if (message.signing_infos?.length) {
            obj.signing_infos = message.signing_infos.map((e) => exports.SigningInfo.toJSON(e));
        }
        if (message.missed_blocks?.length) {
            obj.missed_blocks = message.missed_blocks.map((e) => slashing_1.ValidatorMissedBlockArrayLegacyMissedHeights.toJSON(e));
        }
        return obj;
    },
    create(base) {
        return exports.GenesisStateLegacyMissingHeights.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseGenesisStateLegacyMissingHeights();
        message.params = object.params !== undefined && object.params !== null ? slashing_1.Params.fromPartial(object.params) : undefined;
        message.signing_infos = object.signing_infos?.map((e) => exports.SigningInfo.fromPartial(e)) || [];
        message.missed_blocks = object.missed_blocks?.map((e) => slashing_1.ValidatorMissedBlockArrayLegacyMissedHeights.fromPartial(e)) || [];
        return message;
    }
};
exports.GenesisStateLegacyV43 = {
    $type: "cosmos.slashing.v1beta1.GenesisStateLegacyV43",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.params !== undefined) {
            slashing_1.Params.encode(message.params, writer.uint32(10).fork()).join();
        }
        for (const v of message.signing_infos) {
            exports.SigningInfo.encode(v, writer.uint32(18).fork()).join();
        }
        for (const v of message.missed_blocks) {
            exports.ValidatorMissedBlocks.encode(v, writer.uint32(26).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseGenesisStateLegacyV43();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.params = slashing_1.Params.decode(reader, reader.uint32());
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.signing_infos.push(exports.SigningInfo.decode(reader, reader.uint32()));
                    continue;
                case 3:
                    if (tag !== 26) {
                        break;
                    }
                    message.missed_blocks.push(exports.ValidatorMissedBlocks.decode(reader, reader.uint32()));
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
            params: isSet(object.params) ? slashing_1.Params.fromJSON(object.params) : undefined,
            signing_infos: globalThis.Array.isArray(object?.signing_infos) ? object.signing_infos.map((e) => exports.SigningInfo.fromJSON(e)) : [],
            missed_blocks: globalThis.Array.isArray(object?.missed_blocks) ? object.missed_blocks.map((e) => exports.ValidatorMissedBlocks.fromJSON(e)) : []
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.params !== undefined) {
            obj.params = slashing_1.Params.toJSON(message.params);
        }
        if (message.signing_infos?.length) {
            obj.signing_infos = message.signing_infos.map((e) => exports.SigningInfo.toJSON(e));
        }
        if (message.missed_blocks?.length) {
            obj.missed_blocks = message.missed_blocks.map((e) => exports.ValidatorMissedBlocks.toJSON(e));
        }
        return obj;
    },
    create(base) {
        return exports.GenesisStateLegacyV43.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseGenesisStateLegacyV43();
        message.params = object.params !== undefined && object.params !== null ? slashing_1.Params.fromPartial(object.params) : undefined;
        message.signing_infos = object.signing_infos?.map((e) => exports.SigningInfo.fromPartial(e)) || [];
        message.missed_blocks = object.missed_blocks?.map((e) => exports.ValidatorMissedBlocks.fromPartial(e)) || [];
        return message;
    }
};
exports.SigningInfo = {
    $type: "cosmos.slashing.v1beta1.SigningInfo",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.address !== "") {
            writer.uint32(10).string(message.address);
        }
        if (message.validator_signing_info !== undefined) {
            slashing_1.ValidatorSigningInfo.encode(message.validator_signing_info, writer.uint32(18).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseSigningInfo();
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
                    message.validator_signing_info = slashing_1.ValidatorSigningInfo.decode(reader, reader.uint32());
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
            validator_signing_info: isSet(object.validator_signing_info) ? slashing_1.ValidatorSigningInfo.fromJSON(object.validator_signing_info) : undefined
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.address !== "") {
            obj.address = message.address;
        }
        if (message.validator_signing_info !== undefined) {
            obj.validator_signing_info = slashing_1.ValidatorSigningInfo.toJSON(message.validator_signing_info);
        }
        return obj;
    },
    create(base) {
        return exports.SigningInfo.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseSigningInfo();
        message.address = object.address ?? "";
        message.validator_signing_info =
            object.validator_signing_info !== undefined && object.validator_signing_info !== null
                ? slashing_1.ValidatorSigningInfo.fromPartial(object.validator_signing_info)
                : undefined;
        return message;
    }
};
exports.SigningInfoLegacyMissedHeights = {
    $type: "cosmos.slashing.v1beta1.SigningInfoLegacyMissedHeights",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.address !== "") {
            writer.uint32(10).string(message.address);
        }
        if (message.validator_signing_info !== undefined) {
            slashing_1.ValidatorSigningInfoLegacyMissedHeights.encode(message.validator_signing_info, writer.uint32(18).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseSigningInfoLegacyMissedHeights();
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
                    message.validator_signing_info = slashing_1.ValidatorSigningInfoLegacyMissedHeights.decode(reader, reader.uint32());
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
            validator_signing_info: isSet(object.validator_signing_info) ? slashing_1.ValidatorSigningInfoLegacyMissedHeights.fromJSON(object.validator_signing_info) : undefined
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.address !== "") {
            obj.address = message.address;
        }
        if (message.validator_signing_info !== undefined) {
            obj.validator_signing_info = slashing_1.ValidatorSigningInfoLegacyMissedHeights.toJSON(message.validator_signing_info);
        }
        return obj;
    },
    create(base) {
        return exports.SigningInfoLegacyMissedHeights.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseSigningInfoLegacyMissedHeights();
        message.address = object.address ?? "";
        message.validator_signing_info =
            object.validator_signing_info !== undefined && object.validator_signing_info !== null
                ? slashing_1.ValidatorSigningInfoLegacyMissedHeights.fromPartial(object.validator_signing_info)
                : undefined;
        return message;
    }
};
exports.ValidatorMissedBlocks = {
    $type: "cosmos.slashing.v1beta1.ValidatorMissedBlocks",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.address !== "") {
            writer.uint32(10).string(message.address);
        }
        for (const v of message.missed_blocks) {
            exports.MissedBlock.encode(v, writer.uint32(18).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseValidatorMissedBlocks();
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
                    message.missed_blocks.push(exports.MissedBlock.decode(reader, reader.uint32()));
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
            missed_blocks: globalThis.Array.isArray(object?.missed_blocks) ? object.missed_blocks.map((e) => exports.MissedBlock.fromJSON(e)) : []
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.address !== "") {
            obj.address = message.address;
        }
        if (message.missed_blocks?.length) {
            obj.missed_blocks = message.missed_blocks.map((e) => exports.MissedBlock.toJSON(e));
        }
        return obj;
    },
    create(base) {
        return exports.ValidatorMissedBlocks.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseValidatorMissedBlocks();
        message.address = object.address ?? "";
        message.missed_blocks = object.missed_blocks?.map((e) => exports.MissedBlock.fromPartial(e)) || [];
        return message;
    }
};
exports.MissedBlock = {
    $type: "cosmos.slashing.v1beta1.MissedBlock",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.index !== 0) {
            writer.uint32(8).int64(message.index);
        }
        if (message.missed !== false) {
            writer.uint32(16).bool(message.missed);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseMissedBlock();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 8) {
                        break;
                    }
                    message.index = longToNumber(reader.int64());
                    continue;
                case 2:
                    if (tag !== 16) {
                        break;
                    }
                    message.missed = reader.bool();
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
            missed: isSet(object.missed) ? globalThis.Boolean(object.missed) : false
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.index !== 0) {
            obj.index = Math.round(message.index);
        }
        if (message.missed !== false) {
            obj.missed = message.missed;
        }
        return obj;
    },
    create(base) {
        return exports.MissedBlock.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseMissedBlock();
        message.index = object.index ?? 0;
        message.missed = object.missed ?? false;
        return message;
    }
};
function createBaseGenesisState() {
    return { params: undefined, signing_infos: [], missed_blocks: [] };
}
function createBaseGenesisStateLegacyMissingHeights() {
    return { params: undefined, signing_infos: [], missed_blocks: [] };
}
function createBaseGenesisStateLegacyV43() {
    return { params: undefined, signing_infos: [], missed_blocks: [] };
}
function createBaseSigningInfo() {
    return { address: "", validator_signing_info: undefined };
}
function createBaseSigningInfoLegacyMissedHeights() {
    return { address: "", validator_signing_info: undefined };
}
function createBaseValidatorMissedBlocks() {
    return { address: "", missed_blocks: [] };
}
function createBaseMissedBlock() {
    return { index: 0, missed: false };
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
    ["/cosmos.slashing.v1beta1.GenesisState", exports.GenesisState],
    ["/cosmos.slashing.v1beta1.GenesisStateLegacyMissingHeights", exports.GenesisStateLegacyMissingHeights],
    ["/cosmos.slashing.v1beta1.GenesisStateLegacyV43", exports.GenesisStateLegacyV43],
    ["/cosmos.slashing.v1beta1.SigningInfo", exports.SigningInfo],
    ["/cosmos.slashing.v1beta1.SigningInfoLegacyMissedHeights", exports.SigningInfoLegacyMissedHeights],
    ["/cosmos.slashing.v1beta1.ValidatorMissedBlocks", exports.ValidatorMissedBlocks],
    ["/cosmos.slashing.v1beta1.MissedBlock", exports.MissedBlock]
];
exports.aminoConverters = {
    "/cosmos.slashing.v1beta1.GenesisState": {
        aminoType: "cosmos-sdk/GenesisState",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.slashing.v1beta1.GenesisStateLegacyMissingHeights": {
        aminoType: "cosmos-sdk/GenesisStateLegacyMissingHeights",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.slashing.v1beta1.GenesisStateLegacyV43": {
        aminoType: "cosmos-sdk/GenesisStateLegacyV43",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.slashing.v1beta1.SigningInfo": {
        aminoType: "cosmos-sdk/SigningInfo",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.slashing.v1beta1.SigningInfoLegacyMissedHeights": {
        aminoType: "cosmos-sdk/SigningInfoLegacyMissedHeights",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.slashing.v1beta1.ValidatorMissedBlocks": {
        aminoType: "cosmos-sdk/ValidatorMissedBlocks",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.slashing.v1beta1.MissedBlock": {
        aminoType: "cosmos-sdk/MissedBlock",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    }
};
