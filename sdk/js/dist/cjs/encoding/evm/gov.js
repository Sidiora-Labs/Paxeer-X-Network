"use strict";
Object.defineProperty(exports, "__esModule", { value: true });
exports.aminoConverters = exports.registry = exports.AddERCNativePointerProposalV2 = exports.AddCWERC1155PointerProposal = exports.AddCWERC721PointerProposal = exports.AddCWERC20PointerProposal = exports.AddERCCW1155PointerProposal = exports.AddERCCW721PointerProposal = exports.AddERCCW20PointerProposal = exports.AddERCNativePointerProposal = void 0;
const wire_1 = require("@bufbuild/protobuf/wire");
exports.AddERCNativePointerProposal = {
    $type: "paxprotocol.paxchain.evm.AddERCNativePointerProposal",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.title !== "") {
            writer.uint32(10).string(message.title);
        }
        if (message.description !== "") {
            writer.uint32(18).string(message.description);
        }
        if (message.token !== "") {
            writer.uint32(26).string(message.token);
        }
        if (message.pointer !== "") {
            writer.uint32(34).string(message.pointer);
        }
        if (message.version !== 0) {
            writer.uint32(40).uint32(message.version);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseAddERCNativePointerProposal();
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
                    message.token = reader.string();
                    continue;
                case 4:
                    if (tag !== 34) {
                        break;
                    }
                    message.pointer = reader.string();
                    continue;
                case 5:
                    if (tag !== 40) {
                        break;
                    }
                    message.version = reader.uint32();
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
            token: isSet(object.token) ? globalThis.String(object.token) : "",
            pointer: isSet(object.pointer) ? globalThis.String(object.pointer) : "",
            version: isSet(object.version) ? globalThis.Number(object.version) : 0
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
        if (message.token !== "") {
            obj.token = message.token;
        }
        if (message.pointer !== "") {
            obj.pointer = message.pointer;
        }
        if (message.version !== 0) {
            obj.version = Math.round(message.version);
        }
        return obj;
    },
    create(base) {
        return exports.AddERCNativePointerProposal.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseAddERCNativePointerProposal();
        message.title = object.title ?? "";
        message.description = object.description ?? "";
        message.token = object.token ?? "";
        message.pointer = object.pointer ?? "";
        message.version = object.version ?? 0;
        return message;
    }
};
exports.AddERCCW20PointerProposal = {
    $type: "paxprotocol.paxchain.evm.AddERCCW20PointerProposal",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.title !== "") {
            writer.uint32(10).string(message.title);
        }
        if (message.description !== "") {
            writer.uint32(18).string(message.description);
        }
        if (message.pointee !== "") {
            writer.uint32(26).string(message.pointee);
        }
        if (message.pointer !== "") {
            writer.uint32(34).string(message.pointer);
        }
        if (message.version !== 0) {
            writer.uint32(40).uint32(message.version);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseAddERCCW20PointerProposal();
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
                    message.pointee = reader.string();
                    continue;
                case 4:
                    if (tag !== 34) {
                        break;
                    }
                    message.pointer = reader.string();
                    continue;
                case 5:
                    if (tag !== 40) {
                        break;
                    }
                    message.version = reader.uint32();
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
            pointee: isSet(object.pointee) ? globalThis.String(object.pointee) : "",
            pointer: isSet(object.pointer) ? globalThis.String(object.pointer) : "",
            version: isSet(object.version) ? globalThis.Number(object.version) : 0
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
        if (message.pointee !== "") {
            obj.pointee = message.pointee;
        }
        if (message.pointer !== "") {
            obj.pointer = message.pointer;
        }
        if (message.version !== 0) {
            obj.version = Math.round(message.version);
        }
        return obj;
    },
    create(base) {
        return exports.AddERCCW20PointerProposal.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseAddERCCW20PointerProposal();
        message.title = object.title ?? "";
        message.description = object.description ?? "";
        message.pointee = object.pointee ?? "";
        message.pointer = object.pointer ?? "";
        message.version = object.version ?? 0;
        return message;
    }
};
exports.AddERCCW721PointerProposal = {
    $type: "paxprotocol.paxchain.evm.AddERCCW721PointerProposal",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.title !== "") {
            writer.uint32(10).string(message.title);
        }
        if (message.description !== "") {
            writer.uint32(18).string(message.description);
        }
        if (message.pointee !== "") {
            writer.uint32(26).string(message.pointee);
        }
        if (message.pointer !== "") {
            writer.uint32(34).string(message.pointer);
        }
        if (message.version !== 0) {
            writer.uint32(40).uint32(message.version);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseAddERCCW721PointerProposal();
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
                    message.pointee = reader.string();
                    continue;
                case 4:
                    if (tag !== 34) {
                        break;
                    }
                    message.pointer = reader.string();
                    continue;
                case 5:
                    if (tag !== 40) {
                        break;
                    }
                    message.version = reader.uint32();
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
            pointee: isSet(object.pointee) ? globalThis.String(object.pointee) : "",
            pointer: isSet(object.pointer) ? globalThis.String(object.pointer) : "",
            version: isSet(object.version) ? globalThis.Number(object.version) : 0
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
        if (message.pointee !== "") {
            obj.pointee = message.pointee;
        }
        if (message.pointer !== "") {
            obj.pointer = message.pointer;
        }
        if (message.version !== 0) {
            obj.version = Math.round(message.version);
        }
        return obj;
    },
    create(base) {
        return exports.AddERCCW721PointerProposal.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseAddERCCW721PointerProposal();
        message.title = object.title ?? "";
        message.description = object.description ?? "";
        message.pointee = object.pointee ?? "";
        message.pointer = object.pointer ?? "";
        message.version = object.version ?? 0;
        return message;
    }
};
exports.AddERCCW1155PointerProposal = {
    $type: "paxprotocol.paxchain.evm.AddERCCW1155PointerProposal",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.title !== "") {
            writer.uint32(10).string(message.title);
        }
        if (message.description !== "") {
            writer.uint32(18).string(message.description);
        }
        if (message.pointee !== "") {
            writer.uint32(26).string(message.pointee);
        }
        if (message.pointer !== "") {
            writer.uint32(34).string(message.pointer);
        }
        if (message.version !== 0) {
            writer.uint32(40).uint32(message.version);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseAddERCCW1155PointerProposal();
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
                    message.pointee = reader.string();
                    continue;
                case 4:
                    if (tag !== 34) {
                        break;
                    }
                    message.pointer = reader.string();
                    continue;
                case 5:
                    if (tag !== 40) {
                        break;
                    }
                    message.version = reader.uint32();
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
            pointee: isSet(object.pointee) ? globalThis.String(object.pointee) : "",
            pointer: isSet(object.pointer) ? globalThis.String(object.pointer) : "",
            version: isSet(object.version) ? globalThis.Number(object.version) : 0
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
        if (message.pointee !== "") {
            obj.pointee = message.pointee;
        }
        if (message.pointer !== "") {
            obj.pointer = message.pointer;
        }
        if (message.version !== 0) {
            obj.version = Math.round(message.version);
        }
        return obj;
    },
    create(base) {
        return exports.AddERCCW1155PointerProposal.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseAddERCCW1155PointerProposal();
        message.title = object.title ?? "";
        message.description = object.description ?? "";
        message.pointee = object.pointee ?? "";
        message.pointer = object.pointer ?? "";
        message.version = object.version ?? 0;
        return message;
    }
};
exports.AddCWERC20PointerProposal = {
    $type: "paxprotocol.paxchain.evm.AddCWERC20PointerProposal",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.title !== "") {
            writer.uint32(10).string(message.title);
        }
        if (message.description !== "") {
            writer.uint32(18).string(message.description);
        }
        if (message.pointee !== "") {
            writer.uint32(26).string(message.pointee);
        }
        if (message.pointer !== "") {
            writer.uint32(34).string(message.pointer);
        }
        if (message.version !== 0) {
            writer.uint32(40).uint32(message.version);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseAddCWERC20PointerProposal();
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
                    message.pointee = reader.string();
                    continue;
                case 4:
                    if (tag !== 34) {
                        break;
                    }
                    message.pointer = reader.string();
                    continue;
                case 5:
                    if (tag !== 40) {
                        break;
                    }
                    message.version = reader.uint32();
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
            pointee: isSet(object.pointee) ? globalThis.String(object.pointee) : "",
            pointer: isSet(object.pointer) ? globalThis.String(object.pointer) : "",
            version: isSet(object.version) ? globalThis.Number(object.version) : 0
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
        if (message.pointee !== "") {
            obj.pointee = message.pointee;
        }
        if (message.pointer !== "") {
            obj.pointer = message.pointer;
        }
        if (message.version !== 0) {
            obj.version = Math.round(message.version);
        }
        return obj;
    },
    create(base) {
        return exports.AddCWERC20PointerProposal.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseAddCWERC20PointerProposal();
        message.title = object.title ?? "";
        message.description = object.description ?? "";
        message.pointee = object.pointee ?? "";
        message.pointer = object.pointer ?? "";
        message.version = object.version ?? 0;
        return message;
    }
};
exports.AddCWERC721PointerProposal = {
    $type: "paxprotocol.paxchain.evm.AddCWERC721PointerProposal",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.title !== "") {
            writer.uint32(10).string(message.title);
        }
        if (message.description !== "") {
            writer.uint32(18).string(message.description);
        }
        if (message.pointee !== "") {
            writer.uint32(26).string(message.pointee);
        }
        if (message.pointer !== "") {
            writer.uint32(34).string(message.pointer);
        }
        if (message.version !== 0) {
            writer.uint32(40).uint32(message.version);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseAddCWERC721PointerProposal();
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
                    message.pointee = reader.string();
                    continue;
                case 4:
                    if (tag !== 34) {
                        break;
                    }
                    message.pointer = reader.string();
                    continue;
                case 5:
                    if (tag !== 40) {
                        break;
                    }
                    message.version = reader.uint32();
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
            pointee: isSet(object.pointee) ? globalThis.String(object.pointee) : "",
            pointer: isSet(object.pointer) ? globalThis.String(object.pointer) : "",
            version: isSet(object.version) ? globalThis.Number(object.version) : 0
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
        if (message.pointee !== "") {
            obj.pointee = message.pointee;
        }
        if (message.pointer !== "") {
            obj.pointer = message.pointer;
        }
        if (message.version !== 0) {
            obj.version = Math.round(message.version);
        }
        return obj;
    },
    create(base) {
        return exports.AddCWERC721PointerProposal.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseAddCWERC721PointerProposal();
        message.title = object.title ?? "";
        message.description = object.description ?? "";
        message.pointee = object.pointee ?? "";
        message.pointer = object.pointer ?? "";
        message.version = object.version ?? 0;
        return message;
    }
};
exports.AddCWERC1155PointerProposal = {
    $type: "paxprotocol.paxchain.evm.AddCWERC1155PointerProposal",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.title !== "") {
            writer.uint32(10).string(message.title);
        }
        if (message.description !== "") {
            writer.uint32(18).string(message.description);
        }
        if (message.pointee !== "") {
            writer.uint32(26).string(message.pointee);
        }
        if (message.pointer !== "") {
            writer.uint32(34).string(message.pointer);
        }
        if (message.version !== 0) {
            writer.uint32(40).uint32(message.version);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseAddCWERC1155PointerProposal();
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
                    message.pointee = reader.string();
                    continue;
                case 4:
                    if (tag !== 34) {
                        break;
                    }
                    message.pointer = reader.string();
                    continue;
                case 5:
                    if (tag !== 40) {
                        break;
                    }
                    message.version = reader.uint32();
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
            pointee: isSet(object.pointee) ? globalThis.String(object.pointee) : "",
            pointer: isSet(object.pointer) ? globalThis.String(object.pointer) : "",
            version: isSet(object.version) ? globalThis.Number(object.version) : 0
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
        if (message.pointee !== "") {
            obj.pointee = message.pointee;
        }
        if (message.pointer !== "") {
            obj.pointer = message.pointer;
        }
        if (message.version !== 0) {
            obj.version = Math.round(message.version);
        }
        return obj;
    },
    create(base) {
        return exports.AddCWERC1155PointerProposal.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseAddCWERC1155PointerProposal();
        message.title = object.title ?? "";
        message.description = object.description ?? "";
        message.pointee = object.pointee ?? "";
        message.pointer = object.pointer ?? "";
        message.version = object.version ?? 0;
        return message;
    }
};
exports.AddERCNativePointerProposalV2 = {
    $type: "paxprotocol.paxchain.evm.AddERCNativePointerProposalV2",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.title !== "") {
            writer.uint32(10).string(message.title);
        }
        if (message.description !== "") {
            writer.uint32(18).string(message.description);
        }
        if (message.token !== "") {
            writer.uint32(26).string(message.token);
        }
        if (message.name !== "") {
            writer.uint32(34).string(message.name);
        }
        if (message.symbol !== "") {
            writer.uint32(42).string(message.symbol);
        }
        if (message.decimals !== 0) {
            writer.uint32(48).uint32(message.decimals);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseAddERCNativePointerProposalV2();
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
                    message.token = reader.string();
                    continue;
                case 4:
                    if (tag !== 34) {
                        break;
                    }
                    message.name = reader.string();
                    continue;
                case 5:
                    if (tag !== 42) {
                        break;
                    }
                    message.symbol = reader.string();
                    continue;
                case 6:
                    if (tag !== 48) {
                        break;
                    }
                    message.decimals = reader.uint32();
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
            token: isSet(object.token) ? globalThis.String(object.token) : "",
            name: isSet(object.name) ? globalThis.String(object.name) : "",
            symbol: isSet(object.symbol) ? globalThis.String(object.symbol) : "",
            decimals: isSet(object.decimals) ? globalThis.Number(object.decimals) : 0
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
        if (message.token !== "") {
            obj.token = message.token;
        }
        if (message.name !== "") {
            obj.name = message.name;
        }
        if (message.symbol !== "") {
            obj.symbol = message.symbol;
        }
        if (message.decimals !== 0) {
            obj.decimals = Math.round(message.decimals);
        }
        return obj;
    },
    create(base) {
        return exports.AddERCNativePointerProposalV2.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseAddERCNativePointerProposalV2();
        message.title = object.title ?? "";
        message.description = object.description ?? "";
        message.token = object.token ?? "";
        message.name = object.name ?? "";
        message.symbol = object.symbol ?? "";
        message.decimals = object.decimals ?? 0;
        return message;
    }
};
function createBaseAddERCNativePointerProposal() {
    return { title: "", description: "", token: "", pointer: "", version: 0 };
}
function createBaseAddERCCW20PointerProposal() {
    return { title: "", description: "", pointee: "", pointer: "", version: 0 };
}
function createBaseAddERCCW721PointerProposal() {
    return { title: "", description: "", pointee: "", pointer: "", version: 0 };
}
function createBaseAddERCCW1155PointerProposal() {
    return { title: "", description: "", pointee: "", pointer: "", version: 0 };
}
function createBaseAddCWERC20PointerProposal() {
    return { title: "", description: "", pointee: "", pointer: "", version: 0 };
}
function createBaseAddCWERC721PointerProposal() {
    return { title: "", description: "", pointee: "", pointer: "", version: 0 };
}
function createBaseAddCWERC1155PointerProposal() {
    return { title: "", description: "", pointee: "", pointer: "", version: 0 };
}
function createBaseAddERCNativePointerProposalV2() {
    return { title: "", description: "", token: "", name: "", symbol: "", decimals: 0 };
}
function isSet(value) {
    return value !== null && value !== undefined;
}
exports.registry = [
    ["/paxprotocol.paxchain.evm.AddERCNativePointerProposal", exports.AddERCNativePointerProposal],
    ["/paxprotocol.paxchain.evm.AddERCCW20PointerProposal", exports.AddERCCW20PointerProposal],
    ["/paxprotocol.paxchain.evm.AddERCCW721PointerProposal", exports.AddERCCW721PointerProposal],
    ["/paxprotocol.paxchain.evm.AddERCCW1155PointerProposal", exports.AddERCCW1155PointerProposal],
    ["/paxprotocol.paxchain.evm.AddCWERC20PointerProposal", exports.AddCWERC20PointerProposal],
    ["/paxprotocol.paxchain.evm.AddCWERC721PointerProposal", exports.AddCWERC721PointerProposal],
    ["/paxprotocol.paxchain.evm.AddCWERC1155PointerProposal", exports.AddCWERC1155PointerProposal],
    ["/paxprotocol.paxchain.evm.AddERCNativePointerProposalV2", exports.AddERCNativePointerProposalV2]
];
exports.aminoConverters = {
    "/paxprotocol.paxchain.evm.AddERCNativePointerProposal": {
        aminoType: "evm/AddERCNativePointerProposal",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/paxprotocol.paxchain.evm.AddERCCW20PointerProposal": {
        aminoType: "evm/AddERCCW20PointerProposal",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/paxprotocol.paxchain.evm.AddERCCW721PointerProposal": {
        aminoType: "evm/AddERCCW721PointerProposal",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/paxprotocol.paxchain.evm.AddERCCW1155PointerProposal": {
        aminoType: "evm/AddERCCW1155PointerProposal",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/paxprotocol.paxchain.evm.AddCWERC20PointerProposal": {
        aminoType: "evm/AddCWERC20PointerProposal",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/paxprotocol.paxchain.evm.AddCWERC721PointerProposal": {
        aminoType: "evm/AddCWERC721PointerProposal",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/paxprotocol.paxchain.evm.AddCWERC1155PointerProposal": {
        aminoType: "evm/AddCWERC1155PointerProposal",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/paxprotocol.paxchain.evm.AddERCNativePointerProposalV2": {
        aminoType: "evm/AddERCNativePointerProposalV2",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    }
};
