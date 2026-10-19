"use strict";
Object.defineProperty(exports, "__esModule", { value: true });
exports.aminoConverters = exports.registry = exports.ModuleVersion = exports.CancelSoftwareUpgradeProposal = exports.SoftwareUpgradeProposal = exports.Plan = void 0;
const wire_1 = require("@bufbuild/protobuf/wire");
const any_1 = require("../../../google/protobuf/any");
const timestamp_1 = require("../../../google/protobuf/timestamp");
exports.Plan = {
    $type: "cosmos.upgrade.v1beta1.Plan",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.name !== "") {
            writer.uint32(10).string(message.name);
        }
        if (message.time !== undefined) {
            timestamp_1.Timestamp.encode(toTimestamp(message.time), writer.uint32(18).fork()).join();
        }
        if (message.height !== 0) {
            writer.uint32(24).int64(message.height);
        }
        if (message.info !== "") {
            writer.uint32(34).string(message.info);
        }
        if (message.upgraded_client_state !== undefined) {
            any_1.Any.encode(message.upgraded_client_state, writer.uint32(42).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBasePlan();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.name = reader.string();
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.time = fromTimestamp(timestamp_1.Timestamp.decode(reader, reader.uint32()));
                    continue;
                case 3:
                    if (tag !== 24) {
                        break;
                    }
                    message.height = longToNumber(reader.int64());
                    continue;
                case 4:
                    if (tag !== 34) {
                        break;
                    }
                    message.info = reader.string();
                    continue;
                case 5:
                    if (tag !== 42) {
                        break;
                    }
                    message.upgraded_client_state = any_1.Any.decode(reader, reader.uint32());
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
            name: isSet(object.name) ? globalThis.String(object.name) : "",
            time: isSet(object.time) ? fromJsonTimestamp(object.time) : undefined,
            height: isSet(object.height) ? globalThis.Number(object.height) : 0,
            info: isSet(object.info) ? globalThis.String(object.info) : "",
            upgraded_client_state: isSet(object.upgraded_client_state) ? any_1.Any.fromJSON(object.upgraded_client_state) : undefined
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.name !== "") {
            obj.name = message.name;
        }
        if (message.time !== undefined) {
            obj.time = message.time.toISOString();
        }
        if (message.height !== 0) {
            obj.height = Math.round(message.height);
        }
        if (message.info !== "") {
            obj.info = message.info;
        }
        if (message.upgraded_client_state !== undefined) {
            obj.upgraded_client_state = any_1.Any.toJSON(message.upgraded_client_state);
        }
        return obj;
    },
    create(base) {
        return exports.Plan.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBasePlan();
        message.name = object.name ?? "";
        message.time = object.time ?? undefined;
        message.height = object.height ?? 0;
        message.info = object.info ?? "";
        message.upgraded_client_state =
            object.upgraded_client_state !== undefined && object.upgraded_client_state !== null ? any_1.Any.fromPartial(object.upgraded_client_state) : undefined;
        return message;
    }
};
exports.SoftwareUpgradeProposal = {
    $type: "cosmos.upgrade.v1beta1.SoftwareUpgradeProposal",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.title !== "") {
            writer.uint32(10).string(message.title);
        }
        if (message.description !== "") {
            writer.uint32(18).string(message.description);
        }
        if (message.plan !== undefined) {
            exports.Plan.encode(message.plan, writer.uint32(26).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseSoftwareUpgradeProposal();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.title = reader.string();
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.description = reader.string();
                    continue;
                case 3:
                    if (tag !== 26) {
                        break;
                    }
                    message.plan = exports.Plan.decode(reader, reader.uint32());
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
            title: isSet(object.title) ? globalThis.String(object.title) : "",
            description: isSet(object.description) ? globalThis.String(object.description) : "",
            plan: isSet(object.plan) ? exports.Plan.fromJSON(object.plan) : undefined
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.title !== "") {
            obj.title = message.title;
        }
        if (message.description !== "") {
            obj.description = message.description;
        }
        if (message.plan !== undefined) {
            obj.plan = exports.Plan.toJSON(message.plan);
        }
        return obj;
    },
    create(base) {
        return exports.SoftwareUpgradeProposal.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseSoftwareUpgradeProposal();
        message.title = object.title ?? "";
        message.description = object.description ?? "";
        message.plan = object.plan !== undefined && object.plan !== null ? exports.Plan.fromPartial(object.plan) : undefined;
        return message;
    }
};
exports.CancelSoftwareUpgradeProposal = {
    $type: "cosmos.upgrade.v1beta1.CancelSoftwareUpgradeProposal",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.title !== "") {
            writer.uint32(10).string(message.title);
        }
        if (message.description !== "") {
            writer.uint32(18).string(message.description);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseCancelSoftwareUpgradeProposal();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.title = reader.string();
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.description = reader.string();
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
            title: isSet(object.title) ? globalThis.String(object.title) : "",
            description: isSet(object.description) ? globalThis.String(object.description) : ""
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.title !== "") {
            obj.title = message.title;
        }
        if (message.description !== "") {
            obj.description = message.description;
        }
        return obj;
    },
    create(base) {
        return exports.CancelSoftwareUpgradeProposal.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseCancelSoftwareUpgradeProposal();
        message.title = object.title ?? "";
        message.description = object.description ?? "";
        return message;
    }
};
exports.ModuleVersion = {
    $type: "cosmos.upgrade.v1beta1.ModuleVersion",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.name !== "") {
            writer.uint32(10).string(message.name);
        }
        if (message.version !== 0) {
            writer.uint32(16).uint64(message.version);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseModuleVersion();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.name = reader.string();
                    continue;
                case 2:
                    if (tag !== 16) {
                        break;
                    }
                    message.version = longToNumber(reader.uint64());
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
            name: isSet(object.name) ? globalThis.String(object.name) : "",
            version: isSet(object.version) ? globalThis.Number(object.version) : 0
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.name !== "") {
            obj.name = message.name;
        }
        if (message.version !== 0) {
            obj.version = Math.round(message.version);
        }
        return obj;
    },
    create(base) {
        return exports.ModuleVersion.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseModuleVersion();
        message.name = object.name ?? "";
        message.version = object.version ?? 0;
        return message;
    }
};
function createBasePlan() {
    return { name: "", time: undefined, height: 0, info: "", upgraded_client_state: undefined };
}
function createBaseSoftwareUpgradeProposal() {
    return { title: "", description: "", plan: undefined };
}
function createBaseCancelSoftwareUpgradeProposal() {
    return { title: "", description: "" };
}
function createBaseModuleVersion() {
    return { name: "", version: 0 };
}
function toTimestamp(date) {
    const seconds = Math.trunc(date.getTime() / 1000);
    const nanos = (date.getTime() % 1000) * 1000000;
    return { seconds, nanos };
}
function fromTimestamp(t) {
    let millis = (t.seconds || 0) * 1000;
    millis += (t.nanos || 0) / 1000000;
    return new globalThis.Date(millis);
}
function fromJsonTimestamp(o) {
    if (o instanceof globalThis.Date) {
        return o;
    }
    else if (typeof o === "string") {
        return new globalThis.Date(o);
    }
    else {
        return fromTimestamp(timestamp_1.Timestamp.fromJSON(o));
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
    ["/cosmos.upgrade.v1beta1.Plan", exports.Plan],
    ["/cosmos.upgrade.v1beta1.SoftwareUpgradeProposal", exports.SoftwareUpgradeProposal],
    ["/cosmos.upgrade.v1beta1.CancelSoftwareUpgradeProposal", exports.CancelSoftwareUpgradeProposal],
    ["/cosmos.upgrade.v1beta1.ModuleVersion", exports.ModuleVersion]
];
exports.aminoConverters = {
    "/cosmos.upgrade.v1beta1.Plan": {
        aminoType: "cosmos-sdk/Plan",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.upgrade.v1beta1.SoftwareUpgradeProposal": {
        aminoType: "cosmos-sdk/SoftwareUpgradeProposal",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.upgrade.v1beta1.CancelSoftwareUpgradeProposal": {
        aminoType: "cosmos-sdk/CancelSoftwareUpgradeProposal",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.upgrade.v1beta1.ModuleVersion": {
        aminoType: "cosmos-sdk/ModuleVersion",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    }
};
