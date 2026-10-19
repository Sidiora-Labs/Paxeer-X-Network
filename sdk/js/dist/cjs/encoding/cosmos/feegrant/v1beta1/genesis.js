"use strict";
Object.defineProperty(exports, "__esModule", { value: true });
exports.aminoConverters = exports.registry = exports.GenesisState = void 0;
const wire_1 = require("@bufbuild/protobuf/wire");
const feegrant_1 = require("./feegrant");
exports.GenesisState = {
    $type: "cosmos.feegrant.v1beta1.GenesisState",
    encode(message, writer = new wire_1.BinaryWriter()) {
        for (const v of message.allowances) {
            feegrant_1.Grant.encode(v, writer.uint32(10).fork()).join();
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
                    message.allowances.push(feegrant_1.Grant.decode(reader, reader.uint32()));
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
            allowances: globalThis.Array.isArray(object?.allowances) ? object.allowances.map((e) => feegrant_1.Grant.fromJSON(e)) : []
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.allowances?.length) {
            obj.allowances = message.allowances.map((e) => feegrant_1.Grant.toJSON(e));
        }
        return obj;
    },
    create(base) {
        return exports.GenesisState.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseGenesisState();
        message.allowances = object.allowances?.map((e) => feegrant_1.Grant.fromPartial(e)) || [];
        return message;
    }
};
function createBaseGenesisState() {
    return { allowances: [] };
}
exports.registry = [["/cosmos.feegrant.v1beta1.GenesisState", exports.GenesisState]];
exports.aminoConverters = {
    "/cosmos.feegrant.v1beta1.GenesisState": {
        aminoType: "cosmos-sdk/GenesisState",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    }
};
