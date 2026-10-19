"use strict";
Object.defineProperty(exports, "__esModule", { value: true });
exports.aminoConverters = exports.registry = exports.QueryMethodDescriptor = exports.QueryServiceDescriptor = exports.QueryServicesDescriptor = exports.GetTxDescriptorResponse = exports.GetTxDescriptorRequest = exports.GetQueryServicesDescriptorResponse = exports.GetQueryServicesDescriptorRequest = exports.GetConfigurationDescriptorResponse = exports.GetConfigurationDescriptorRequest = exports.GetCodecDescriptorResponse = exports.GetCodecDescriptorRequest = exports.GetChainDescriptorResponse = exports.GetChainDescriptorRequest = exports.GetAuthnDescriptorResponse = exports.GetAuthnDescriptorRequest = exports.MsgDescriptor = exports.ConfigurationDescriptor = exports.InterfaceAcceptingMessageDescriptor = exports.InterfaceImplementerDescriptor = exports.InterfaceDescriptor = exports.CodecDescriptor = exports.ChainDescriptor = exports.SigningModeDescriptor = exports.AuthnDescriptor = exports.TxDescriptor = exports.AppDescriptor = void 0;
const wire_1 = require("@bufbuild/protobuf/wire");
exports.AppDescriptor = {
    $type: "cosmos.base.reflection.v2alpha1.AppDescriptor",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.authn !== undefined) {
            exports.AuthnDescriptor.encode(message.authn, writer.uint32(10).fork()).join();
        }
        if (message.chain !== undefined) {
            exports.ChainDescriptor.encode(message.chain, writer.uint32(18).fork()).join();
        }
        if (message.codec !== undefined) {
            exports.CodecDescriptor.encode(message.codec, writer.uint32(26).fork()).join();
        }
        if (message.configuration !== undefined) {
            exports.ConfigurationDescriptor.encode(message.configuration, writer.uint32(34).fork()).join();
        }
        if (message.query_services !== undefined) {
            exports.QueryServicesDescriptor.encode(message.query_services, writer.uint32(42).fork()).join();
        }
        if (message.tx !== undefined) {
            exports.TxDescriptor.encode(message.tx, writer.uint32(50).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseAppDescriptor();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.authn = exports.AuthnDescriptor.decode(reader, reader.uint32());
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.chain = exports.ChainDescriptor.decode(reader, reader.uint32());
                    continue;
                case 3:
                    if (tag !== 26) {
                        break;
                    }
                    message.codec = exports.CodecDescriptor.decode(reader, reader.uint32());
                    continue;
                case 4:
                    if (tag !== 34) {
                        break;
                    }
                    message.configuration = exports.ConfigurationDescriptor.decode(reader, reader.uint32());
                    continue;
                case 5:
                    if (tag !== 42) {
                        break;
                    }
                    message.query_services = exports.QueryServicesDescriptor.decode(reader, reader.uint32());
                    continue;
                case 6:
                    if (tag !== 50) {
                        break;
                    }
                    message.tx = exports.TxDescriptor.decode(reader, reader.uint32());
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
            authn: isSet(object.authn) ? exports.AuthnDescriptor.fromJSON(object.authn) : undefined,
            chain: isSet(object.chain) ? exports.ChainDescriptor.fromJSON(object.chain) : undefined,
            codec: isSet(object.codec) ? exports.CodecDescriptor.fromJSON(object.codec) : undefined,
            configuration: isSet(object.configuration) ? exports.ConfigurationDescriptor.fromJSON(object.configuration) : undefined,
            query_services: isSet(object.query_services) ? exports.QueryServicesDescriptor.fromJSON(object.query_services) : undefined,
            tx: isSet(object.tx) ? exports.TxDescriptor.fromJSON(object.tx) : undefined
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.authn !== undefined) {
            obj.authn = exports.AuthnDescriptor.toJSON(message.authn);
        }
        if (message.chain !== undefined) {
            obj.chain = exports.ChainDescriptor.toJSON(message.chain);
        }
        if (message.codec !== undefined) {
            obj.codec = exports.CodecDescriptor.toJSON(message.codec);
        }
        if (message.configuration !== undefined) {
            obj.configuration = exports.ConfigurationDescriptor.toJSON(message.configuration);
        }
        if (message.query_services !== undefined) {
            obj.query_services = exports.QueryServicesDescriptor.toJSON(message.query_services);
        }
        if (message.tx !== undefined) {
            obj.tx = exports.TxDescriptor.toJSON(message.tx);
        }
        return obj;
    },
    create(base) {
        return exports.AppDescriptor.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseAppDescriptor();
        message.authn = object.authn !== undefined && object.authn !== null ? exports.AuthnDescriptor.fromPartial(object.authn) : undefined;
        message.chain = object.chain !== undefined && object.chain !== null ? exports.ChainDescriptor.fromPartial(object.chain) : undefined;
        message.codec = object.codec !== undefined && object.codec !== null ? exports.CodecDescriptor.fromPartial(object.codec) : undefined;
        message.configuration =
            object.configuration !== undefined && object.configuration !== null ? exports.ConfigurationDescriptor.fromPartial(object.configuration) : undefined;
        message.query_services =
            object.query_services !== undefined && object.query_services !== null ? exports.QueryServicesDescriptor.fromPartial(object.query_services) : undefined;
        message.tx = object.tx !== undefined && object.tx !== null ? exports.TxDescriptor.fromPartial(object.tx) : undefined;
        return message;
    }
};
exports.TxDescriptor = {
    $type: "cosmos.base.reflection.v2alpha1.TxDescriptor",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.fullname !== "") {
            writer.uint32(10).string(message.fullname);
        }
        for (const v of message.msgs) {
            exports.MsgDescriptor.encode(v, writer.uint32(18).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseTxDescriptor();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.fullname = reader.string();
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.msgs.push(exports.MsgDescriptor.decode(reader, reader.uint32()));
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
            fullname: isSet(object.fullname) ? globalThis.String(object.fullname) : "",
            msgs: globalThis.Array.isArray(object?.msgs) ? object.msgs.map((e) => exports.MsgDescriptor.fromJSON(e)) : []
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.fullname !== "") {
            obj.fullname = message.fullname;
        }
        if (message.msgs?.length) {
            obj.msgs = message.msgs.map((e) => exports.MsgDescriptor.toJSON(e));
        }
        return obj;
    },
    create(base) {
        return exports.TxDescriptor.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseTxDescriptor();
        message.fullname = object.fullname ?? "";
        message.msgs = object.msgs?.map((e) => exports.MsgDescriptor.fromPartial(e)) || [];
        return message;
    }
};
exports.AuthnDescriptor = {
    $type: "cosmos.base.reflection.v2alpha1.AuthnDescriptor",
    encode(message, writer = new wire_1.BinaryWriter()) {
        for (const v of message.sign_modes) {
            exports.SigningModeDescriptor.encode(v, writer.uint32(10).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseAuthnDescriptor();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.sign_modes.push(exports.SigningModeDescriptor.decode(reader, reader.uint32()));
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
            sign_modes: globalThis.Array.isArray(object?.sign_modes) ? object.sign_modes.map((e) => exports.SigningModeDescriptor.fromJSON(e)) : []
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.sign_modes?.length) {
            obj.sign_modes = message.sign_modes.map((e) => exports.SigningModeDescriptor.toJSON(e));
        }
        return obj;
    },
    create(base) {
        return exports.AuthnDescriptor.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseAuthnDescriptor();
        message.sign_modes = object.sign_modes?.map((e) => exports.SigningModeDescriptor.fromPartial(e)) || [];
        return message;
    }
};
exports.SigningModeDescriptor = {
    $type: "cosmos.base.reflection.v2alpha1.SigningModeDescriptor",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.name !== "") {
            writer.uint32(10).string(message.name);
        }
        if (message.number !== 0) {
            writer.uint32(16).int32(message.number);
        }
        if (message.authn_info_provider_method_fullname !== "") {
            writer.uint32(26).string(message.authn_info_provider_method_fullname);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseSigningModeDescriptor();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.name = reader.string();
                    continue;
                case 2:
                    if (tag !== 16) {
                        break;
                    }
                    message.number = reader.int32();
                    continue;
                case 3:
                    if (tag !== 26) {
                        break;
                    }
                    message.authn_info_provider_method_fullname = reader.string();
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
            name: isSet(object.name) ? globalThis.String(object.name) : "",
            number: isSet(object.number) ? globalThis.Number(object.number) : 0,
            authn_info_provider_method_fullname: isSet(object.authn_info_provider_method_fullname)
                ? globalThis.String(object.authn_info_provider_method_fullname)
                : ""
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.name !== "") {
            obj.name = message.name;
        }
        if (message.number !== 0) {
            obj.number = Math.round(message.number);
        }
        if (message.authn_info_provider_method_fullname !== "") {
            obj.authn_info_provider_method_fullname = message.authn_info_provider_method_fullname;
        }
        return obj;
    },
    create(base) {
        return exports.SigningModeDescriptor.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseSigningModeDescriptor();
        message.name = object.name ?? "";
        message.number = object.number ?? 0;
        message.authn_info_provider_method_fullname = object.authn_info_provider_method_fullname ?? "";
        return message;
    }
};
exports.ChainDescriptor = {
    $type: "cosmos.base.reflection.v2alpha1.ChainDescriptor",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.id !== "") {
            writer.uint32(10).string(message.id);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseChainDescriptor();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.id = reader.string();
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
        return { id: isSet(object.id) ? globalThis.String(object.id) : "" };
    },
    toJSON(message) {
        const obj = {};
        if (message.id !== "") {
            obj.id = message.id;
        }
        return obj;
    },
    create(base) {
        return exports.ChainDescriptor.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseChainDescriptor();
        message.id = object.id ?? "";
        return message;
    }
};
exports.CodecDescriptor = {
    $type: "cosmos.base.reflection.v2alpha1.CodecDescriptor",
    encode(message, writer = new wire_1.BinaryWriter()) {
        for (const v of message.interfaces) {
            exports.InterfaceDescriptor.encode(v, writer.uint32(10).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseCodecDescriptor();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.interfaces.push(exports.InterfaceDescriptor.decode(reader, reader.uint32()));
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
            interfaces: globalThis.Array.isArray(object?.interfaces) ? object.interfaces.map((e) => exports.InterfaceDescriptor.fromJSON(e)) : []
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.interfaces?.length) {
            obj.interfaces = message.interfaces.map((e) => exports.InterfaceDescriptor.toJSON(e));
        }
        return obj;
    },
    create(base) {
        return exports.CodecDescriptor.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseCodecDescriptor();
        message.interfaces = object.interfaces?.map((e) => exports.InterfaceDescriptor.fromPartial(e)) || [];
        return message;
    }
};
exports.InterfaceDescriptor = {
    $type: "cosmos.base.reflection.v2alpha1.InterfaceDescriptor",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.fullname !== "") {
            writer.uint32(10).string(message.fullname);
        }
        for (const v of message.interface_accepting_messages) {
            exports.InterfaceAcceptingMessageDescriptor.encode(v, writer.uint32(18).fork()).join();
        }
        for (const v of message.interface_implementers) {
            exports.InterfaceImplementerDescriptor.encode(v, writer.uint32(26).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseInterfaceDescriptor();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.fullname = reader.string();
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.interface_accepting_messages.push(exports.InterfaceAcceptingMessageDescriptor.decode(reader, reader.uint32()));
                    continue;
                case 3:
                    if (tag !== 26) {
                        break;
                    }
                    message.interface_implementers.push(exports.InterfaceImplementerDescriptor.decode(reader, reader.uint32()));
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
            fullname: isSet(object.fullname) ? globalThis.String(object.fullname) : "",
            interface_accepting_messages: globalThis.Array.isArray(object?.interface_accepting_messages)
                ? object.interface_accepting_messages.map((e) => exports.InterfaceAcceptingMessageDescriptor.fromJSON(e))
                : [],
            interface_implementers: globalThis.Array.isArray(object?.interface_implementers)
                ? object.interface_implementers.map((e) => exports.InterfaceImplementerDescriptor.fromJSON(e))
                : []
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.fullname !== "") {
            obj.fullname = message.fullname;
        }
        if (message.interface_accepting_messages?.length) {
            obj.interface_accepting_messages = message.interface_accepting_messages.map((e) => exports.InterfaceAcceptingMessageDescriptor.toJSON(e));
        }
        if (message.interface_implementers?.length) {
            obj.interface_implementers = message.interface_implementers.map((e) => exports.InterfaceImplementerDescriptor.toJSON(e));
        }
        return obj;
    },
    create(base) {
        return exports.InterfaceDescriptor.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseInterfaceDescriptor();
        message.fullname = object.fullname ?? "";
        message.interface_accepting_messages = object.interface_accepting_messages?.map((e) => exports.InterfaceAcceptingMessageDescriptor.fromPartial(e)) || [];
        message.interface_implementers = object.interface_implementers?.map((e) => exports.InterfaceImplementerDescriptor.fromPartial(e)) || [];
        return message;
    }
};
exports.InterfaceImplementerDescriptor = {
    $type: "cosmos.base.reflection.v2alpha1.InterfaceImplementerDescriptor",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.fullname !== "") {
            writer.uint32(10).string(message.fullname);
        }
        if (message.type_url !== "") {
            writer.uint32(18).string(message.type_url);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseInterfaceImplementerDescriptor();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.fullname = reader.string();
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.type_url = reader.string();
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
            fullname: isSet(object.fullname) ? globalThis.String(object.fullname) : "",
            type_url: isSet(object.type_url) ? globalThis.String(object.type_url) : ""
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.fullname !== "") {
            obj.fullname = message.fullname;
        }
        if (message.type_url !== "") {
            obj.type_url = message.type_url;
        }
        return obj;
    },
    create(base) {
        return exports.InterfaceImplementerDescriptor.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseInterfaceImplementerDescriptor();
        message.fullname = object.fullname ?? "";
        message.type_url = object.type_url ?? "";
        return message;
    }
};
exports.InterfaceAcceptingMessageDescriptor = {
    $type: "cosmos.base.reflection.v2alpha1.InterfaceAcceptingMessageDescriptor",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.fullname !== "") {
            writer.uint32(10).string(message.fullname);
        }
        for (const v of message.field_descriptor_names) {
            writer.uint32(18).string(v);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseInterfaceAcceptingMessageDescriptor();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.fullname = reader.string();
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.field_descriptor_names.push(reader.string());
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
            fullname: isSet(object.fullname) ? globalThis.String(object.fullname) : "",
            field_descriptor_names: globalThis.Array.isArray(object?.field_descriptor_names)
                ? object.field_descriptor_names.map((e) => globalThis.String(e))
                : []
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.fullname !== "") {
            obj.fullname = message.fullname;
        }
        if (message.field_descriptor_names?.length) {
            obj.field_descriptor_names = message.field_descriptor_names;
        }
        return obj;
    },
    create(base) {
        return exports.InterfaceAcceptingMessageDescriptor.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseInterfaceAcceptingMessageDescriptor();
        message.fullname = object.fullname ?? "";
        message.field_descriptor_names = object.field_descriptor_names?.map((e) => e) || [];
        return message;
    }
};
exports.ConfigurationDescriptor = {
    $type: "cosmos.base.reflection.v2alpha1.ConfigurationDescriptor",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.bech32_account_address_prefix !== "") {
            writer.uint32(10).string(message.bech32_account_address_prefix);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseConfigurationDescriptor();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.bech32_account_address_prefix = reader.string();
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
            bech32_account_address_prefix: isSet(object.bech32_account_address_prefix) ? globalThis.String(object.bech32_account_address_prefix) : ""
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.bech32_account_address_prefix !== "") {
            obj.bech32_account_address_prefix = message.bech32_account_address_prefix;
        }
        return obj;
    },
    create(base) {
        return exports.ConfigurationDescriptor.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseConfigurationDescriptor();
        message.bech32_account_address_prefix = object.bech32_account_address_prefix ?? "";
        return message;
    }
};
exports.MsgDescriptor = {
    $type: "cosmos.base.reflection.v2alpha1.MsgDescriptor",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.msg_type_url !== "") {
            writer.uint32(10).string(message.msg_type_url);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseMsgDescriptor();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.msg_type_url = reader.string();
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
        return { msg_type_url: isSet(object.msg_type_url) ? globalThis.String(object.msg_type_url) : "" };
    },
    toJSON(message) {
        const obj = {};
        if (message.msg_type_url !== "") {
            obj.msg_type_url = message.msg_type_url;
        }
        return obj;
    },
    create(base) {
        return exports.MsgDescriptor.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseMsgDescriptor();
        message.msg_type_url = object.msg_type_url ?? "";
        return message;
    }
};
exports.GetAuthnDescriptorRequest = {
    $type: "cosmos.base.reflection.v2alpha1.GetAuthnDescriptorRequest",
    encode(_, writer = new wire_1.BinaryWriter()) {
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseGetAuthnDescriptorRequest();
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
        return exports.GetAuthnDescriptorRequest.fromPartial(base ?? {});
    },
    fromPartial(_) {
        const message = createBaseGetAuthnDescriptorRequest();
        return message;
    }
};
exports.GetAuthnDescriptorResponse = {
    $type: "cosmos.base.reflection.v2alpha1.GetAuthnDescriptorResponse",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.authn !== undefined) {
            exports.AuthnDescriptor.encode(message.authn, writer.uint32(10).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseGetAuthnDescriptorResponse();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.authn = exports.AuthnDescriptor.decode(reader, reader.uint32());
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
        return { authn: isSet(object.authn) ? exports.AuthnDescriptor.fromJSON(object.authn) : undefined };
    },
    toJSON(message) {
        const obj = {};
        if (message.authn !== undefined) {
            obj.authn = exports.AuthnDescriptor.toJSON(message.authn);
        }
        return obj;
    },
    create(base) {
        return exports.GetAuthnDescriptorResponse.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseGetAuthnDescriptorResponse();
        message.authn = object.authn !== undefined && object.authn !== null ? exports.AuthnDescriptor.fromPartial(object.authn) : undefined;
        return message;
    }
};
exports.GetChainDescriptorRequest = {
    $type: "cosmos.base.reflection.v2alpha1.GetChainDescriptorRequest",
    encode(_, writer = new wire_1.BinaryWriter()) {
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseGetChainDescriptorRequest();
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
        return exports.GetChainDescriptorRequest.fromPartial(base ?? {});
    },
    fromPartial(_) {
        const message = createBaseGetChainDescriptorRequest();
        return message;
    }
};
exports.GetChainDescriptorResponse = {
    $type: "cosmos.base.reflection.v2alpha1.GetChainDescriptorResponse",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.chain !== undefined) {
            exports.ChainDescriptor.encode(message.chain, writer.uint32(10).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseGetChainDescriptorResponse();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.chain = exports.ChainDescriptor.decode(reader, reader.uint32());
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
        return { chain: isSet(object.chain) ? exports.ChainDescriptor.fromJSON(object.chain) : undefined };
    },
    toJSON(message) {
        const obj = {};
        if (message.chain !== undefined) {
            obj.chain = exports.ChainDescriptor.toJSON(message.chain);
        }
        return obj;
    },
    create(base) {
        return exports.GetChainDescriptorResponse.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseGetChainDescriptorResponse();
        message.chain = object.chain !== undefined && object.chain !== null ? exports.ChainDescriptor.fromPartial(object.chain) : undefined;
        return message;
    }
};
exports.GetCodecDescriptorRequest = {
    $type: "cosmos.base.reflection.v2alpha1.GetCodecDescriptorRequest",
    encode(_, writer = new wire_1.BinaryWriter()) {
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseGetCodecDescriptorRequest();
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
        return exports.GetCodecDescriptorRequest.fromPartial(base ?? {});
    },
    fromPartial(_) {
        const message = createBaseGetCodecDescriptorRequest();
        return message;
    }
};
exports.GetCodecDescriptorResponse = {
    $type: "cosmos.base.reflection.v2alpha1.GetCodecDescriptorResponse",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.codec !== undefined) {
            exports.CodecDescriptor.encode(message.codec, writer.uint32(10).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseGetCodecDescriptorResponse();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.codec = exports.CodecDescriptor.decode(reader, reader.uint32());
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
        return { codec: isSet(object.codec) ? exports.CodecDescriptor.fromJSON(object.codec) : undefined };
    },
    toJSON(message) {
        const obj = {};
        if (message.codec !== undefined) {
            obj.codec = exports.CodecDescriptor.toJSON(message.codec);
        }
        return obj;
    },
    create(base) {
        return exports.GetCodecDescriptorResponse.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseGetCodecDescriptorResponse();
        message.codec = object.codec !== undefined && object.codec !== null ? exports.CodecDescriptor.fromPartial(object.codec) : undefined;
        return message;
    }
};
exports.GetConfigurationDescriptorRequest = {
    $type: "cosmos.base.reflection.v2alpha1.GetConfigurationDescriptorRequest",
    encode(_, writer = new wire_1.BinaryWriter()) {
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseGetConfigurationDescriptorRequest();
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
        return exports.GetConfigurationDescriptorRequest.fromPartial(base ?? {});
    },
    fromPartial(_) {
        const message = createBaseGetConfigurationDescriptorRequest();
        return message;
    }
};
exports.GetConfigurationDescriptorResponse = {
    $type: "cosmos.base.reflection.v2alpha1.GetConfigurationDescriptorResponse",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.config !== undefined) {
            exports.ConfigurationDescriptor.encode(message.config, writer.uint32(10).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseGetConfigurationDescriptorResponse();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.config = exports.ConfigurationDescriptor.decode(reader, reader.uint32());
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
        return { config: isSet(object.config) ? exports.ConfigurationDescriptor.fromJSON(object.config) : undefined };
    },
    toJSON(message) {
        const obj = {};
        if (message.config !== undefined) {
            obj.config = exports.ConfigurationDescriptor.toJSON(message.config);
        }
        return obj;
    },
    create(base) {
        return exports.GetConfigurationDescriptorResponse.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseGetConfigurationDescriptorResponse();
        message.config = object.config !== undefined && object.config !== null ? exports.ConfigurationDescriptor.fromPartial(object.config) : undefined;
        return message;
    }
};
exports.GetQueryServicesDescriptorRequest = {
    $type: "cosmos.base.reflection.v2alpha1.GetQueryServicesDescriptorRequest",
    encode(_, writer = new wire_1.BinaryWriter()) {
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseGetQueryServicesDescriptorRequest();
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
        return exports.GetQueryServicesDescriptorRequest.fromPartial(base ?? {});
    },
    fromPartial(_) {
        const message = createBaseGetQueryServicesDescriptorRequest();
        return message;
    }
};
exports.GetQueryServicesDescriptorResponse = {
    $type: "cosmos.base.reflection.v2alpha1.GetQueryServicesDescriptorResponse",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.queries !== undefined) {
            exports.QueryServicesDescriptor.encode(message.queries, writer.uint32(10).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseGetQueryServicesDescriptorResponse();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.queries = exports.QueryServicesDescriptor.decode(reader, reader.uint32());
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
        return { queries: isSet(object.queries) ? exports.QueryServicesDescriptor.fromJSON(object.queries) : undefined };
    },
    toJSON(message) {
        const obj = {};
        if (message.queries !== undefined) {
            obj.queries = exports.QueryServicesDescriptor.toJSON(message.queries);
        }
        return obj;
    },
    create(base) {
        return exports.GetQueryServicesDescriptorResponse.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseGetQueryServicesDescriptorResponse();
        message.queries = object.queries !== undefined && object.queries !== null ? exports.QueryServicesDescriptor.fromPartial(object.queries) : undefined;
        return message;
    }
};
exports.GetTxDescriptorRequest = {
    $type: "cosmos.base.reflection.v2alpha1.GetTxDescriptorRequest",
    encode(_, writer = new wire_1.BinaryWriter()) {
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseGetTxDescriptorRequest();
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
        return exports.GetTxDescriptorRequest.fromPartial(base ?? {});
    },
    fromPartial(_) {
        const message = createBaseGetTxDescriptorRequest();
        return message;
    }
};
exports.GetTxDescriptorResponse = {
    $type: "cosmos.base.reflection.v2alpha1.GetTxDescriptorResponse",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.tx !== undefined) {
            exports.TxDescriptor.encode(message.tx, writer.uint32(10).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseGetTxDescriptorResponse();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.tx = exports.TxDescriptor.decode(reader, reader.uint32());
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
        return { tx: isSet(object.tx) ? exports.TxDescriptor.fromJSON(object.tx) : undefined };
    },
    toJSON(message) {
        const obj = {};
        if (message.tx !== undefined) {
            obj.tx = exports.TxDescriptor.toJSON(message.tx);
        }
        return obj;
    },
    create(base) {
        return exports.GetTxDescriptorResponse.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseGetTxDescriptorResponse();
        message.tx = object.tx !== undefined && object.tx !== null ? exports.TxDescriptor.fromPartial(object.tx) : undefined;
        return message;
    }
};
exports.QueryServicesDescriptor = {
    $type: "cosmos.base.reflection.v2alpha1.QueryServicesDescriptor",
    encode(message, writer = new wire_1.BinaryWriter()) {
        for (const v of message.query_services) {
            exports.QueryServiceDescriptor.encode(v, writer.uint32(10).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseQueryServicesDescriptor();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.query_services.push(exports.QueryServiceDescriptor.decode(reader, reader.uint32()));
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
            query_services: globalThis.Array.isArray(object?.query_services) ? object.query_services.map((e) => exports.QueryServiceDescriptor.fromJSON(e)) : []
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.query_services?.length) {
            obj.query_services = message.query_services.map((e) => exports.QueryServiceDescriptor.toJSON(e));
        }
        return obj;
    },
    create(base) {
        return exports.QueryServicesDescriptor.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryServicesDescriptor();
        message.query_services = object.query_services?.map((e) => exports.QueryServiceDescriptor.fromPartial(e)) || [];
        return message;
    }
};
exports.QueryServiceDescriptor = {
    $type: "cosmos.base.reflection.v2alpha1.QueryServiceDescriptor",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.fullname !== "") {
            writer.uint32(10).string(message.fullname);
        }
        if (message.is_module !== false) {
            writer.uint32(16).bool(message.is_module);
        }
        for (const v of message.methods) {
            exports.QueryMethodDescriptor.encode(v, writer.uint32(26).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseQueryServiceDescriptor();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.fullname = reader.string();
                    continue;
                case 2:
                    if (tag !== 16) {
                        break;
                    }
                    message.is_module = reader.bool();
                    continue;
                case 3:
                    if (tag !== 26) {
                        break;
                    }
                    message.methods.push(exports.QueryMethodDescriptor.decode(reader, reader.uint32()));
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
            fullname: isSet(object.fullname) ? globalThis.String(object.fullname) : "",
            is_module: isSet(object.is_module) ? globalThis.Boolean(object.is_module) : false,
            methods: globalThis.Array.isArray(object?.methods) ? object.methods.map((e) => exports.QueryMethodDescriptor.fromJSON(e)) : []
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.fullname !== "") {
            obj.fullname = message.fullname;
        }
        if (message.is_module !== false) {
            obj.is_module = message.is_module;
        }
        if (message.methods?.length) {
            obj.methods = message.methods.map((e) => exports.QueryMethodDescriptor.toJSON(e));
        }
        return obj;
    },
    create(base) {
        return exports.QueryServiceDescriptor.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryServiceDescriptor();
        message.fullname = object.fullname ?? "";
        message.is_module = object.is_module ?? false;
        message.methods = object.methods?.map((e) => exports.QueryMethodDescriptor.fromPartial(e)) || [];
        return message;
    }
};
exports.QueryMethodDescriptor = {
    $type: "cosmos.base.reflection.v2alpha1.QueryMethodDescriptor",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.name !== "") {
            writer.uint32(10).string(message.name);
        }
        if (message.full_query_path !== "") {
            writer.uint32(18).string(message.full_query_path);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseQueryMethodDescriptor();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.name = reader.string();
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.full_query_path = reader.string();
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
            name: isSet(object.name) ? globalThis.String(object.name) : "",
            full_query_path: isSet(object.full_query_path) ? globalThis.String(object.full_query_path) : ""
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.name !== "") {
            obj.name = message.name;
        }
        if (message.full_query_path !== "") {
            obj.full_query_path = message.full_query_path;
        }
        return obj;
    },
    create(base) {
        return exports.QueryMethodDescriptor.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryMethodDescriptor();
        message.name = object.name ?? "";
        message.full_query_path = object.full_query_path ?? "";
        return message;
    }
};
function createBaseAppDescriptor() {
    return {
        authn: undefined,
        chain: undefined,
        codec: undefined,
        configuration: undefined,
        query_services: undefined,
        tx: undefined
    };
}
function createBaseTxDescriptor() {
    return { fullname: "", msgs: [] };
}
function createBaseAuthnDescriptor() {
    return { sign_modes: [] };
}
function createBaseSigningModeDescriptor() {
    return { name: "", number: 0, authn_info_provider_method_fullname: "" };
}
function createBaseChainDescriptor() {
    return { id: "" };
}
function createBaseCodecDescriptor() {
    return { interfaces: [] };
}
function createBaseInterfaceDescriptor() {
    return { fullname: "", interface_accepting_messages: [], interface_implementers: [] };
}
function createBaseInterfaceImplementerDescriptor() {
    return { fullname: "", type_url: "" };
}
function createBaseInterfaceAcceptingMessageDescriptor() {
    return { fullname: "", field_descriptor_names: [] };
}
function createBaseConfigurationDescriptor() {
    return { bech32_account_address_prefix: "" };
}
function createBaseMsgDescriptor() {
    return { msg_type_url: "" };
}
function createBaseGetAuthnDescriptorRequest() {
    return {};
}
function createBaseGetAuthnDescriptorResponse() {
    return { authn: undefined };
}
function createBaseGetChainDescriptorRequest() {
    return {};
}
function createBaseGetChainDescriptorResponse() {
    return { chain: undefined };
}
function createBaseGetCodecDescriptorRequest() {
    return {};
}
function createBaseGetCodecDescriptorResponse() {
    return { codec: undefined };
}
function createBaseGetConfigurationDescriptorRequest() {
    return {};
}
function createBaseGetConfigurationDescriptorResponse() {
    return { config: undefined };
}
function createBaseGetQueryServicesDescriptorRequest() {
    return {};
}
function createBaseGetQueryServicesDescriptorResponse() {
    return { queries: undefined };
}
function createBaseGetTxDescriptorRequest() {
    return {};
}
function createBaseGetTxDescriptorResponse() {
    return { tx: undefined };
}
function createBaseQueryServicesDescriptor() {
    return { query_services: [] };
}
function createBaseQueryServiceDescriptor() {
    return { fullname: "", is_module: false, methods: [] };
}
function createBaseQueryMethodDescriptor() {
    return { name: "", full_query_path: "" };
}
function isSet(value) {
    return value !== null && value !== undefined;
}
exports.registry = [
    ["/cosmos.base.reflection.v2alpha1.AppDescriptor", exports.AppDescriptor],
    ["/cosmos.base.reflection.v2alpha1.TxDescriptor", exports.TxDescriptor],
    ["/cosmos.base.reflection.v2alpha1.AuthnDescriptor", exports.AuthnDescriptor],
    ["/cosmos.base.reflection.v2alpha1.SigningModeDescriptor", exports.SigningModeDescriptor],
    ["/cosmos.base.reflection.v2alpha1.ChainDescriptor", exports.ChainDescriptor],
    ["/cosmos.base.reflection.v2alpha1.CodecDescriptor", exports.CodecDescriptor],
    ["/cosmos.base.reflection.v2alpha1.InterfaceDescriptor", exports.InterfaceDescriptor],
    ["/cosmos.base.reflection.v2alpha1.InterfaceImplementerDescriptor", exports.InterfaceImplementerDescriptor],
    ["/cosmos.base.reflection.v2alpha1.InterfaceAcceptingMessageDescriptor", exports.InterfaceAcceptingMessageDescriptor],
    ["/cosmos.base.reflection.v2alpha1.ConfigurationDescriptor", exports.ConfigurationDescriptor],
    ["/cosmos.base.reflection.v2alpha1.MsgDescriptor", exports.MsgDescriptor],
    ["/cosmos.base.reflection.v2alpha1.GetAuthnDescriptorRequest", exports.GetAuthnDescriptorRequest],
    ["/cosmos.base.reflection.v2alpha1.GetAuthnDescriptorResponse", exports.GetAuthnDescriptorResponse],
    ["/cosmos.base.reflection.v2alpha1.GetChainDescriptorRequest", exports.GetChainDescriptorRequest],
    ["/cosmos.base.reflection.v2alpha1.GetChainDescriptorResponse", exports.GetChainDescriptorResponse],
    ["/cosmos.base.reflection.v2alpha1.GetCodecDescriptorRequest", exports.GetCodecDescriptorRequest],
    ["/cosmos.base.reflection.v2alpha1.GetCodecDescriptorResponse", exports.GetCodecDescriptorResponse],
    ["/cosmos.base.reflection.v2alpha1.GetConfigurationDescriptorRequest", exports.GetConfigurationDescriptorRequest],
    ["/cosmos.base.reflection.v2alpha1.GetConfigurationDescriptorResponse", exports.GetConfigurationDescriptorResponse],
    ["/cosmos.base.reflection.v2alpha1.GetQueryServicesDescriptorRequest", exports.GetQueryServicesDescriptorRequest],
    ["/cosmos.base.reflection.v2alpha1.GetQueryServicesDescriptorResponse", exports.GetQueryServicesDescriptorResponse],
    ["/cosmos.base.reflection.v2alpha1.GetTxDescriptorRequest", exports.GetTxDescriptorRequest],
    ["/cosmos.base.reflection.v2alpha1.GetTxDescriptorResponse", exports.GetTxDescriptorResponse],
    ["/cosmos.base.reflection.v2alpha1.QueryServicesDescriptor", exports.QueryServicesDescriptor],
    ["/cosmos.base.reflection.v2alpha1.QueryServiceDescriptor", exports.QueryServiceDescriptor],
    ["/cosmos.base.reflection.v2alpha1.QueryMethodDescriptor", exports.QueryMethodDescriptor]
];
exports.aminoConverters = {
    "/cosmos.base.reflection.v2alpha1.AppDescriptor": {
        aminoType: "cosmos-sdk/AppDescriptor",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.base.reflection.v2alpha1.TxDescriptor": {
        aminoType: "cosmos-sdk/TxDescriptor",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.base.reflection.v2alpha1.AuthnDescriptor": {
        aminoType: "cosmos-sdk/AuthnDescriptor",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.base.reflection.v2alpha1.SigningModeDescriptor": {
        aminoType: "cosmos-sdk/SigningModeDescriptor",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.base.reflection.v2alpha1.ChainDescriptor": {
        aminoType: "cosmos-sdk/ChainDescriptor",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.base.reflection.v2alpha1.CodecDescriptor": {
        aminoType: "cosmos-sdk/CodecDescriptor",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.base.reflection.v2alpha1.InterfaceDescriptor": {
        aminoType: "cosmos-sdk/InterfaceDescriptor",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.base.reflection.v2alpha1.InterfaceImplementerDescriptor": {
        aminoType: "cosmos-sdk/InterfaceImplementerDescriptor",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.base.reflection.v2alpha1.InterfaceAcceptingMessageDescriptor": {
        aminoType: "cosmos-sdk/InterfaceAcceptingMessageDescriptor",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.base.reflection.v2alpha1.ConfigurationDescriptor": {
        aminoType: "cosmos-sdk/ConfigurationDescriptor",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.base.reflection.v2alpha1.MsgDescriptor": {
        aminoType: "cosmos-sdk/MsgDescriptor",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.base.reflection.v2alpha1.GetAuthnDescriptorRequest": {
        aminoType: "cosmos-sdk/GetAuthnDescriptorRequest",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.base.reflection.v2alpha1.GetAuthnDescriptorResponse": {
        aminoType: "cosmos-sdk/GetAuthnDescriptorResponse",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.base.reflection.v2alpha1.GetChainDescriptorRequest": {
        aminoType: "cosmos-sdk/GetChainDescriptorRequest",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.base.reflection.v2alpha1.GetChainDescriptorResponse": {
        aminoType: "cosmos-sdk/GetChainDescriptorResponse",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.base.reflection.v2alpha1.GetCodecDescriptorRequest": {
        aminoType: "cosmos-sdk/GetCodecDescriptorRequest",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.base.reflection.v2alpha1.GetCodecDescriptorResponse": {
        aminoType: "cosmos-sdk/GetCodecDescriptorResponse",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.base.reflection.v2alpha1.GetConfigurationDescriptorRequest": {
        aminoType: "cosmos-sdk/GetConfigurationDescriptorRequest",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.base.reflection.v2alpha1.GetConfigurationDescriptorResponse": {
        aminoType: "cosmos-sdk/GetConfigurationDescriptorResponse",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.base.reflection.v2alpha1.GetQueryServicesDescriptorRequest": {
        aminoType: "cosmos-sdk/GetQueryServicesDescriptorRequest",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.base.reflection.v2alpha1.GetQueryServicesDescriptorResponse": {
        aminoType: "cosmos-sdk/GetQueryServicesDescriptorResponse",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.base.reflection.v2alpha1.GetTxDescriptorRequest": {
        aminoType: "cosmos-sdk/GetTxDescriptorRequest",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.base.reflection.v2alpha1.GetTxDescriptorResponse": {
        aminoType: "cosmos-sdk/GetTxDescriptorResponse",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.base.reflection.v2alpha1.QueryServicesDescriptor": {
        aminoType: "cosmos-sdk/QueryServicesDescriptor",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.base.reflection.v2alpha1.QueryServiceDescriptor": {
        aminoType: "cosmos-sdk/QueryServiceDescriptor",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.base.reflection.v2alpha1.QueryMethodDescriptor": {
        aminoType: "cosmos-sdk/QueryMethodDescriptor",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    }
};
