"use strict";
Object.defineProperty(exports, "__esModule", { value: true });
exports.aminoConverters = exports.registry = exports.Fee = exports.ModeInfoMulti = exports.ModeInfoSingle = exports.ModeInfo = exports.SignerInfo = exports.AuthInfo = exports.TxBody = exports.SignDoc = exports.TxRaw = exports.Tx = void 0;
const wire_1 = require("@bufbuild/protobuf/wire");
const any_1 = require("../../../google/protobuf/any");
const coin_1 = require("../../base/v1beta1/coin");
const multisig_1 = require("../../crypto/multisig/v1beta1/multisig");
const signing_1 = require("../signing/v1beta1/signing");
exports.Tx = {
    $type: "cosmos.tx.v1beta1.Tx",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.body !== undefined) {
            exports.TxBody.encode(message.body, writer.uint32(10).fork()).join();
        }
        if (message.auth_info !== undefined) {
            exports.AuthInfo.encode(message.auth_info, writer.uint32(18).fork()).join();
        }
        for (const v of message.signatures) {
            writer.uint32(26).bytes(v);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseTx();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.body = exports.TxBody.decode(reader, reader.uint32());
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.auth_info = exports.AuthInfo.decode(reader, reader.uint32());
                    continue;
                case 3:
                    if (tag !== 26) {
                        break;
                    }
                    message.signatures.push(reader.bytes());
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
            body: isSet(object.body) ? exports.TxBody.fromJSON(object.body) : undefined,
            auth_info: isSet(object.auth_info) ? exports.AuthInfo.fromJSON(object.auth_info) : undefined,
            signatures: globalThis.Array.isArray(object?.signatures) ? object.signatures.map((e) => bytesFromBase64(e)) : []
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.body !== undefined) {
            obj.body = exports.TxBody.toJSON(message.body);
        }
        if (message.auth_info !== undefined) {
            obj.auth_info = exports.AuthInfo.toJSON(message.auth_info);
        }
        if (message.signatures?.length) {
            obj.signatures = message.signatures.map((e) => base64FromBytes(e));
        }
        return obj;
    },
    create(base) {
        return exports.Tx.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseTx();
        message.body = object.body !== undefined && object.body !== null ? exports.TxBody.fromPartial(object.body) : undefined;
        message.auth_info = object.auth_info !== undefined && object.auth_info !== null ? exports.AuthInfo.fromPartial(object.auth_info) : undefined;
        message.signatures = object.signatures?.map((e) => e) || [];
        return message;
    }
};
exports.TxRaw = {
    $type: "cosmos.tx.v1beta1.TxRaw",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.body_bytes.length !== 0) {
            writer.uint32(10).bytes(message.body_bytes);
        }
        if (message.auth_info_bytes.length !== 0) {
            writer.uint32(18).bytes(message.auth_info_bytes);
        }
        for (const v of message.signatures) {
            writer.uint32(26).bytes(v);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseTxRaw();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.body_bytes = reader.bytes();
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.auth_info_bytes = reader.bytes();
                    continue;
                case 3:
                    if (tag !== 26) {
                        break;
                    }
                    message.signatures.push(reader.bytes());
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
            body_bytes: isSet(object.body_bytes) ? bytesFromBase64(object.body_bytes) : new Uint8Array(0),
            auth_info_bytes: isSet(object.auth_info_bytes) ? bytesFromBase64(object.auth_info_bytes) : new Uint8Array(0),
            signatures: globalThis.Array.isArray(object?.signatures) ? object.signatures.map((e) => bytesFromBase64(e)) : []
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.body_bytes.length !== 0) {
            obj.body_bytes = base64FromBytes(message.body_bytes);
        }
        if (message.auth_info_bytes.length !== 0) {
            obj.auth_info_bytes = base64FromBytes(message.auth_info_bytes);
        }
        if (message.signatures?.length) {
            obj.signatures = message.signatures.map((e) => base64FromBytes(e));
        }
        return obj;
    },
    create(base) {
        return exports.TxRaw.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseTxRaw();
        message.body_bytes = object.body_bytes ?? new Uint8Array(0);
        message.auth_info_bytes = object.auth_info_bytes ?? new Uint8Array(0);
        message.signatures = object.signatures?.map((e) => e) || [];
        return message;
    }
};
exports.SignDoc = {
    $type: "cosmos.tx.v1beta1.SignDoc",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.body_bytes.length !== 0) {
            writer.uint32(10).bytes(message.body_bytes);
        }
        if (message.auth_info_bytes.length !== 0) {
            writer.uint32(18).bytes(message.auth_info_bytes);
        }
        if (message.chain_id !== "") {
            writer.uint32(26).string(message.chain_id);
        }
        if (message.account_number !== 0) {
            writer.uint32(32).uint64(message.account_number);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseSignDoc();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.body_bytes = reader.bytes();
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.auth_info_bytes = reader.bytes();
                    continue;
                case 3:
                    if (tag !== 26) {
                        break;
                    }
                    message.chain_id = reader.string();
                    continue;
                case 4:
                    if (tag !== 32) {
                        break;
                    }
                    message.account_number = longToNumber(reader.uint64());
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
            body_bytes: isSet(object.body_bytes) ? bytesFromBase64(object.body_bytes) : new Uint8Array(0),
            auth_info_bytes: isSet(object.auth_info_bytes) ? bytesFromBase64(object.auth_info_bytes) : new Uint8Array(0),
            chain_id: isSet(object.chain_id) ? globalThis.String(object.chain_id) : "",
            account_number: isSet(object.account_number) ? globalThis.Number(object.account_number) : 0
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.body_bytes.length !== 0) {
            obj.body_bytes = base64FromBytes(message.body_bytes);
        }
        if (message.auth_info_bytes.length !== 0) {
            obj.auth_info_bytes = base64FromBytes(message.auth_info_bytes);
        }
        if (message.chain_id !== "") {
            obj.chain_id = message.chain_id;
        }
        if (message.account_number !== 0) {
            obj.account_number = Math.round(message.account_number);
        }
        return obj;
    },
    create(base) {
        return exports.SignDoc.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseSignDoc();
        message.body_bytes = object.body_bytes ?? new Uint8Array(0);
        message.auth_info_bytes = object.auth_info_bytes ?? new Uint8Array(0);
        message.chain_id = object.chain_id ?? "";
        message.account_number = object.account_number ?? 0;
        return message;
    }
};
exports.TxBody = {
    $type: "cosmos.tx.v1beta1.TxBody",
    encode(message, writer = new wire_1.BinaryWriter()) {
        for (const v of message.messages) {
            any_1.Any.encode(v, writer.uint32(10).fork()).join();
        }
        if (message.memo !== "") {
            writer.uint32(18).string(message.memo);
        }
        if (message.timeout_height !== 0) {
            writer.uint32(24).uint64(message.timeout_height);
        }
        for (const v of message.extension_options) {
            any_1.Any.encode(v, writer.uint32(8186).fork()).join();
        }
        for (const v of message.non_critical_extension_options) {
            any_1.Any.encode(v, writer.uint32(16378).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseTxBody();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.messages.push(any_1.Any.decode(reader, reader.uint32()));
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.memo = reader.string();
                    continue;
                case 3:
                    if (tag !== 24) {
                        break;
                    }
                    message.timeout_height = longToNumber(reader.uint64());
                    continue;
                case 1023:
                    if (tag !== 8186) {
                        break;
                    }
                    message.extension_options.push(any_1.Any.decode(reader, reader.uint32()));
                    continue;
                case 2047:
                    if (tag !== 16378) {
                        break;
                    }
                    message.non_critical_extension_options.push(any_1.Any.decode(reader, reader.uint32()));
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
            messages: globalThis.Array.isArray(object?.messages) ? object.messages.map((e) => any_1.Any.fromJSON(e)) : [],
            memo: isSet(object.memo) ? globalThis.String(object.memo) : "",
            timeout_height: isSet(object.timeout_height) ? globalThis.Number(object.timeout_height) : 0,
            extension_options: globalThis.Array.isArray(object?.extension_options) ? object.extension_options.map((e) => any_1.Any.fromJSON(e)) : [],
            non_critical_extension_options: globalThis.Array.isArray(object?.non_critical_extension_options)
                ? object.non_critical_extension_options.map((e) => any_1.Any.fromJSON(e))
                : []
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.messages?.length) {
            obj.messages = message.messages.map((e) => any_1.Any.toJSON(e));
        }
        if (message.memo !== "") {
            obj.memo = message.memo;
        }
        if (message.timeout_height !== 0) {
            obj.timeout_height = Math.round(message.timeout_height);
        }
        if (message.extension_options?.length) {
            obj.extension_options = message.extension_options.map((e) => any_1.Any.toJSON(e));
        }
        if (message.non_critical_extension_options?.length) {
            obj.non_critical_extension_options = message.non_critical_extension_options.map((e) => any_1.Any.toJSON(e));
        }
        return obj;
    },
    create(base) {
        return exports.TxBody.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseTxBody();
        message.messages = object.messages?.map((e) => any_1.Any.fromPartial(e)) || [];
        message.memo = object.memo ?? "";
        message.timeout_height = object.timeout_height ?? 0;
        message.extension_options = object.extension_options?.map((e) => any_1.Any.fromPartial(e)) || [];
        message.non_critical_extension_options = object.non_critical_extension_options?.map((e) => any_1.Any.fromPartial(e)) || [];
        return message;
    }
};
exports.AuthInfo = {
    $type: "cosmos.tx.v1beta1.AuthInfo",
    encode(message, writer = new wire_1.BinaryWriter()) {
        for (const v of message.signer_infos) {
            exports.SignerInfo.encode(v, writer.uint32(10).fork()).join();
        }
        if (message.fee !== undefined) {
            exports.Fee.encode(message.fee, writer.uint32(18).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseAuthInfo();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.signer_infos.push(exports.SignerInfo.decode(reader, reader.uint32()));
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.fee = exports.Fee.decode(reader, reader.uint32());
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
            signer_infos: globalThis.Array.isArray(object?.signer_infos) ? object.signer_infos.map((e) => exports.SignerInfo.fromJSON(e)) : [],
            fee: isSet(object.fee) ? exports.Fee.fromJSON(object.fee) : undefined
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.signer_infos?.length) {
            obj.signer_infos = message.signer_infos.map((e) => exports.SignerInfo.toJSON(e));
        }
        if (message.fee !== undefined) {
            obj.fee = exports.Fee.toJSON(message.fee);
        }
        return obj;
    },
    create(base) {
        return exports.AuthInfo.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseAuthInfo();
        message.signer_infos = object.signer_infos?.map((e) => exports.SignerInfo.fromPartial(e)) || [];
        message.fee = object.fee !== undefined && object.fee !== null ? exports.Fee.fromPartial(object.fee) : undefined;
        return message;
    }
};
exports.SignerInfo = {
    $type: "cosmos.tx.v1beta1.SignerInfo",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.public_key !== undefined) {
            any_1.Any.encode(message.public_key, writer.uint32(10).fork()).join();
        }
        if (message.mode_info !== undefined) {
            exports.ModeInfo.encode(message.mode_info, writer.uint32(18).fork()).join();
        }
        if (message.sequence !== 0) {
            writer.uint32(24).uint64(message.sequence);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseSignerInfo();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.public_key = any_1.Any.decode(reader, reader.uint32());
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.mode_info = exports.ModeInfo.decode(reader, reader.uint32());
                    continue;
                case 3:
                    if (tag !== 24) {
                        break;
                    }
                    message.sequence = longToNumber(reader.uint64());
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
            public_key: isSet(object.public_key) ? any_1.Any.fromJSON(object.public_key) : undefined,
            mode_info: isSet(object.mode_info) ? exports.ModeInfo.fromJSON(object.mode_info) : undefined,
            sequence: isSet(object.sequence) ? globalThis.Number(object.sequence) : 0
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.public_key !== undefined) {
            obj.public_key = any_1.Any.toJSON(message.public_key);
        }
        if (message.mode_info !== undefined) {
            obj.mode_info = exports.ModeInfo.toJSON(message.mode_info);
        }
        if (message.sequence !== 0) {
            obj.sequence = Math.round(message.sequence);
        }
        return obj;
    },
    create(base) {
        return exports.SignerInfo.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseSignerInfo();
        message.public_key = object.public_key !== undefined && object.public_key !== null ? any_1.Any.fromPartial(object.public_key) : undefined;
        message.mode_info = object.mode_info !== undefined && object.mode_info !== null ? exports.ModeInfo.fromPartial(object.mode_info) : undefined;
        message.sequence = object.sequence ?? 0;
        return message;
    }
};
exports.ModeInfo = {
    $type: "cosmos.tx.v1beta1.ModeInfo",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.single !== undefined) {
            exports.ModeInfoSingle.encode(message.single, writer.uint32(10).fork()).join();
        }
        if (message.multi !== undefined) {
            exports.ModeInfoMulti.encode(message.multi, writer.uint32(18).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseModeInfo();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.single = exports.ModeInfoSingle.decode(reader, reader.uint32());
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.multi = exports.ModeInfoMulti.decode(reader, reader.uint32());
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
            single: isSet(object.single) ? exports.ModeInfoSingle.fromJSON(object.single) : undefined,
            multi: isSet(object.multi) ? exports.ModeInfoMulti.fromJSON(object.multi) : undefined
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.single !== undefined) {
            obj.single = exports.ModeInfoSingle.toJSON(message.single);
        }
        if (message.multi !== undefined) {
            obj.multi = exports.ModeInfoMulti.toJSON(message.multi);
        }
        return obj;
    },
    create(base) {
        return exports.ModeInfo.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseModeInfo();
        message.single = object.single !== undefined && object.single !== null ? exports.ModeInfoSingle.fromPartial(object.single) : undefined;
        message.multi = object.multi !== undefined && object.multi !== null ? exports.ModeInfoMulti.fromPartial(object.multi) : undefined;
        return message;
    }
};
exports.ModeInfoSingle = {
    $type: "cosmos.tx.v1beta1.ModeInfo.Single",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.mode !== 0) {
            writer.uint32(8).int32(message.mode);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseModeInfoSingle();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 8) {
                        break;
                    }
                    message.mode = reader.int32();
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
        return { mode: isSet(object.mode) ? (0, signing_1.signModeFromJSON)(object.mode) : 0 };
    },
    toJSON(message) {
        const obj = {};
        if (message.mode !== 0) {
            obj.mode = (0, signing_1.signModeToJSON)(message.mode);
        }
        return obj;
    },
    create(base) {
        return exports.ModeInfoSingle.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseModeInfoSingle();
        message.mode = object.mode ?? 0;
        return message;
    }
};
exports.ModeInfoMulti = {
    $type: "cosmos.tx.v1beta1.ModeInfo.Multi",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.bitarray !== undefined) {
            multisig_1.CompactBitArray.encode(message.bitarray, writer.uint32(10).fork()).join();
        }
        for (const v of message.mode_infos) {
            exports.ModeInfo.encode(v, writer.uint32(18).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseModeInfoMulti();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.bitarray = multisig_1.CompactBitArray.decode(reader, reader.uint32());
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.mode_infos.push(exports.ModeInfo.decode(reader, reader.uint32()));
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
            bitarray: isSet(object.bitarray) ? multisig_1.CompactBitArray.fromJSON(object.bitarray) : undefined,
            mode_infos: globalThis.Array.isArray(object?.mode_infos) ? object.mode_infos.map((e) => exports.ModeInfo.fromJSON(e)) : []
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.bitarray !== undefined) {
            obj.bitarray = multisig_1.CompactBitArray.toJSON(message.bitarray);
        }
        if (message.mode_infos?.length) {
            obj.mode_infos = message.mode_infos.map((e) => exports.ModeInfo.toJSON(e));
        }
        return obj;
    },
    create(base) {
        return exports.ModeInfoMulti.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseModeInfoMulti();
        message.bitarray = object.bitarray !== undefined && object.bitarray !== null ? multisig_1.CompactBitArray.fromPartial(object.bitarray) : undefined;
        message.mode_infos = object.mode_infos?.map((e) => exports.ModeInfo.fromPartial(e)) || [];
        return message;
    }
};
exports.Fee = {
    $type: "cosmos.tx.v1beta1.Fee",
    encode(message, writer = new wire_1.BinaryWriter()) {
        for (const v of message.amount) {
            coin_1.Coin.encode(v, writer.uint32(10).fork()).join();
        }
        if (message.gas_limit !== 0) {
            writer.uint32(16).uint64(message.gas_limit);
        }
        if (message.payer !== "") {
            writer.uint32(26).string(message.payer);
        }
        if (message.granter !== "") {
            writer.uint32(34).string(message.granter);
        }
        if (message.gas_estimate !== 0) {
            writer.uint32(40).uint64(message.gas_estimate);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseFee();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.amount.push(coin_1.Coin.decode(reader, reader.uint32()));
                    continue;
                case 2:
                    if (tag !== 16) {
                        break;
                    }
                    message.gas_limit = longToNumber(reader.uint64());
                    continue;
                case 3:
                    if (tag !== 26) {
                        break;
                    }
                    message.payer = reader.string();
                    continue;
                case 4:
                    if (tag !== 34) {
                        break;
                    }
                    message.granter = reader.string();
                    continue;
                case 5:
                    if (tag !== 40) {
                        break;
                    }
                    message.gas_estimate = longToNumber(reader.uint64());
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
            amount: globalThis.Array.isArray(object?.amount) ? object.amount.map((e) => coin_1.Coin.fromJSON(e)) : [],
            gas_limit: isSet(object.gas_limit) ? globalThis.Number(object.gas_limit) : 0,
            payer: isSet(object.payer) ? globalThis.String(object.payer) : "",
            granter: isSet(object.granter) ? globalThis.String(object.granter) : "",
            gas_estimate: isSet(object.gas_estimate) ? globalThis.Number(object.gas_estimate) : 0
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.amount?.length) {
            obj.amount = message.amount.map((e) => coin_1.Coin.toJSON(e));
        }
        if (message.gas_limit !== 0) {
            obj.gas_limit = Math.round(message.gas_limit);
        }
        if (message.payer !== "") {
            obj.payer = message.payer;
        }
        if (message.granter !== "") {
            obj.granter = message.granter;
        }
        if (message.gas_estimate !== 0) {
            obj.gas_estimate = Math.round(message.gas_estimate);
        }
        return obj;
    },
    create(base) {
        return exports.Fee.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseFee();
        message.amount = object.amount?.map((e) => coin_1.Coin.fromPartial(e)) || [];
        message.gas_limit = object.gas_limit ?? 0;
        message.payer = object.payer ?? "";
        message.granter = object.granter ?? "";
        message.gas_estimate = object.gas_estimate ?? 0;
        return message;
    }
};
function createBaseTx() {
    return { body: undefined, auth_info: undefined, signatures: [] };
}
function createBaseTxRaw() {
    return { body_bytes: new Uint8Array(0), auth_info_bytes: new Uint8Array(0), signatures: [] };
}
function createBaseSignDoc() {
    return { body_bytes: new Uint8Array(0), auth_info_bytes: new Uint8Array(0), chain_id: "", account_number: 0 };
}
function createBaseTxBody() {
    return { messages: [], memo: "", timeout_height: 0, extension_options: [], non_critical_extension_options: [] };
}
function createBaseAuthInfo() {
    return { signer_infos: [], fee: undefined };
}
function createBaseSignerInfo() {
    return { public_key: undefined, mode_info: undefined, sequence: 0 };
}
function createBaseModeInfo() {
    return { single: undefined, multi: undefined };
}
function createBaseModeInfoSingle() {
    return { mode: 0 };
}
function createBaseModeInfoMulti() {
    return { bitarray: undefined, mode_infos: [] };
}
function createBaseFee() {
    return { amount: [], gas_limit: 0, payer: "", granter: "", gas_estimate: 0 };
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
exports.registry = [
    ["/cosmos.tx.v1beta1.Tx", exports.Tx],
    ["/cosmos.tx.v1beta1.TxRaw", exports.TxRaw],
    ["/cosmos.tx.v1beta1.SignDoc", exports.SignDoc],
    ["/cosmos.tx.v1beta1.TxBody", exports.TxBody],
    ["/cosmos.tx.v1beta1.AuthInfo", exports.AuthInfo],
    ["/cosmos.tx.v1beta1.SignerInfo", exports.SignerInfo],
    ["/cosmos.tx.v1beta1.ModeInfo", exports.ModeInfo],
    ["/cosmos.tx.v1beta1.ModeInfo.Single", exports.ModeInfoSingle],
    ["/cosmos.tx.v1beta1.ModeInfo.Multi", exports.ModeInfoMulti],
    ["/cosmos.tx.v1beta1.Fee", exports.Fee]
];
exports.aminoConverters = {
    "/cosmos.tx.v1beta1.Tx": {
        aminoType: "cosmos-sdk/Tx",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.tx.v1beta1.TxRaw": {
        aminoType: "cosmos-sdk/TxRaw",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.tx.v1beta1.SignDoc": {
        aminoType: "cosmos-sdk/SignDoc",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.tx.v1beta1.TxBody": {
        aminoType: "cosmos-sdk/TxBody",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.tx.v1beta1.AuthInfo": {
        aminoType: "cosmos-sdk/AuthInfo",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.tx.v1beta1.SignerInfo": {
        aminoType: "cosmos-sdk/SignerInfo",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.tx.v1beta1.ModeInfo": {
        aminoType: "cosmos-sdk/ModeInfo",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.tx.v1beta1.ModeInfo.Single": {
        aminoType: "cosmos-sdk/Single",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.tx.v1beta1.ModeInfo.Multi": {
        aminoType: "cosmos-sdk/Multi",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.tx.v1beta1.Fee": {
        aminoType: "cosmos-sdk/Fee",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    }
};
