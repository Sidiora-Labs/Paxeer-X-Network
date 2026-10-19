import { BinaryReader, BinaryWriter } from "@bufbuild/protobuf/wire";
import { Coin } from "../cosmos/base/v1beta1/coin";
import { Any } from "../google/protobuf/any";
import { assetTypeFromJSON, assetTypeToJSON, pointerTypeFromJSON, pointerTypeToJSON } from "./enums";
import { Log } from "./receipt";
export const MsgEVMTransaction = {
    $type: "paxprotocol.paxchain.evm.MsgEVMTransaction",
    encode(message, writer = new BinaryWriter()) {
        if (message.data !== undefined) {
            Any.encode(message.data, writer.uint32(10).fork()).join();
        }
        if (message.derived.length !== 0) {
            writer.uint32(18).bytes(message.derived);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseMsgEVMTransaction();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.data = Any.decode(reader, reader.uint32());
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.derived = reader.bytes();
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
            data: isSet(object.data) ? Any.fromJSON(object.data) : undefined,
            derived: isSet(object.derived) ? bytesFromBase64(object.derived) : new Uint8Array(0)
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.data !== undefined) {
            obj.data = Any.toJSON(message.data);
        }
        if (message.derived.length !== 0) {
            obj.derived = base64FromBytes(message.derived);
        }
        return obj;
    },
    create(base) {
        return MsgEVMTransaction.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseMsgEVMTransaction();
        message.data = object.data !== undefined && object.data !== null ? Any.fromPartial(object.data) : undefined;
        message.derived = object.derived ?? new Uint8Array(0);
        return message;
    }
};
export const MsgEVMTransactionResponse = {
    $type: "paxprotocol.paxchain.evm.MsgEVMTransactionResponse",
    encode(message, writer = new BinaryWriter()) {
        if (message.gas_used !== 0) {
            writer.uint32(8).uint64(message.gas_used);
        }
        if (message.vm_error !== "") {
            writer.uint32(18).string(message.vm_error);
        }
        if (message.return_data.length !== 0) {
            writer.uint32(26).bytes(message.return_data);
        }
        if (message.hash !== "") {
            writer.uint32(34).string(message.hash);
        }
        for (const v of message.logs) {
            Log.encode(v, writer.uint32(42).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseMsgEVMTransactionResponse();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 8) {
                        break;
                    }
                    message.gas_used = longToNumber(reader.uint64());
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.vm_error = reader.string();
                    continue;
                case 3:
                    if (tag !== 26) {
                        break;
                    }
                    message.return_data = reader.bytes();
                    continue;
                case 4:
                    if (tag !== 34) {
                        break;
                    }
                    message.hash = reader.string();
                    continue;
                case 5:
                    if (tag !== 42) {
                        break;
                    }
                    message.logs.push(Log.decode(reader, reader.uint32()));
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
            gas_used: isSet(object.gas_used) ? globalThis.Number(object.gas_used) : 0,
            vm_error: isSet(object.vm_error) ? globalThis.String(object.vm_error) : "",
            return_data: isSet(object.return_data) ? bytesFromBase64(object.return_data) : new Uint8Array(0),
            hash: isSet(object.hash) ? globalThis.String(object.hash) : "",
            logs: globalThis.Array.isArray(object?.logs) ? object.logs.map((e) => Log.fromJSON(e)) : []
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.gas_used !== 0) {
            obj.gas_used = Math.round(message.gas_used);
        }
        if (message.vm_error !== "") {
            obj.vm_error = message.vm_error;
        }
        if (message.return_data.length !== 0) {
            obj.return_data = base64FromBytes(message.return_data);
        }
        if (message.hash !== "") {
            obj.hash = message.hash;
        }
        if (message.logs?.length) {
            obj.logs = message.logs.map((e) => Log.toJSON(e));
        }
        return obj;
    },
    create(base) {
        return MsgEVMTransactionResponse.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseMsgEVMTransactionResponse();
        message.gas_used = object.gas_used ?? 0;
        message.vm_error = object.vm_error ?? "";
        message.return_data = object.return_data ?? new Uint8Array(0);
        message.hash = object.hash ?? "";
        message.logs = object.logs?.map((e) => Log.fromPartial(e)) || [];
        return message;
    }
};
export const MsgInternalEVMCall = {
    $type: "paxprotocol.paxchain.evm.MsgInternalEVMCall",
    encode(message, writer = new BinaryWriter()) {
        if (message.sender !== "") {
            writer.uint32(10).string(message.sender);
        }
        if (message.value !== "") {
            writer.uint32(18).string(message.value);
        }
        if (message.to !== "") {
            writer.uint32(26).string(message.to);
        }
        if (message.data.length !== 0) {
            writer.uint32(34).bytes(message.data);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseMsgInternalEVMCall();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.sender = reader.string();
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.value = reader.string();
                    continue;
                case 3:
                    if (tag !== 26) {
                        break;
                    }
                    message.to = reader.string();
                    continue;
                case 4:
                    if (tag !== 34) {
                        break;
                    }
                    message.data = reader.bytes();
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
            sender: isSet(object.sender) ? globalThis.String(object.sender) : "",
            value: isSet(object.value) ? globalThis.String(object.value) : "",
            to: isSet(object.to) ? globalThis.String(object.to) : "",
            data: isSet(object.data) ? bytesFromBase64(object.data) : new Uint8Array(0)
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.sender !== "") {
            obj.sender = message.sender;
        }
        if (message.value !== "") {
            obj.value = message.value;
        }
        if (message.to !== "") {
            obj.to = message.to;
        }
        if (message.data.length !== 0) {
            obj.data = base64FromBytes(message.data);
        }
        return obj;
    },
    create(base) {
        return MsgInternalEVMCall.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseMsgInternalEVMCall();
        message.sender = object.sender ?? "";
        message.value = object.value ?? "";
        message.to = object.to ?? "";
        message.data = object.data ?? new Uint8Array(0);
        return message;
    }
};
export const MsgInternalEVMCallResponse = {
    $type: "paxprotocol.paxchain.evm.MsgInternalEVMCallResponse",
    encode(_, writer = new BinaryWriter()) {
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseMsgInternalEVMCallResponse();
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
        return MsgInternalEVMCallResponse.fromPartial(base ?? {});
    },
    fromPartial(_) {
        const message = createBaseMsgInternalEVMCallResponse();
        return message;
    }
};
export const MsgInternalEVMDelegateCall = {
    $type: "paxprotocol.paxchain.evm.MsgInternalEVMDelegateCall",
    encode(message, writer = new BinaryWriter()) {
        if (message.sender !== "") {
            writer.uint32(10).string(message.sender);
        }
        if (message.codeHash.length !== 0) {
            writer.uint32(18).bytes(message.codeHash);
        }
        if (message.to !== "") {
            writer.uint32(26).string(message.to);
        }
        if (message.data.length !== 0) {
            writer.uint32(34).bytes(message.data);
        }
        if (message.fromContract !== "") {
            writer.uint32(42).string(message.fromContract);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseMsgInternalEVMDelegateCall();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.sender = reader.string();
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.codeHash = reader.bytes();
                    continue;
                case 3:
                    if (tag !== 26) {
                        break;
                    }
                    message.to = reader.string();
                    continue;
                case 4:
                    if (tag !== 34) {
                        break;
                    }
                    message.data = reader.bytes();
                    continue;
                case 5:
                    if (tag !== 42) {
                        break;
                    }
                    message.fromContract = reader.string();
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
            sender: isSet(object.sender) ? globalThis.String(object.sender) : "",
            codeHash: isSet(object.codeHash) ? bytesFromBase64(object.codeHash) : new Uint8Array(0),
            to: isSet(object.to) ? globalThis.String(object.to) : "",
            data: isSet(object.data) ? bytesFromBase64(object.data) : new Uint8Array(0),
            fromContract: isSet(object.fromContract) ? globalThis.String(object.fromContract) : ""
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.sender !== "") {
            obj.sender = message.sender;
        }
        if (message.codeHash.length !== 0) {
            obj.codeHash = base64FromBytes(message.codeHash);
        }
        if (message.to !== "") {
            obj.to = message.to;
        }
        if (message.data.length !== 0) {
            obj.data = base64FromBytes(message.data);
        }
        if (message.fromContract !== "") {
            obj.fromContract = message.fromContract;
        }
        return obj;
    },
    create(base) {
        return MsgInternalEVMDelegateCall.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseMsgInternalEVMDelegateCall();
        message.sender = object.sender ?? "";
        message.codeHash = object.codeHash ?? new Uint8Array(0);
        message.to = object.to ?? "";
        message.data = object.data ?? new Uint8Array(0);
        message.fromContract = object.fromContract ?? "";
        return message;
    }
};
export const MsgInternalEVMDelegateCallResponse = {
    $type: "paxprotocol.paxchain.evm.MsgInternalEVMDelegateCallResponse",
    encode(_, writer = new BinaryWriter()) {
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseMsgInternalEVMDelegateCallResponse();
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
        return MsgInternalEVMDelegateCallResponse.fromPartial(base ?? {});
    },
    fromPartial(_) {
        const message = createBaseMsgInternalEVMDelegateCallResponse();
        return message;
    }
};
export const MsgSend = {
    $type: "paxprotocol.paxchain.evm.MsgSend",
    encode(message, writer = new BinaryWriter()) {
        if (message.from_address !== "") {
            writer.uint32(10).string(message.from_address);
        }
        if (message.to_address !== "") {
            writer.uint32(18).string(message.to_address);
        }
        for (const v of message.amount) {
            Coin.encode(v, writer.uint32(26).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseMsgSend();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.from_address = reader.string();
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.to_address = reader.string();
                    continue;
                case 3:
                    if (tag !== 26) {
                        break;
                    }
                    message.amount.push(Coin.decode(reader, reader.uint32()));
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
            from_address: isSet(object.from_address) ? globalThis.String(object.from_address) : "",
            to_address: isSet(object.to_address) ? globalThis.String(object.to_address) : "",
            amount: globalThis.Array.isArray(object?.amount) ? object.amount.map((e) => Coin.fromJSON(e)) : []
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.from_address !== "") {
            obj.from_address = message.from_address;
        }
        if (message.to_address !== "") {
            obj.to_address = message.to_address;
        }
        if (message.amount?.length) {
            obj.amount = message.amount.map((e) => Coin.toJSON(e));
        }
        return obj;
    },
    create(base) {
        return MsgSend.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseMsgSend();
        message.from_address = object.from_address ?? "";
        message.to_address = object.to_address ?? "";
        message.amount = object.amount?.map((e) => Coin.fromPartial(e)) || [];
        return message;
    }
};
export const MsgSendResponse = {
    $type: "paxprotocol.paxchain.evm.MsgSendResponse",
    encode(_, writer = new BinaryWriter()) {
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseMsgSendResponse();
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
        return MsgSendResponse.fromPartial(base ?? {});
    },
    fromPartial(_) {
        const message = createBaseMsgSendResponse();
        return message;
    }
};
export const MsgRegisterPointer = {
    $type: "paxprotocol.paxchain.evm.MsgRegisterPointer",
    encode(message, writer = new BinaryWriter()) {
        if (message.sender !== "") {
            writer.uint32(10).string(message.sender);
        }
        if (message.pointer_type !== 0) {
            writer.uint32(16).int32(message.pointer_type);
        }
        if (message.erc_address !== "") {
            writer.uint32(26).string(message.erc_address);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseMsgRegisterPointer();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.sender = reader.string();
                    continue;
                case 2:
                    if (tag !== 16) {
                        break;
                    }
                    message.pointer_type = reader.int32();
                    continue;
                case 3:
                    if (tag !== 26) {
                        break;
                    }
                    message.erc_address = reader.string();
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
            sender: isSet(object.sender) ? globalThis.String(object.sender) : "",
            pointer_type: isSet(object.pointer_type) ? pointerTypeFromJSON(object.pointer_type) : 0,
            erc_address: isSet(object.erc_address) ? globalThis.String(object.erc_address) : ""
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.sender !== "") {
            obj.sender = message.sender;
        }
        if (message.pointer_type !== 0) {
            obj.pointer_type = pointerTypeToJSON(message.pointer_type);
        }
        if (message.erc_address !== "") {
            obj.erc_address = message.erc_address;
        }
        return obj;
    },
    create(base) {
        return MsgRegisterPointer.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseMsgRegisterPointer();
        message.sender = object.sender ?? "";
        message.pointer_type = object.pointer_type ?? 0;
        message.erc_address = object.erc_address ?? "";
        return message;
    }
};
export const MsgRegisterPointerResponse = {
    $type: "paxprotocol.paxchain.evm.MsgRegisterPointerResponse",
    encode(message, writer = new BinaryWriter()) {
        if (message.pointer_address !== "") {
            writer.uint32(10).string(message.pointer_address);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseMsgRegisterPointerResponse();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.pointer_address = reader.string();
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
        return { pointer_address: isSet(object.pointer_address) ? globalThis.String(object.pointer_address) : "" };
    },
    toJSON(message) {
        const obj = {};
        if (message.pointer_address !== "") {
            obj.pointer_address = message.pointer_address;
        }
        return obj;
    },
    create(base) {
        return MsgRegisterPointerResponse.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseMsgRegisterPointerResponse();
        message.pointer_address = object.pointer_address ?? "";
        return message;
    }
};
export const MsgAssociateContractAddress = {
    $type: "paxprotocol.paxchain.evm.MsgAssociateContractAddress",
    encode(message, writer = new BinaryWriter()) {
        if (message.sender !== "") {
            writer.uint32(10).string(message.sender);
        }
        if (message.address !== "") {
            writer.uint32(18).string(message.address);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseMsgAssociateContractAddress();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.sender = reader.string();
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.address = reader.string();
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
            sender: isSet(object.sender) ? globalThis.String(object.sender) : "",
            address: isSet(object.address) ? globalThis.String(object.address) : ""
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.sender !== "") {
            obj.sender = message.sender;
        }
        if (message.address !== "") {
            obj.address = message.address;
        }
        return obj;
    },
    create(base) {
        return MsgAssociateContractAddress.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseMsgAssociateContractAddress();
        message.sender = object.sender ?? "";
        message.address = object.address ?? "";
        return message;
    }
};
export const MsgAssociateContractAddressResponse = {
    $type: "paxprotocol.paxchain.evm.MsgAssociateContractAddressResponse",
    encode(_, writer = new BinaryWriter()) {
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseMsgAssociateContractAddressResponse();
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
        return MsgAssociateContractAddressResponse.fromPartial(base ?? {});
    },
    fromPartial(_) {
        const message = createBaseMsgAssociateContractAddressResponse();
        return message;
    }
};
export const MsgAssociate = {
    $type: "paxprotocol.paxchain.evm.MsgAssociate",
    encode(message, writer = new BinaryWriter()) {
        if (message.sender !== "") {
            writer.uint32(10).string(message.sender);
        }
        if (message.custom_message !== "") {
            writer.uint32(18).string(message.custom_message);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseMsgAssociate();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.sender = reader.string();
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.custom_message = reader.string();
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
            sender: isSet(object.sender) ? globalThis.String(object.sender) : "",
            custom_message: isSet(object.custom_message) ? globalThis.String(object.custom_message) : ""
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.sender !== "") {
            obj.sender = message.sender;
        }
        if (message.custom_message !== "") {
            obj.custom_message = message.custom_message;
        }
        return obj;
    },
    create(base) {
        return MsgAssociate.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseMsgAssociate();
        message.sender = object.sender ?? "";
        message.custom_message = object.custom_message ?? "";
        return message;
    }
};
export const MsgAssociateResponse = {
    $type: "paxprotocol.paxchain.evm.MsgAssociateResponse",
    encode(_, writer = new BinaryWriter()) {
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseMsgAssociateResponse();
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
        return MsgAssociateResponse.fromPartial(base ?? {});
    },
    fromPartial(_) {
        const message = createBaseMsgAssociateResponse();
        return message;
    }
};
export const MsgClaim = {
    $type: "paxprotocol.paxchain.evm.MsgClaim",
    encode(message, writer = new BinaryWriter()) {
        if (message.sender !== "") {
            writer.uint32(10).string(message.sender);
        }
        if (message.claimer !== "") {
            writer.uint32(18).string(message.claimer);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseMsgClaim();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.sender = reader.string();
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.claimer = reader.string();
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
            sender: isSet(object.sender) ? globalThis.String(object.sender) : "",
            claimer: isSet(object.claimer) ? globalThis.String(object.claimer) : ""
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.sender !== "") {
            obj.sender = message.sender;
        }
        if (message.claimer !== "") {
            obj.claimer = message.claimer;
        }
        return obj;
    },
    create(base) {
        return MsgClaim.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseMsgClaim();
        message.sender = object.sender ?? "";
        message.claimer = object.claimer ?? "";
        return message;
    }
};
export const Asset = {
    $type: "paxprotocol.paxchain.evm.Asset",
    encode(message, writer = new BinaryWriter()) {
        if (message.asset_type !== 0) {
            writer.uint32(8).int32(message.asset_type);
        }
        if (message.contract_address !== "") {
            writer.uint32(18).string(message.contract_address);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseAsset();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 8) {
                        break;
                    }
                    message.asset_type = reader.int32();
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.contract_address = reader.string();
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
            asset_type: isSet(object.asset_type) ? assetTypeFromJSON(object.asset_type) : 0,
            contract_address: isSet(object.contract_address) ? globalThis.String(object.contract_address) : ""
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.asset_type !== 0) {
            obj.asset_type = assetTypeToJSON(message.asset_type);
        }
        if (message.contract_address !== "") {
            obj.contract_address = message.contract_address;
        }
        return obj;
    },
    create(base) {
        return Asset.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseAsset();
        message.asset_type = object.asset_type ?? 0;
        message.contract_address = object.contract_address ?? "";
        return message;
    }
};
export const MsgClaimSpecific = {
    $type: "paxprotocol.paxchain.evm.MsgClaimSpecific",
    encode(message, writer = new BinaryWriter()) {
        if (message.sender !== "") {
            writer.uint32(10).string(message.sender);
        }
        if (message.claimer !== "") {
            writer.uint32(18).string(message.claimer);
        }
        for (const v of message.assets) {
            Asset.encode(v, writer.uint32(26).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseMsgClaimSpecific();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.sender = reader.string();
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.claimer = reader.string();
                    continue;
                case 3:
                    if (tag !== 26) {
                        break;
                    }
                    message.assets.push(Asset.decode(reader, reader.uint32()));
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
            sender: isSet(object.sender) ? globalThis.String(object.sender) : "",
            claimer: isSet(object.claimer) ? globalThis.String(object.claimer) : "",
            assets: globalThis.Array.isArray(object?.assets) ? object.assets.map((e) => Asset.fromJSON(e)) : []
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.sender !== "") {
            obj.sender = message.sender;
        }
        if (message.claimer !== "") {
            obj.claimer = message.claimer;
        }
        if (message.assets?.length) {
            obj.assets = message.assets.map((e) => Asset.toJSON(e));
        }
        return obj;
    },
    create(base) {
        return MsgClaimSpecific.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseMsgClaimSpecific();
        message.sender = object.sender ?? "";
        message.claimer = object.claimer ?? "";
        message.assets = object.assets?.map((e) => Asset.fromPartial(e)) || [];
        return message;
    }
};
function createBaseMsgEVMTransaction() {
    return { data: undefined, derived: new Uint8Array(0) };
}
function createBaseMsgEVMTransactionResponse() {
    return { gas_used: 0, vm_error: "", return_data: new Uint8Array(0), hash: "", logs: [] };
}
function createBaseMsgInternalEVMCall() {
    return { sender: "", value: "", to: "", data: new Uint8Array(0) };
}
function createBaseMsgInternalEVMCallResponse() {
    return {};
}
function createBaseMsgInternalEVMDelegateCall() {
    return { sender: "", codeHash: new Uint8Array(0), to: "", data: new Uint8Array(0), fromContract: "" };
}
function createBaseMsgInternalEVMDelegateCallResponse() {
    return {};
}
function createBaseMsgSend() {
    return { from_address: "", to_address: "", amount: [] };
}
function createBaseMsgSendResponse() {
    return {};
}
function createBaseMsgRegisterPointer() {
    return { sender: "", pointer_type: 0, erc_address: "" };
}
function createBaseMsgRegisterPointerResponse() {
    return { pointer_address: "" };
}
function createBaseMsgAssociateContractAddress() {
    return { sender: "", address: "" };
}
function createBaseMsgAssociateContractAddressResponse() {
    return {};
}
function createBaseMsgAssociate() {
    return { sender: "", custom_message: "" };
}
function createBaseMsgAssociateResponse() {
    return {};
}
function createBaseMsgClaim() {
    return { sender: "", claimer: "" };
}
function createBaseAsset() {
    return { asset_type: 0, contract_address: "" };
}
function createBaseMsgClaimSpecific() {
    return { sender: "", claimer: "", assets: [] };
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
function longToNumber(int64) {
    const num = globalThis.Number(int64.toString());
    if (num > globalThis.Number.MAX_SAFE_INTEGER) {
        throw new globalThis.Error("Value is larger than Number.MAX_SAFE_INTEGER");
    }
    if (num < globalThis.Number.MIN_SAFE_INTEGER) {
        throw new globalThis.Error("Value is smaller than Number.MIN_SAFE_INTEGER");
    }
    return num;
}
function isSet(value) {
    return value !== null && value !== undefined;
}
export const registry = [
    ["/paxprotocol.paxchain.evm.MsgEVMTransaction", MsgEVMTransaction],
    ["/paxprotocol.paxchain.evm.MsgEVMTransactionResponse", MsgEVMTransactionResponse],
    ["/paxprotocol.paxchain.evm.MsgInternalEVMCall", MsgInternalEVMCall],
    ["/paxprotocol.paxchain.evm.MsgInternalEVMCallResponse", MsgInternalEVMCallResponse],
    ["/paxprotocol.paxchain.evm.MsgInternalEVMDelegateCall", MsgInternalEVMDelegateCall],
    ["/paxprotocol.paxchain.evm.MsgInternalEVMDelegateCallResponse", MsgInternalEVMDelegateCallResponse],
    ["/paxprotocol.paxchain.evm.MsgSend", MsgSend],
    ["/paxprotocol.paxchain.evm.MsgSendResponse", MsgSendResponse],
    ["/paxprotocol.paxchain.evm.MsgRegisterPointer", MsgRegisterPointer],
    ["/paxprotocol.paxchain.evm.MsgRegisterPointerResponse", MsgRegisterPointerResponse],
    ["/paxprotocol.paxchain.evm.MsgAssociateContractAddress", MsgAssociateContractAddress],
    ["/paxprotocol.paxchain.evm.MsgAssociateContractAddressResponse", MsgAssociateContractAddressResponse],
    ["/paxprotocol.paxchain.evm.MsgAssociate", MsgAssociate],
    ["/paxprotocol.paxchain.evm.MsgAssociateResponse", MsgAssociateResponse],
    ["/paxprotocol.paxchain.evm.MsgClaim", MsgClaim],
    ["/paxprotocol.paxchain.evm.Asset", Asset],
    ["/paxprotocol.paxchain.evm.MsgClaimSpecific", MsgClaimSpecific]
];
export const aminoConverters = {
    "/paxprotocol.paxchain.evm.MsgEVMTransaction": {
        aminoType: "evm/MsgEVMTransaction",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/paxprotocol.paxchain.evm.MsgEVMTransactionResponse": {
        aminoType: "evm/MsgEVMTransactionResponse",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/paxprotocol.paxchain.evm.MsgInternalEVMCall": {
        aminoType: "evm/MsgInternalEVMCall",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/paxprotocol.paxchain.evm.MsgInternalEVMCallResponse": {
        aminoType: "evm/MsgInternalEVMCallResponse",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/paxprotocol.paxchain.evm.MsgInternalEVMDelegateCall": {
        aminoType: "evm/MsgInternalEVMDelegateCall",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/paxprotocol.paxchain.evm.MsgInternalEVMDelegateCallResponse": {
        aminoType: "evm/MsgInternalEVMDelegateCallResponse",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/paxprotocol.paxchain.evm.MsgSend": {
        aminoType: "evm/MsgSend",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/paxprotocol.paxchain.evm.MsgSendResponse": {
        aminoType: "evm/MsgSendResponse",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/paxprotocol.paxchain.evm.MsgRegisterPointer": {
        aminoType: "evm/MsgRegisterPointer",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/paxprotocol.paxchain.evm.MsgRegisterPointerResponse": {
        aminoType: "evm/MsgRegisterPointerResponse",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/paxprotocol.paxchain.evm.MsgAssociateContractAddress": {
        aminoType: "evm/MsgAssociateContractAddress",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/paxprotocol.paxchain.evm.MsgAssociateContractAddressResponse": {
        aminoType: "evm/MsgAssociateContractAddressResponse",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/paxprotocol.paxchain.evm.MsgAssociate": {
        aminoType: "evm/MsgAssociate",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/paxprotocol.paxchain.evm.MsgAssociateResponse": {
        aminoType: "evm/MsgAssociateResponse",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/paxprotocol.paxchain.evm.MsgClaim": {
        aminoType: "evm/MsgClaim",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/paxprotocol.paxchain.evm.Asset": {
        aminoType: "evm/Asset",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/paxprotocol.paxchain.evm.MsgClaimSpecific": {
        aminoType: "evm/MsgClaimSpecific",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    }
};
