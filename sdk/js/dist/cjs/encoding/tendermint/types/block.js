"use strict";
Object.defineProperty(exports, "__esModule", { value: true });
exports.aminoConverters = exports.registry = exports.Block = void 0;
const wire_1 = require("@bufbuild/protobuf/wire");
const evidence_1 = require("./evidence");
const types_1 = require("./types");
exports.Block = {
    $type: "tendermint.types.Block",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.header !== undefined) {
            types_1.Header.encode(message.header, writer.uint32(10).fork()).join();
        }
        if (message.data !== undefined) {
            types_1.Data.encode(message.data, writer.uint32(18).fork()).join();
        }
        if (message.evidence !== undefined) {
            evidence_1.EvidenceList.encode(message.evidence, writer.uint32(26).fork()).join();
        }
        if (message.last_commit !== undefined) {
            types_1.Commit.encode(message.last_commit, writer.uint32(34).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseBlock();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.header = types_1.Header.decode(reader, reader.uint32());
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.data = types_1.Data.decode(reader, reader.uint32());
                    continue;
                case 3:
                    if (tag !== 26) {
                        break;
                    }
                    message.evidence = evidence_1.EvidenceList.decode(reader, reader.uint32());
                    continue;
                case 4:
                    if (tag !== 34) {
                        break;
                    }
                    message.last_commit = types_1.Commit.decode(reader, reader.uint32());
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
            header: isSet(object.header) ? types_1.Header.fromJSON(object.header) : undefined,
            data: isSet(object.data) ? types_1.Data.fromJSON(object.data) : undefined,
            evidence: isSet(object.evidence) ? evidence_1.EvidenceList.fromJSON(object.evidence) : undefined,
            last_commit: isSet(object.last_commit) ? types_1.Commit.fromJSON(object.last_commit) : undefined
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.header !== undefined) {
            obj.header = types_1.Header.toJSON(message.header);
        }
        if (message.data !== undefined) {
            obj.data = types_1.Data.toJSON(message.data);
        }
        if (message.evidence !== undefined) {
            obj.evidence = evidence_1.EvidenceList.toJSON(message.evidence);
        }
        if (message.last_commit !== undefined) {
            obj.last_commit = types_1.Commit.toJSON(message.last_commit);
        }
        return obj;
    },
    create(base) {
        return exports.Block.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseBlock();
        message.header = object.header !== undefined && object.header !== null ? types_1.Header.fromPartial(object.header) : undefined;
        message.data = object.data !== undefined && object.data !== null ? types_1.Data.fromPartial(object.data) : undefined;
        message.evidence = object.evidence !== undefined && object.evidence !== null ? evidence_1.EvidenceList.fromPartial(object.evidence) : undefined;
        message.last_commit = object.last_commit !== undefined && object.last_commit !== null ? types_1.Commit.fromPartial(object.last_commit) : undefined;
        return message;
    }
};
function createBaseBlock() {
    return { header: undefined, data: undefined, evidence: undefined, last_commit: undefined };
}
function isSet(value) {
    return value !== null && value !== undefined;
}
exports.registry = [["/tendermint.types.Block", exports.Block]];
exports.aminoConverters = {
    "/tendermint.types.Block": {
        aminoType: "tendermint.types.Block",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    }
};
