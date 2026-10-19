"use strict";
Object.defineProperty(exports, "__esModule", { value: true });
exports.aminoConverters = exports.registry = exports.MsgSubmitEvidenceResponse = exports.MsgSubmitEvidence = void 0;
const wire_1 = require("@bufbuild/protobuf/wire");
const any_1 = require("../../../google/protobuf/any");
exports.MsgSubmitEvidence = {
    $type: "cosmos.evidence.v1beta1.MsgSubmitEvidence",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.submitter !== "") {
            writer.uint32(10).string(message.submitter);
        }
        if (message.evidence !== undefined) {
            any_1.Any.encode(message.evidence, writer.uint32(18).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseMsgSubmitEvidence();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.submitter = reader.string();
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.evidence = any_1.Any.decode(reader, reader.uint32());
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
            submitter: isSet(object.submitter) ? globalThis.String(object.submitter) : "",
            evidence: isSet(object.evidence) ? any_1.Any.fromJSON(object.evidence) : undefined
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.submitter !== "") {
            obj.submitter = message.submitter;
        }
        if (message.evidence !== undefined) {
            obj.evidence = any_1.Any.toJSON(message.evidence);
        }
        return obj;
    },
    create(base) {
        return exports.MsgSubmitEvidence.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseMsgSubmitEvidence();
        message.submitter = object.submitter ?? "";
        message.evidence = object.evidence !== undefined && object.evidence !== null ? any_1.Any.fromPartial(object.evidence) : undefined;
        return message;
    }
};
exports.MsgSubmitEvidenceResponse = {
    $type: "cosmos.evidence.v1beta1.MsgSubmitEvidenceResponse",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.hash.length !== 0) {
            writer.uint32(34).bytes(message.hash);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseMsgSubmitEvidenceResponse();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 4:
                    if (tag !== 34) {
                        break;
                    }
                    message.hash = reader.bytes();
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
        return { hash: isSet(object.hash) ? bytesFromBase64(object.hash) : new Uint8Array(0) };
    },
    toJSON(message) {
        const obj = {};
        if (message.hash.length !== 0) {
            obj.hash = base64FromBytes(message.hash);
        }
        return obj;
    },
    create(base) {
        return exports.MsgSubmitEvidenceResponse.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseMsgSubmitEvidenceResponse();
        message.hash = object.hash ?? new Uint8Array(0);
        return message;
    }
};
function createBaseMsgSubmitEvidence() {
    return { submitter: "", evidence: undefined };
}
function createBaseMsgSubmitEvidenceResponse() {
    return { hash: new Uint8Array(0) };
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
function isSet(value) {
    return value !== null && value !== undefined;
}
exports.registry = [
    ["/cosmos.evidence.v1beta1.MsgSubmitEvidence", exports.MsgSubmitEvidence],
    ["/cosmos.evidence.v1beta1.MsgSubmitEvidenceResponse", exports.MsgSubmitEvidenceResponse]
];
exports.aminoConverters = {
    "/cosmos.evidence.v1beta1.MsgSubmitEvidence": {
        aminoType: "cosmos-sdk/MsgSubmitEvidence",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.evidence.v1beta1.MsgSubmitEvidenceResponse": {
        aminoType: "cosmos-sdk/MsgSubmitEvidenceResponse",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    }
};
