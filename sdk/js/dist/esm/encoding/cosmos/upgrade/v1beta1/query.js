import { BinaryReader, BinaryWriter } from "@bufbuild/protobuf/wire";
import { ModuleVersion, Plan } from "./upgrade";
export const QueryCurrentPlanRequest = {
    $type: "cosmos.upgrade.v1beta1.QueryCurrentPlanRequest",
    encode(_, writer = new BinaryWriter()) {
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseQueryCurrentPlanRequest();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
            }
            if ((tag & 7) === 4 || tag === 0) {
                break;
            }
            reader.skip(tag & 7);
        }
        return message;
    },
    fromJSON(_) {
        return {};
    },
    toJSON(_) {
        const obj = {};
        return obj;
    },
    create(base) {
        return QueryCurrentPlanRequest.fromPartial(base ?? {});
    },
    fromPartial(_) {
        const message = createBaseQueryCurrentPlanRequest();
        return message;
    }
};
export const QueryCurrentPlanResponse = {
    $type: "cosmos.upgrade.v1beta1.QueryCurrentPlanResponse",
    encode(message, writer = new BinaryWriter()) {
        if (message.plan !== undefined) {
            Plan.encode(message.plan, writer.uint32(10).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseQueryCurrentPlanResponse();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.plan = Plan.decode(reader, reader.uint32());
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
        return { plan: isSet(object.plan) ? Plan.fromJSON(object.plan) : undefined };
    },
    toJSON(message) {
        const obj = {};
        if (message.plan !== undefined) {
            obj.plan = Plan.toJSON(message.plan);
        }
        return obj;
    },
    create(base) {
        return QueryCurrentPlanResponse.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryCurrentPlanResponse();
        message.plan = object.plan !== undefined && object.plan !== null ? Plan.fromPartial(object.plan) : undefined;
        return message;
    }
};
export const QueryAppliedPlanRequest = {
    $type: "cosmos.upgrade.v1beta1.QueryAppliedPlanRequest",
    encode(message, writer = new BinaryWriter()) {
        if (message.name !== "") {
            writer.uint32(10).string(message.name);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseQueryAppliedPlanRequest();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
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
        return { name: isSet(object.name) ? globalThis.String(object.name) : "" };
    },
    toJSON(message) {
        const obj = {};
        if (message.name !== "") {
            obj.name = message.name;
        }
        return obj;
    },
    create(base) {
        return QueryAppliedPlanRequest.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryAppliedPlanRequest();
        message.name = object.name ?? "";
        return message;
    }
};
export const QueryAppliedPlanResponse = {
    $type: "cosmos.upgrade.v1beta1.QueryAppliedPlanResponse",
    encode(message, writer = new BinaryWriter()) {
        if (message.height !== 0) {
            writer.uint32(8).int64(message.height);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseQueryAppliedPlanResponse();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 8) {
                        break;
                    }
                    message.height = longToNumber(reader.int64());
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
        return { height: isSet(object.height) ? globalThis.Number(object.height) : 0 };
    },
    toJSON(message) {
        const obj = {};
        if (message.height !== 0) {
            obj.height = Math.round(message.height);
        }
        return obj;
    },
    create(base) {
        return QueryAppliedPlanResponse.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryAppliedPlanResponse();
        message.height = object.height ?? 0;
        return message;
    }
};
export const QueryUpgradedConsensusStateRequest = {
    $type: "cosmos.upgrade.v1beta1.QueryUpgradedConsensusStateRequest",
    encode(message, writer = new BinaryWriter()) {
        if (message.last_height !== 0) {
            writer.uint32(8).int64(message.last_height);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseQueryUpgradedConsensusStateRequest();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 8) {
                        break;
                    }
                    message.last_height = longToNumber(reader.int64());
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
        return { last_height: isSet(object.last_height) ? globalThis.Number(object.last_height) : 0 };
    },
    toJSON(message) {
        const obj = {};
        if (message.last_height !== 0) {
            obj.last_height = Math.round(message.last_height);
        }
        return obj;
    },
    create(base) {
        return QueryUpgradedConsensusStateRequest.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryUpgradedConsensusStateRequest();
        message.last_height = object.last_height ?? 0;
        return message;
    }
};
export const QueryUpgradedConsensusStateResponse = {
    $type: "cosmos.upgrade.v1beta1.QueryUpgradedConsensusStateResponse",
    encode(message, writer = new BinaryWriter()) {
        if (message.upgraded_consensus_state.length !== 0) {
            writer.uint32(18).bytes(message.upgraded_consensus_state);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseQueryUpgradedConsensusStateResponse();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.upgraded_consensus_state = reader.bytes();
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
            upgraded_consensus_state: isSet(object.upgraded_consensus_state) ? bytesFromBase64(object.upgraded_consensus_state) : new Uint8Array(0)
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.upgraded_consensus_state.length !== 0) {
            obj.upgraded_consensus_state = base64FromBytes(message.upgraded_consensus_state);
        }
        return obj;
    },
    create(base) {
        return QueryUpgradedConsensusStateResponse.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryUpgradedConsensusStateResponse();
        message.upgraded_consensus_state = object.upgraded_consensus_state ?? new Uint8Array(0);
        return message;
    }
};
export const QueryModuleVersionsRequest = {
    $type: "cosmos.upgrade.v1beta1.QueryModuleVersionsRequest",
    encode(message, writer = new BinaryWriter()) {
        if (message.module_name !== "") {
            writer.uint32(10).string(message.module_name);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseQueryModuleVersionsRequest();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.module_name = reader.string();
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
        return { module_name: isSet(object.module_name) ? globalThis.String(object.module_name) : "" };
    },
    toJSON(message) {
        const obj = {};
        if (message.module_name !== "") {
            obj.module_name = message.module_name;
        }
        return obj;
    },
    create(base) {
        return QueryModuleVersionsRequest.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryModuleVersionsRequest();
        message.module_name = object.module_name ?? "";
        return message;
    }
};
export const QueryModuleVersionsResponse = {
    $type: "cosmos.upgrade.v1beta1.QueryModuleVersionsResponse",
    encode(message, writer = new BinaryWriter()) {
        for (const v of message.module_versions) {
            ModuleVersion.encode(v, writer.uint32(10).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseQueryModuleVersionsResponse();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.module_versions.push(ModuleVersion.decode(reader, reader.uint32()));
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
            module_versions: globalThis.Array.isArray(object?.module_versions) ? object.module_versions.map((e) => ModuleVersion.fromJSON(e)) : []
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.module_versions?.length) {
            obj.module_versions = message.module_versions.map((e) => ModuleVersion.toJSON(e));
        }
        return obj;
    },
    create(base) {
        return QueryModuleVersionsResponse.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryModuleVersionsResponse();
        message.module_versions = object.module_versions?.map((e) => ModuleVersion.fromPartial(e)) || [];
        return message;
    }
};
function createBaseQueryCurrentPlanRequest() {
    return {};
}
function createBaseQueryCurrentPlanResponse() {
    return { plan: undefined };
}
function createBaseQueryAppliedPlanRequest() {
    return { name: "" };
}
function createBaseQueryAppliedPlanResponse() {
    return { height: 0 };
}
function createBaseQueryUpgradedConsensusStateRequest() {
    return { last_height: 0 };
}
function createBaseQueryUpgradedConsensusStateResponse() {
    return { upgraded_consensus_state: new Uint8Array(0) };
}
function createBaseQueryModuleVersionsRequest() {
    return { module_name: "" };
}
function createBaseQueryModuleVersionsResponse() {
    return { module_versions: [] };
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
export const registry = [
    ["/cosmos.upgrade.v1beta1.QueryCurrentPlanRequest", QueryCurrentPlanRequest],
    ["/cosmos.upgrade.v1beta1.QueryCurrentPlanResponse", QueryCurrentPlanResponse],
    ["/cosmos.upgrade.v1beta1.QueryAppliedPlanRequest", QueryAppliedPlanRequest],
    ["/cosmos.upgrade.v1beta1.QueryAppliedPlanResponse", QueryAppliedPlanResponse],
    ["/cosmos.upgrade.v1beta1.QueryUpgradedConsensusStateRequest", QueryUpgradedConsensusStateRequest],
    ["/cosmos.upgrade.v1beta1.QueryUpgradedConsensusStateResponse", QueryUpgradedConsensusStateResponse],
    ["/cosmos.upgrade.v1beta1.QueryModuleVersionsRequest", QueryModuleVersionsRequest],
    ["/cosmos.upgrade.v1beta1.QueryModuleVersionsResponse", QueryModuleVersionsResponse]
];
export const aminoConverters = {
    "/cosmos.upgrade.v1beta1.QueryCurrentPlanRequest": {
        aminoType: "cosmos-sdk/QueryCurrentPlanRequest",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.upgrade.v1beta1.QueryCurrentPlanResponse": {
        aminoType: "cosmos-sdk/QueryCurrentPlanResponse",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.upgrade.v1beta1.QueryAppliedPlanRequest": {
        aminoType: "cosmos-sdk/QueryAppliedPlanRequest",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.upgrade.v1beta1.QueryAppliedPlanResponse": {
        aminoType: "cosmos-sdk/QueryAppliedPlanResponse",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.upgrade.v1beta1.QueryUpgradedConsensusStateRequest": {
        aminoType: "cosmos-sdk/QueryUpgradedConsensusStateRequest",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.upgrade.v1beta1.QueryUpgradedConsensusStateResponse": {
        aminoType: "cosmos-sdk/QueryUpgradedConsensusStateResponse",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.upgrade.v1beta1.QueryModuleVersionsRequest": {
        aminoType: "cosmos-sdk/QueryModuleVersionsRequest",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.upgrade.v1beta1.QueryModuleVersionsResponse": {
        aminoType: "cosmos-sdk/QueryModuleVersionsResponse",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    }
};
