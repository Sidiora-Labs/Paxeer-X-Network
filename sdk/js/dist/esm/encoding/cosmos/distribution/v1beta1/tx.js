import { BinaryReader, BinaryWriter } from "@bufbuild/protobuf/wire";
import { Coin } from "../../base/v1beta1/coin";
export const MsgSetWithdrawAddress = {
    $type: "cosmos.distribution.v1beta1.MsgSetWithdrawAddress",
    encode(message, writer = new BinaryWriter()) {
        if (message.delegator_address !== "") {
            writer.uint32(10).string(message.delegator_address);
        }
        if (message.withdraw_address !== "") {
            writer.uint32(18).string(message.withdraw_address);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseMsgSetWithdrawAddress();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.delegator_address = reader.string();
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.withdraw_address = reader.string();
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
            delegator_address: isSet(object.delegator_address) ? globalThis.String(object.delegator_address) : "",
            withdraw_address: isSet(object.withdraw_address) ? globalThis.String(object.withdraw_address) : ""
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.delegator_address !== "") {
            obj.delegator_address = message.delegator_address;
        }
        if (message.withdraw_address !== "") {
            obj.withdraw_address = message.withdraw_address;
        }
        return obj;
    },
    create(base) {
        return MsgSetWithdrawAddress.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseMsgSetWithdrawAddress();
        message.delegator_address = object.delegator_address ?? "";
        message.withdraw_address = object.withdraw_address ?? "";
        return message;
    }
};
export const MsgSetWithdrawAddressResponse = {
    $type: "cosmos.distribution.v1beta1.MsgSetWithdrawAddressResponse",
    encode(_, writer = new BinaryWriter()) {
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseMsgSetWithdrawAddressResponse();
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
        return MsgSetWithdrawAddressResponse.fromPartial(base ?? {});
    },
    fromPartial(_) {
        const message = createBaseMsgSetWithdrawAddressResponse();
        return message;
    }
};
export const MsgWithdrawDelegatorReward = {
    $type: "cosmos.distribution.v1beta1.MsgWithdrawDelegatorReward",
    encode(message, writer = new BinaryWriter()) {
        if (message.delegator_address !== "") {
            writer.uint32(10).string(message.delegator_address);
        }
        if (message.validator_address !== "") {
            writer.uint32(18).string(message.validator_address);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseMsgWithdrawDelegatorReward();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.delegator_address = reader.string();
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.validator_address = reader.string();
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
            delegator_address: isSet(object.delegator_address) ? globalThis.String(object.delegator_address) : "",
            validator_address: isSet(object.validator_address) ? globalThis.String(object.validator_address) : ""
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.delegator_address !== "") {
            obj.delegator_address = message.delegator_address;
        }
        if (message.validator_address !== "") {
            obj.validator_address = message.validator_address;
        }
        return obj;
    },
    create(base) {
        return MsgWithdrawDelegatorReward.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseMsgWithdrawDelegatorReward();
        message.delegator_address = object.delegator_address ?? "";
        message.validator_address = object.validator_address ?? "";
        return message;
    }
};
export const MsgWithdrawDelegatorRewardResponse = {
    $type: "cosmos.distribution.v1beta1.MsgWithdrawDelegatorRewardResponse",
    encode(_, writer = new BinaryWriter()) {
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseMsgWithdrawDelegatorRewardResponse();
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
        return MsgWithdrawDelegatorRewardResponse.fromPartial(base ?? {});
    },
    fromPartial(_) {
        const message = createBaseMsgWithdrawDelegatorRewardResponse();
        return message;
    }
};
export const MsgWithdrawValidatorCommission = {
    $type: "cosmos.distribution.v1beta1.MsgWithdrawValidatorCommission",
    encode(message, writer = new BinaryWriter()) {
        if (message.validator_address !== "") {
            writer.uint32(10).string(message.validator_address);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseMsgWithdrawValidatorCommission();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.validator_address = reader.string();
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
        return { validator_address: isSet(object.validator_address) ? globalThis.String(object.validator_address) : "" };
    },
    toJSON(message) {
        const obj = {};
        if (message.validator_address !== "") {
            obj.validator_address = message.validator_address;
        }
        return obj;
    },
    create(base) {
        return MsgWithdrawValidatorCommission.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseMsgWithdrawValidatorCommission();
        message.validator_address = object.validator_address ?? "";
        return message;
    }
};
export const MsgWithdrawValidatorCommissionResponse = {
    $type: "cosmos.distribution.v1beta1.MsgWithdrawValidatorCommissionResponse",
    encode(_, writer = new BinaryWriter()) {
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseMsgWithdrawValidatorCommissionResponse();
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
        return MsgWithdrawValidatorCommissionResponse.fromPartial(base ?? {});
    },
    fromPartial(_) {
        const message = createBaseMsgWithdrawValidatorCommissionResponse();
        return message;
    }
};
export const MsgFundCommunityPool = {
    $type: "cosmos.distribution.v1beta1.MsgFundCommunityPool",
    encode(message, writer = new BinaryWriter()) {
        for (const v of message.amount) {
            Coin.encode(v, writer.uint32(10).fork()).join();
        }
        if (message.depositor !== "") {
            writer.uint32(18).string(message.depositor);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseMsgFundCommunityPool();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.amount.push(Coin.decode(reader, reader.uint32()));
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.depositor = reader.string();
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
            amount: globalThis.Array.isArray(object?.amount) ? object.amount.map((e) => Coin.fromJSON(e)) : [],
            depositor: isSet(object.depositor) ? globalThis.String(object.depositor) : ""
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.amount?.length) {
            obj.amount = message.amount.map((e) => Coin.toJSON(e));
        }
        if (message.depositor !== "") {
            obj.depositor = message.depositor;
        }
        return obj;
    },
    create(base) {
        return MsgFundCommunityPool.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseMsgFundCommunityPool();
        message.amount = object.amount?.map((e) => Coin.fromPartial(e)) || [];
        message.depositor = object.depositor ?? "";
        return message;
    }
};
export const MsgFundCommunityPoolResponse = {
    $type: "cosmos.distribution.v1beta1.MsgFundCommunityPoolResponse",
    encode(_, writer = new BinaryWriter()) {
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseMsgFundCommunityPoolResponse();
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
        return MsgFundCommunityPoolResponse.fromPartial(base ?? {});
    },
    fromPartial(_) {
        const message = createBaseMsgFundCommunityPoolResponse();
        return message;
    }
};
function createBaseMsgSetWithdrawAddress() {
    return { delegator_address: "", withdraw_address: "" };
}
function createBaseMsgSetWithdrawAddressResponse() {
    return {};
}
function createBaseMsgWithdrawDelegatorReward() {
    return { delegator_address: "", validator_address: "" };
}
function createBaseMsgWithdrawDelegatorRewardResponse() {
    return {};
}
function createBaseMsgWithdrawValidatorCommission() {
    return { validator_address: "" };
}
function createBaseMsgWithdrawValidatorCommissionResponse() {
    return {};
}
function createBaseMsgFundCommunityPool() {
    return { amount: [], depositor: "" };
}
function createBaseMsgFundCommunityPoolResponse() {
    return {};
}
function isSet(value) {
    return value !== null && value !== undefined;
}
export const registry = [
    ["/cosmos.distribution.v1beta1.MsgSetWithdrawAddress", MsgSetWithdrawAddress],
    ["/cosmos.distribution.v1beta1.MsgSetWithdrawAddressResponse", MsgSetWithdrawAddressResponse],
    ["/cosmos.distribution.v1beta1.MsgWithdrawDelegatorReward", MsgWithdrawDelegatorReward],
    ["/cosmos.distribution.v1beta1.MsgWithdrawDelegatorRewardResponse", MsgWithdrawDelegatorRewardResponse],
    ["/cosmos.distribution.v1beta1.MsgWithdrawValidatorCommission", MsgWithdrawValidatorCommission],
    ["/cosmos.distribution.v1beta1.MsgWithdrawValidatorCommissionResponse", MsgWithdrawValidatorCommissionResponse],
    ["/cosmos.distribution.v1beta1.MsgFundCommunityPool", MsgFundCommunityPool],
    ["/cosmos.distribution.v1beta1.MsgFundCommunityPoolResponse", MsgFundCommunityPoolResponse]
];
export const aminoConverters = {
    "/cosmos.distribution.v1beta1.MsgSetWithdrawAddress": {
        aminoType: "cosmos-sdk/MsgSetWithdrawAddress",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.distribution.v1beta1.MsgSetWithdrawAddressResponse": {
        aminoType: "cosmos-sdk/MsgSetWithdrawAddressResponse",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.distribution.v1beta1.MsgWithdrawDelegatorReward": {
        aminoType: "cosmos-sdk/MsgWithdrawDelegatorReward",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.distribution.v1beta1.MsgWithdrawDelegatorRewardResponse": {
        aminoType: "cosmos-sdk/MsgWithdrawDelegatorRewardResponse",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.distribution.v1beta1.MsgWithdrawValidatorCommission": {
        aminoType: "cosmos-sdk/MsgWithdrawValidatorCommission",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.distribution.v1beta1.MsgWithdrawValidatorCommissionResponse": {
        aminoType: "cosmos-sdk/MsgWithdrawValidatorCommissionResponse",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.distribution.v1beta1.MsgFundCommunityPool": {
        aminoType: "cosmos-sdk/MsgFundCommunityPool",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.distribution.v1beta1.MsgFundCommunityPoolResponse": {
        aminoType: "cosmos-sdk/MsgFundCommunityPoolResponse",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    }
};
