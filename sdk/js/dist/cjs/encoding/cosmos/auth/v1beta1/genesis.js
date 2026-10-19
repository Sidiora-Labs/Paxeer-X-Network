"use strict";
Object.defineProperty(exports, "__esModule", { value: true });
exports.aminoConverters = exports.registry = exports.GenesisState = void 0;
const wire_1 = require("@bufbuild/protobuf/wire");
const any_1 = require("../../../google/protobuf/any");
const auth_1 = require("./auth");
exports.GenesisState = {
    $type: "cosmos.auth.v1beta1.GenesisState",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.params !== undefined) {
            auth_1.Params.encode(message.params, writer.uint32(10).fork()).join();
        }
        for (const v of message.accounts) {
            any_1.Any.encode(v, writer.uint32(18).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseGenesisState();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.params = auth_1.Params.decode(reader, reader.uint32());
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.accounts.push(any_1.Any.decode(reader, reader.uint32()));
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
            params: isSet(object.params) ? auth_1.Params.fromJSON(object.params) : undefined,
            accounts: globalThis.Array.isArray(object?.accounts) ? object.accounts.map((e) => any_1.Any.fromJSON(e)) : []
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.params !== undefined) {
            obj.params = auth_1.Params.toJSON(message.params);
        }
        if (message.accounts?.length) {
            obj.accounts = message.accounts.map((e) => any_1.Any.toJSON(e));
        }
        return obj;
    },
    create(base) {
        return exports.GenesisState.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseGenesisState();
        message.params = object.params !== undefined && object.params !== null ? auth_1.Params.fromPartial(object.params) : undefined;
        message.accounts = object.accounts?.map((e) => any_1.Any.fromPartial(e)) || [];
        return message;
    }
};
function createBaseGenesisState() {
    return { params: undefined, accounts: [] };
}
function isSet(value) {
    return value !== null && value !== undefined;
}
exports.registry = [["/cosmos.auth.v1beta1.GenesisState", exports.GenesisState]];
exports.aminoConverters = {
    "/cosmos.auth.v1beta1.GenesisState": {
        aminoType: "cosmos-sdk/GenesisState",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    }
};
