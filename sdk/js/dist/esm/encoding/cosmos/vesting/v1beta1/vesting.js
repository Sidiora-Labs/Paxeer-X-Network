import { BinaryReader, BinaryWriter } from "@bufbuild/protobuf/wire";
import { BaseAccount } from "../../auth/v1beta1/auth";
import { Coin } from "../../base/v1beta1/coin";
export const BaseVestingAccount = {
    $type: "cosmos.vesting.v1beta1.BaseVestingAccount",
    encode(message, writer = new BinaryWriter()) {
        if (message.base_account !== undefined) {
            BaseAccount.encode(message.base_account, writer.uint32(10).fork()).join();
        }
        for (const v of message.original_vesting) {
            Coin.encode(v, writer.uint32(18).fork()).join();
        }
        for (const v of message.delegated_free) {
            Coin.encode(v, writer.uint32(26).fork()).join();
        }
        for (const v of message.delegated_vesting) {
            Coin.encode(v, writer.uint32(34).fork()).join();
        }
        if (message.end_time !== 0) {
            writer.uint32(40).int64(message.end_time);
        }
        if (message.admin !== "") {
            writer.uint32(50).string(message.admin);
        }
        if (message.cancelled_time !== 0) {
            writer.uint32(56).int64(message.cancelled_time);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseBaseVestingAccount();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.base_account = BaseAccount.decode(reader, reader.uint32());
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.original_vesting.push(Coin.decode(reader, reader.uint32()));
                    continue;
                case 3:
                    if (tag !== 26) {
                        break;
                    }
                    message.delegated_free.push(Coin.decode(reader, reader.uint32()));
                    continue;
                case 4:
                    if (tag !== 34) {
                        break;
                    }
                    message.delegated_vesting.push(Coin.decode(reader, reader.uint32()));
                    continue;
                case 5:
                    if (tag !== 40) {
                        break;
                    }
                    message.end_time = longToNumber(reader.int64());
                    continue;
                case 6:
                    if (tag !== 50) {
                        break;
                    }
                    message.admin = reader.string();
                    continue;
                case 7:
                    if (tag !== 56) {
                        break;
                    }
                    message.cancelled_time = longToNumber(reader.int64());
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
            base_account: isSet(object.base_account) ? BaseAccount.fromJSON(object.base_account) : undefined,
            original_vesting: globalThis.Array.isArray(object?.original_vesting) ? object.original_vesting.map((e) => Coin.fromJSON(e)) : [],
            delegated_free: globalThis.Array.isArray(object?.delegated_free) ? object.delegated_free.map((e) => Coin.fromJSON(e)) : [],
            delegated_vesting: globalThis.Array.isArray(object?.delegated_vesting) ? object.delegated_vesting.map((e) => Coin.fromJSON(e)) : [],
            end_time: isSet(object.end_time) ? globalThis.Number(object.end_time) : 0,
            admin: isSet(object.admin) ? globalThis.String(object.admin) : "",
            cancelled_time: isSet(object.cancelled_time) ? globalThis.Number(object.cancelled_time) : 0
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.base_account !== undefined) {
            obj.base_account = BaseAccount.toJSON(message.base_account);
        }
        if (message.original_vesting?.length) {
            obj.original_vesting = message.original_vesting.map((e) => Coin.toJSON(e));
        }
        if (message.delegated_free?.length) {
            obj.delegated_free = message.delegated_free.map((e) => Coin.toJSON(e));
        }
        if (message.delegated_vesting?.length) {
            obj.delegated_vesting = message.delegated_vesting.map((e) => Coin.toJSON(e));
        }
        if (message.end_time !== 0) {
            obj.end_time = Math.round(message.end_time);
        }
        if (message.admin !== "") {
            obj.admin = message.admin;
        }
        if (message.cancelled_time !== 0) {
            obj.cancelled_time = Math.round(message.cancelled_time);
        }
        return obj;
    },
    create(base) {
        return BaseVestingAccount.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseBaseVestingAccount();
        message.base_account = object.base_account !== undefined && object.base_account !== null ? BaseAccount.fromPartial(object.base_account) : undefined;
        message.original_vesting = object.original_vesting?.map((e) => Coin.fromPartial(e)) || [];
        message.delegated_free = object.delegated_free?.map((e) => Coin.fromPartial(e)) || [];
        message.delegated_vesting = object.delegated_vesting?.map((e) => Coin.fromPartial(e)) || [];
        message.end_time = object.end_time ?? 0;
        message.admin = object.admin ?? "";
        message.cancelled_time = object.cancelled_time ?? 0;
        return message;
    }
};
export const ContinuousVestingAccount = {
    $type: "cosmos.vesting.v1beta1.ContinuousVestingAccount",
    encode(message, writer = new BinaryWriter()) {
        if (message.base_vesting_account !== undefined) {
            BaseVestingAccount.encode(message.base_vesting_account, writer.uint32(10).fork()).join();
        }
        if (message.start_time !== 0) {
            writer.uint32(16).int64(message.start_time);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseContinuousVestingAccount();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.base_vesting_account = BaseVestingAccount.decode(reader, reader.uint32());
                    continue;
                case 2:
                    if (tag !== 16) {
                        break;
                    }
                    message.start_time = longToNumber(reader.int64());
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
            base_vesting_account: isSet(object.base_vesting_account) ? BaseVestingAccount.fromJSON(object.base_vesting_account) : undefined,
            start_time: isSet(object.start_time) ? globalThis.Number(object.start_time) : 0
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.base_vesting_account !== undefined) {
            obj.base_vesting_account = BaseVestingAccount.toJSON(message.base_vesting_account);
        }
        if (message.start_time !== 0) {
            obj.start_time = Math.round(message.start_time);
        }
        return obj;
    },
    create(base) {
        return ContinuousVestingAccount.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseContinuousVestingAccount();
        message.base_vesting_account =
            object.base_vesting_account !== undefined && object.base_vesting_account !== null
                ? BaseVestingAccount.fromPartial(object.base_vesting_account)
                : undefined;
        message.start_time = object.start_time ?? 0;
        return message;
    }
};
export const DelayedVestingAccount = {
    $type: "cosmos.vesting.v1beta1.DelayedVestingAccount",
    encode(message, writer = new BinaryWriter()) {
        if (message.base_vesting_account !== undefined) {
            BaseVestingAccount.encode(message.base_vesting_account, writer.uint32(10).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseDelayedVestingAccount();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.base_vesting_account = BaseVestingAccount.decode(reader, reader.uint32());
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
            base_vesting_account: isSet(object.base_vesting_account) ? BaseVestingAccount.fromJSON(object.base_vesting_account) : undefined
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.base_vesting_account !== undefined) {
            obj.base_vesting_account = BaseVestingAccount.toJSON(message.base_vesting_account);
        }
        return obj;
    },
    create(base) {
        return DelayedVestingAccount.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseDelayedVestingAccount();
        message.base_vesting_account =
            object.base_vesting_account !== undefined && object.base_vesting_account !== null
                ? BaseVestingAccount.fromPartial(object.base_vesting_account)
                : undefined;
        return message;
    }
};
export const Period = {
    $type: "cosmos.vesting.v1beta1.Period",
    encode(message, writer = new BinaryWriter()) {
        if (message.length !== 0) {
            writer.uint32(8).int64(message.length);
        }
        for (const v of message.amount) {
            Coin.encode(v, writer.uint32(18).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBasePeriod();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 8) {
                        break;
                    }
                    message.length = longToNumber(reader.int64());
                    continue;
                case 2:
                    if (tag !== 18) {
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
            length: isSet(object.length) ? globalThis.Number(object.length) : 0,
            amount: globalThis.Array.isArray(object?.amount) ? object.amount.map((e) => Coin.fromJSON(e)) : []
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.length !== 0) {
            obj.length = Math.round(message.length);
        }
        if (message.amount?.length) {
            obj.amount = message.amount.map((e) => Coin.toJSON(e));
        }
        return obj;
    },
    create(base) {
        return Period.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBasePeriod();
        message.length = object.length ?? 0;
        message.amount = object.amount?.map((e) => Coin.fromPartial(e)) || [];
        return message;
    }
};
export const PeriodicVestingAccount = {
    $type: "cosmos.vesting.v1beta1.PeriodicVestingAccount",
    encode(message, writer = new BinaryWriter()) {
        if (message.base_vesting_account !== undefined) {
            BaseVestingAccount.encode(message.base_vesting_account, writer.uint32(10).fork()).join();
        }
        if (message.start_time !== 0) {
            writer.uint32(16).int64(message.start_time);
        }
        for (const v of message.vesting_periods) {
            Period.encode(v, writer.uint32(26).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBasePeriodicVestingAccount();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.base_vesting_account = BaseVestingAccount.decode(reader, reader.uint32());
                    continue;
                case 2:
                    if (tag !== 16) {
                        break;
                    }
                    message.start_time = longToNumber(reader.int64());
                    continue;
                case 3:
                    if (tag !== 26) {
                        break;
                    }
                    message.vesting_periods.push(Period.decode(reader, reader.uint32()));
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
            base_vesting_account: isSet(object.base_vesting_account) ? BaseVestingAccount.fromJSON(object.base_vesting_account) : undefined,
            start_time: isSet(object.start_time) ? globalThis.Number(object.start_time) : 0,
            vesting_periods: globalThis.Array.isArray(object?.vesting_periods) ? object.vesting_periods.map((e) => Period.fromJSON(e)) : []
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.base_vesting_account !== undefined) {
            obj.base_vesting_account = BaseVestingAccount.toJSON(message.base_vesting_account);
        }
        if (message.start_time !== 0) {
            obj.start_time = Math.round(message.start_time);
        }
        if (message.vesting_periods?.length) {
            obj.vesting_periods = message.vesting_periods.map((e) => Period.toJSON(e));
        }
        return obj;
    },
    create(base) {
        return PeriodicVestingAccount.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBasePeriodicVestingAccount();
        message.base_vesting_account =
            object.base_vesting_account !== undefined && object.base_vesting_account !== null
                ? BaseVestingAccount.fromPartial(object.base_vesting_account)
                : undefined;
        message.start_time = object.start_time ?? 0;
        message.vesting_periods = object.vesting_periods?.map((e) => Period.fromPartial(e)) || [];
        return message;
    }
};
export const PermanentLockedAccount = {
    $type: "cosmos.vesting.v1beta1.PermanentLockedAccount",
    encode(message, writer = new BinaryWriter()) {
        if (message.base_vesting_account !== undefined) {
            BaseVestingAccount.encode(message.base_vesting_account, writer.uint32(10).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBasePermanentLockedAccount();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.base_vesting_account = BaseVestingAccount.decode(reader, reader.uint32());
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
            base_vesting_account: isSet(object.base_vesting_account) ? BaseVestingAccount.fromJSON(object.base_vesting_account) : undefined
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.base_vesting_account !== undefined) {
            obj.base_vesting_account = BaseVestingAccount.toJSON(message.base_vesting_account);
        }
        return obj;
    },
    create(base) {
        return PermanentLockedAccount.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBasePermanentLockedAccount();
        message.base_vesting_account =
            object.base_vesting_account !== undefined && object.base_vesting_account !== null
                ? BaseVestingAccount.fromPartial(object.base_vesting_account)
                : undefined;
        return message;
    }
};
function createBaseBaseVestingAccount() {
    return {
        base_account: undefined,
        original_vesting: [],
        delegated_free: [],
        delegated_vesting: [],
        end_time: 0,
        admin: "",
        cancelled_time: 0
    };
}
function createBaseContinuousVestingAccount() {
    return { base_vesting_account: undefined, start_time: 0 };
}
function createBaseDelayedVestingAccount() {
    return { base_vesting_account: undefined };
}
function createBasePeriod() {
    return { length: 0, amount: [] };
}
function createBasePeriodicVestingAccount() {
    return { base_vesting_account: undefined, start_time: 0, vesting_periods: [] };
}
function createBasePermanentLockedAccount() {
    return { base_vesting_account: undefined };
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
    ["/cosmos.vesting.v1beta1.BaseVestingAccount", BaseVestingAccount],
    ["/cosmos.vesting.v1beta1.ContinuousVestingAccount", ContinuousVestingAccount],
    ["/cosmos.vesting.v1beta1.DelayedVestingAccount", DelayedVestingAccount],
    ["/cosmos.vesting.v1beta1.Period", Period],
    ["/cosmos.vesting.v1beta1.PeriodicVestingAccount", PeriodicVestingAccount],
    ["/cosmos.vesting.v1beta1.PermanentLockedAccount", PermanentLockedAccount]
];
export const aminoConverters = {
    "/cosmos.vesting.v1beta1.BaseVestingAccount": {
        aminoType: "cosmos-sdk/BaseVestingAccount",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.vesting.v1beta1.ContinuousVestingAccount": {
        aminoType: "cosmos-sdk/ContinuousVestingAccount",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.vesting.v1beta1.DelayedVestingAccount": {
        aminoType: "cosmos-sdk/DelayedVestingAccount",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.vesting.v1beta1.Period": {
        aminoType: "cosmos-sdk/Period",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.vesting.v1beta1.PeriodicVestingAccount": {
        aminoType: "cosmos-sdk/PeriodicVestingAccount",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.vesting.v1beta1.PermanentLockedAccount": {
        aminoType: "cosmos-sdk/PermanentLockedAccount",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    }
};
