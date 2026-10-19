"use strict";
Object.defineProperty(exports, "__esModule", { value: true });
exports.aminoConverters = exports.registry = exports.QueryCommunityPoolResponse = exports.QueryCommunityPoolRequest = exports.QueryDelegatorWithdrawAddressResponse = exports.QueryDelegatorWithdrawAddressRequest = exports.QueryDelegatorValidatorsResponse = exports.QueryDelegatorValidatorsRequest = exports.QueryDelegationTotalRewardsResponse = exports.QueryDelegationTotalRewardsRequest = exports.QueryDelegationRewardsResponse = exports.QueryDelegationRewardsRequest = exports.QueryValidatorSlashesResponse = exports.QueryValidatorSlashesRequest = exports.QueryValidatorCommissionResponse = exports.QueryValidatorCommissionRequest = exports.QueryValidatorOutstandingRewardsResponse = exports.QueryValidatorOutstandingRewardsRequest = exports.QueryParamsResponse = exports.QueryParamsRequest = void 0;
const wire_1 = require("@bufbuild/protobuf/wire");
const pagination_1 = require("../../base/query/v1beta1/pagination");
const coin_1 = require("../../base/v1beta1/coin");
const distribution_1 = require("./distribution");
exports.QueryParamsRequest = {
    $type: "cosmos.distribution.v1beta1.QueryParamsRequest",
    encode(_, writer = new wire_1.BinaryWriter()) {
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
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
        return exports.QueryParamsRequest.fromPartial(base ?? {});
    },
    fromPartial(_) {
        const message = createBaseQueryParamsRequest();
        return message;
    }
};
exports.QueryParamsResponse = {
    $type: "cosmos.distribution.v1beta1.QueryParamsResponse",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.params !== undefined) {
            distribution_1.Params.encode(message.params, writer.uint32(10).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseQueryParamsResponse();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.params = distribution_1.Params.decode(reader, reader.uint32());
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
        return { params: isSet(object.params) ? distribution_1.Params.fromJSON(object.params) : undefined };
    },
    toJSON(message) {
        const obj = {};
        if (message.params !== undefined) {
            obj.params = distribution_1.Params.toJSON(message.params);
        }
        return obj;
    },
    create(base) {
        return exports.QueryParamsResponse.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryParamsResponse();
        message.params = object.params !== undefined && object.params !== null ? distribution_1.Params.fromPartial(object.params) : undefined;
        return message;
    }
};
exports.QueryValidatorOutstandingRewardsRequest = {
    $type: "cosmos.distribution.v1beta1.QueryValidatorOutstandingRewardsRequest",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.validator_address !== "") {
            writer.uint32(10).string(message.validator_address);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
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
        return exports.QueryValidatorOutstandingRewardsRequest.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryValidatorOutstandingRewardsRequest();
        message.validator_address = object.validator_address ?? "";
        return message;
    }
};
exports.QueryValidatorOutstandingRewardsResponse = {
    $type: "cosmos.distribution.v1beta1.QueryValidatorOutstandingRewardsResponse",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.rewards !== undefined) {
            distribution_1.ValidatorOutstandingRewards.encode(message.rewards, writer.uint32(10).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseQueryValidatorOutstandingRewardsResponse();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.rewards = distribution_1.ValidatorOutstandingRewards.decode(reader, reader.uint32());
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
        return { rewards: isSet(object.rewards) ? distribution_1.ValidatorOutstandingRewards.fromJSON(object.rewards) : undefined };
    },
    toJSON(message) {
        const obj = {};
        if (message.rewards !== undefined) {
            obj.rewards = distribution_1.ValidatorOutstandingRewards.toJSON(message.rewards);
        }
        return obj;
    },
    create(base) {
        return exports.QueryValidatorOutstandingRewardsResponse.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryValidatorOutstandingRewardsResponse();
        message.rewards = object.rewards !== undefined && object.rewards !== null ? distribution_1.ValidatorOutstandingRewards.fromPartial(object.rewards) : undefined;
        return message;
    }
};
exports.QueryValidatorCommissionRequest = {
    $type: "cosmos.distribution.v1beta1.QueryValidatorCommissionRequest",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.validator_address !== "") {
            writer.uint32(10).string(message.validator_address);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
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
        return exports.QueryValidatorCommissionRequest.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryValidatorCommissionRequest();
        message.validator_address = object.validator_address ?? "";
        return message;
    }
};
exports.QueryValidatorCommissionResponse = {
    $type: "cosmos.distribution.v1beta1.QueryValidatorCommissionResponse",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.commission !== undefined) {
            distribution_1.ValidatorAccumulatedCommission.encode(message.commission, writer.uint32(10).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseQueryValidatorCommissionResponse();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.commission = distribution_1.ValidatorAccumulatedCommission.decode(reader, reader.uint32());
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
            commission: isSet(object.commission) ? distribution_1.ValidatorAccumulatedCommission.fromJSON(object.commission) : undefined
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.commission !== undefined) {
            obj.commission = distribution_1.ValidatorAccumulatedCommission.toJSON(message.commission);
        }
        return obj;
    },
    create(base) {
        return exports.QueryValidatorCommissionResponse.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryValidatorCommissionResponse();
        message.commission =
            object.commission !== undefined && object.commission !== null ? distribution_1.ValidatorAccumulatedCommission.fromPartial(object.commission) : undefined;
        return message;
    }
};
exports.QueryValidatorSlashesRequest = {
    $type: "cosmos.distribution.v1beta1.QueryValidatorSlashesRequest",
    encode(message, writer = new wire_1.BinaryWriter()) {
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
            pagination_1.PageRequest.encode(message.pagination, writer.uint32(34).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
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
                    message.pagination = pagination_1.PageRequest.decode(reader, reader.uint32());
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
            pagination: isSet(object.pagination) ? pagination_1.PageRequest.fromJSON(object.pagination) : undefined
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
            obj.pagination = pagination_1.PageRequest.toJSON(message.pagination);
        }
        return obj;
    },
    create(base) {
        return exports.QueryValidatorSlashesRequest.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryValidatorSlashesRequest();
        message.validator_address = object.validator_address ?? "";
        message.starting_height = object.starting_height ?? 0;
        message.ending_height = object.ending_height ?? 0;
        message.pagination = object.pagination !== undefined && object.pagination !== null ? pagination_1.PageRequest.fromPartial(object.pagination) : undefined;
        return message;
    }
};
exports.QueryValidatorSlashesResponse = {
    $type: "cosmos.distribution.v1beta1.QueryValidatorSlashesResponse",
    encode(message, writer = new wire_1.BinaryWriter()) {
        for (const v of message.slashes) {
            distribution_1.ValidatorSlashEvent.encode(v, writer.uint32(10).fork()).join();
        }
        if (message.pagination !== undefined) {
            pagination_1.PageResponse.encode(message.pagination, writer.uint32(18).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseQueryValidatorSlashesResponse();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.slashes.push(distribution_1.ValidatorSlashEvent.decode(reader, reader.uint32()));
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.pagination = pagination_1.PageResponse.decode(reader, reader.uint32());
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
            slashes: globalThis.Array.isArray(object?.slashes) ? object.slashes.map((e) => distribution_1.ValidatorSlashEvent.fromJSON(e)) : [],
            pagination: isSet(object.pagination) ? pagination_1.PageResponse.fromJSON(object.pagination) : undefined
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.slashes?.length) {
            obj.slashes = message.slashes.map((e) => distribution_1.ValidatorSlashEvent.toJSON(e));
        }
        if (message.pagination !== undefined) {
            obj.pagination = pagination_1.PageResponse.toJSON(message.pagination);
        }
        return obj;
    },
    create(base) {
        return exports.QueryValidatorSlashesResponse.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryValidatorSlashesResponse();
        message.slashes = object.slashes?.map((e) => distribution_1.ValidatorSlashEvent.fromPartial(e)) || [];
        message.pagination = object.pagination !== undefined && object.pagination !== null ? pagination_1.PageResponse.fromPartial(object.pagination) : undefined;
        return message;
    }
};
exports.QueryDelegationRewardsRequest = {
    $type: "cosmos.distribution.v1beta1.QueryDelegationRewardsRequest",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.delegator_address !== "") {
            writer.uint32(10).string(message.delegator_address);
        }
        if (message.validator_address !== "") {
            writer.uint32(18).string(message.validator_address);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
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
        return exports.QueryDelegationRewardsRequest.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryDelegationRewardsRequest();
        message.delegator_address = object.delegator_address ?? "";
        message.validator_address = object.validator_address ?? "";
        return message;
    }
};
exports.QueryDelegationRewardsResponse = {
    $type: "cosmos.distribution.v1beta1.QueryDelegationRewardsResponse",
    encode(message, writer = new wire_1.BinaryWriter()) {
        for (const v of message.rewards) {
            coin_1.DecCoin.encode(v, writer.uint32(10).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseQueryDelegationRewardsResponse();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.rewards.push(coin_1.DecCoin.decode(reader, reader.uint32()));
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
            rewards: globalThis.Array.isArray(object?.rewards) ? object.rewards.map((e) => coin_1.DecCoin.fromJSON(e)) : []
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.rewards?.length) {
            obj.rewards = message.rewards.map((e) => coin_1.DecCoin.toJSON(e));
        }
        return obj;
    },
    create(base) {
        return exports.QueryDelegationRewardsResponse.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryDelegationRewardsResponse();
        message.rewards = object.rewards?.map((e) => coin_1.DecCoin.fromPartial(e)) || [];
        return message;
    }
};
exports.QueryDelegationTotalRewardsRequest = {
    $type: "cosmos.distribution.v1beta1.QueryDelegationTotalRewardsRequest",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.delegator_address !== "") {
            writer.uint32(10).string(message.delegator_address);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
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
        return exports.QueryDelegationTotalRewardsRequest.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryDelegationTotalRewardsRequest();
        message.delegator_address = object.delegator_address ?? "";
        return message;
    }
};
exports.QueryDelegationTotalRewardsResponse = {
    $type: "cosmos.distribution.v1beta1.QueryDelegationTotalRewardsResponse",
    encode(message, writer = new wire_1.BinaryWriter()) {
        for (const v of message.rewards) {
            distribution_1.DelegationDelegatorReward.encode(v, writer.uint32(10).fork()).join();
        }
        for (const v of message.total) {
            coin_1.DecCoin.encode(v, writer.uint32(18).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseQueryDelegationTotalRewardsResponse();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.rewards.push(distribution_1.DelegationDelegatorReward.decode(reader, reader.uint32()));
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.total.push(coin_1.DecCoin.decode(reader, reader.uint32()));
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
            rewards: globalThis.Array.isArray(object?.rewards) ? object.rewards.map((e) => distribution_1.DelegationDelegatorReward.fromJSON(e)) : [],
            total: globalThis.Array.isArray(object?.total) ? object.total.map((e) => coin_1.DecCoin.fromJSON(e)) : []
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.rewards?.length) {
            obj.rewards = message.rewards.map((e) => distribution_1.DelegationDelegatorReward.toJSON(e));
        }
        if (message.total?.length) {
            obj.total = message.total.map((e) => coin_1.DecCoin.toJSON(e));
        }
        return obj;
    },
    create(base) {
        return exports.QueryDelegationTotalRewardsResponse.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryDelegationTotalRewardsResponse();
        message.rewards = object.rewards?.map((e) => distribution_1.DelegationDelegatorReward.fromPartial(e)) || [];
        message.total = object.total?.map((e) => coin_1.DecCoin.fromPartial(e)) || [];
        return message;
    }
};
exports.QueryDelegatorValidatorsRequest = {
    $type: "cosmos.distribution.v1beta1.QueryDelegatorValidatorsRequest",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.delegator_address !== "") {
            writer.uint32(10).string(message.delegator_address);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
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
        return exports.QueryDelegatorValidatorsRequest.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryDelegatorValidatorsRequest();
        message.delegator_address = object.delegator_address ?? "";
        return message;
    }
};
exports.QueryDelegatorValidatorsResponse = {
    $type: "cosmos.distribution.v1beta1.QueryDelegatorValidatorsResponse",
    encode(message, writer = new wire_1.BinaryWriter()) {
        for (const v of message.validators) {
            writer.uint32(10).string(v);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
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
        return exports.QueryDelegatorValidatorsResponse.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryDelegatorValidatorsResponse();
        message.validators = object.validators?.map((e) => e) || [];
        return message;
    }
};
exports.QueryDelegatorWithdrawAddressRequest = {
    $type: "cosmos.distribution.v1beta1.QueryDelegatorWithdrawAddressRequest",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.delegator_address !== "") {
            writer.uint32(10).string(message.delegator_address);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
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
        return exports.QueryDelegatorWithdrawAddressRequest.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryDelegatorWithdrawAddressRequest();
        message.delegator_address = object.delegator_address ?? "";
        return message;
    }
};
exports.QueryDelegatorWithdrawAddressResponse = {
    $type: "cosmos.distribution.v1beta1.QueryDelegatorWithdrawAddressResponse",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.withdraw_address !== "") {
            writer.uint32(10).string(message.withdraw_address);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
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
        return exports.QueryDelegatorWithdrawAddressResponse.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryDelegatorWithdrawAddressResponse();
        message.withdraw_address = object.withdraw_address ?? "";
        return message;
    }
};
exports.QueryCommunityPoolRequest = {
    $type: "cosmos.distribution.v1beta1.QueryCommunityPoolRequest",
    encode(_, writer = new wire_1.BinaryWriter()) {
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
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
        return exports.QueryCommunityPoolRequest.fromPartial(base ?? {});
    },
    fromPartial(_) {
        const message = createBaseQueryCommunityPoolRequest();
        return message;
    }
};
exports.QueryCommunityPoolResponse = {
    $type: "cosmos.distribution.v1beta1.QueryCommunityPoolResponse",
    encode(message, writer = new wire_1.BinaryWriter()) {
        for (const v of message.pool) {
            coin_1.DecCoin.encode(v, writer.uint32(10).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseQueryCommunityPoolResponse();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.pool.push(coin_1.DecCoin.decode(reader, reader.uint32()));
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
        return { pool: globalThis.Array.isArray(object?.pool) ? object.pool.map((e) => coin_1.DecCoin.fromJSON(e)) : [] };
    },
    toJSON(message) {
        const obj = {};
        if (message.pool?.length) {
            obj.pool = message.pool.map((e) => coin_1.DecCoin.toJSON(e));
        }
        return obj;
    },
    create(base) {
        return exports.QueryCommunityPoolResponse.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryCommunityPoolResponse();
        message.pool = object.pool?.map((e) => coin_1.DecCoin.fromPartial(e)) || [];
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
exports.registry = [
    ["/cosmos.distribution.v1beta1.QueryParamsRequest", exports.QueryParamsRequest],
    ["/cosmos.distribution.v1beta1.QueryParamsResponse", exports.QueryParamsResponse],
    ["/cosmos.distribution.v1beta1.QueryValidatorOutstandingRewardsRequest", exports.QueryValidatorOutstandingRewardsRequest],
    ["/cosmos.distribution.v1beta1.QueryValidatorOutstandingRewardsResponse", exports.QueryValidatorOutstandingRewardsResponse],
    ["/cosmos.distribution.v1beta1.QueryValidatorCommissionRequest", exports.QueryValidatorCommissionRequest],
    ["/cosmos.distribution.v1beta1.QueryValidatorCommissionResponse", exports.QueryValidatorCommissionResponse],
    ["/cosmos.distribution.v1beta1.QueryValidatorSlashesRequest", exports.QueryValidatorSlashesRequest],
    ["/cosmos.distribution.v1beta1.QueryValidatorSlashesResponse", exports.QueryValidatorSlashesResponse],
    ["/cosmos.distribution.v1beta1.QueryDelegationRewardsRequest", exports.QueryDelegationRewardsRequest],
    ["/cosmos.distribution.v1beta1.QueryDelegationRewardsResponse", exports.QueryDelegationRewardsResponse],
    ["/cosmos.distribution.v1beta1.QueryDelegationTotalRewardsRequest", exports.QueryDelegationTotalRewardsRequest],
    ["/cosmos.distribution.v1beta1.QueryDelegationTotalRewardsResponse", exports.QueryDelegationTotalRewardsResponse],
    ["/cosmos.distribution.v1beta1.QueryDelegatorValidatorsRequest", exports.QueryDelegatorValidatorsRequest],
    ["/cosmos.distribution.v1beta1.QueryDelegatorValidatorsResponse", exports.QueryDelegatorValidatorsResponse],
    ["/cosmos.distribution.v1beta1.QueryDelegatorWithdrawAddressRequest", exports.QueryDelegatorWithdrawAddressRequest],
    ["/cosmos.distribution.v1beta1.QueryDelegatorWithdrawAddressResponse", exports.QueryDelegatorWithdrawAddressResponse],
    ["/cosmos.distribution.v1beta1.QueryCommunityPoolRequest", exports.QueryCommunityPoolRequest],
    ["/cosmos.distribution.v1beta1.QueryCommunityPoolResponse", exports.QueryCommunityPoolResponse]
];
exports.aminoConverters = {
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
