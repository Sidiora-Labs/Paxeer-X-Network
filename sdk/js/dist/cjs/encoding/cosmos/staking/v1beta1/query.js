"use strict";
Object.defineProperty(exports, "__esModule", { value: true });
exports.aminoConverters = exports.registry = exports.QueryParamsResponse = exports.QueryParamsRequest = exports.QueryPoolResponse = exports.QueryPoolRequest = exports.QueryHistoricalInfoResponse = exports.QueryHistoricalInfoRequest = exports.QueryDelegatorValidatorResponse = exports.QueryDelegatorValidatorRequest = exports.QueryDelegatorValidatorsResponse = exports.QueryDelegatorValidatorsRequest = exports.QueryRedelegationsResponse = exports.QueryRedelegationsRequest = exports.QueryDelegatorUnbondingDelegationsResponse = exports.QueryDelegatorUnbondingDelegationsRequest = exports.QueryDelegatorDelegationsResponse = exports.QueryDelegatorDelegationsRequest = exports.QueryUnbondingDelegationResponse = exports.QueryUnbondingDelegationRequest = exports.QueryDelegationResponse = exports.QueryDelegationRequest = exports.QueryValidatorUnbondingDelegationsResponse = exports.QueryValidatorUnbondingDelegationsRequest = exports.QueryValidatorDelegationsResponse = exports.QueryValidatorDelegationsRequest = exports.QueryValidatorResponse = exports.QueryValidatorRequest = exports.QueryValidatorsResponse = exports.QueryValidatorsRequest = void 0;
const wire_1 = require("@bufbuild/protobuf/wire");
const pagination_1 = require("../../base/query/v1beta1/pagination");
const staking_1 = require("./staking");
exports.QueryValidatorsRequest = {
    $type: "cosmos.staking.v1beta1.QueryValidatorsRequest",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.status !== "") {
            writer.uint32(10).string(message.status);
        }
        if (message.pagination !== undefined) {
            pagination_1.PageRequest.encode(message.pagination, writer.uint32(18).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseQueryValidatorsRequest();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.status = reader.string();
                    continue;
                case 2:
                    if (tag !== 18) {
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
            status: isSet(object.status) ? globalThis.String(object.status) : "",
            pagination: isSet(object.pagination) ? pagination_1.PageRequest.fromJSON(object.pagination) : undefined
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.status !== "") {
            obj.status = message.status;
        }
        if (message.pagination !== undefined) {
            obj.pagination = pagination_1.PageRequest.toJSON(message.pagination);
        }
        return obj;
    },
    create(base) {
        return exports.QueryValidatorsRequest.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryValidatorsRequest();
        message.status = object.status ?? "";
        message.pagination = object.pagination !== undefined && object.pagination !== null ? pagination_1.PageRequest.fromPartial(object.pagination) : undefined;
        return message;
    }
};
exports.QueryValidatorsResponse = {
    $type: "cosmos.staking.v1beta1.QueryValidatorsResponse",
    encode(message, writer = new wire_1.BinaryWriter()) {
        for (const v of message.validators) {
            staking_1.Validator.encode(v, writer.uint32(10).fork()).join();
        }
        if (message.pagination !== undefined) {
            pagination_1.PageResponse.encode(message.pagination, writer.uint32(18).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseQueryValidatorsResponse();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.validators.push(staking_1.Validator.decode(reader, reader.uint32()));
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
            validators: globalThis.Array.isArray(object?.validators) ? object.validators.map((e) => staking_1.Validator.fromJSON(e)) : [],
            pagination: isSet(object.pagination) ? pagination_1.PageResponse.fromJSON(object.pagination) : undefined
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.validators?.length) {
            obj.validators = message.validators.map((e) => staking_1.Validator.toJSON(e));
        }
        if (message.pagination !== undefined) {
            obj.pagination = pagination_1.PageResponse.toJSON(message.pagination);
        }
        return obj;
    },
    create(base) {
        return exports.QueryValidatorsResponse.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryValidatorsResponse();
        message.validators = object.validators?.map((e) => staking_1.Validator.fromPartial(e)) || [];
        message.pagination = object.pagination !== undefined && object.pagination !== null ? pagination_1.PageResponse.fromPartial(object.pagination) : undefined;
        return message;
    }
};
exports.QueryValidatorRequest = {
    $type: "cosmos.staking.v1beta1.QueryValidatorRequest",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.validator_addr !== "") {
            writer.uint32(10).string(message.validator_addr);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseQueryValidatorRequest();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.validator_addr = reader.string();
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
        return { validator_addr: isSet(object.validator_addr) ? globalThis.String(object.validator_addr) : "" };
    },
    toJSON(message) {
        const obj = {};
        if (message.validator_addr !== "") {
            obj.validator_addr = message.validator_addr;
        }
        return obj;
    },
    create(base) {
        return exports.QueryValidatorRequest.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryValidatorRequest();
        message.validator_addr = object.validator_addr ?? "";
        return message;
    }
};
exports.QueryValidatorResponse = {
    $type: "cosmos.staking.v1beta1.QueryValidatorResponse",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.validator !== undefined) {
            staking_1.Validator.encode(message.validator, writer.uint32(10).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseQueryValidatorResponse();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.validator = staking_1.Validator.decode(reader, reader.uint32());
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
        return { validator: isSet(object.validator) ? staking_1.Validator.fromJSON(object.validator) : undefined };
    },
    toJSON(message) {
        const obj = {};
        if (message.validator !== undefined) {
            obj.validator = staking_1.Validator.toJSON(message.validator);
        }
        return obj;
    },
    create(base) {
        return exports.QueryValidatorResponse.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryValidatorResponse();
        message.validator = object.validator !== undefined && object.validator !== null ? staking_1.Validator.fromPartial(object.validator) : undefined;
        return message;
    }
};
exports.QueryValidatorDelegationsRequest = {
    $type: "cosmos.staking.v1beta1.QueryValidatorDelegationsRequest",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.validator_addr !== "") {
            writer.uint32(10).string(message.validator_addr);
        }
        if (message.pagination !== undefined) {
            pagination_1.PageRequest.encode(message.pagination, writer.uint32(18).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseQueryValidatorDelegationsRequest();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.validator_addr = reader.string();
                    continue;
                case 2:
                    if (tag !== 18) {
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
            validator_addr: isSet(object.validator_addr) ? globalThis.String(object.validator_addr) : "",
            pagination: isSet(object.pagination) ? pagination_1.PageRequest.fromJSON(object.pagination) : undefined
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.validator_addr !== "") {
            obj.validator_addr = message.validator_addr;
        }
        if (message.pagination !== undefined) {
            obj.pagination = pagination_1.PageRequest.toJSON(message.pagination);
        }
        return obj;
    },
    create(base) {
        return exports.QueryValidatorDelegationsRequest.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryValidatorDelegationsRequest();
        message.validator_addr = object.validator_addr ?? "";
        message.pagination = object.pagination !== undefined && object.pagination !== null ? pagination_1.PageRequest.fromPartial(object.pagination) : undefined;
        return message;
    }
};
exports.QueryValidatorDelegationsResponse = {
    $type: "cosmos.staking.v1beta1.QueryValidatorDelegationsResponse",
    encode(message, writer = new wire_1.BinaryWriter()) {
        for (const v of message.delegation_responses) {
            staking_1.DelegationResponse.encode(v, writer.uint32(10).fork()).join();
        }
        if (message.pagination !== undefined) {
            pagination_1.PageResponse.encode(message.pagination, writer.uint32(18).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseQueryValidatorDelegationsResponse();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.delegation_responses.push(staking_1.DelegationResponse.decode(reader, reader.uint32()));
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
            delegation_responses: globalThis.Array.isArray(object?.delegation_responses)
                ? object.delegation_responses.map((e) => staking_1.DelegationResponse.fromJSON(e))
                : [],
            pagination: isSet(object.pagination) ? pagination_1.PageResponse.fromJSON(object.pagination) : undefined
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.delegation_responses?.length) {
            obj.delegation_responses = message.delegation_responses.map((e) => staking_1.DelegationResponse.toJSON(e));
        }
        if (message.pagination !== undefined) {
            obj.pagination = pagination_1.PageResponse.toJSON(message.pagination);
        }
        return obj;
    },
    create(base) {
        return exports.QueryValidatorDelegationsResponse.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryValidatorDelegationsResponse();
        message.delegation_responses = object.delegation_responses?.map((e) => staking_1.DelegationResponse.fromPartial(e)) || [];
        message.pagination = object.pagination !== undefined && object.pagination !== null ? pagination_1.PageResponse.fromPartial(object.pagination) : undefined;
        return message;
    }
};
exports.QueryValidatorUnbondingDelegationsRequest = {
    $type: "cosmos.staking.v1beta1.QueryValidatorUnbondingDelegationsRequest",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.validator_addr !== "") {
            writer.uint32(10).string(message.validator_addr);
        }
        if (message.pagination !== undefined) {
            pagination_1.PageRequest.encode(message.pagination, writer.uint32(18).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseQueryValidatorUnbondingDelegationsRequest();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.validator_addr = reader.string();
                    continue;
                case 2:
                    if (tag !== 18) {
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
            validator_addr: isSet(object.validator_addr) ? globalThis.String(object.validator_addr) : "",
            pagination: isSet(object.pagination) ? pagination_1.PageRequest.fromJSON(object.pagination) : undefined
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.validator_addr !== "") {
            obj.validator_addr = message.validator_addr;
        }
        if (message.pagination !== undefined) {
            obj.pagination = pagination_1.PageRequest.toJSON(message.pagination);
        }
        return obj;
    },
    create(base) {
        return exports.QueryValidatorUnbondingDelegationsRequest.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryValidatorUnbondingDelegationsRequest();
        message.validator_addr = object.validator_addr ?? "";
        message.pagination = object.pagination !== undefined && object.pagination !== null ? pagination_1.PageRequest.fromPartial(object.pagination) : undefined;
        return message;
    }
};
exports.QueryValidatorUnbondingDelegationsResponse = {
    $type: "cosmos.staking.v1beta1.QueryValidatorUnbondingDelegationsResponse",
    encode(message, writer = new wire_1.BinaryWriter()) {
        for (const v of message.unbonding_responses) {
            staking_1.UnbondingDelegation.encode(v, writer.uint32(10).fork()).join();
        }
        if (message.pagination !== undefined) {
            pagination_1.PageResponse.encode(message.pagination, writer.uint32(18).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseQueryValidatorUnbondingDelegationsResponse();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.unbonding_responses.push(staking_1.UnbondingDelegation.decode(reader, reader.uint32()));
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
            unbonding_responses: globalThis.Array.isArray(object?.unbonding_responses)
                ? object.unbonding_responses.map((e) => staking_1.UnbondingDelegation.fromJSON(e))
                : [],
            pagination: isSet(object.pagination) ? pagination_1.PageResponse.fromJSON(object.pagination) : undefined
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.unbonding_responses?.length) {
            obj.unbonding_responses = message.unbonding_responses.map((e) => staking_1.UnbondingDelegation.toJSON(e));
        }
        if (message.pagination !== undefined) {
            obj.pagination = pagination_1.PageResponse.toJSON(message.pagination);
        }
        return obj;
    },
    create(base) {
        return exports.QueryValidatorUnbondingDelegationsResponse.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryValidatorUnbondingDelegationsResponse();
        message.unbonding_responses = object.unbonding_responses?.map((e) => staking_1.UnbondingDelegation.fromPartial(e)) || [];
        message.pagination = object.pagination !== undefined && object.pagination !== null ? pagination_1.PageResponse.fromPartial(object.pagination) : undefined;
        return message;
    }
};
exports.QueryDelegationRequest = {
    $type: "cosmos.staking.v1beta1.QueryDelegationRequest",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.delegator_addr !== "") {
            writer.uint32(10).string(message.delegator_addr);
        }
        if (message.validator_addr !== "") {
            writer.uint32(18).string(message.validator_addr);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseQueryDelegationRequest();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.delegator_addr = reader.string();
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.validator_addr = reader.string();
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
            delegator_addr: isSet(object.delegator_addr) ? globalThis.String(object.delegator_addr) : "",
            validator_addr: isSet(object.validator_addr) ? globalThis.String(object.validator_addr) : ""
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.delegator_addr !== "") {
            obj.delegator_addr = message.delegator_addr;
        }
        if (message.validator_addr !== "") {
            obj.validator_addr = message.validator_addr;
        }
        return obj;
    },
    create(base) {
        return exports.QueryDelegationRequest.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryDelegationRequest();
        message.delegator_addr = object.delegator_addr ?? "";
        message.validator_addr = object.validator_addr ?? "";
        return message;
    }
};
exports.QueryDelegationResponse = {
    $type: "cosmos.staking.v1beta1.QueryDelegationResponse",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.delegation_response !== undefined) {
            staking_1.DelegationResponse.encode(message.delegation_response, writer.uint32(10).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseQueryDelegationResponse();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.delegation_response = staking_1.DelegationResponse.decode(reader, reader.uint32());
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
            delegation_response: isSet(object.delegation_response) ? staking_1.DelegationResponse.fromJSON(object.delegation_response) : undefined
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.delegation_response !== undefined) {
            obj.delegation_response = staking_1.DelegationResponse.toJSON(message.delegation_response);
        }
        return obj;
    },
    create(base) {
        return exports.QueryDelegationResponse.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryDelegationResponse();
        message.delegation_response =
            object.delegation_response !== undefined && object.delegation_response !== null ? staking_1.DelegationResponse.fromPartial(object.delegation_response) : undefined;
        return message;
    }
};
exports.QueryUnbondingDelegationRequest = {
    $type: "cosmos.staking.v1beta1.QueryUnbondingDelegationRequest",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.delegator_addr !== "") {
            writer.uint32(10).string(message.delegator_addr);
        }
        if (message.validator_addr !== "") {
            writer.uint32(18).string(message.validator_addr);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseQueryUnbondingDelegationRequest();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.delegator_addr = reader.string();
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.validator_addr = reader.string();
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
            delegator_addr: isSet(object.delegator_addr) ? globalThis.String(object.delegator_addr) : "",
            validator_addr: isSet(object.validator_addr) ? globalThis.String(object.validator_addr) : ""
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.delegator_addr !== "") {
            obj.delegator_addr = message.delegator_addr;
        }
        if (message.validator_addr !== "") {
            obj.validator_addr = message.validator_addr;
        }
        return obj;
    },
    create(base) {
        return exports.QueryUnbondingDelegationRequest.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryUnbondingDelegationRequest();
        message.delegator_addr = object.delegator_addr ?? "";
        message.validator_addr = object.validator_addr ?? "";
        return message;
    }
};
exports.QueryUnbondingDelegationResponse = {
    $type: "cosmos.staking.v1beta1.QueryUnbondingDelegationResponse",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.unbond !== undefined) {
            staking_1.UnbondingDelegation.encode(message.unbond, writer.uint32(10).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseQueryUnbondingDelegationResponse();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.unbond = staking_1.UnbondingDelegation.decode(reader, reader.uint32());
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
        return { unbond: isSet(object.unbond) ? staking_1.UnbondingDelegation.fromJSON(object.unbond) : undefined };
    },
    toJSON(message) {
        const obj = {};
        if (message.unbond !== undefined) {
            obj.unbond = staking_1.UnbondingDelegation.toJSON(message.unbond);
        }
        return obj;
    },
    create(base) {
        return exports.QueryUnbondingDelegationResponse.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryUnbondingDelegationResponse();
        message.unbond = object.unbond !== undefined && object.unbond !== null ? staking_1.UnbondingDelegation.fromPartial(object.unbond) : undefined;
        return message;
    }
};
exports.QueryDelegatorDelegationsRequest = {
    $type: "cosmos.staking.v1beta1.QueryDelegatorDelegationsRequest",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.delegator_addr !== "") {
            writer.uint32(10).string(message.delegator_addr);
        }
        if (message.pagination !== undefined) {
            pagination_1.PageRequest.encode(message.pagination, writer.uint32(18).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseQueryDelegatorDelegationsRequest();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.delegator_addr = reader.string();
                    continue;
                case 2:
                    if (tag !== 18) {
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
            delegator_addr: isSet(object.delegator_addr) ? globalThis.String(object.delegator_addr) : "",
            pagination: isSet(object.pagination) ? pagination_1.PageRequest.fromJSON(object.pagination) : undefined
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.delegator_addr !== "") {
            obj.delegator_addr = message.delegator_addr;
        }
        if (message.pagination !== undefined) {
            obj.pagination = pagination_1.PageRequest.toJSON(message.pagination);
        }
        return obj;
    },
    create(base) {
        return exports.QueryDelegatorDelegationsRequest.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryDelegatorDelegationsRequest();
        message.delegator_addr = object.delegator_addr ?? "";
        message.pagination = object.pagination !== undefined && object.pagination !== null ? pagination_1.PageRequest.fromPartial(object.pagination) : undefined;
        return message;
    }
};
exports.QueryDelegatorDelegationsResponse = {
    $type: "cosmos.staking.v1beta1.QueryDelegatorDelegationsResponse",
    encode(message, writer = new wire_1.BinaryWriter()) {
        for (const v of message.delegation_responses) {
            staking_1.DelegationResponse.encode(v, writer.uint32(10).fork()).join();
        }
        if (message.pagination !== undefined) {
            pagination_1.PageResponse.encode(message.pagination, writer.uint32(18).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseQueryDelegatorDelegationsResponse();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.delegation_responses.push(staking_1.DelegationResponse.decode(reader, reader.uint32()));
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
            delegation_responses: globalThis.Array.isArray(object?.delegation_responses)
                ? object.delegation_responses.map((e) => staking_1.DelegationResponse.fromJSON(e))
                : [],
            pagination: isSet(object.pagination) ? pagination_1.PageResponse.fromJSON(object.pagination) : undefined
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.delegation_responses?.length) {
            obj.delegation_responses = message.delegation_responses.map((e) => staking_1.DelegationResponse.toJSON(e));
        }
        if (message.pagination !== undefined) {
            obj.pagination = pagination_1.PageResponse.toJSON(message.pagination);
        }
        return obj;
    },
    create(base) {
        return exports.QueryDelegatorDelegationsResponse.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryDelegatorDelegationsResponse();
        message.delegation_responses = object.delegation_responses?.map((e) => staking_1.DelegationResponse.fromPartial(e)) || [];
        message.pagination = object.pagination !== undefined && object.pagination !== null ? pagination_1.PageResponse.fromPartial(object.pagination) : undefined;
        return message;
    }
};
exports.QueryDelegatorUnbondingDelegationsRequest = {
    $type: "cosmos.staking.v1beta1.QueryDelegatorUnbondingDelegationsRequest",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.delegator_addr !== "") {
            writer.uint32(10).string(message.delegator_addr);
        }
        if (message.pagination !== undefined) {
            pagination_1.PageRequest.encode(message.pagination, writer.uint32(18).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseQueryDelegatorUnbondingDelegationsRequest();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.delegator_addr = reader.string();
                    continue;
                case 2:
                    if (tag !== 18) {
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
            delegator_addr: isSet(object.delegator_addr) ? globalThis.String(object.delegator_addr) : "",
            pagination: isSet(object.pagination) ? pagination_1.PageRequest.fromJSON(object.pagination) : undefined
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.delegator_addr !== "") {
            obj.delegator_addr = message.delegator_addr;
        }
        if (message.pagination !== undefined) {
            obj.pagination = pagination_1.PageRequest.toJSON(message.pagination);
        }
        return obj;
    },
    create(base) {
        return exports.QueryDelegatorUnbondingDelegationsRequest.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryDelegatorUnbondingDelegationsRequest();
        message.delegator_addr = object.delegator_addr ?? "";
        message.pagination = object.pagination !== undefined && object.pagination !== null ? pagination_1.PageRequest.fromPartial(object.pagination) : undefined;
        return message;
    }
};
exports.QueryDelegatorUnbondingDelegationsResponse = {
    $type: "cosmos.staking.v1beta1.QueryDelegatorUnbondingDelegationsResponse",
    encode(message, writer = new wire_1.BinaryWriter()) {
        for (const v of message.unbonding_responses) {
            staking_1.UnbondingDelegation.encode(v, writer.uint32(10).fork()).join();
        }
        if (message.pagination !== undefined) {
            pagination_1.PageResponse.encode(message.pagination, writer.uint32(18).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseQueryDelegatorUnbondingDelegationsResponse();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.unbonding_responses.push(staking_1.UnbondingDelegation.decode(reader, reader.uint32()));
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
            unbonding_responses: globalThis.Array.isArray(object?.unbonding_responses)
                ? object.unbonding_responses.map((e) => staking_1.UnbondingDelegation.fromJSON(e))
                : [],
            pagination: isSet(object.pagination) ? pagination_1.PageResponse.fromJSON(object.pagination) : undefined
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.unbonding_responses?.length) {
            obj.unbonding_responses = message.unbonding_responses.map((e) => staking_1.UnbondingDelegation.toJSON(e));
        }
        if (message.pagination !== undefined) {
            obj.pagination = pagination_1.PageResponse.toJSON(message.pagination);
        }
        return obj;
    },
    create(base) {
        return exports.QueryDelegatorUnbondingDelegationsResponse.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryDelegatorUnbondingDelegationsResponse();
        message.unbonding_responses = object.unbonding_responses?.map((e) => staking_1.UnbondingDelegation.fromPartial(e)) || [];
        message.pagination = object.pagination !== undefined && object.pagination !== null ? pagination_1.PageResponse.fromPartial(object.pagination) : undefined;
        return message;
    }
};
exports.QueryRedelegationsRequest = {
    $type: "cosmos.staking.v1beta1.QueryRedelegationsRequest",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.delegator_addr !== "") {
            writer.uint32(10).string(message.delegator_addr);
        }
        if (message.src_validator_addr !== "") {
            writer.uint32(18).string(message.src_validator_addr);
        }
        if (message.dst_validator_addr !== "") {
            writer.uint32(26).string(message.dst_validator_addr);
        }
        if (message.pagination !== undefined) {
            pagination_1.PageRequest.encode(message.pagination, writer.uint32(34).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseQueryRedelegationsRequest();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.delegator_addr = reader.string();
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.src_validator_addr = reader.string();
                    continue;
                case 3:
                    if (tag !== 26) {
                        break;
                    }
                    message.dst_validator_addr = reader.string();
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
            delegator_addr: isSet(object.delegator_addr) ? globalThis.String(object.delegator_addr) : "",
            src_validator_addr: isSet(object.src_validator_addr) ? globalThis.String(object.src_validator_addr) : "",
            dst_validator_addr: isSet(object.dst_validator_addr) ? globalThis.String(object.dst_validator_addr) : "",
            pagination: isSet(object.pagination) ? pagination_1.PageRequest.fromJSON(object.pagination) : undefined
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.delegator_addr !== "") {
            obj.delegator_addr = message.delegator_addr;
        }
        if (message.src_validator_addr !== "") {
            obj.src_validator_addr = message.src_validator_addr;
        }
        if (message.dst_validator_addr !== "") {
            obj.dst_validator_addr = message.dst_validator_addr;
        }
        if (message.pagination !== undefined) {
            obj.pagination = pagination_1.PageRequest.toJSON(message.pagination);
        }
        return obj;
    },
    create(base) {
        return exports.QueryRedelegationsRequest.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryRedelegationsRequest();
        message.delegator_addr = object.delegator_addr ?? "";
        message.src_validator_addr = object.src_validator_addr ?? "";
        message.dst_validator_addr = object.dst_validator_addr ?? "";
        message.pagination = object.pagination !== undefined && object.pagination !== null ? pagination_1.PageRequest.fromPartial(object.pagination) : undefined;
        return message;
    }
};
exports.QueryRedelegationsResponse = {
    $type: "cosmos.staking.v1beta1.QueryRedelegationsResponse",
    encode(message, writer = new wire_1.BinaryWriter()) {
        for (const v of message.redelegation_responses) {
            staking_1.RedelegationResponse.encode(v, writer.uint32(10).fork()).join();
        }
        if (message.pagination !== undefined) {
            pagination_1.PageResponse.encode(message.pagination, writer.uint32(18).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseQueryRedelegationsResponse();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.redelegation_responses.push(staking_1.RedelegationResponse.decode(reader, reader.uint32()));
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
            redelegation_responses: globalThis.Array.isArray(object?.redelegation_responses)
                ? object.redelegation_responses.map((e) => staking_1.RedelegationResponse.fromJSON(e))
                : [],
            pagination: isSet(object.pagination) ? pagination_1.PageResponse.fromJSON(object.pagination) : undefined
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.redelegation_responses?.length) {
            obj.redelegation_responses = message.redelegation_responses.map((e) => staking_1.RedelegationResponse.toJSON(e));
        }
        if (message.pagination !== undefined) {
            obj.pagination = pagination_1.PageResponse.toJSON(message.pagination);
        }
        return obj;
    },
    create(base) {
        return exports.QueryRedelegationsResponse.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryRedelegationsResponse();
        message.redelegation_responses = object.redelegation_responses?.map((e) => staking_1.RedelegationResponse.fromPartial(e)) || [];
        message.pagination = object.pagination !== undefined && object.pagination !== null ? pagination_1.PageResponse.fromPartial(object.pagination) : undefined;
        return message;
    }
};
exports.QueryDelegatorValidatorsRequest = {
    $type: "cosmos.staking.v1beta1.QueryDelegatorValidatorsRequest",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.delegator_addr !== "") {
            writer.uint32(10).string(message.delegator_addr);
        }
        if (message.pagination !== undefined) {
            pagination_1.PageRequest.encode(message.pagination, writer.uint32(18).fork()).join();
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
                    message.delegator_addr = reader.string();
                    continue;
                case 2:
                    if (tag !== 18) {
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
            delegator_addr: isSet(object.delegator_addr) ? globalThis.String(object.delegator_addr) : "",
            pagination: isSet(object.pagination) ? pagination_1.PageRequest.fromJSON(object.pagination) : undefined
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.delegator_addr !== "") {
            obj.delegator_addr = message.delegator_addr;
        }
        if (message.pagination !== undefined) {
            obj.pagination = pagination_1.PageRequest.toJSON(message.pagination);
        }
        return obj;
    },
    create(base) {
        return exports.QueryDelegatorValidatorsRequest.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryDelegatorValidatorsRequest();
        message.delegator_addr = object.delegator_addr ?? "";
        message.pagination = object.pagination !== undefined && object.pagination !== null ? pagination_1.PageRequest.fromPartial(object.pagination) : undefined;
        return message;
    }
};
exports.QueryDelegatorValidatorsResponse = {
    $type: "cosmos.staking.v1beta1.QueryDelegatorValidatorsResponse",
    encode(message, writer = new wire_1.BinaryWriter()) {
        for (const v of message.validators) {
            staking_1.Validator.encode(v, writer.uint32(10).fork()).join();
        }
        if (message.pagination !== undefined) {
            pagination_1.PageResponse.encode(message.pagination, writer.uint32(18).fork()).join();
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
                    message.validators.push(staking_1.Validator.decode(reader, reader.uint32()));
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
            validators: globalThis.Array.isArray(object?.validators) ? object.validators.map((e) => staking_1.Validator.fromJSON(e)) : [],
            pagination: isSet(object.pagination) ? pagination_1.PageResponse.fromJSON(object.pagination) : undefined
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.validators?.length) {
            obj.validators = message.validators.map((e) => staking_1.Validator.toJSON(e));
        }
        if (message.pagination !== undefined) {
            obj.pagination = pagination_1.PageResponse.toJSON(message.pagination);
        }
        return obj;
    },
    create(base) {
        return exports.QueryDelegatorValidatorsResponse.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryDelegatorValidatorsResponse();
        message.validators = object.validators?.map((e) => staking_1.Validator.fromPartial(e)) || [];
        message.pagination = object.pagination !== undefined && object.pagination !== null ? pagination_1.PageResponse.fromPartial(object.pagination) : undefined;
        return message;
    }
};
exports.QueryDelegatorValidatorRequest = {
    $type: "cosmos.staking.v1beta1.QueryDelegatorValidatorRequest",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.delegator_addr !== "") {
            writer.uint32(10).string(message.delegator_addr);
        }
        if (message.validator_addr !== "") {
            writer.uint32(18).string(message.validator_addr);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseQueryDelegatorValidatorRequest();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.delegator_addr = reader.string();
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.validator_addr = reader.string();
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
            delegator_addr: isSet(object.delegator_addr) ? globalThis.String(object.delegator_addr) : "",
            validator_addr: isSet(object.validator_addr) ? globalThis.String(object.validator_addr) : ""
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.delegator_addr !== "") {
            obj.delegator_addr = message.delegator_addr;
        }
        if (message.validator_addr !== "") {
            obj.validator_addr = message.validator_addr;
        }
        return obj;
    },
    create(base) {
        return exports.QueryDelegatorValidatorRequest.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryDelegatorValidatorRequest();
        message.delegator_addr = object.delegator_addr ?? "";
        message.validator_addr = object.validator_addr ?? "";
        return message;
    }
};
exports.QueryDelegatorValidatorResponse = {
    $type: "cosmos.staking.v1beta1.QueryDelegatorValidatorResponse",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.validator !== undefined) {
            staking_1.Validator.encode(message.validator, writer.uint32(10).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseQueryDelegatorValidatorResponse();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.validator = staking_1.Validator.decode(reader, reader.uint32());
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
        return { validator: isSet(object.validator) ? staking_1.Validator.fromJSON(object.validator) : undefined };
    },
    toJSON(message) {
        const obj = {};
        if (message.validator !== undefined) {
            obj.validator = staking_1.Validator.toJSON(message.validator);
        }
        return obj;
    },
    create(base) {
        return exports.QueryDelegatorValidatorResponse.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryDelegatorValidatorResponse();
        message.validator = object.validator !== undefined && object.validator !== null ? staking_1.Validator.fromPartial(object.validator) : undefined;
        return message;
    }
};
exports.QueryHistoricalInfoRequest = {
    $type: "cosmos.staking.v1beta1.QueryHistoricalInfoRequest",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.height !== 0) {
            writer.uint32(8).int64(message.height);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseQueryHistoricalInfoRequest();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 8) {
                        break;
                    }
                    message.height = longToNumber(reader.int64());
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
        return { height: isSet(object.height) ? globalThis.Number(object.height) : 0 };
    },
    toJSON(message) {
        const obj = {};
        if (message.height !== 0) {
            obj.height = Math.round(message.height);
        }
        return obj;
    },
    create(base) {
        return exports.QueryHistoricalInfoRequest.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryHistoricalInfoRequest();
        message.height = object.height ?? 0;
        return message;
    }
};
exports.QueryHistoricalInfoResponse = {
    $type: "cosmos.staking.v1beta1.QueryHistoricalInfoResponse",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.hist !== undefined) {
            staking_1.HistoricalInfo.encode(message.hist, writer.uint32(10).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseQueryHistoricalInfoResponse();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.hist = staking_1.HistoricalInfo.decode(reader, reader.uint32());
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
        return { hist: isSet(object.hist) ? staking_1.HistoricalInfo.fromJSON(object.hist) : undefined };
    },
    toJSON(message) {
        const obj = {};
        if (message.hist !== undefined) {
            obj.hist = staking_1.HistoricalInfo.toJSON(message.hist);
        }
        return obj;
    },
    create(base) {
        return exports.QueryHistoricalInfoResponse.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryHistoricalInfoResponse();
        message.hist = object.hist !== undefined && object.hist !== null ? staking_1.HistoricalInfo.fromPartial(object.hist) : undefined;
        return message;
    }
};
exports.QueryPoolRequest = {
    $type: "cosmos.staking.v1beta1.QueryPoolRequest",
    encode(_, writer = new wire_1.BinaryWriter()) {
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseQueryPoolRequest();
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
        return exports.QueryPoolRequest.fromPartial(base ?? {});
    },
    fromPartial(_) {
        const message = createBaseQueryPoolRequest();
        return message;
    }
};
exports.QueryPoolResponse = {
    $type: "cosmos.staking.v1beta1.QueryPoolResponse",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.pool !== undefined) {
            staking_1.Pool.encode(message.pool, writer.uint32(10).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseQueryPoolResponse();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.pool = staking_1.Pool.decode(reader, reader.uint32());
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
        return { pool: isSet(object.pool) ? staking_1.Pool.fromJSON(object.pool) : undefined };
    },
    toJSON(message) {
        const obj = {};
        if (message.pool !== undefined) {
            obj.pool = staking_1.Pool.toJSON(message.pool);
        }
        return obj;
    },
    create(base) {
        return exports.QueryPoolResponse.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryPoolResponse();
        message.pool = object.pool !== undefined && object.pool !== null ? staking_1.Pool.fromPartial(object.pool) : undefined;
        return message;
    }
};
exports.QueryParamsRequest = {
    $type: "cosmos.staking.v1beta1.QueryParamsRequest",
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
    $type: "cosmos.staking.v1beta1.QueryParamsResponse",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.params !== undefined) {
            staking_1.Params.encode(message.params, writer.uint32(10).fork()).join();
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
                    message.params = staking_1.Params.decode(reader, reader.uint32());
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
        return { params: isSet(object.params) ? staking_1.Params.fromJSON(object.params) : undefined };
    },
    toJSON(message) {
        const obj = {};
        if (message.params !== undefined) {
            obj.params = staking_1.Params.toJSON(message.params);
        }
        return obj;
    },
    create(base) {
        return exports.QueryParamsResponse.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryParamsResponse();
        message.params = object.params !== undefined && object.params !== null ? staking_1.Params.fromPartial(object.params) : undefined;
        return message;
    }
};
function createBaseQueryValidatorsRequest() {
    return { status: "", pagination: undefined };
}
function createBaseQueryValidatorsResponse() {
    return { validators: [], pagination: undefined };
}
function createBaseQueryValidatorRequest() {
    return { validator_addr: "" };
}
function createBaseQueryValidatorResponse() {
    return { validator: undefined };
}
function createBaseQueryValidatorDelegationsRequest() {
    return { validator_addr: "", pagination: undefined };
}
function createBaseQueryValidatorDelegationsResponse() {
    return { delegation_responses: [], pagination: undefined };
}
function createBaseQueryValidatorUnbondingDelegationsRequest() {
    return { validator_addr: "", pagination: undefined };
}
function createBaseQueryValidatorUnbondingDelegationsResponse() {
    return { unbonding_responses: [], pagination: undefined };
}
function createBaseQueryDelegationRequest() {
    return { delegator_addr: "", validator_addr: "" };
}
function createBaseQueryDelegationResponse() {
    return { delegation_response: undefined };
}
function createBaseQueryUnbondingDelegationRequest() {
    return { delegator_addr: "", validator_addr: "" };
}
function createBaseQueryUnbondingDelegationResponse() {
    return { unbond: undefined };
}
function createBaseQueryDelegatorDelegationsRequest() {
    return { delegator_addr: "", pagination: undefined };
}
function createBaseQueryDelegatorDelegationsResponse() {
    return { delegation_responses: [], pagination: undefined };
}
function createBaseQueryDelegatorUnbondingDelegationsRequest() {
    return { delegator_addr: "", pagination: undefined };
}
function createBaseQueryDelegatorUnbondingDelegationsResponse() {
    return { unbonding_responses: [], pagination: undefined };
}
function createBaseQueryRedelegationsRequest() {
    return { delegator_addr: "", src_validator_addr: "", dst_validator_addr: "", pagination: undefined };
}
function createBaseQueryRedelegationsResponse() {
    return { redelegation_responses: [], pagination: undefined };
}
function createBaseQueryDelegatorValidatorsRequest() {
    return { delegator_addr: "", pagination: undefined };
}
function createBaseQueryDelegatorValidatorsResponse() {
    return { validators: [], pagination: undefined };
}
function createBaseQueryDelegatorValidatorRequest() {
    return { delegator_addr: "", validator_addr: "" };
}
function createBaseQueryDelegatorValidatorResponse() {
    return { validator: undefined };
}
function createBaseQueryHistoricalInfoRequest() {
    return { height: 0 };
}
function createBaseQueryHistoricalInfoResponse() {
    return { hist: undefined };
}
function createBaseQueryPoolRequest() {
    return {};
}
function createBaseQueryPoolResponse() {
    return { pool: undefined };
}
function createBaseQueryParamsRequest() {
    return {};
}
function createBaseQueryParamsResponse() {
    return { params: undefined };
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
    ["/cosmos.staking.v1beta1.QueryValidatorsRequest", exports.QueryValidatorsRequest],
    ["/cosmos.staking.v1beta1.QueryValidatorsResponse", exports.QueryValidatorsResponse],
    ["/cosmos.staking.v1beta1.QueryValidatorRequest", exports.QueryValidatorRequest],
    ["/cosmos.staking.v1beta1.QueryValidatorResponse", exports.QueryValidatorResponse],
    ["/cosmos.staking.v1beta1.QueryValidatorDelegationsRequest", exports.QueryValidatorDelegationsRequest],
    ["/cosmos.staking.v1beta1.QueryValidatorDelegationsResponse", exports.QueryValidatorDelegationsResponse],
    ["/cosmos.staking.v1beta1.QueryValidatorUnbondingDelegationsRequest", exports.QueryValidatorUnbondingDelegationsRequest],
    ["/cosmos.staking.v1beta1.QueryValidatorUnbondingDelegationsResponse", exports.QueryValidatorUnbondingDelegationsResponse],
    ["/cosmos.staking.v1beta1.QueryDelegationRequest", exports.QueryDelegationRequest],
    ["/cosmos.staking.v1beta1.QueryDelegationResponse", exports.QueryDelegationResponse],
    ["/cosmos.staking.v1beta1.QueryUnbondingDelegationRequest", exports.QueryUnbondingDelegationRequest],
    ["/cosmos.staking.v1beta1.QueryUnbondingDelegationResponse", exports.QueryUnbondingDelegationResponse],
    ["/cosmos.staking.v1beta1.QueryDelegatorDelegationsRequest", exports.QueryDelegatorDelegationsRequest],
    ["/cosmos.staking.v1beta1.QueryDelegatorDelegationsResponse", exports.QueryDelegatorDelegationsResponse],
    ["/cosmos.staking.v1beta1.QueryDelegatorUnbondingDelegationsRequest", exports.QueryDelegatorUnbondingDelegationsRequest],
    ["/cosmos.staking.v1beta1.QueryDelegatorUnbondingDelegationsResponse", exports.QueryDelegatorUnbondingDelegationsResponse],
    ["/cosmos.staking.v1beta1.QueryRedelegationsRequest", exports.QueryRedelegationsRequest],
    ["/cosmos.staking.v1beta1.QueryRedelegationsResponse", exports.QueryRedelegationsResponse],
    ["/cosmos.staking.v1beta1.QueryDelegatorValidatorsRequest", exports.QueryDelegatorValidatorsRequest],
    ["/cosmos.staking.v1beta1.QueryDelegatorValidatorsResponse", exports.QueryDelegatorValidatorsResponse],
    ["/cosmos.staking.v1beta1.QueryDelegatorValidatorRequest", exports.QueryDelegatorValidatorRequest],
    ["/cosmos.staking.v1beta1.QueryDelegatorValidatorResponse", exports.QueryDelegatorValidatorResponse],
    ["/cosmos.staking.v1beta1.QueryHistoricalInfoRequest", exports.QueryHistoricalInfoRequest],
    ["/cosmos.staking.v1beta1.QueryHistoricalInfoResponse", exports.QueryHistoricalInfoResponse],
    ["/cosmos.staking.v1beta1.QueryPoolRequest", exports.QueryPoolRequest],
    ["/cosmos.staking.v1beta1.QueryPoolResponse", exports.QueryPoolResponse],
    ["/cosmos.staking.v1beta1.QueryParamsRequest", exports.QueryParamsRequest],
    ["/cosmos.staking.v1beta1.QueryParamsResponse", exports.QueryParamsResponse]
];
exports.aminoConverters = {
    "/cosmos.staking.v1beta1.QueryValidatorsRequest": {
        aminoType: "cosmos-sdk/QueryValidatorsRequest",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.staking.v1beta1.QueryValidatorsResponse": {
        aminoType: "cosmos-sdk/QueryValidatorsResponse",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.staking.v1beta1.QueryValidatorRequest": {
        aminoType: "cosmos-sdk/QueryValidatorRequest",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.staking.v1beta1.QueryValidatorResponse": {
        aminoType: "cosmos-sdk/QueryValidatorResponse",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.staking.v1beta1.QueryValidatorDelegationsRequest": {
        aminoType: "cosmos-sdk/QueryValidatorDelegationsRequest",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.staking.v1beta1.QueryValidatorDelegationsResponse": {
        aminoType: "cosmos-sdk/QueryValidatorDelegationsResponse",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.staking.v1beta1.QueryValidatorUnbondingDelegationsRequest": {
        aminoType: "cosmos-sdk/QueryValidatorUnbondingDelegationsRequest",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.staking.v1beta1.QueryValidatorUnbondingDelegationsResponse": {
        aminoType: "cosmos-sdk/QueryValidatorUnbondingDelegationsResponse",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.staking.v1beta1.QueryDelegationRequest": {
        aminoType: "cosmos-sdk/QueryDelegationRequest",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.staking.v1beta1.QueryDelegationResponse": {
        aminoType: "cosmos-sdk/QueryDelegationResponse",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.staking.v1beta1.QueryUnbondingDelegationRequest": {
        aminoType: "cosmos-sdk/QueryUnbondingDelegationRequest",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.staking.v1beta1.QueryUnbondingDelegationResponse": {
        aminoType: "cosmos-sdk/QueryUnbondingDelegationResponse",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.staking.v1beta1.QueryDelegatorDelegationsRequest": {
        aminoType: "cosmos-sdk/QueryDelegatorDelegationsRequest",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.staking.v1beta1.QueryDelegatorDelegationsResponse": {
        aminoType: "cosmos-sdk/QueryDelegatorDelegationsResponse",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.staking.v1beta1.QueryDelegatorUnbondingDelegationsRequest": {
        aminoType: "cosmos-sdk/QueryDelegatorUnbondingDelegationsRequest",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.staking.v1beta1.QueryDelegatorUnbondingDelegationsResponse": {
        aminoType: "cosmos-sdk/QueryDelegatorUnbondingDelegationsResponse",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.staking.v1beta1.QueryRedelegationsRequest": {
        aminoType: "cosmos-sdk/QueryRedelegationsRequest",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.staking.v1beta1.QueryRedelegationsResponse": {
        aminoType: "cosmos-sdk/QueryRedelegationsResponse",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.staking.v1beta1.QueryDelegatorValidatorsRequest": {
        aminoType: "cosmos-sdk/QueryDelegatorValidatorsRequest",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.staking.v1beta1.QueryDelegatorValidatorsResponse": {
        aminoType: "cosmos-sdk/QueryDelegatorValidatorsResponse",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.staking.v1beta1.QueryDelegatorValidatorRequest": {
        aminoType: "cosmos-sdk/QueryDelegatorValidatorRequest",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.staking.v1beta1.QueryDelegatorValidatorResponse": {
        aminoType: "cosmos-sdk/QueryDelegatorValidatorResponse",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.staking.v1beta1.QueryHistoricalInfoRequest": {
        aminoType: "cosmos-sdk/QueryHistoricalInfoRequest",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.staking.v1beta1.QueryHistoricalInfoResponse": {
        aminoType: "cosmos-sdk/QueryHistoricalInfoResponse",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.staking.v1beta1.QueryPoolRequest": {
        aminoType: "cosmos-sdk/QueryPoolRequest",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.staking.v1beta1.QueryPoolResponse": {
        aminoType: "cosmos-sdk/QueryPoolResponse",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.staking.v1beta1.QueryParamsRequest": {
        aminoType: "cosmos-sdk/QueryParamsRequest",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.staking.v1beta1.QueryParamsResponse": {
        aminoType: "cosmos-sdk/QueryParamsResponse",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    }
};
