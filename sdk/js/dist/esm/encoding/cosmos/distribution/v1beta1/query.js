import { BinaryReader, BinaryWriter } from "@bufbuild/protobuf/wire";
import { PageRequest, PageResponse } from "../../base/query/v1beta1/pagination";
import { DecCoin } from "../../base/v1beta1/coin";
import { DelegationDelegatorReward, Params, ValidatorAccumulatedCommission, ValidatorOutstandingRewards, ValidatorSlashEvent } from "./distribution";
export const QueryParamsRequest = {
    $type: "cosmos.distribution.v1beta1.QueryParamsRequest",
    encode(_, writer = new BinaryWriter()) {
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseQueryParamsRequest();
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
        return QueryParamsRequest.fromPartial(base ?? {});
    },
    fromPartial(_) {
        const message = createBaseQueryParamsRequest();
        return message;
    }
};
export const QueryParamsResponse = {
    $type: "cosmos.distribution.v1beta1.QueryParamsResponse",
    encode(message, writer = new BinaryWriter()) {
        if (message.params !== undefined) {
            Params.encode(message.params, writer.uint32(10).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseQueryParamsResponse();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.params = Params.decode(reader, reader.uint32());
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
        return { params: isSet(object.params) ? Params.fromJSON(object.params) : undefined };
    },
    toJSON(message) {
        const obj = {};
        if (message.params !== undefined) {
            obj.params = Params.toJSON(message.params);
        }
        return obj;
    },
    create(base) {
        return QueryParamsResponse.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryParamsResponse();
        message.params = object.params !== undefined && object.params !== null ? Params.fromPartial(object.params) : undefined;
        return message;
    }
};
export const QueryValidatorOutstandingRewardsRequest = {
    $type: "cosmos.distribution.v1beta1.QueryValidatorOutstandingRewardsRequest",
    encode(message, writer = new BinaryWriter()) {
        if (message.validator_address !== "") {
            writer.uint32(10).string(message.validator_address);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseQueryValidatorOutstandingRewardsRequest();
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
        return QueryValidatorOutstandingRewardsRequest.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryValidatorOutstandingRewardsRequest();
        message.validator_address = object.validator_address ?? "";
        return message;
    }
};
export const QueryValidatorOutstandingRewardsResponse = {
    $type: "cosmos.distribution.v1beta1.QueryValidatorOutstandingRewardsResponse",
    encode(message, writer = new BinaryWriter()) {
        if (message.rewards !== undefined) {
            ValidatorOutstandingRewards.encode(message.rewards, writer.uint32(10).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseQueryValidatorOutstandingRewardsResponse();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.rewards = ValidatorOutstandingRewards.decode(reader, reader.uint32());
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
        return { rewards: isSet(object.rewards) ? ValidatorOutstandingRewards.fromJSON(object.rewards) : undefined };
    },
    toJSON(message) {
        const obj = {};
        if (message.rewards !== undefined) {
            obj.rewards = ValidatorOutstandingRewards.toJSON(message.rewards);
        }
        return obj;
    },
    create(base) {
        return QueryValidatorOutstandingRewardsResponse.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryValidatorOutstandingRewardsResponse();
        message.rewards = object.rewards !== undefined && object.rewards !== null ? ValidatorOutstandingRewards.fromPartial(object.rewards) : undefined;
        return message;
    }
};
export const QueryValidatorCommissionRequest = {
    $type: "cosmos.distribution.v1beta1.QueryValidatorCommissionRequest",
    encode(message, writer = new BinaryWriter()) {
        if (message.validator_address !== "") {
            writer.uint32(10).string(message.validator_address);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseQueryValidatorCommissionRequest();
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
        return QueryValidatorCommissionRequest.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryValidatorCommissionRequest();
        message.validator_address = object.validator_address ?? "";
        return message;
    }
};
export const QueryValidatorCommissionResponse = {
    $type: "cosmos.distribution.v1beta1.QueryValidatorCommissionResponse",
    encode(message, writer = new BinaryWriter()) {
        if (message.commission !== undefined) {
            ValidatorAccumulatedCommission.encode(message.commission, writer.uint32(10).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseQueryValidatorCommissionResponse();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.commission = ValidatorAccumulatedCommission.decode(reader, reader.uint32());
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
            commission: isSet(object.commission) ? ValidatorAccumulatedCommission.fromJSON(object.commission) : undefined
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.commission !== undefined) {
            obj.commission = ValidatorAccumulatedCommission.toJSON(message.commission);
        }
        return obj;
    },
    create(base) {
        return QueryValidatorCommissionResponse.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryValidatorCommissionResponse();
        message.commission =
            object.commission !== undefined && object.commission !== null ? ValidatorAccumulatedCommission.fromPartial(object.commission) : undefined;
        return message;
    }
};
export const QueryValidatorSlashesRequest = {
    $type: "cosmos.distribution.v1beta1.QueryValidatorSlashesRequest",
    encode(message, writer = new BinaryWriter()) {
        if (message.validator_address !== "") {
            writer.uint32(10).string(message.validator_address);
        }
        if (message.starting_height !== 0) {
            writer.uint32(16).uint64(message.starting_height);
        }
        if (message.ending_height !== 0) {
            writer.uint32(24).uint64(message.ending_height);
        }
        if (message.pagination !== undefined) {
            PageRequest.encode(message.pagination, writer.uint32(34).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseQueryValidatorSlashesRequest();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.validator_address = reader.string();
                    continue;
                case 2:
                    if (tag !== 16) {
                        break;
                    }
                    message.starting_height = longToNumber(reader.uint64());
                    continue;
                case 3:
                    if (tag !== 24) {
                        break;
                    }
                    message.ending_height = longToNumber(reader.uint64());
                    continue;
                case 4:
                    if (tag !== 34) {
                        break;
                    }
                    message.pagination = PageRequest.decode(reader, reader.uint32());
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
            validator_address: isSet(object.validator_address) ? globalThis.String(object.validator_address) : "",
            starting_height: isSet(object.starting_height) ? globalThis.Number(object.starting_height) : 0,
            ending_height: isSet(object.ending_height) ? globalThis.Number(object.ending_height) : 0,
            pagination: isSet(object.pagination) ? PageRequest.fromJSON(object.pagination) : undefined
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.validator_address !== "") {
            obj.validator_address = message.validator_address;
        }
        if (message.starting_height !== 0) {
            obj.starting_height = Math.round(message.starting_height);
        }
        if (message.ending_height !== 0) {
            obj.ending_height = Math.round(message.ending_height);
        }
        if (message.pagination !== undefined) {
            obj.pagination = PageRequest.toJSON(message.pagination);
        }
        return obj;
    },
    create(base) {
        return QueryValidatorSlashesRequest.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryValidatorSlashesRequest();
        message.validator_address = object.validator_address ?? "";
        message.starting_height = object.starting_height ?? 0;
        message.ending_height = object.ending_height ?? 0;
        message.pagination = object.pagination !== undefined && object.pagination !== null ? PageRequest.fromPartial(object.pagination) : undefined;
        return message;
    }
};
export const QueryValidatorSlashesResponse = {
    $type: "cosmos.distribution.v1beta1.QueryValidatorSlashesResponse",
    encode(message, writer = new BinaryWriter()) {
        for (const v of message.slashes) {
            ValidatorSlashEvent.encode(v, writer.uint32(10).fork()).join();
        }
        if (message.pagination !== undefined) {
            PageResponse.encode(message.pagination, writer.uint32(18).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseQueryValidatorSlashesResponse();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.slashes.push(ValidatorSlashEvent.decode(reader, reader.uint32()));
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.pagination = PageResponse.decode(reader, reader.uint32());
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
            slashes: globalThis.Array.isArray(object?.slashes) ? object.slashes.map((e) => ValidatorSlashEvent.fromJSON(e)) : [],
            pagination: isSet(object.pagination) ? PageResponse.fromJSON(object.pagination) : undefined
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.slashes?.length) {
            obj.slashes = message.slashes.map((e) => ValidatorSlashEvent.toJSON(e));
        }
        if (message.pagination !== undefined) {
            obj.pagination = PageResponse.toJSON(message.pagination);
        }
        return obj;
    },
    create(base) {
        return QueryValidatorSlashesResponse.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryValidatorSlashesResponse();
        message.slashes = object.slashes?.map((e) => ValidatorSlashEvent.fromPartial(e)) || [];
        message.pagination = object.pagination !== undefined && object.pagination !== null ? PageResponse.fromPartial(object.pagination) : undefined;
        return message;
    }
};
export const QueryDelegationRewardsRequest = {
    $type: "cosmos.distribution.v1beta1.QueryDelegationRewardsRequest",
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
        const message = createBaseQueryDelegationRewardsRequest();
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
        return QueryDelegationRewardsRequest.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryDelegationRewardsRequest();
        message.delegator_address = object.delegator_address ?? "";
        message.validator_address = object.validator_address ?? "";
        return message;
    }
};
export const QueryDelegationRewardsResponse = {
    $type: "cosmos.distribution.v1beta1.QueryDelegationRewardsResponse",
    encode(message, writer = new BinaryWriter()) {
        for (const v of message.rewards) {
            DecCoin.encode(v, writer.uint32(10).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseQueryDelegationRewardsResponse();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.rewards.push(DecCoin.decode(reader, reader.uint32()));
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
            rewards: globalThis.Array.isArray(object?.rewards) ? object.rewards.map((e) => DecCoin.fromJSON(e)) : []
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.rewards?.length) {
            obj.rewards = message.rewards.map((e) => DecCoin.toJSON(e));
        }
        return obj;
    },
    create(base) {
        return QueryDelegationRewardsResponse.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryDelegationRewardsResponse();
        message.rewards = object.rewards?.map((e) => DecCoin.fromPartial(e)) || [];
        return message;
    }
};
export const QueryDelegationTotalRewardsRequest = {
    $type: "cosmos.distribution.v1beta1.QueryDelegationTotalRewardsRequest",
    encode(message, writer = new BinaryWriter()) {
        if (message.delegator_address !== "") {
            writer.uint32(10).string(message.delegator_address);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseQueryDelegationTotalRewardsRequest();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.delegator_address = reader.string();
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
        return { delegator_address: isSet(object.delegator_address) ? globalThis.String(object.delegator_address) : "" };
    },
    toJSON(message) {
        const obj = {};
        if (message.delegator_address !== "") {
            obj.delegator_address = message.delegator_address;
        }
        return obj;
    },
    create(base) {
        return QueryDelegationTotalRewardsRequest.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryDelegationTotalRewardsRequest();
        message.delegator_address = object.delegator_address ?? "";
        return message;
    }
};
export const QueryDelegationTotalRewardsResponse = {
    $type: "cosmos.distribution.v1beta1.QueryDelegationTotalRewardsResponse",
    encode(message, writer = new BinaryWriter()) {
        for (const v of message.rewards) {
            DelegationDelegatorReward.encode(v, writer.uint32(10).fork()).join();
        }
        for (const v of message.total) {
            DecCoin.encode(v, writer.uint32(18).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseQueryDelegationTotalRewardsResponse();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.rewards.push(DelegationDelegatorReward.decode(reader, reader.uint32()));
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.total.push(DecCoin.decode(reader, reader.uint32()));
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
            rewards: globalThis.Array.isArray(object?.rewards) ? object.rewards.map((e) => DelegationDelegatorReward.fromJSON(e)) : [],
            total: globalThis.Array.isArray(object?.total) ? object.total.map((e) => DecCoin.fromJSON(e)) : []
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.rewards?.length) {
            obj.rewards = message.rewards.map((e) => DelegationDelegatorReward.toJSON(e));
        }
        if (message.total?.length) {
            obj.total = message.total.map((e) => DecCoin.toJSON(e));
        }
        return obj;
    },
    create(base) {
        return QueryDelegationTotalRewardsResponse.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryDelegationTotalRewardsResponse();
        message.rewards = object.rewards?.map((e) => DelegationDelegatorReward.fromPartial(e)) || [];
        message.total = object.total?.map((e) => DecCoin.fromPartial(e)) || [];
        return message;
    }
};
export const QueryDelegatorValidatorsRequest = {
    $type: "cosmos.distribution.v1beta1.QueryDelegatorValidatorsRequest",
    encode(message, writer = new BinaryWriter()) {
        if (message.delegator_address !== "") {
            writer.uint32(10).string(message.delegator_address);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseQueryDelegatorValidatorsRequest();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.delegator_address = reader.string();
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
        return { delegator_address: isSet(object.delegator_address) ? globalThis.String(object.delegator_address) : "" };
    },
    toJSON(message) {
        const obj = {};
        if (message.delegator_address !== "") {
            obj.delegator_address = message.delegator_address;
        }
        return obj;
    },
    create(base) {
        return QueryDelegatorValidatorsRequest.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryDelegatorValidatorsRequest();
        message.delegator_address = object.delegator_address ?? "";
        return message;
    }
};
export const QueryDelegatorValidatorsResponse = {
    $type: "cosmos.distribution.v1beta1.QueryDelegatorValidatorsResponse",
    encode(message, writer = new BinaryWriter()) {
        for (const v of message.validators) {
            writer.uint32(10).string(v);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseQueryDelegatorValidatorsResponse();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.validators.push(reader.string());
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
            validators: globalThis.Array.isArray(object?.validators) ? object.validators.map((e) => globalThis.String(e)) : []
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.validators?.length) {
            obj.validators = message.validators;
        }
        return obj;
    },
    create(base) {
        return QueryDelegatorValidatorsResponse.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryDelegatorValidatorsResponse();
        message.validators = object.validators?.map((e) => e) || [];
        return message;
    }
};
export const QueryDelegatorWithdrawAddressRequest = {
    $type: "cosmos.distribution.v1beta1.QueryDelegatorWithdrawAddressRequest",
    encode(message, writer = new BinaryWriter()) {
        if (message.delegator_address !== "") {
            writer.uint32(10).string(message.delegator_address);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseQueryDelegatorWithdrawAddressRequest();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.delegator_address = reader.string();
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
        return { delegator_address: isSet(object.delegator_address) ? globalThis.String(object.delegator_address) : "" };
    },
    toJSON(message) {
        const obj = {};
        if (message.delegator_address !== "") {
            obj.delegator_address = message.delegator_address;
        }
        return obj;
    },
    create(base) {
        return QueryDelegatorWithdrawAddressRequest.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryDelegatorWithdrawAddressRequest();
        message.delegator_address = object.delegator_address ?? "";
        return message;
    }
};
export const QueryDelegatorWithdrawAddressResponse = {
    $type: "cosmos.distribution.v1beta1.QueryDelegatorWithdrawAddressResponse",
    encode(message, writer = new BinaryWriter()) {
        if (message.withdraw_address !== "") {
            writer.uint32(10).string(message.withdraw_address);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseQueryDelegatorWithdrawAddressResponse();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
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
        return { withdraw_address: isSet(object.withdraw_address) ? globalThis.String(object.withdraw_address) : "" };
    },
    toJSON(message) {
        const obj = {};
        if (message.withdraw_address !== "") {
            obj.withdraw_address = message.withdraw_address;
        }
        return obj;
    },
    create(base) {
        return QueryDelegatorWithdrawAddressResponse.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryDelegatorWithdrawAddressResponse();
        message.withdraw_address = object.withdraw_address ?? "";
        return message;
    }
};
export const QueryCommunityPoolRequest = {
    $type: "cosmos.distribution.v1beta1.QueryCommunityPoolRequest",
    encode(_, writer = new BinaryWriter()) {
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseQueryCommunityPoolRequest();
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
        return QueryCommunityPoolRequest.fromPartial(base ?? {});
    },
    fromPartial(_) {
        const message = createBaseQueryCommunityPoolRequest();
        return message;
    }
};
export const QueryCommunityPoolResponse = {
    $type: "cosmos.distribution.v1beta1.QueryCommunityPoolResponse",
    encode(message, writer = new BinaryWriter()) {
        for (const v of message.pool) {
            DecCoin.encode(v, writer.uint32(10).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseQueryCommunityPoolResponse();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.pool.push(DecCoin.decode(reader, reader.uint32()));
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
        return { pool: globalThis.Array.isArray(object?.pool) ? object.pool.map((e) => DecCoin.fromJSON(e)) : [] };
    },
    toJSON(message) {
        const obj = {};
        if (message.pool?.length) {
            obj.pool = message.pool.map((e) => DecCoin.toJSON(e));
        }
        return obj;
    },
    create(base) {
        return QueryCommunityPoolResponse.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryCommunityPoolResponse();
        message.pool = object.pool?.map((e) => DecCoin.fromPartial(e)) || [];
        return message;
    }
};
function createBaseQueryParamsRequest() {
    return {};
}
function createBaseQueryParamsResponse() {
    return { params: undefined };
}
function createBaseQueryValidatorOutstandingRewardsRequest() {
    return { validator_address: "" };
}
function createBaseQueryValidatorOutstandingRewardsResponse() {
    return { rewards: undefined };
}
function createBaseQueryValidatorCommissionRequest() {
    return { validator_address: "" };
}
function createBaseQueryValidatorCommissionResponse() {
    return { commission: undefined };
}
function createBaseQueryValidatorSlashesRequest() {
    return { validator_address: "", starting_height: 0, ending_height: 0, pagination: undefined };
}
function createBaseQueryValidatorSlashesResponse() {
    return { slashes: [], pagination: undefined };
}
function createBaseQueryDelegationRewardsRequest() {
    return { delegator_address: "", validator_address: "" };
}
function createBaseQueryDelegationRewardsResponse() {
    return { rewards: [] };
}
function createBaseQueryDelegationTotalRewardsRequest() {
    return { delegator_address: "" };
}
function createBaseQueryDelegationTotalRewardsResponse() {
    return { rewards: [], total: [] };
}
function createBaseQueryDelegatorValidatorsRequest() {
    return { delegator_address: "" };
}
function createBaseQueryDelegatorValidatorsResponse() {
    return { validators: [] };
}
function createBaseQueryDelegatorWithdrawAddressRequest() {
    return { delegator_address: "" };
}
function createBaseQueryDelegatorWithdrawAddressResponse() {
    return { withdraw_address: "" };
}
function createBaseQueryCommunityPoolRequest() {
    return {};
}
function createBaseQueryCommunityPoolResponse() {
    return { pool: [] };
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
    ["/cosmos.distribution.v1beta1.QueryParamsRequest", QueryParamsRequest],
    ["/cosmos.distribution.v1beta1.QueryParamsResponse", QueryParamsResponse],
    ["/cosmos.distribution.v1beta1.QueryValidatorOutstandingRewardsRequest", QueryValidatorOutstandingRewardsRequest],
    ["/cosmos.distribution.v1beta1.QueryValidatorOutstandingRewardsResponse", QueryValidatorOutstandingRewardsResponse],
    ["/cosmos.distribution.v1beta1.QueryValidatorCommissionRequest", QueryValidatorCommissionRequest],
    ["/cosmos.distribution.v1beta1.QueryValidatorCommissionResponse", QueryValidatorCommissionResponse],
    ["/cosmos.distribution.v1beta1.QueryValidatorSlashesRequest", QueryValidatorSlashesRequest],
    ["/cosmos.distribution.v1beta1.QueryValidatorSlashesResponse", QueryValidatorSlashesResponse],
    ["/cosmos.distribution.v1beta1.QueryDelegationRewardsRequest", QueryDelegationRewardsRequest],
    ["/cosmos.distribution.v1beta1.QueryDelegationRewardsResponse", QueryDelegationRewardsResponse],
    ["/cosmos.distribution.v1beta1.QueryDelegationTotalRewardsRequest", QueryDelegationTotalRewardsRequest],
    ["/cosmos.distribution.v1beta1.QueryDelegationTotalRewardsResponse", QueryDelegationTotalRewardsResponse],
    ["/cosmos.distribution.v1beta1.QueryDelegatorValidatorsRequest", QueryDelegatorValidatorsRequest],
    ["/cosmos.distribution.v1beta1.QueryDelegatorValidatorsResponse", QueryDelegatorValidatorsResponse],
    ["/cosmos.distribution.v1beta1.QueryDelegatorWithdrawAddressRequest", QueryDelegatorWithdrawAddressRequest],
    ["/cosmos.distribution.v1beta1.QueryDelegatorWithdrawAddressResponse", QueryDelegatorWithdrawAddressResponse],
    ["/cosmos.distribution.v1beta1.QueryCommunityPoolRequest", QueryCommunityPoolRequest],
    ["/cosmos.distribution.v1beta1.QueryCommunityPoolResponse", QueryCommunityPoolResponse]
];
export const aminoConverters = {
    "/cosmos.distribution.v1beta1.QueryParamsRequest": {
        aminoType: "cosmos-sdk/QueryParamsRequest",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.distribution.v1beta1.QueryParamsResponse": {
        aminoType: "cosmos-sdk/QueryParamsResponse",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.distribution.v1beta1.QueryValidatorOutstandingRewardsRequest": {
        aminoType: "cosmos-sdk/QueryValidatorOutstandingRewardsRequest",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.distribution.v1beta1.QueryValidatorOutstandingRewardsResponse": {
        aminoType: "cosmos-sdk/QueryValidatorOutstandingRewardsResponse",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.distribution.v1beta1.QueryValidatorCommissionRequest": {
        aminoType: "cosmos-sdk/QueryValidatorCommissionRequest",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.distribution.v1beta1.QueryValidatorCommissionResponse": {
        aminoType: "cosmos-sdk/QueryValidatorCommissionResponse",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.distribution.v1beta1.QueryValidatorSlashesRequest": {
        aminoType: "cosmos-sdk/QueryValidatorSlashesRequest",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.distribution.v1beta1.QueryValidatorSlashesResponse": {
        aminoType: "cosmos-sdk/QueryValidatorSlashesResponse",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.distribution.v1beta1.QueryDelegationRewardsRequest": {
        aminoType: "cosmos-sdk/QueryDelegationRewardsRequest",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.distribution.v1beta1.QueryDelegationRewardsResponse": {
        aminoType: "cosmos-sdk/QueryDelegationRewardsResponse",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.distribution.v1beta1.QueryDelegationTotalRewardsRequest": {
        aminoType: "cosmos-sdk/QueryDelegationTotalRewardsRequest",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.distribution.v1beta1.QueryDelegationTotalRewardsResponse": {
        aminoType: "cosmos-sdk/QueryDelegationTotalRewardsResponse",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.distribution.v1beta1.QueryDelegatorValidatorsRequest": {
        aminoType: "cosmos-sdk/QueryDelegatorValidatorsRequest",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.distribution.v1beta1.QueryDelegatorValidatorsResponse": {
        aminoType: "cosmos-sdk/QueryDelegatorValidatorsResponse",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.distribution.v1beta1.QueryDelegatorWithdrawAddressRequest": {
        aminoType: "cosmos-sdk/QueryDelegatorWithdrawAddressRequest",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.distribution.v1beta1.QueryDelegatorWithdrawAddressResponse": {
        aminoType: "cosmos-sdk/QueryDelegatorWithdrawAddressResponse",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.distribution.v1beta1.QueryCommunityPoolRequest": {
        aminoType: "cosmos-sdk/QueryCommunityPoolRequest",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.distribution.v1beta1.QueryCommunityPoolResponse": {
        aminoType: "cosmos-sdk/QueryCommunityPoolResponse",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    }
};
