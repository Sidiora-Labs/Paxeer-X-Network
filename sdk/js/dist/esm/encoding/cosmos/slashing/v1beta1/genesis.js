import { BinaryReader, BinaryWriter } from "@bufbuild/protobuf/wire";
import { Params, ValidatorMissedBlockArray, ValidatorMissedBlockArrayLegacyMissedHeights, ValidatorSigningInfo, ValidatorSigningInfoLegacyMissedHeights } from "./slashing";
export const GenesisState = {
    $type: "cosmos.slashing.v1beta1.GenesisState",
    encode(message, writer = new BinaryWriter()) {
        if (message.params !== undefined) {
            Params.encode(message.params, writer.uint32(10).fork()).join();
        }
        for (const v of message.signing_infos) {
            SigningInfo.encode(v, writer.uint32(18).fork()).join();
        }
        for (const v of message.missed_blocks) {
            ValidatorMissedBlockArray.encode(v, writer.uint32(26).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseGenesisState();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.params = Params.decode(reader, reader.uint32());
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.signing_infos.push(SigningInfo.decode(reader, reader.uint32()));
                    continue;
                case 3:
                    if (tag !== 26) {
                        break;
                    }
                    message.missed_blocks.push(ValidatorMissedBlockArray.decode(reader, reader.uint32()));
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
            params: isSet(object.params) ? Params.fromJSON(object.params) : undefined,
            signing_infos: globalThis.Array.isArray(object?.signing_infos) ? object.signing_infos.map((e) => SigningInfo.fromJSON(e)) : [],
            missed_blocks: globalThis.Array.isArray(object?.missed_blocks) ? object.missed_blocks.map((e) => ValidatorMissedBlockArray.fromJSON(e)) : []
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.params !== undefined) {
            obj.params = Params.toJSON(message.params);
        }
        if (message.signing_infos?.length) {
            obj.signing_infos = message.signing_infos.map((e) => SigningInfo.toJSON(e));
        }
        if (message.missed_blocks?.length) {
            obj.missed_blocks = message.missed_blocks.map((e) => ValidatorMissedBlockArray.toJSON(e));
        }
        return obj;
    },
    create(base) {
        return GenesisState.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseGenesisState();
        message.params = object.params !== undefined && object.params !== null ? Params.fromPartial(object.params) : undefined;
        message.signing_infos = object.signing_infos?.map((e) => SigningInfo.fromPartial(e)) || [];
        message.missed_blocks = object.missed_blocks?.map((e) => ValidatorMissedBlockArray.fromPartial(e)) || [];
        return message;
    }
};
export const GenesisStateLegacyMissingHeights = {
    $type: "cosmos.slashing.v1beta1.GenesisStateLegacyMissingHeights",
    encode(message, writer = new BinaryWriter()) {
        if (message.params !== undefined) {
            Params.encode(message.params, writer.uint32(10).fork()).join();
        }
        for (const v of message.signing_infos) {
            SigningInfo.encode(v, writer.uint32(18).fork()).join();
        }
        for (const v of message.missed_blocks) {
            ValidatorMissedBlockArrayLegacyMissedHeights.encode(v, writer.uint32(26).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseGenesisStateLegacyMissingHeights();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.params = Params.decode(reader, reader.uint32());
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.signing_infos.push(SigningInfo.decode(reader, reader.uint32()));
                    continue;
                case 3:
                    if (tag !== 26) {
                        break;
                    }
                    message.missed_blocks.push(ValidatorMissedBlockArrayLegacyMissedHeights.decode(reader, reader.uint32()));
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
            params: isSet(object.params) ? Params.fromJSON(object.params) : undefined,
            signing_infos: globalThis.Array.isArray(object?.signing_infos) ? object.signing_infos.map((e) => SigningInfo.fromJSON(e)) : [],
            missed_blocks: globalThis.Array.isArray(object?.missed_blocks)
                ? object.missed_blocks.map((e) => ValidatorMissedBlockArrayLegacyMissedHeights.fromJSON(e))
                : []
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.params !== undefined) {
            obj.params = Params.toJSON(message.params);
        }
        if (message.signing_infos?.length) {
            obj.signing_infos = message.signing_infos.map((e) => SigningInfo.toJSON(e));
        }
        if (message.missed_blocks?.length) {
            obj.missed_blocks = message.missed_blocks.map((e) => ValidatorMissedBlockArrayLegacyMissedHeights.toJSON(e));
        }
        return obj;
    },
    create(base) {
        return GenesisStateLegacyMissingHeights.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseGenesisStateLegacyMissingHeights();
        message.params = object.params !== undefined && object.params !== null ? Params.fromPartial(object.params) : undefined;
        message.signing_infos = object.signing_infos?.map((e) => SigningInfo.fromPartial(e)) || [];
        message.missed_blocks = object.missed_blocks?.map((e) => ValidatorMissedBlockArrayLegacyMissedHeights.fromPartial(e)) || [];
        return message;
    }
};
export const GenesisStateLegacyV43 = {
    $type: "cosmos.slashing.v1beta1.GenesisStateLegacyV43",
    encode(message, writer = new BinaryWriter()) {
        if (message.params !== undefined) {
            Params.encode(message.params, writer.uint32(10).fork()).join();
        }
        for (const v of message.signing_infos) {
            SigningInfo.encode(v, writer.uint32(18).fork()).join();
        }
        for (const v of message.missed_blocks) {
            ValidatorMissedBlocks.encode(v, writer.uint32(26).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseGenesisStateLegacyV43();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.params = Params.decode(reader, reader.uint32());
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.signing_infos.push(SigningInfo.decode(reader, reader.uint32()));
                    continue;
                case 3:
                    if (tag !== 26) {
                        break;
                    }
                    message.missed_blocks.push(ValidatorMissedBlocks.decode(reader, reader.uint32()));
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
            params: isSet(object.params) ? Params.fromJSON(object.params) : undefined,
            signing_infos: globalThis.Array.isArray(object?.signing_infos) ? object.signing_infos.map((e) => SigningInfo.fromJSON(e)) : [],
            missed_blocks: globalThis.Array.isArray(object?.missed_blocks) ? object.missed_blocks.map((e) => ValidatorMissedBlocks.fromJSON(e)) : []
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.params !== undefined) {
            obj.params = Params.toJSON(message.params);
        }
        if (message.signing_infos?.length) {
            obj.signing_infos = message.signing_infos.map((e) => SigningInfo.toJSON(e));
        }
        if (message.missed_blocks?.length) {
            obj.missed_blocks = message.missed_blocks.map((e) => ValidatorMissedBlocks.toJSON(e));
        }
        return obj;
    },
    create(base) {
        return GenesisStateLegacyV43.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseGenesisStateLegacyV43();
        message.params = object.params !== undefined && object.params !== null ? Params.fromPartial(object.params) : undefined;
        message.signing_infos = object.signing_infos?.map((e) => SigningInfo.fromPartial(e)) || [];
        message.missed_blocks = object.missed_blocks?.map((e) => ValidatorMissedBlocks.fromPartial(e)) || [];
        return message;
    }
};
export const SigningInfo = {
    $type: "cosmos.slashing.v1beta1.SigningInfo",
    encode(message, writer = new BinaryWriter()) {
        if (message.address !== "") {
            writer.uint32(10).string(message.address);
        }
        if (message.validator_signing_info !== undefined) {
            ValidatorSigningInfo.encode(message.validator_signing_info, writer.uint32(18).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
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
                    message.validator_signing_info = ValidatorSigningInfo.decode(reader, reader.uint32());
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
            validator_signing_info: isSet(object.validator_signing_info) ? ValidatorSigningInfo.fromJSON(object.validator_signing_info) : undefined
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.address !== "") {
            obj.address = message.address;
        }
        if (message.validator_signing_info !== undefined) {
            obj.validator_signing_info = ValidatorSigningInfo.toJSON(message.validator_signing_info);
        }
        return obj;
    },
    create(base) {
        return SigningInfo.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseSigningInfo();
        message.address = object.address ?? "";
        message.validator_signing_info =
            object.validator_signing_info !== undefined && object.validator_signing_info !== null
                ? ValidatorSigningInfo.fromPartial(object.validator_signing_info)
                : undefined;
        return message;
    }
};
export const SigningInfoLegacyMissedHeights = {
    $type: "cosmos.slashing.v1beta1.SigningInfoLegacyMissedHeights",
    encode(message, writer = new BinaryWriter()) {
        if (message.address !== "") {
            writer.uint32(10).string(message.address);
        }
        if (message.validator_signing_info !== undefined) {
            ValidatorSigningInfoLegacyMissedHeights.encode(message.validator_signing_info, writer.uint32(18).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
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
                    message.validator_signing_info = ValidatorSigningInfoLegacyMissedHeights.decode(reader, reader.uint32());
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
            validator_signing_info: isSet(object.validator_signing_info) ? ValidatorSigningInfoLegacyMissedHeights.fromJSON(object.validator_signing_info) : undefined
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.address !== "") {
            obj.address = message.address;
        }
        if (message.validator_signing_info !== undefined) {
            obj.validator_signing_info = ValidatorSigningInfoLegacyMissedHeights.toJSON(message.validator_signing_info);
        }
        return obj;
    },
    create(base) {
        return SigningInfoLegacyMissedHeights.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseSigningInfoLegacyMissedHeights();
        message.address = object.address ?? "";
        message.validator_signing_info =
            object.validator_signing_info !== undefined && object.validator_signing_info !== null
                ? ValidatorSigningInfoLegacyMissedHeights.fromPartial(object.validator_signing_info)
                : undefined;
        return message;
    }
};
export const ValidatorMissedBlocks = {
    $type: "cosmos.slashing.v1beta1.ValidatorMissedBlocks",
    encode(message, writer = new BinaryWriter()) {
        if (message.address !== "") {
            writer.uint32(10).string(message.address);
        }
        for (const v of message.missed_blocks) {
            MissedBlock.encode(v, writer.uint32(18).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
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
                    message.missed_blocks.push(MissedBlock.decode(reader, reader.uint32()));
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
            missed_blocks: globalThis.Array.isArray(object?.missed_blocks) ? object.missed_blocks.map((e) => MissedBlock.fromJSON(e)) : []
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.address !== "") {
            obj.address = message.address;
        }
        if (message.missed_blocks?.length) {
            obj.missed_blocks = message.missed_blocks.map((e) => MissedBlock.toJSON(e));
        }
        return obj;
    },
    create(base) {
        return ValidatorMissedBlocks.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseValidatorMissedBlocks();
        message.address = object.address ?? "";
        message.missed_blocks = object.missed_blocks?.map((e) => MissedBlock.fromPartial(e)) || [];
        return message;
    }
};
export const MissedBlock = {
    $type: "cosmos.slashing.v1beta1.MissedBlock",
    encode(message, writer = new BinaryWriter()) {
        if (message.index !== 0) {
            writer.uint32(8).int64(message.index);
        }
        if (message.missed !== false) {
            writer.uint32(16).bool(message.missed);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
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
        return MissedBlock.fromPartial(base ?? {});
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
export const registry = [
    ["/cosmos.slashing.v1beta1.GenesisState", GenesisState],
    ["/cosmos.slashing.v1beta1.GenesisStateLegacyMissingHeights", GenesisStateLegacyMissingHeights],
    ["/cosmos.slashing.v1beta1.GenesisStateLegacyV43", GenesisStateLegacyV43],
    ["/cosmos.slashing.v1beta1.SigningInfo", SigningInfo],
    ["/cosmos.slashing.v1beta1.SigningInfoLegacyMissedHeights", SigningInfoLegacyMissedHeights],
    ["/cosmos.slashing.v1beta1.ValidatorMissedBlocks", ValidatorMissedBlocks],
    ["/cosmos.slashing.v1beta1.MissedBlock", MissedBlock]
];
export const aminoConverters = {
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
