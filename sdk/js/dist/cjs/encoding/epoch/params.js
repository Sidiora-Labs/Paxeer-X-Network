"use strict";
Object.defineProperty(exports, "__esModule", { value: true });
exports.aminoConverters = exports.registry = exports.Params = void 0;
const wire_1 = require("@bufbuild/protobuf/wire");
exports.Params = {
    $type: "paxprotocol.paxchain.epoch.Params",
    encode(_, writer = new wire_1.BinaryWriter()) {
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseParams();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
            }
            if ((tag & 7) === 4 || tag === 0) {
                break;
            }
            reader.skip(tag & 7);
        }
        return message;
    },
    fromJSON(_) {
        return {};
    },
    toJSON(_) {
        const obj = {};
        return obj;
    },
    create(base) {
        return exports.Params.fromPartial(base ?? {});
    },
    fromPartial(_) {
        const message = createBaseParams();
        return message;
    }
};
function createBaseParams() {
    return {};
}
exports.registry = [["/paxprotocol.paxchain.epoch.Params", exports.Params]];
exports.aminoConverters = {
    "/paxprotocol.paxchain.epoch.Params": {
        aminoType: "epoch/Params",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    }
};
