"use strict";
Object.defineProperty(exports, "__esModule", { value: true });
exports.aminoConverters = exports.registry = exports.GenesisState = void 0;
const wire_1 = require("@bufbuild/protobuf/wire");
const authz_1 = require("./authz");
exports.GenesisState = {
    $type: "cosmos.authz.v1beta1.GenesisState",
    encode(message, writer = new wire_1.BinaryWriter()) {
        for (const v of message.authorization) {
            authz_1.GrantAuthorization.encode(v, writer.uint32(10).fork()).join();
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
                    message.authorization.push(authz_1.GrantAuthorization.decode(reader, reader.uint32()));
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
            authorization: globalThis.Array.isArray(object?.authorization) ? object.authorization.map((e) => authz_1.GrantAuthorization.fromJSON(e)) : []
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.authorization?.length) {
            obj.authorization = message.authorization.map((e) => authz_1.GrantAuthorization.toJSON(e));
        }
        return obj;
    },
    create(base) {
        return exports.GenesisState.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseGenesisState();
        message.authorization = object.authorization?.map((e) => authz_1.GrantAuthorization.fromPartial(e)) || [];
        return message;
    }
};
function createBaseGenesisState() {
    return { authorization: [] };
}
exports.registry = [["/cosmos.authz.v1beta1.GenesisState", exports.GenesisState]];
exports.aminoConverters = {
    "/cosmos.authz.v1beta1.GenesisState": {
        aminoType: "cosmos-sdk/GenesisState",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    }
};
