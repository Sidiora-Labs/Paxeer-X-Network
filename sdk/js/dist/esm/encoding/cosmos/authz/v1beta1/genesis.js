import { BinaryReader, BinaryWriter } from "@bufbuild/protobuf/wire";
import { GrantAuthorization } from "./authz";
export const GenesisState = {
    $type: "cosmos.authz.v1beta1.GenesisState",
    encode(message, writer = new BinaryWriter()) {
        for (const v of message.authorization) {
            GrantAuthorization.encode(v, writer.uint32(10).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseGenesisState();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.authorization.push(GrantAuthorization.decode(reader, reader.uint32()));
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
            authorization: globalThis.Array.isArray(object?.authorization) ? object.authorization.map((e) => GrantAuthorization.fromJSON(e)) : []
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.authorization?.length) {
            obj.authorization = message.authorization.map((e) => GrantAuthorization.toJSON(e));
        }
        return obj;
    },
    create(base) {
        return GenesisState.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseGenesisState();
        message.authorization = object.authorization?.map((e) => GrantAuthorization.fromPartial(e)) || [];
        return message;
    }
};
function createBaseGenesisState() {
    return { authorization: [] };
}
export const registry = [["/cosmos.authz.v1beta1.GenesisState", GenesisState]];
export const aminoConverters = {
    "/cosmos.authz.v1beta1.GenesisState": {
        aminoType: "cosmos-sdk/GenesisState",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    }
};
