"use strict";
Object.defineProperty(exports, "__esModule", { value: true });
exports.aminoConverters = exports.registry = exports.StakeAuthorizationValidators = exports.StakeAuthorization = void 0;
exports.authorizationTypeFromJSON = authorizationTypeFromJSON;
exports.authorizationTypeToJSON = authorizationTypeToJSON;
const wire_1 = require("@bufbuild/protobuf/wire");
const coin_1 = require("../../base/v1beta1/coin");
const v1beta1_1 = require("../../../../types/cosmos/staking/v1beta1");
exports.StakeAuthorization = {
    $type: "cosmos.staking.v1beta1.StakeAuthorization",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.max_tokens !== undefined) {
            coin_1.Coin.encode(message.max_tokens, writer.uint32(10).fork()).join();
        }
        if (message.allow_list !== undefined) {
            exports.StakeAuthorizationValidators.encode(message.allow_list, writer.uint32(18).fork()).join();
        }
        if (message.deny_list !== undefined) {
            exports.StakeAuthorizationValidators.encode(message.deny_list, writer.uint32(26).fork()).join();
        }
        if (message.authorization_type !== 0) {
            writer.uint32(32).int32(message.authorization_type);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseStakeAuthorization();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.max_tokens = coin_1.Coin.decode(reader, reader.uint32());
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.allow_list = exports.StakeAuthorizationValidators.decode(reader, reader.uint32());
                    continue;
                case 3:
                    if (tag !== 26) {
                        break;
                    }
                    message.deny_list = exports.StakeAuthorizationValidators.decode(reader, reader.uint32());
                    continue;
                case 4:
                    if (tag !== 32) {
                        break;
                    }
                    message.authorization_type = reader.int32();
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
            max_tokens: isSet(object.max_tokens) ? coin_1.Coin.fromJSON(object.max_tokens) : undefined,
            allow_list: isSet(object.allow_list) ? exports.StakeAuthorizationValidators.fromJSON(object.allow_list) : undefined,
            deny_list: isSet(object.deny_list) ? exports.StakeAuthorizationValidators.fromJSON(object.deny_list) : undefined,
            authorization_type: isSet(object.authorization_type) ? authorizationTypeFromJSON(object.authorization_type) : 0
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.max_tokens !== undefined) {
            obj.max_tokens = coin_1.Coin.toJSON(message.max_tokens);
        }
        if (message.allow_list !== undefined) {
            obj.allow_list = exports.StakeAuthorizationValidators.toJSON(message.allow_list);
        }
        if (message.deny_list !== undefined) {
            obj.deny_list = exports.StakeAuthorizationValidators.toJSON(message.deny_list);
        }
        if (message.authorization_type !== 0) {
            obj.authorization_type = authorizationTypeToJSON(message.authorization_type);
        }
        return obj;
    },
    create(base) {
        return exports.StakeAuthorization.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseStakeAuthorization();
        message.max_tokens = object.max_tokens !== undefined && object.max_tokens !== null ? coin_1.Coin.fromPartial(object.max_tokens) : undefined;
        message.allow_list =
            object.allow_list !== undefined && object.allow_list !== null ? exports.StakeAuthorizationValidators.fromPartial(object.allow_list) : undefined;
        message.deny_list = object.deny_list !== undefined && object.deny_list !== null ? exports.StakeAuthorizationValidators.fromPartial(object.deny_list) : undefined;
        message.authorization_type = object.authorization_type ?? 0;
        return message;
    }
};
exports.StakeAuthorizationValidators = {
    $type: "cosmos.staking.v1beta1.StakeAuthorization.Validators",
    encode(message, writer = new wire_1.BinaryWriter()) {
        for (const v of message.address) {
            writer.uint32(10).string(v);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseStakeAuthorizationValidators();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.address.push(reader.string());
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
            address: globalThis.Array.isArray(object?.address) ? object.address.map((e) => globalThis.String(e)) : []
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.address?.length) {
            obj.address = message.address;
        }
        return obj;
    },
    create(base) {
        return exports.StakeAuthorizationValidators.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseStakeAuthorizationValidators();
        message.address = object.address?.map((e) => e) || [];
        return message;
    }
};
function authorizationTypeFromJSON(object) {
    switch (object) {
        case 0:
        case "AUTHORIZATION_TYPE_UNSPECIFIED":
            return v1beta1_1.AuthorizationType.AUTHORIZATION_TYPE_UNSPECIFIED;
        case 1:
        case "AUTHORIZATION_TYPE_DELEGATE":
            return v1beta1_1.AuthorizationType.AUTHORIZATION_TYPE_DELEGATE;
        case 2:
        case "AUTHORIZATION_TYPE_UNDELEGATE":
            return v1beta1_1.AuthorizationType.AUTHORIZATION_TYPE_UNDELEGATE;
        case 3:
        case "AUTHORIZATION_TYPE_REDELEGATE":
            return v1beta1_1.AuthorizationType.AUTHORIZATION_TYPE_REDELEGATE;
        case -1:
        case "UNRECOGNIZED":
        default:
            return v1beta1_1.AuthorizationType.UNRECOGNIZED;
    }
}
function authorizationTypeToJSON(object) {
    switch (object) {
        case v1beta1_1.AuthorizationType.AUTHORIZATION_TYPE_UNSPECIFIED:
            return "AUTHORIZATION_TYPE_UNSPECIFIED";
        case v1beta1_1.AuthorizationType.AUTHORIZATION_TYPE_DELEGATE:
            return "AUTHORIZATION_TYPE_DELEGATE";
        case v1beta1_1.AuthorizationType.AUTHORIZATION_TYPE_UNDELEGATE:
            return "AUTHORIZATION_TYPE_UNDELEGATE";
        case v1beta1_1.AuthorizationType.AUTHORIZATION_TYPE_REDELEGATE:
            return "AUTHORIZATION_TYPE_REDELEGATE";
        case v1beta1_1.AuthorizationType.UNRECOGNIZED:
        default:
            return "UNRECOGNIZED";
    }
}
function createBaseStakeAuthorization() {
    return { max_tokens: undefined, allow_list: undefined, deny_list: undefined, authorization_type: 0 };
}
function createBaseStakeAuthorizationValidators() {
    return { address: [] };
}
function isSet(value) {
    return value !== null && value !== undefined;
}
exports.registry = [
    ["/cosmos.staking.v1beta1.StakeAuthorization", exports.StakeAuthorization],
    ["/cosmos.staking.v1beta1.StakeAuthorization.Validators", exports.StakeAuthorizationValidators]
];
exports.aminoConverters = {
    "/cosmos.staking.v1beta1.StakeAuthorization": {
        aminoType: "cosmos-sdk/StakeAuthorization",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.staking.v1beta1.StakeAuthorization.Validators": {
        aminoType: "cosmos-sdk/Validators",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    }
};
