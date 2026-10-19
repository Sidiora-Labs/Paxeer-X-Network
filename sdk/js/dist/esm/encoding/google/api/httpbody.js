import { BinaryReader, BinaryWriter } from "@bufbuild/protobuf/wire";
import { Any } from "../protobuf/any";
export const HttpBody = {
    $type: "google.api.HttpBody",
    encode(message, writer = new BinaryWriter()) {
        if (message.content_type !== "") {
            writer.uint32(10).string(message.content_type);
        }
        if (message.data.length !== 0) {
            writer.uint32(18).bytes(message.data);
        }
        for (const v of message.extensions) {
            Any.encode(v, writer.uint32(26).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseHttpBody();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.content_type = reader.string();
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.data = reader.bytes();
                    continue;
                case 3:
                    if (tag !== 26) {
                        break;
                    }
                    message.extensions.push(Any.decode(reader, reader.uint32()));
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
            content_type: isSet(object.content_type) ? globalThis.String(object.content_type) : "",
            data: isSet(object.data) ? bytesFromBase64(object.data) : new Uint8Array(0),
            extensions: globalThis.Array.isArray(object?.extensions) ? object.extensions.map((e) => Any.fromJSON(e)) : []
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.content_type !== "") {
            obj.content_type = message.content_type;
        }
        if (message.data.length !== 0) {
            obj.data = base64FromBytes(message.data);
        }
        if (message.extensions?.length) {
            obj.extensions = message.extensions.map((e) => Any.toJSON(e));
        }
        return obj;
    },
    create(base) {
        return HttpBody.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseHttpBody();
        message.content_type = object.content_type ?? "";
        message.data = object.data ?? new Uint8Array(0);
        message.extensions = object.extensions?.map((e) => Any.fromPartial(e)) || [];
        return message;
    }
};
function createBaseHttpBody() {
    return { content_type: "", data: new Uint8Array(0), extensions: [] };
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
export const registry = [["/google.api.HttpBody", HttpBody]];
export const aminoConverters = {
    "/google.api.HttpBody": {
        aminoType: "google.api.HttpBody",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    }
};
