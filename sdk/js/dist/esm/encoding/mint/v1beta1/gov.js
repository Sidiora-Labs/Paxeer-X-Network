import { BinaryReader, BinaryWriter } from "@bufbuild/protobuf/wire";
import { Minter } from "./mint";
export const UpdateMinterProposal = {
    $type: "paxprotocol.paxchain.mint.UpdateMinterProposal",
    encode(message, writer = new BinaryWriter()) {
        if (message.title !== "") {
            writer.uint32(10).string(message.title);
        }
        if (message.description !== "") {
            writer.uint32(18).string(message.description);
        }
        if (message.minter !== undefined) {
            Minter.encode(message.minter, writer.uint32(26).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseUpdateMinterProposal();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.title = reader.string();
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.description = reader.string();
                    continue;
                case 3:
                    if (tag !== 26) {
                        break;
                    }
                    message.minter = Minter.decode(reader, reader.uint32());
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
            title: isSet(object.title) ? globalThis.String(object.title) : "",
            description: isSet(object.description) ? globalThis.String(object.description) : "",
            minter: isSet(object.minter) ? Minter.fromJSON(object.minter) : undefined
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.title !== "") {
            obj.title = message.title;
        }
        if (message.description !== "") {
            obj.description = message.description;
        }
        if (message.minter !== undefined) {
            obj.minter = Minter.toJSON(message.minter);
        }
        return obj;
    },
    create(base) {
        return UpdateMinterProposal.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseUpdateMinterProposal();
        message.title = object.title ?? "";
        message.description = object.description ?? "";
        message.minter = object.minter !== undefined && object.minter !== null ? Minter.fromPartial(object.minter) : undefined;
        return message;
    }
};
function createBaseUpdateMinterProposal() {
    return { title: "", description: "", minter: undefined };
}
function isSet(value) {
    return value !== null && value !== undefined;
}
export const registry = [["/paxprotocol.paxchain.mint.UpdateMinterProposal", UpdateMinterProposal]];
export const aminoConverters = {
    "/paxprotocol.paxchain.mint.UpdateMinterProposal": {
        aminoType: "mint/UpdateMinterProposal",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    }
};
