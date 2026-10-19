"use strict";
Object.defineProperty(exports, "__esModule", { value: true });
exports.aminoConverters = exports.registry = exports.CustomHttpPattern = exports.HttpRule = exports.Http = void 0;
const wire_1 = require("@bufbuild/protobuf/wire");
exports.Http = {
    $type: "google.api.Http",
    encode(message, writer = new wire_1.BinaryWriter()) {
        for (const v of message.rules) {
            exports.HttpRule.encode(v, writer.uint32(10).fork()).join();
        }
        if (message.fully_decode_reserved_expansion !== false) {
            writer.uint32(16).bool(message.fully_decode_reserved_expansion);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseHttp();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.rules.push(exports.HttpRule.decode(reader, reader.uint32()));
                    continue;
                case 2:
                    if (tag !== 16) {
                        break;
                    }
                    message.fully_decode_reserved_expansion = reader.bool();
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
            rules: globalThis.Array.isArray(object?.rules) ? object.rules.map((e) => exports.HttpRule.fromJSON(e)) : [],
            fully_decode_reserved_expansion: isSet(object.fully_decode_reserved_expansion) ? globalThis.Boolean(object.fully_decode_reserved_expansion) : false
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.rules?.length) {
            obj.rules = message.rules.map((e) => exports.HttpRule.toJSON(e));
        }
        if (message.fully_decode_reserved_expansion !== false) {
            obj.fully_decode_reserved_expansion = message.fully_decode_reserved_expansion;
        }
        return obj;
    },
    create(base) {
        return exports.Http.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseHttp();
        message.rules = object.rules?.map((e) => exports.HttpRule.fromPartial(e)) || [];
        message.fully_decode_reserved_expansion = object.fully_decode_reserved_expansion ?? false;
        return message;
    }
};
exports.HttpRule = {
    $type: "google.api.HttpRule",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.selector !== "") {
            writer.uint32(10).string(message.selector);
        }
        if (message.get !== undefined) {
            writer.uint32(18).string(message.get);
        }
        if (message.put !== undefined) {
            writer.uint32(26).string(message.put);
        }
        if (message.post !== undefined) {
            writer.uint32(34).string(message.post);
        }
        if (message.delete !== undefined) {
            writer.uint32(42).string(message.delete);
        }
        if (message.patch !== undefined) {
            writer.uint32(50).string(message.patch);
        }
        if (message.custom !== undefined) {
            exports.CustomHttpPattern.encode(message.custom, writer.uint32(66).fork()).join();
        }
        if (message.body !== "") {
            writer.uint32(58).string(message.body);
        }
        if (message.response_body !== "") {
            writer.uint32(98).string(message.response_body);
        }
        for (const v of message.additional_bindings) {
            exports.HttpRule.encode(v, writer.uint32(90).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseHttpRule();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.selector = reader.string();
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.get = reader.string();
                    continue;
                case 3:
                    if (tag !== 26) {
                        break;
                    }
                    message.put = reader.string();
                    continue;
                case 4:
                    if (tag !== 34) {
                        break;
                    }
                    message.post = reader.string();
                    continue;
                case 5:
                    if (tag !== 42) {
                        break;
                    }
                    message.delete = reader.string();
                    continue;
                case 6:
                    if (tag !== 50) {
                        break;
                    }
                    message.patch = reader.string();
                    continue;
                case 8:
                    if (tag !== 66) {
                        break;
                    }
                    message.custom = exports.CustomHttpPattern.decode(reader, reader.uint32());
                    continue;
                case 7:
                    if (tag !== 58) {
                        break;
                    }
                    message.body = reader.string();
                    continue;
                case 12:
                    if (tag !== 98) {
                        break;
                    }
                    message.response_body = reader.string();
                    continue;
                case 11:
                    if (tag !== 90) {
                        break;
                    }
                    message.additional_bindings.push(exports.HttpRule.decode(reader, reader.uint32()));
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
            selector: isSet(object.selector) ? globalThis.String(object.selector) : "",
            get: isSet(object.get) ? globalThis.String(object.get) : undefined,
            put: isSet(object.put) ? globalThis.String(object.put) : undefined,
            post: isSet(object.post) ? globalThis.String(object.post) : undefined,
            delete: isSet(object.delete) ? globalThis.String(object.delete) : undefined,
            patch: isSet(object.patch) ? globalThis.String(object.patch) : undefined,
            custom: isSet(object.custom) ? exports.CustomHttpPattern.fromJSON(object.custom) : undefined,
            body: isSet(object.body) ? globalThis.String(object.body) : "",
            response_body: isSet(object.response_body) ? globalThis.String(object.response_body) : "",
            additional_bindings: globalThis.Array.isArray(object?.additional_bindings) ? object.additional_bindings.map((e) => exports.HttpRule.fromJSON(e)) : []
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.selector !== "") {
            obj.selector = message.selector;
        }
        if (message.get !== undefined) {
            obj.get = message.get;
        }
        if (message.put !== undefined) {
            obj.put = message.put;
        }
        if (message.post !== undefined) {
            obj.post = message.post;
        }
        if (message.delete !== undefined) {
            obj.delete = message.delete;
        }
        if (message.patch !== undefined) {
            obj.patch = message.patch;
        }
        if (message.custom !== undefined) {
            obj.custom = exports.CustomHttpPattern.toJSON(message.custom);
        }
        if (message.body !== "") {
            obj.body = message.body;
        }
        if (message.response_body !== "") {
            obj.response_body = message.response_body;
        }
        if (message.additional_bindings?.length) {
            obj.additional_bindings = message.additional_bindings.map((e) => exports.HttpRule.toJSON(e));
        }
        return obj;
    },
    create(base) {
        return exports.HttpRule.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseHttpRule();
        message.selector = object.selector ?? "";
        message.get = object.get ?? undefined;
        message.put = object.put ?? undefined;
        message.post = object.post ?? undefined;
        message.delete = object.delete ?? undefined;
        message.patch = object.patch ?? undefined;
        message.custom = object.custom !== undefined && object.custom !== null ? exports.CustomHttpPattern.fromPartial(object.custom) : undefined;
        message.body = object.body ?? "";
        message.response_body = object.response_body ?? "";
        message.additional_bindings = object.additional_bindings?.map((e) => exports.HttpRule.fromPartial(e)) || [];
        return message;
    }
};
exports.CustomHttpPattern = {
    $type: "google.api.CustomHttpPattern",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.kind !== "") {
            writer.uint32(10).string(message.kind);
        }
        if (message.path !== "") {
            writer.uint32(18).string(message.path);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseCustomHttpPattern();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.kind = reader.string();
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.path = reader.string();
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
            kind: isSet(object.kind) ? globalThis.String(object.kind) : "",
            path: isSet(object.path) ? globalThis.String(object.path) : ""
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.kind !== "") {
            obj.kind = message.kind;
        }
        if (message.path !== "") {
            obj.path = message.path;
        }
        return obj;
    },
    create(base) {
        return exports.CustomHttpPattern.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseCustomHttpPattern();
        message.kind = object.kind ?? "";
        message.path = object.path ?? "";
        return message;
    }
};
function createBaseHttp() {
    return { rules: [], fully_decode_reserved_expansion: false };
}
function createBaseHttpRule() {
    return {
        selector: "",
        get: undefined,
        put: undefined,
        post: undefined,
        delete: undefined,
        patch: undefined,
        custom: undefined,
        body: "",
        response_body: "",
        additional_bindings: []
    };
}
function createBaseCustomHttpPattern() {
    return { kind: "", path: "" };
}
function isSet(value) {
    return value !== null && value !== undefined;
}
exports.registry = [
    ["/google.api.Http", exports.Http],
    ["/google.api.HttpRule", exports.HttpRule],
    ["/google.api.CustomHttpPattern", exports.CustomHttpPattern]
];
exports.aminoConverters = {
    "/google.api.Http": {
        aminoType: "google.api.Http",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/google.api.HttpRule": {
        aminoType: "google.api.HttpRule",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/google.api.CustomHttpPattern": {
        aminoType: "google.api.CustomHttpPattern",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    }
};
