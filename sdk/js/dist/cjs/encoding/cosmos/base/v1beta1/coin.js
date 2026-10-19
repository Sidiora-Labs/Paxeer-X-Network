"use strict";
Object.defineProperty(exports, "__esModule", { value: true });
exports.aminoConverters = exports.registry = exports.DecProto = exports.IntProto = exports.DecCoin = exports.Coin = void 0;
const wire_1 = require("@bufbuild/protobuf/wire");
exports.Coin = {
    $type: "cosmos.base.v1beta1.Coin",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.denom !== "") {
            writer.uint32(10).string(message.denom);
        }
        if (message.amount !== "") {
            writer.uint32(18).string(message.amount);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseCoin();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.denom = reader.string();
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.amount = reader.string();
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
            denom: isSet(object.denom) ? globalThis.String(object.denom) : "",
            amount: isSet(object.amount) ? globalThis.String(object.amount) : ""
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.denom !== "") {
            obj.denom = message.denom;
        }
        if (message.amount !== "") {
            obj.amount = message.amount;
        }
        return obj;
    },
    create(base) {
        return exports.Coin.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseCoin();
        message.denom = object.denom ?? "";
        message.amount = object.amount ?? "";
        return message;
    }
};
exports.DecCoin = {
    $type: "cosmos.base.v1beta1.DecCoin",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.denom !== "") {
            writer.uint32(10).string(message.denom);
        }
        if (message.amount !== "") {
            writer.uint32(18).string(message.amount);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseDecCoin();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.denom = reader.string();
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.amount = reader.string();
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
            denom: isSet(object.denom) ? globalThis.String(object.denom) : "",
            amount: isSet(object.amount) ? globalThis.String(object.amount) : ""
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.denom !== "") {
            obj.denom = message.denom;
        }
        if (message.amount !== "") {
            obj.amount = message.amount;
        }
        return obj;
    },
    create(base) {
        return exports.DecCoin.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseDecCoin();
        message.denom = object.denom ?? "";
        message.amount = object.amount ?? "";
        return message;
    }
};
exports.IntProto = {
    $type: "cosmos.base.v1beta1.IntProto",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.int !== "") {
            writer.uint32(10).string(message.int);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseIntProto();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.int = reader.string();
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
        return { int: isSet(object.int) ? globalThis.String(object.int) : "" };
    },
    toJSON(message) {
        const obj = {};
        if (message.int !== "") {
            obj.int = message.int;
        }
        return obj;
    },
    create(base) {
        return exports.IntProto.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseIntProto();
        message.int = object.int ?? "";
        return message;
    }
};
exports.DecProto = {
    $type: "cosmos.base.v1beta1.DecProto",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.dec !== "") {
            writer.uint32(10).string(message.dec);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseDecProto();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.dec = reader.string();
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
        return { dec: isSet(object.dec) ? globalThis.String(object.dec) : "" };
    },
    toJSON(message) {
        const obj = {};
        if (message.dec !== "") {
            obj.dec = message.dec;
        }
        return obj;
    },
    create(base) {
        return exports.DecProto.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseDecProto();
        message.dec = object.dec ?? "";
        return message;
    }
};
function createBaseCoin() {
    return { denom: "", amount: "" };
}
function createBaseDecCoin() {
    return { denom: "", amount: "" };
}
function createBaseIntProto() {
    return { int: "" };
}
function createBaseDecProto() {
    return { dec: "" };
}
function isSet(value) {
    return value !== null && value !== undefined;
}
exports.registry = [
    ["/cosmos.base.v1beta1.Coin", exports.Coin],
    ["/cosmos.base.v1beta1.DecCoin", exports.DecCoin],
    ["/cosmos.base.v1beta1.IntProto", exports.IntProto],
    ["/cosmos.base.v1beta1.DecProto", exports.DecProto]
];
exports.aminoConverters = {
    "/cosmos.base.v1beta1.Coin": {
        aminoType: "cosmos-sdk/Coin",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.base.v1beta1.DecCoin": {
        aminoType: "cosmos-sdk/DecCoin",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.base.v1beta1.IntProto": {
        aminoType: "cosmos-sdk/IntProto",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.base.v1beta1.DecProto": {
        aminoType: "cosmos-sdk/DecProto",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    }
};
