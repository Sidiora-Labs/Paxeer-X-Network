import { BinaryReader, BinaryWriter } from "@bufbuild/protobuf/wire";
import { Any } from "../../../google/protobuf/any";
import { PageRequest, PageResponse } from "../../base/query/v1beta1/pagination";
export const QueryEvidenceRequest = {
    $type: "cosmos.evidence.v1beta1.QueryEvidenceRequest",
    encode(message, writer = new BinaryWriter()) {
        if (message.evidence_hash.length !== 0) {
            writer.uint32(10).bytes(message.evidence_hash);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseQueryEvidenceRequest();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.evidence_hash = reader.bytes();
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
        return { evidence_hash: isSet(object.evidence_hash) ? bytesFromBase64(object.evidence_hash) : new Uint8Array(0) };
    },
    toJSON(message) {
        const obj = {};
        if (message.evidence_hash.length !== 0) {
            obj.evidence_hash = base64FromBytes(message.evidence_hash);
        }
        return obj;
    },
    create(base) {
        return QueryEvidenceRequest.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryEvidenceRequest();
        message.evidence_hash = object.evidence_hash ?? new Uint8Array(0);
        return message;
    }
};
export const QueryEvidenceResponse = {
    $type: "cosmos.evidence.v1beta1.QueryEvidenceResponse",
    encode(message, writer = new BinaryWriter()) {
        if (message.evidence !== undefined) {
            Any.encode(message.evidence, writer.uint32(10).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseQueryEvidenceResponse();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.evidence = Any.decode(reader, reader.uint32());
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
        return { evidence: isSet(object.evidence) ? Any.fromJSON(object.evidence) : undefined };
    },
    toJSON(message) {
        const obj = {};
        if (message.evidence !== undefined) {
            obj.evidence = Any.toJSON(message.evidence);
        }
        return obj;
    },
    create(base) {
        return QueryEvidenceResponse.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryEvidenceResponse();
        message.evidence = object.evidence !== undefined && object.evidence !== null ? Any.fromPartial(object.evidence) : undefined;
        return message;
    }
};
export const QueryAllEvidenceRequest = {
    $type: "cosmos.evidence.v1beta1.QueryAllEvidenceRequest",
    encode(message, writer = new BinaryWriter()) {
        if (message.pagination !== undefined) {
            PageRequest.encode(message.pagination, writer.uint32(10).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseQueryAllEvidenceRequest();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.pagination = PageRequest.decode(reader, reader.uint32());
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
        return { pagination: isSet(object.pagination) ? PageRequest.fromJSON(object.pagination) : undefined };
    },
    toJSON(message) {
        const obj = {};
        if (message.pagination !== undefined) {
            obj.pagination = PageRequest.toJSON(message.pagination);
        }
        return obj;
    },
    create(base) {
        return QueryAllEvidenceRequest.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryAllEvidenceRequest();
        message.pagination = object.pagination !== undefined && object.pagination !== null ? PageRequest.fromPartial(object.pagination) : undefined;
        return message;
    }
};
export const QueryAllEvidenceResponse = {
    $type: "cosmos.evidence.v1beta1.QueryAllEvidenceResponse",
    encode(message, writer = new BinaryWriter()) {
        for (const v of message.evidence) {
            Any.encode(v, writer.uint32(10).fork()).join();
        }
        if (message.pagination !== undefined) {
            PageResponse.encode(message.pagination, writer.uint32(18).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseQueryAllEvidenceResponse();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.evidence.push(Any.decode(reader, reader.uint32()));
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.pagination = PageResponse.decode(reader, reader.uint32());
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
            evidence: globalThis.Array.isArray(object?.evidence) ? object.evidence.map((e) => Any.fromJSON(e)) : [],
            pagination: isSet(object.pagination) ? PageResponse.fromJSON(object.pagination) : undefined
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.evidence?.length) {
            obj.evidence = message.evidence.map((e) => Any.toJSON(e));
        }
        if (message.pagination !== undefined) {
            obj.pagination = PageResponse.toJSON(message.pagination);
        }
        return obj;
    },
    create(base) {
        return QueryAllEvidenceResponse.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryAllEvidenceResponse();
        message.evidence = object.evidence?.map((e) => Any.fromPartial(e)) || [];
        message.pagination = object.pagination !== undefined && object.pagination !== null ? PageResponse.fromPartial(object.pagination) : undefined;
        return message;
    }
};
function createBaseQueryEvidenceRequest() {
    return { evidence_hash: new Uint8Array(0) };
}
function createBaseQueryEvidenceResponse() {
    return { evidence: undefined };
}
function createBaseQueryAllEvidenceRequest() {
    return { pagination: undefined };
}
function createBaseQueryAllEvidenceResponse() {
    return { evidence: [], pagination: undefined };
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
export const registry = [
    ["/cosmos.evidence.v1beta1.QueryEvidenceRequest", QueryEvidenceRequest],
    ["/cosmos.evidence.v1beta1.QueryEvidenceResponse", QueryEvidenceResponse],
    ["/cosmos.evidence.v1beta1.QueryAllEvidenceRequest", QueryAllEvidenceRequest],
    ["/cosmos.evidence.v1beta1.QueryAllEvidenceResponse", QueryAllEvidenceResponse]
];
export const aminoConverters = {
    "/cosmos.evidence.v1beta1.QueryEvidenceRequest": {
        aminoType: "cosmos-sdk/QueryEvidenceRequest",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.evidence.v1beta1.QueryEvidenceResponse": {
        aminoType: "cosmos-sdk/QueryEvidenceResponse",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.evidence.v1beta1.QueryAllEvidenceRequest": {
        aminoType: "cosmos-sdk/QueryAllEvidenceRequest",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.evidence.v1beta1.QueryAllEvidenceResponse": {
        aminoType: "cosmos-sdk/QueryAllEvidenceResponse",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    }
};
