import { BinaryReader, BinaryWriter } from "@bufbuild/protobuf/wire";
export const AppDescriptor = {
    $type: "cosmos.base.reflection.v2alpha1.AppDescriptor",
    encode(message, writer = new BinaryWriter()) {
        if (message.authn !== undefined) {
            AuthnDescriptor.encode(message.authn, writer.uint32(10).fork()).join();
        }
        if (message.chain !== undefined) {
            ChainDescriptor.encode(message.chain, writer.uint32(18).fork()).join();
        }
        if (message.codec !== undefined) {
            CodecDescriptor.encode(message.codec, writer.uint32(26).fork()).join();
        }
        if (message.configuration !== undefined) {
            ConfigurationDescriptor.encode(message.configuration, writer.uint32(34).fork()).join();
        }
        if (message.query_services !== undefined) {
            QueryServicesDescriptor.encode(message.query_services, writer.uint32(42).fork()).join();
        }
        if (message.tx !== undefined) {
            TxDescriptor.encode(message.tx, writer.uint32(50).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseAppDescriptor();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.authn = AuthnDescriptor.decode(reader, reader.uint32());
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.chain = ChainDescriptor.decode(reader, reader.uint32());
                    continue;
                case 3:
                    if (tag !== 26) {
                        break;
                    }
                    message.codec = CodecDescriptor.decode(reader, reader.uint32());
                    continue;
                case 4:
                    if (tag !== 34) {
                        break;
                    }
                    message.configuration = ConfigurationDescriptor.decode(reader, reader.uint32());
                    continue;
                case 5:
                    if (tag !== 42) {
                        break;
                    }
                    message.query_services = QueryServicesDescriptor.decode(reader, reader.uint32());
                    continue;
                case 6:
                    if (tag !== 50) {
                        break;
                    }
                    message.tx = TxDescriptor.decode(reader, reader.uint32());
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
            authn: isSet(object.authn) ? AuthnDescriptor.fromJSON(object.authn) : undefined,
            chain: isSet(object.chain) ? ChainDescriptor.fromJSON(object.chain) : undefined,
            codec: isSet(object.codec) ? CodecDescriptor.fromJSON(object.codec) : undefined,
            configuration: isSet(object.configuration) ? ConfigurationDescriptor.fromJSON(object.configuration) : undefined,
            query_services: isSet(object.query_services) ? QueryServicesDescriptor.fromJSON(object.query_services) : undefined,
            tx: isSet(object.tx) ? TxDescriptor.fromJSON(object.tx) : undefined
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.authn !== undefined) {
            obj.authn = AuthnDescriptor.toJSON(message.authn);
        }
        if (message.chain !== undefined) {
            obj.chain = ChainDescriptor.toJSON(message.chain);
        }
        if (message.codec !== undefined) {
            obj.codec = CodecDescriptor.toJSON(message.codec);
        }
        if (message.configuration !== undefined) {
            obj.configuration = ConfigurationDescriptor.toJSON(message.configuration);
        }
        if (message.query_services !== undefined) {
            obj.query_services = QueryServicesDescriptor.toJSON(message.query_services);
        }
        if (message.tx !== undefined) {
            obj.tx = TxDescriptor.toJSON(message.tx);
        }
        return obj;
    },
    create(base) {
        return AppDescriptor.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseAppDescriptor();
        message.authn = object.authn !== undefined && object.authn !== null ? AuthnDescriptor.fromPartial(object.authn) : undefined;
        message.chain = object.chain !== undefined && object.chain !== null ? ChainDescriptor.fromPartial(object.chain) : undefined;
        message.codec = object.codec !== undefined && object.codec !== null ? CodecDescriptor.fromPartial(object.codec) : undefined;
        message.configuration =
            object.configuration !== undefined && object.configuration !== null ? ConfigurationDescriptor.fromPartial(object.configuration) : undefined;
        message.query_services =
            object.query_services !== undefined && object.query_services !== null ? QueryServicesDescriptor.fromPartial(object.query_services) : undefined;
        message.tx = object.tx !== undefined && object.tx !== null ? TxDescriptor.fromPartial(object.tx) : undefined;
        return message;
    }
};
export const TxDescriptor = {
    $type: "cosmos.base.reflection.v2alpha1.TxDescriptor",
    encode(message, writer = new BinaryWriter()) {
        if (message.fullname !== "") {
            writer.uint32(10).string(message.fullname);
        }
        for (const v of message.msgs) {
            MsgDescriptor.encode(v, writer.uint32(18).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
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
                    message.msgs.push(MsgDescriptor.decode(reader, reader.uint32()));
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
            msgs: globalThis.Array.isArray(object?.msgs) ? object.msgs.map((e) => MsgDescriptor.fromJSON(e)) : []
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.fullname !== "") {
            obj.fullname = message.fullname;
        }
        if (message.msgs?.length) {
            obj.msgs = message.msgs.map((e) => MsgDescriptor.toJSON(e));
        }
        return obj;
    },
    create(base) {
        return TxDescriptor.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseTxDescriptor();
        message.fullname = object.fullname ?? "";
        message.msgs = object.msgs?.map((e) => MsgDescriptor.fromPartial(e)) || [];
        return message;
    }
};
export const AuthnDescriptor = {
    $type: "cosmos.base.reflection.v2alpha1.AuthnDescriptor",
    encode(message, writer = new BinaryWriter()) {
        for (const v of message.sign_modes) {
            SigningModeDescriptor.encode(v, writer.uint32(10).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseAuthnDescriptor();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.sign_modes.push(SigningModeDescriptor.decode(reader, reader.uint32()));
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
            sign_modes: globalThis.Array.isArray(object?.sign_modes) ? object.sign_modes.map((e) => SigningModeDescriptor.fromJSON(e)) : []
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.sign_modes?.length) {
            obj.sign_modes = message.sign_modes.map((e) => SigningModeDescriptor.toJSON(e));
        }
        return obj;
    },
    create(base) {
        return AuthnDescriptor.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseAuthnDescriptor();
        message.sign_modes = object.sign_modes?.map((e) => SigningModeDescriptor.fromPartial(e)) || [];
        return message;
    }
};
export const SigningModeDescriptor = {
    $type: "cosmos.base.reflection.v2alpha1.SigningModeDescriptor",
    encode(message, writer = new BinaryWriter()) {
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
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
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
        return SigningModeDescriptor.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseSigningModeDescriptor();
        message.name = object.name ?? "";
        message.number = object.number ?? 0;
        message.authn_info_provider_method_fullname = object.authn_info_provider_method_fullname ?? "";
        return message;
    }
};
export const ChainDescriptor = {
    $type: "cosmos.base.reflection.v2alpha1.ChainDescriptor",
    encode(message, writer = new BinaryWriter()) {
        if (message.id !== "") {
            writer.uint32(10).string(message.id);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
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
        return ChainDescriptor.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseChainDescriptor();
        message.id = object.id ?? "";
        return message;
    }
};
export const CodecDescriptor = {
    $type: "cosmos.base.reflection.v2alpha1.CodecDescriptor",
    encode(message, writer = new BinaryWriter()) {
        for (const v of message.interfaces) {
            InterfaceDescriptor.encode(v, writer.uint32(10).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseCodecDescriptor();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.interfaces.push(InterfaceDescriptor.decode(reader, reader.uint32()));
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
            interfaces: globalThis.Array.isArray(object?.interfaces) ? object.interfaces.map((e) => InterfaceDescriptor.fromJSON(e)) : []
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.interfaces?.length) {
            obj.interfaces = message.interfaces.map((e) => InterfaceDescriptor.toJSON(e));
        }
        return obj;
    },
    create(base) {
        return CodecDescriptor.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseCodecDescriptor();
        message.interfaces = object.interfaces?.map((e) => InterfaceDescriptor.fromPartial(e)) || [];
        return message;
    }
};
export const InterfaceDescriptor = {
    $type: "cosmos.base.reflection.v2alpha1.InterfaceDescriptor",
    encode(message, writer = new BinaryWriter()) {
        if (message.fullname !== "") {
            writer.uint32(10).string(message.fullname);
        }
        for (const v of message.interface_accepting_messages) {
            InterfaceAcceptingMessageDescriptor.encode(v, writer.uint32(18).fork()).join();
        }
        for (const v of message.interface_implementers) {
            InterfaceImplementerDescriptor.encode(v, writer.uint32(26).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
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
                    message.interface_accepting_messages.push(InterfaceAcceptingMessageDescriptor.decode(reader, reader.uint32()));
                    continue;
                case 3:
                    if (tag !== 26) {
                        break;
                    }
                    message.interface_implementers.push(InterfaceImplementerDescriptor.decode(reader, reader.uint32()));
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
                ? object.interface_accepting_messages.map((e) => InterfaceAcceptingMessageDescriptor.fromJSON(e))
                : [],
            interface_implementers: globalThis.Array.isArray(object?.interface_implementers)
                ? object.interface_implementers.map((e) => InterfaceImplementerDescriptor.fromJSON(e))
                : []
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.fullname !== "") {
            obj.fullname = message.fullname;
        }
        if (message.interface_accepting_messages?.length) {
            obj.interface_accepting_messages = message.interface_accepting_messages.map((e) => InterfaceAcceptingMessageDescriptor.toJSON(e));
        }
        if (message.interface_implementers?.length) {
            obj.interface_implementers = message.interface_implementers.map((e) => InterfaceImplementerDescriptor.toJSON(e));
        }
        return obj;
    },
    create(base) {
        return InterfaceDescriptor.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseInterfaceDescriptor();
        message.fullname = object.fullname ?? "";
        message.interface_accepting_messages = object.interface_accepting_messages?.map((e) => InterfaceAcceptingMessageDescriptor.fromPartial(e)) || [];
        message.interface_implementers = object.interface_implementers?.map((e) => InterfaceImplementerDescriptor.fromPartial(e)) || [];
        return message;
    }
};
export const InterfaceImplementerDescriptor = {
    $type: "cosmos.base.reflection.v2alpha1.InterfaceImplementerDescriptor",
    encode(message, writer = new BinaryWriter()) {
        if (message.fullname !== "") {
            writer.uint32(10).string(message.fullname);
        }
        if (message.type_url !== "") {
            writer.uint32(18).string(message.type_url);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
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
        return InterfaceImplementerDescriptor.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseInterfaceImplementerDescriptor();
        message.fullname = object.fullname ?? "";
        message.type_url = object.type_url ?? "";
        return message;
    }
};
export const InterfaceAcceptingMessageDescriptor = {
    $type: "cosmos.base.reflection.v2alpha1.InterfaceAcceptingMessageDescriptor",
    encode(message, writer = new BinaryWriter()) {
        if (message.fullname !== "") {
            writer.uint32(10).string(message.fullname);
        }
        for (const v of message.field_descriptor_names) {
            writer.uint32(18).string(v);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
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
        return InterfaceAcceptingMessageDescriptor.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseInterfaceAcceptingMessageDescriptor();
        message.fullname = object.fullname ?? "";
        message.field_descriptor_names = object.field_descriptor_names?.map((e) => e) || [];
        return message;
    }
};
export const ConfigurationDescriptor = {
    $type: "cosmos.base.reflection.v2alpha1.ConfigurationDescriptor",
    encode(message, writer = new BinaryWriter()) {
        if (message.bech32_account_address_prefix !== "") {
            writer.uint32(10).string(message.bech32_account_address_prefix);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
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
        return ConfigurationDescriptor.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseConfigurationDescriptor();
        message.bech32_account_address_prefix = object.bech32_account_address_prefix ?? "";
        return message;
    }
};
export const MsgDescriptor = {
    $type: "cosmos.base.reflection.v2alpha1.MsgDescriptor",
    encode(message, writer = new BinaryWriter()) {
        if (message.msg_type_url !== "") {
            writer.uint32(10).string(message.msg_type_url);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
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
        return MsgDescriptor.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseMsgDescriptor();
        message.msg_type_url = object.msg_type_url ?? "";
        return message;
    }
};
export const GetAuthnDescriptorRequest = {
    $type: "cosmos.base.reflection.v2alpha1.GetAuthnDescriptorRequest",
    encode(_, writer = new BinaryWriter()) {
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
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
        return GetAuthnDescriptorRequest.fromPartial(base ?? {});
    },
    fromPartial(_) {
        const message = createBaseGetAuthnDescriptorRequest();
        return message;
    }
};
export const GetAuthnDescriptorResponse = {
    $type: "cosmos.base.reflection.v2alpha1.GetAuthnDescriptorResponse",
    encode(message, writer = new BinaryWriter()) {
        if (message.authn !== undefined) {
            AuthnDescriptor.encode(message.authn, writer.uint32(10).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseGetAuthnDescriptorResponse();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.authn = AuthnDescriptor.decode(reader, reader.uint32());
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
        return { authn: isSet(object.authn) ? AuthnDescriptor.fromJSON(object.authn) : undefined };
    },
    toJSON(message) {
        const obj = {};
        if (message.authn !== undefined) {
            obj.authn = AuthnDescriptor.toJSON(message.authn);
        }
        return obj;
    },
    create(base) {
        return GetAuthnDescriptorResponse.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseGetAuthnDescriptorResponse();
        message.authn = object.authn !== undefined && object.authn !== null ? AuthnDescriptor.fromPartial(object.authn) : undefined;
        return message;
    }
};
export const GetChainDescriptorRequest = {
    $type: "cosmos.base.reflection.v2alpha1.GetChainDescriptorRequest",
    encode(_, writer = new BinaryWriter()) {
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
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
        return GetChainDescriptorRequest.fromPartial(base ?? {});
    },
    fromPartial(_) {
        const message = createBaseGetChainDescriptorRequest();
        return message;
    }
};
export const GetChainDescriptorResponse = {
    $type: "cosmos.base.reflection.v2alpha1.GetChainDescriptorResponse",
    encode(message, writer = new BinaryWriter()) {
        if (message.chain !== undefined) {
            ChainDescriptor.encode(message.chain, writer.uint32(10).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseGetChainDescriptorResponse();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.chain = ChainDescriptor.decode(reader, reader.uint32());
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
        return { chain: isSet(object.chain) ? ChainDescriptor.fromJSON(object.chain) : undefined };
    },
    toJSON(message) {
        const obj = {};
        if (message.chain !== undefined) {
            obj.chain = ChainDescriptor.toJSON(message.chain);
        }
        return obj;
    },
    create(base) {
        return GetChainDescriptorResponse.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseGetChainDescriptorResponse();
        message.chain = object.chain !== undefined && object.chain !== null ? ChainDescriptor.fromPartial(object.chain) : undefined;
        return message;
    }
};
export const GetCodecDescriptorRequest = {
    $type: "cosmos.base.reflection.v2alpha1.GetCodecDescriptorRequest",
    encode(_, writer = new BinaryWriter()) {
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
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
        return GetCodecDescriptorRequest.fromPartial(base ?? {});
    },
    fromPartial(_) {
        const message = createBaseGetCodecDescriptorRequest();
        return message;
    }
};
export const GetCodecDescriptorResponse = {
    $type: "cosmos.base.reflection.v2alpha1.GetCodecDescriptorResponse",
    encode(message, writer = new BinaryWriter()) {
        if (message.codec !== undefined) {
            CodecDescriptor.encode(message.codec, writer.uint32(10).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseGetCodecDescriptorResponse();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.codec = CodecDescriptor.decode(reader, reader.uint32());
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
        return { codec: isSet(object.codec) ? CodecDescriptor.fromJSON(object.codec) : undefined };
    },
    toJSON(message) {
        const obj = {};
        if (message.codec !== undefined) {
            obj.codec = CodecDescriptor.toJSON(message.codec);
        }
        return obj;
    },
    create(base) {
        return GetCodecDescriptorResponse.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseGetCodecDescriptorResponse();
        message.codec = object.codec !== undefined && object.codec !== null ? CodecDescriptor.fromPartial(object.codec) : undefined;
        return message;
    }
};
export const GetConfigurationDescriptorRequest = {
    $type: "cosmos.base.reflection.v2alpha1.GetConfigurationDescriptorRequest",
    encode(_, writer = new BinaryWriter()) {
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
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
        return GetConfigurationDescriptorRequest.fromPartial(base ?? {});
    },
    fromPartial(_) {
        const message = createBaseGetConfigurationDescriptorRequest();
        return message;
    }
};
export const GetConfigurationDescriptorResponse = {
    $type: "cosmos.base.reflection.v2alpha1.GetConfigurationDescriptorResponse",
    encode(message, writer = new BinaryWriter()) {
        if (message.config !== undefined) {
            ConfigurationDescriptor.encode(message.config, writer.uint32(10).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseGetConfigurationDescriptorResponse();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.config = ConfigurationDescriptor.decode(reader, reader.uint32());
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
        return { config: isSet(object.config) ? ConfigurationDescriptor.fromJSON(object.config) : undefined };
    },
    toJSON(message) {
        const obj = {};
        if (message.config !== undefined) {
            obj.config = ConfigurationDescriptor.toJSON(message.config);
        }
        return obj;
    },
    create(base) {
        return GetConfigurationDescriptorResponse.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseGetConfigurationDescriptorResponse();
        message.config = object.config !== undefined && object.config !== null ? ConfigurationDescriptor.fromPartial(object.config) : undefined;
        return message;
    }
};
export const GetQueryServicesDescriptorRequest = {
    $type: "cosmos.base.reflection.v2alpha1.GetQueryServicesDescriptorRequest",
    encode(_, writer = new BinaryWriter()) {
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
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
        return GetQueryServicesDescriptorRequest.fromPartial(base ?? {});
    },
    fromPartial(_) {
        const message = createBaseGetQueryServicesDescriptorRequest();
        return message;
    }
};
export const GetQueryServicesDescriptorResponse = {
    $type: "cosmos.base.reflection.v2alpha1.GetQueryServicesDescriptorResponse",
    encode(message, writer = new BinaryWriter()) {
        if (message.queries !== undefined) {
            QueryServicesDescriptor.encode(message.queries, writer.uint32(10).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseGetQueryServicesDescriptorResponse();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.queries = QueryServicesDescriptor.decode(reader, reader.uint32());
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
        return { queries: isSet(object.queries) ? QueryServicesDescriptor.fromJSON(object.queries) : undefined };
    },
    toJSON(message) {
        const obj = {};
        if (message.queries !== undefined) {
            obj.queries = QueryServicesDescriptor.toJSON(message.queries);
        }
        return obj;
    },
    create(base) {
        return GetQueryServicesDescriptorResponse.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseGetQueryServicesDescriptorResponse();
        message.queries = object.queries !== undefined && object.queries !== null ? QueryServicesDescriptor.fromPartial(object.queries) : undefined;
        return message;
    }
};
export const GetTxDescriptorRequest = {
    $type: "cosmos.base.reflection.v2alpha1.GetTxDescriptorRequest",
    encode(_, writer = new BinaryWriter()) {
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
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
        return GetTxDescriptorRequest.fromPartial(base ?? {});
    },
    fromPartial(_) {
        const message = createBaseGetTxDescriptorRequest();
        return message;
    }
};
export const GetTxDescriptorResponse = {
    $type: "cosmos.base.reflection.v2alpha1.GetTxDescriptorResponse",
    encode(message, writer = new BinaryWriter()) {
        if (message.tx !== undefined) {
            TxDescriptor.encode(message.tx, writer.uint32(10).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseGetTxDescriptorResponse();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.tx = TxDescriptor.decode(reader, reader.uint32());
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
        return { tx: isSet(object.tx) ? TxDescriptor.fromJSON(object.tx) : undefined };
    },
    toJSON(message) {
        const obj = {};
        if (message.tx !== undefined) {
            obj.tx = TxDescriptor.toJSON(message.tx);
        }
        return obj;
    },
    create(base) {
        return GetTxDescriptorResponse.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseGetTxDescriptorResponse();
        message.tx = object.tx !== undefined && object.tx !== null ? TxDescriptor.fromPartial(object.tx) : undefined;
        return message;
    }
};
export const QueryServicesDescriptor = {
    $type: "cosmos.base.reflection.v2alpha1.QueryServicesDescriptor",
    encode(message, writer = new BinaryWriter()) {
        for (const v of message.query_services) {
            QueryServiceDescriptor.encode(v, writer.uint32(10).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseQueryServicesDescriptor();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.query_services.push(QueryServiceDescriptor.decode(reader, reader.uint32()));
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
            query_services: globalThis.Array.isArray(object?.query_services) ? object.query_services.map((e) => QueryServiceDescriptor.fromJSON(e)) : []
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.query_services?.length) {
            obj.query_services = message.query_services.map((e) => QueryServiceDescriptor.toJSON(e));
        }
        return obj;
    },
    create(base) {
        return QueryServicesDescriptor.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryServicesDescriptor();
        message.query_services = object.query_services?.map((e) => QueryServiceDescriptor.fromPartial(e)) || [];
        return message;
    }
};
export const QueryServiceDescriptor = {
    $type: "cosmos.base.reflection.v2alpha1.QueryServiceDescriptor",
    encode(message, writer = new BinaryWriter()) {
        if (message.fullname !== "") {
            writer.uint32(10).string(message.fullname);
        }
        if (message.is_module !== false) {
            writer.uint32(16).bool(message.is_module);
        }
        for (const v of message.methods) {
            QueryMethodDescriptor.encode(v, writer.uint32(26).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
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
                    message.methods.push(QueryMethodDescriptor.decode(reader, reader.uint32()));
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
            methods: globalThis.Array.isArray(object?.methods) ? object.methods.map((e) => QueryMethodDescriptor.fromJSON(e)) : []
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
            obj.methods = message.methods.map((e) => QueryMethodDescriptor.toJSON(e));
        }
        return obj;
    },
    create(base) {
        return QueryServiceDescriptor.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryServiceDescriptor();
        message.fullname = object.fullname ?? "";
        message.is_module = object.is_module ?? false;
        message.methods = object.methods?.map((e) => QueryMethodDescriptor.fromPartial(e)) || [];
        return message;
    }
};
export const QueryMethodDescriptor = {
    $type: "cosmos.base.reflection.v2alpha1.QueryMethodDescriptor",
    encode(message, writer = new BinaryWriter()) {
        if (message.name !== "") {
            writer.uint32(10).string(message.name);
        }
        if (message.full_query_path !== "") {
            writer.uint32(18).string(message.full_query_path);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
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
        return QueryMethodDescriptor.fromPartial(base ?? {});
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
export const registry = [
    ["/cosmos.base.reflection.v2alpha1.AppDescriptor", AppDescriptor],
    ["/cosmos.base.reflection.v2alpha1.TxDescriptor", TxDescriptor],
    ["/cosmos.base.reflection.v2alpha1.AuthnDescriptor", AuthnDescriptor],
    ["/cosmos.base.reflection.v2alpha1.SigningModeDescriptor", SigningModeDescriptor],
    ["/cosmos.base.reflection.v2alpha1.ChainDescriptor", ChainDescriptor],
    ["/cosmos.base.reflection.v2alpha1.CodecDescriptor", CodecDescriptor],
    ["/cosmos.base.reflection.v2alpha1.InterfaceDescriptor", InterfaceDescriptor],
    ["/cosmos.base.reflection.v2alpha1.InterfaceImplementerDescriptor", InterfaceImplementerDescriptor],
    ["/cosmos.base.reflection.v2alpha1.InterfaceAcceptingMessageDescriptor", InterfaceAcceptingMessageDescriptor],
    ["/cosmos.base.reflection.v2alpha1.ConfigurationDescriptor", ConfigurationDescriptor],
    ["/cosmos.base.reflection.v2alpha1.MsgDescriptor", MsgDescriptor],
    ["/cosmos.base.reflection.v2alpha1.GetAuthnDescriptorRequest", GetAuthnDescriptorRequest],
    ["/cosmos.base.reflection.v2alpha1.GetAuthnDescriptorResponse", GetAuthnDescriptorResponse],
    ["/cosmos.base.reflection.v2alpha1.GetChainDescriptorRequest", GetChainDescriptorRequest],
    ["/cosmos.base.reflection.v2alpha1.GetChainDescriptorResponse", GetChainDescriptorResponse],
    ["/cosmos.base.reflection.v2alpha1.GetCodecDescriptorRequest", GetCodecDescriptorRequest],
    ["/cosmos.base.reflection.v2alpha1.GetCodecDescriptorResponse", GetCodecDescriptorResponse],
    ["/cosmos.base.reflection.v2alpha1.GetConfigurationDescriptorRequest", GetConfigurationDescriptorRequest],
    ["/cosmos.base.reflection.v2alpha1.GetConfigurationDescriptorResponse", GetConfigurationDescriptorResponse],
    ["/cosmos.base.reflection.v2alpha1.GetQueryServicesDescriptorRequest", GetQueryServicesDescriptorRequest],
    ["/cosmos.base.reflection.v2alpha1.GetQueryServicesDescriptorResponse", GetQueryServicesDescriptorResponse],
    ["/cosmos.base.reflection.v2alpha1.GetTxDescriptorRequest", GetTxDescriptorRequest],
    ["/cosmos.base.reflection.v2alpha1.GetTxDescriptorResponse", GetTxDescriptorResponse],
    ["/cosmos.base.reflection.v2alpha1.QueryServicesDescriptor", QueryServicesDescriptor],
    ["/cosmos.base.reflection.v2alpha1.QueryServiceDescriptor", QueryServiceDescriptor],
    ["/cosmos.base.reflection.v2alpha1.QueryMethodDescriptor", QueryMethodDescriptor]
];
export const aminoConverters = {
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
