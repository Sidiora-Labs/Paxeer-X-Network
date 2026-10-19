"use strict";
Object.defineProperty(exports, "__esModule", { value: true });
exports.aminoConverters = exports.registry = exports.Consensus = exports.App = void 0;
const wire_1 = require("@bufbuild/protobuf/wire");
exports.App = {
    $type: "tendermint.version.App",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.protocol !== 0) {
            writer.uint32(8).uint64(message.protocol);
        }
        if (message.software !== "") {
            writer.uint32(18).string(message.software);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseApp();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 8) {
                        break;
                    }
                    message.protocol = longToNumber(reader.uint64());
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.software = reader.string();
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
            protocol: isSet(object.protocol) ? globalThis.Number(object.protocol) : 0,
            software: isSet(object.software) ? globalThis.String(object.software) : ""
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.protocol !== 0) {
            obj.protocol = Math.round(message.protocol);
        }
        if (message.software !== "") {
            obj.software = message.software;
        }
        return obj;
    },
    create(base) {
        return exports.App.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseApp();
        message.protocol = object.protocol ?? 0;
        message.software = object.software ?? "";
        return message;
    }
};
exports.Consensus = {
    $type: "tendermint.version.Consensus",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.block !== 0) {
            writer.uint32(8).uint64(message.block);
        }
        if (message.app !== 0) {
            writer.uint32(16).uint64(message.app);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseConsensus();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 8) {
                        break;
                    }
                    message.block = longToNumber(reader.uint64());
                    continue;
                case 2:
                    if (tag !== 16) {
                        break;
                    }
                    message.app = longToNumber(reader.uint64());
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
            block: isSet(object.block) ? globalThis.Number(object.block) : 0,
            app: isSet(object.app) ? globalThis.Number(object.app) : 0
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.block !== 0) {
            obj.block = Math.round(message.block);
        }
        if (message.app !== 0) {
            obj.app = Math.round(message.app);
        }
        return obj;
    },
    create(base) {
        return exports.Consensus.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseConsensus();
        message.block = object.block ?? 0;
        message.app = object.app ?? 0;
        return message;
    }
};
function createBaseApp() {
    return { protocol: 0, software: "" };
}
function createBaseConsensus() {
    return { block: 0, app: 0 };
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
    ["/tendermint.version.App", exports.App],
    ["/tendermint.version.Consensus", exports.Consensus]
];
exports.aminoConverters = {
    "/tendermint.version.App": {
        aminoType: "tendermint.version.App",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/tendermint.version.Consensus": {
        aminoType: "tendermint.version.Consensus",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    }
};
