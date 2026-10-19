import { BinaryReader, BinaryWriter } from "@bufbuild/protobuf/wire";
export const ListAllInterfacesRequest = {
    $type: "cosmos.base.reflection.v1beta1.ListAllInterfacesRequest",
    encode(_, writer = new BinaryWriter()) {
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseListAllInterfacesRequest();
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
        return ListAllInterfacesRequest.fromPartial(base ?? {});
    },
    fromPartial(_) {
        const message = createBaseListAllInterfacesRequest();
        return message;
    }
};
export const ListAllInterfacesResponse = {
    $type: "cosmos.base.reflection.v1beta1.ListAllInterfacesResponse",
    encode(message, writer = new BinaryWriter()) {
        for (const v of message.interface_names) {
            writer.uint32(10).string(v);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseListAllInterfacesResponse();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.interface_names.push(reader.string());
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
            interface_names: globalThis.Array.isArray(object?.interface_names) ? object.interface_names.map((e) => globalThis.String(e)) : []
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.interface_names?.length) {
            obj.interface_names = message.interface_names;
        }
        return obj;
    },
    create(base) {
        return ListAllInterfacesResponse.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseListAllInterfacesResponse();
        message.interface_names = object.interface_names?.map((e) => e) || [];
        return message;
    }
};
export const ListImplementationsRequest = {
    $type: "cosmos.base.reflection.v1beta1.ListImplementationsRequest",
    encode(message, writer = new BinaryWriter()) {
        if (message.interface_name !== "") {
            writer.uint32(10).string(message.interface_name);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseListImplementationsRequest();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.interface_name = reader.string();
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
        return { interface_name: isSet(object.interface_name) ? globalThis.String(object.interface_name) : "" };
    },
    toJSON(message) {
        const obj = {};
        if (message.interface_name !== "") {
            obj.interface_name = message.interface_name;
        }
        return obj;
    },
    create(base) {
        return ListImplementationsRequest.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseListImplementationsRequest();
        message.interface_name = object.interface_name ?? "";
        return message;
    }
};
export const ListImplementationsResponse = {
    $type: "cosmos.base.reflection.v1beta1.ListImplementationsResponse",
    encode(message, writer = new BinaryWriter()) {
        for (const v of message.implementation_message_names) {
            writer.uint32(10).string(v);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseListImplementationsResponse();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.implementation_message_names.push(reader.string());
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
            implementation_message_names: globalThis.Array.isArray(object?.implementation_message_names)
                ? object.implementation_message_names.map((e) => globalThis.String(e))
                : []
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.implementation_message_names?.length) {
            obj.implementation_message_names = message.implementation_message_names;
        }
        return obj;
    },
    create(base) {
        return ListImplementationsResponse.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseListImplementationsResponse();
        message.implementation_message_names = object.implementation_message_names?.map((e) => e) || [];
        return message;
    }
};
function createBaseListAllInterfacesRequest() {
    return {};
}
function createBaseListAllInterfacesResponse() {
    return { interface_names: [] };
}
function createBaseListImplementationsRequest() {
    return { interface_name: "" };
}
function createBaseListImplementationsResponse() {
    return { implementation_message_names: [] };
}
function isSet(value) {
    return value !== null && value !== undefined;
}
export const registry = [
    ["/cosmos.base.reflection.v1beta1.ListAllInterfacesRequest", ListAllInterfacesRequest],
    ["/cosmos.base.reflection.v1beta1.ListAllInterfacesResponse", ListAllInterfacesResponse],
    ["/cosmos.base.reflection.v1beta1.ListImplementationsRequest", ListImplementationsRequest],
    ["/cosmos.base.reflection.v1beta1.ListImplementationsResponse", ListImplementationsResponse]
];
export const aminoConverters = {
    "/cosmos.base.reflection.v1beta1.ListAllInterfacesRequest": {
        aminoType: "cosmos-sdk/ListAllInterfacesRequest",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.base.reflection.v1beta1.ListAllInterfacesResponse": {
        aminoType: "cosmos-sdk/ListAllInterfacesResponse",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.base.reflection.v1beta1.ListImplementationsRequest": {
        aminoType: "cosmos-sdk/ListImplementationsRequest",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.base.reflection.v1beta1.ListImplementationsResponse": {
        aminoType: "cosmos-sdk/ListImplementationsResponse",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    }
};
