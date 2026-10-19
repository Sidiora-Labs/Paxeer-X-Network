import { BinaryReader, BinaryWriter } from "@bufbuild/protobuf/wire";
import { PageRequest, PageResponse } from "../../base/query/v1beta1/pagination";
import { DelegationResponse, HistoricalInfo, Params, Pool, RedelegationResponse, UnbondingDelegation, Validator } from "./staking";
export const QueryValidatorsRequest = {
    $type: "cosmos.staking.v1beta1.QueryValidatorsRequest",
    encode(message, writer = new BinaryWriter()) {
        if (message.status !== "") {
            writer.uint32(10).string(message.status);
        }
        if (message.pagination !== undefined) {
            PageRequest.encode(message.pagination, writer.uint32(18).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
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
            status: isSet(object.status) ? globalThis.String(object.status) : "",
            pagination: isSet(object.pagination) ? PageRequest.fromJSON(object.pagination) : undefined
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.status !== "") {
            obj.status = message.status;
        }
        if (message.pagination !== undefined) {
            obj.pagination = PageRequest.toJSON(message.pagination);
        }
        return obj;
    },
    create(base) {
        return QueryValidatorsRequest.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryValidatorsRequest();
        message.status = object.status ?? "";
        message.pagination = object.pagination !== undefined && object.pagination !== null ? PageRequest.fromPartial(object.pagination) : undefined;
        return message;
    }
};
export const QueryValidatorsResponse = {
    $type: "cosmos.staking.v1beta1.QueryValidatorsResponse",
    encode(message, writer = new BinaryWriter()) {
        for (const v of message.validators) {
            Validator.encode(v, writer.uint32(10).fork()).join();
        }
        if (message.pagination !== undefined) {
            PageResponse.encode(message.pagination, writer.uint32(18).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseQueryValidatorsResponse();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.validators.push(Validator.decode(reader, reader.uint32()));
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
            validators: globalThis.Array.isArray(object?.validators) ? object.validators.map((e) => Validator.fromJSON(e)) : [],
            pagination: isSet(object.pagination) ? PageResponse.fromJSON(object.pagination) : undefined
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.validators?.length) {
            obj.validators = message.validators.map((e) => Validator.toJSON(e));
        }
        if (message.pagination !== undefined) {
            obj.pagination = PageResponse.toJSON(message.pagination);
        }
        return obj;
    },
    create(base) {
        return QueryValidatorsResponse.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryValidatorsResponse();
        message.validators = object.validators?.map((e) => Validator.fromPartial(e)) || [];
        message.pagination = object.pagination !== undefined && object.pagination !== null ? PageResponse.fromPartial(object.pagination) : undefined;
        return message;
    }
};
export const QueryValidatorRequest = {
    $type: "cosmos.staking.v1beta1.QueryValidatorRequest",
    encode(message, writer = new BinaryWriter()) {
        if (message.validator_addr !== "") {
            writer.uint32(10).string(message.validator_addr);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
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
        return QueryValidatorRequest.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryValidatorRequest();
        message.validator_addr = object.validator_addr ?? "";
        return message;
    }
};
export const QueryValidatorResponse = {
    $type: "cosmos.staking.v1beta1.QueryValidatorResponse",
    encode(message, writer = new BinaryWriter()) {
        if (message.validator !== undefined) {
            Validator.encode(message.validator, writer.uint32(10).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseQueryValidatorResponse();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.validator = Validator.decode(reader, reader.uint32());
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
        return { validator: isSet(object.validator) ? Validator.fromJSON(object.validator) : undefined };
    },
    toJSON(message) {
        const obj = {};
        if (message.validator !== undefined) {
            obj.validator = Validator.toJSON(message.validator);
        }
        return obj;
    },
    create(base) {
        return QueryValidatorResponse.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryValidatorResponse();
        message.validator = object.validator !== undefined && object.validator !== null ? Validator.fromPartial(object.validator) : undefined;
        return message;
    }
};
export const QueryValidatorDelegationsRequest = {
    $type: "cosmos.staking.v1beta1.QueryValidatorDelegationsRequest",
    encode(message, writer = new BinaryWriter()) {
        if (message.validator_addr !== "") {
            writer.uint32(10).string(message.validator_addr);
        }
        if (message.pagination !== undefined) {
            PageRequest.encode(message.pagination, writer.uint32(18).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
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
            validator_addr: isSet(object.validator_addr) ? globalThis.String(object.validator_addr) : "",
            pagination: isSet(object.pagination) ? PageRequest.fromJSON(object.pagination) : undefined
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.validator_addr !== "") {
            obj.validator_addr = message.validator_addr;
        }
        if (message.pagination !== undefined) {
            obj.pagination = PageRequest.toJSON(message.pagination);
        }
        return obj;
    },
    create(base) {
        return QueryValidatorDelegationsRequest.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryValidatorDelegationsRequest();
        message.validator_addr = object.validator_addr ?? "";
        message.pagination = object.pagination !== undefined && object.pagination !== null ? PageRequest.fromPartial(object.pagination) : undefined;
        return message;
    }
};
export const QueryValidatorDelegationsResponse = {
    $type: "cosmos.staking.v1beta1.QueryValidatorDelegationsResponse",
    encode(message, writer = new BinaryWriter()) {
        for (const v of message.delegation_responses) {
            DelegationResponse.encode(v, writer.uint32(10).fork()).join();
        }
        if (message.pagination !== undefined) {
            PageResponse.encode(message.pagination, writer.uint32(18).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseQueryValidatorDelegationsResponse();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.delegation_responses.push(DelegationResponse.decode(reader, reader.uint32()));
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
            delegation_responses: globalThis.Array.isArray(object?.delegation_responses)
                ? object.delegation_responses.map((e) => DelegationResponse.fromJSON(e))
                : [],
            pagination: isSet(object.pagination) ? PageResponse.fromJSON(object.pagination) : undefined
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.delegation_responses?.length) {
            obj.delegation_responses = message.delegation_responses.map((e) => DelegationResponse.toJSON(e));
        }
        if (message.pagination !== undefined) {
            obj.pagination = PageResponse.toJSON(message.pagination);
        }
        return obj;
    },
    create(base) {
        return QueryValidatorDelegationsResponse.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryValidatorDelegationsResponse();
        message.delegation_responses = object.delegation_responses?.map((e) => DelegationResponse.fromPartial(e)) || [];
        message.pagination = object.pagination !== undefined && object.pagination !== null ? PageResponse.fromPartial(object.pagination) : undefined;
        return message;
    }
};
export const QueryValidatorUnbondingDelegationsRequest = {
    $type: "cosmos.staking.v1beta1.QueryValidatorUnbondingDelegationsRequest",
    encode(message, writer = new BinaryWriter()) {
        if (message.validator_addr !== "") {
            writer.uint32(10).string(message.validator_addr);
        }
        if (message.pagination !== undefined) {
            PageRequest.encode(message.pagination, writer.uint32(18).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
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
            validator_addr: isSet(object.validator_addr) ? globalThis.String(object.validator_addr) : "",
            pagination: isSet(object.pagination) ? PageRequest.fromJSON(object.pagination) : undefined
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.validator_addr !== "") {
            obj.validator_addr = message.validator_addr;
        }
        if (message.pagination !== undefined) {
            obj.pagination = PageRequest.toJSON(message.pagination);
        }
        return obj;
    },
    create(base) {
        return QueryValidatorUnbondingDelegationsRequest.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryValidatorUnbondingDelegationsRequest();
        message.validator_addr = object.validator_addr ?? "";
        message.pagination = object.pagination !== undefined && object.pagination !== null ? PageRequest.fromPartial(object.pagination) : undefined;
        return message;
    }
};
export const QueryValidatorUnbondingDelegationsResponse = {
    $type: "cosmos.staking.v1beta1.QueryValidatorUnbondingDelegationsResponse",
    encode(message, writer = new BinaryWriter()) {
        for (const v of message.unbonding_responses) {
            UnbondingDelegation.encode(v, writer.uint32(10).fork()).join();
        }
        if (message.pagination !== undefined) {
            PageResponse.encode(message.pagination, writer.uint32(18).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseQueryValidatorUnbondingDelegationsResponse();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.unbonding_responses.push(UnbondingDelegation.decode(reader, reader.uint32()));
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
            unbonding_responses: globalThis.Array.isArray(object?.unbonding_responses)
                ? object.unbonding_responses.map((e) => UnbondingDelegation.fromJSON(e))
                : [],
            pagination: isSet(object.pagination) ? PageResponse.fromJSON(object.pagination) : undefined
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.unbonding_responses?.length) {
            obj.unbonding_responses = message.unbonding_responses.map((e) => UnbondingDelegation.toJSON(e));
        }
        if (message.pagination !== undefined) {
            obj.pagination = PageResponse.toJSON(message.pagination);
        }
        return obj;
    },
    create(base) {
        return QueryValidatorUnbondingDelegationsResponse.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryValidatorUnbondingDelegationsResponse();
        message.unbonding_responses = object.unbonding_responses?.map((e) => UnbondingDelegation.fromPartial(e)) || [];
        message.pagination = object.pagination !== undefined && object.pagination !== null ? PageResponse.fromPartial(object.pagination) : undefined;
        return message;
    }
};
export const QueryDelegationRequest = {
    $type: "cosmos.staking.v1beta1.QueryDelegationRequest",
    encode(message, writer = new BinaryWriter()) {
        if (message.delegator_addr !== "") {
            writer.uint32(10).string(message.delegator_addr);
        }
        if (message.validator_addr !== "") {
            writer.uint32(18).string(message.validator_addr);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
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
        return QueryDelegationRequest.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryDelegationRequest();
        message.delegator_addr = object.delegator_addr ?? "";
        message.validator_addr = object.validator_addr ?? "";
        return message;
    }
};
export const QueryDelegationResponse = {
    $type: "cosmos.staking.v1beta1.QueryDelegationResponse",
    encode(message, writer = new BinaryWriter()) {
        if (message.delegation_response !== undefined) {
            DelegationResponse.encode(message.delegation_response, writer.uint32(10).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseQueryDelegationResponse();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.delegation_response = DelegationResponse.decode(reader, reader.uint32());
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
            delegation_response: isSet(object.delegation_response) ? DelegationResponse.fromJSON(object.delegation_response) : undefined
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.delegation_response !== undefined) {
            obj.delegation_response = DelegationResponse.toJSON(message.delegation_response);
        }
        return obj;
    },
    create(base) {
        return QueryDelegationResponse.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryDelegationResponse();
        message.delegation_response =
            object.delegation_response !== undefined && object.delegation_response !== null ? DelegationResponse.fromPartial(object.delegation_response) : undefined;
        return message;
    }
};
export const QueryUnbondingDelegationRequest = {
    $type: "cosmos.staking.v1beta1.QueryUnbondingDelegationRequest",
    encode(message, writer = new BinaryWriter()) {
        if (message.delegator_addr !== "") {
            writer.uint32(10).string(message.delegator_addr);
        }
        if (message.validator_addr !== "") {
            writer.uint32(18).string(message.validator_addr);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
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
        return QueryUnbondingDelegationRequest.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryUnbondingDelegationRequest();
        message.delegator_addr = object.delegator_addr ?? "";
        message.validator_addr = object.validator_addr ?? "";
        return message;
    }
};
export const QueryUnbondingDelegationResponse = {
    $type: "cosmos.staking.v1beta1.QueryUnbondingDelegationResponse",
    encode(message, writer = new BinaryWriter()) {
        if (message.unbond !== undefined) {
            UnbondingDelegation.encode(message.unbond, writer.uint32(10).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseQueryUnbondingDelegationResponse();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.unbond = UnbondingDelegation.decode(reader, reader.uint32());
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
        return { unbond: isSet(object.unbond) ? UnbondingDelegation.fromJSON(object.unbond) : undefined };
    },
    toJSON(message) {
        const obj = {};
        if (message.unbond !== undefined) {
            obj.unbond = UnbondingDelegation.toJSON(message.unbond);
        }
        return obj;
    },
    create(base) {
        return QueryUnbondingDelegationResponse.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryUnbondingDelegationResponse();
        message.unbond = object.unbond !== undefined && object.unbond !== null ? UnbondingDelegation.fromPartial(object.unbond) : undefined;
        return message;
    }
};
export const QueryDelegatorDelegationsRequest = {
    $type: "cosmos.staking.v1beta1.QueryDelegatorDelegationsRequest",
    encode(message, writer = new BinaryWriter()) {
        if (message.delegator_addr !== "") {
            writer.uint32(10).string(message.delegator_addr);
        }
        if (message.pagination !== undefined) {
            PageRequest.encode(message.pagination, writer.uint32(18).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
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
            delegator_addr: isSet(object.delegator_addr) ? globalThis.String(object.delegator_addr) : "",
            pagination: isSet(object.pagination) ? PageRequest.fromJSON(object.pagination) : undefined
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.delegator_addr !== "") {
            obj.delegator_addr = message.delegator_addr;
        }
        if (message.pagination !== undefined) {
            obj.pagination = PageRequest.toJSON(message.pagination);
        }
        return obj;
    },
    create(base) {
        return QueryDelegatorDelegationsRequest.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryDelegatorDelegationsRequest();
        message.delegator_addr = object.delegator_addr ?? "";
        message.pagination = object.pagination !== undefined && object.pagination !== null ? PageRequest.fromPartial(object.pagination) : undefined;
        return message;
    }
};
export const QueryDelegatorDelegationsResponse = {
    $type: "cosmos.staking.v1beta1.QueryDelegatorDelegationsResponse",
    encode(message, writer = new BinaryWriter()) {
        for (const v of message.delegation_responses) {
            DelegationResponse.encode(v, writer.uint32(10).fork()).join();
        }
        if (message.pagination !== undefined) {
            PageResponse.encode(message.pagination, writer.uint32(18).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseQueryDelegatorDelegationsResponse();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.delegation_responses.push(DelegationResponse.decode(reader, reader.uint32()));
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
            delegation_responses: globalThis.Array.isArray(object?.delegation_responses)
                ? object.delegation_responses.map((e) => DelegationResponse.fromJSON(e))
                : [],
            pagination: isSet(object.pagination) ? PageResponse.fromJSON(object.pagination) : undefined
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.delegation_responses?.length) {
            obj.delegation_responses = message.delegation_responses.map((e) => DelegationResponse.toJSON(e));
        }
        if (message.pagination !== undefined) {
            obj.pagination = PageResponse.toJSON(message.pagination);
        }
        return obj;
    },
    create(base) {
        return QueryDelegatorDelegationsResponse.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryDelegatorDelegationsResponse();
        message.delegation_responses = object.delegation_responses?.map((e) => DelegationResponse.fromPartial(e)) || [];
        message.pagination = object.pagination !== undefined && object.pagination !== null ? PageResponse.fromPartial(object.pagination) : undefined;
        return message;
    }
};
export const QueryDelegatorUnbondingDelegationsRequest = {
    $type: "cosmos.staking.v1beta1.QueryDelegatorUnbondingDelegationsRequest",
    encode(message, writer = new BinaryWriter()) {
        if (message.delegator_addr !== "") {
            writer.uint32(10).string(message.delegator_addr);
        }
        if (message.pagination !== undefined) {
            PageRequest.encode(message.pagination, writer.uint32(18).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
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
            delegator_addr: isSet(object.delegator_addr) ? globalThis.String(object.delegator_addr) : "",
            pagination: isSet(object.pagination) ? PageRequest.fromJSON(object.pagination) : undefined
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.delegator_addr !== "") {
            obj.delegator_addr = message.delegator_addr;
        }
        if (message.pagination !== undefined) {
            obj.pagination = PageRequest.toJSON(message.pagination);
        }
        return obj;
    },
    create(base) {
        return QueryDelegatorUnbondingDelegationsRequest.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryDelegatorUnbondingDelegationsRequest();
        message.delegator_addr = object.delegator_addr ?? "";
        message.pagination = object.pagination !== undefined && object.pagination !== null ? PageRequest.fromPartial(object.pagination) : undefined;
        return message;
    }
};
export const QueryDelegatorUnbondingDelegationsResponse = {
    $type: "cosmos.staking.v1beta1.QueryDelegatorUnbondingDelegationsResponse",
    encode(message, writer = new BinaryWriter()) {
        for (const v of message.unbonding_responses) {
            UnbondingDelegation.encode(v, writer.uint32(10).fork()).join();
        }
        if (message.pagination !== undefined) {
            PageResponse.encode(message.pagination, writer.uint32(18).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseQueryDelegatorUnbondingDelegationsResponse();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.unbonding_responses.push(UnbondingDelegation.decode(reader, reader.uint32()));
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
            unbonding_responses: globalThis.Array.isArray(object?.unbonding_responses)
                ? object.unbonding_responses.map((e) => UnbondingDelegation.fromJSON(e))
                : [],
            pagination: isSet(object.pagination) ? PageResponse.fromJSON(object.pagination) : undefined
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.unbonding_responses?.length) {
            obj.unbonding_responses = message.unbonding_responses.map((e) => UnbondingDelegation.toJSON(e));
        }
        if (message.pagination !== undefined) {
            obj.pagination = PageResponse.toJSON(message.pagination);
        }
        return obj;
    },
    create(base) {
        return QueryDelegatorUnbondingDelegationsResponse.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryDelegatorUnbondingDelegationsResponse();
        message.unbonding_responses = object.unbonding_responses?.map((e) => UnbondingDelegation.fromPartial(e)) || [];
        message.pagination = object.pagination !== undefined && object.pagination !== null ? PageResponse.fromPartial(object.pagination) : undefined;
        return message;
    }
};
export const QueryRedelegationsRequest = {
    $type: "cosmos.staking.v1beta1.QueryRedelegationsRequest",
    encode(message, writer = new BinaryWriter()) {
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
            PageRequest.encode(message.pagination, writer.uint32(34).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
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
            delegator_addr: isSet(object.delegator_addr) ? globalThis.String(object.delegator_addr) : "",
            src_validator_addr: isSet(object.src_validator_addr) ? globalThis.String(object.src_validator_addr) : "",
            dst_validator_addr: isSet(object.dst_validator_addr) ? globalThis.String(object.dst_validator_addr) : "",
            pagination: isSet(object.pagination) ? PageRequest.fromJSON(object.pagination) : undefined
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
            obj.pagination = PageRequest.toJSON(message.pagination);
        }
        return obj;
    },
    create(base) {
        return QueryRedelegationsRequest.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryRedelegationsRequest();
        message.delegator_addr = object.delegator_addr ?? "";
        message.src_validator_addr = object.src_validator_addr ?? "";
        message.dst_validator_addr = object.dst_validator_addr ?? "";
        message.pagination = object.pagination !== undefined && object.pagination !== null ? PageRequest.fromPartial(object.pagination) : undefined;
        return message;
    }
};
export const QueryRedelegationsResponse = {
    $type: "cosmos.staking.v1beta1.QueryRedelegationsResponse",
    encode(message, writer = new BinaryWriter()) {
        for (const v of message.redelegation_responses) {
            RedelegationResponse.encode(v, writer.uint32(10).fork()).join();
        }
        if (message.pagination !== undefined) {
            PageResponse.encode(message.pagination, writer.uint32(18).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseQueryRedelegationsResponse();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.redelegation_responses.push(RedelegationResponse.decode(reader, reader.uint32()));
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
            redelegation_responses: globalThis.Array.isArray(object?.redelegation_responses)
                ? object.redelegation_responses.map((e) => RedelegationResponse.fromJSON(e))
                : [],
            pagination: isSet(object.pagination) ? PageResponse.fromJSON(object.pagination) : undefined
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.redelegation_responses?.length) {
            obj.redelegation_responses = message.redelegation_responses.map((e) => RedelegationResponse.toJSON(e));
        }
        if (message.pagination !== undefined) {
            obj.pagination = PageResponse.toJSON(message.pagination);
        }
        return obj;
    },
    create(base) {
        return QueryRedelegationsResponse.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryRedelegationsResponse();
        message.redelegation_responses = object.redelegation_responses?.map((e) => RedelegationResponse.fromPartial(e)) || [];
        message.pagination = object.pagination !== undefined && object.pagination !== null ? PageResponse.fromPartial(object.pagination) : undefined;
        return message;
    }
};
export const QueryDelegatorValidatorsRequest = {
    $type: "cosmos.staking.v1beta1.QueryDelegatorValidatorsRequest",
    encode(message, writer = new BinaryWriter()) {
        if (message.delegator_addr !== "") {
            writer.uint32(10).string(message.delegator_addr);
        }
        if (message.pagination !== undefined) {
            PageRequest.encode(message.pagination, writer.uint32(18).fork()).join();
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
                    message.delegator_addr = reader.string();
                    continue;
                case 2:
                    if (tag !== 18) {
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
            delegator_addr: isSet(object.delegator_addr) ? globalThis.String(object.delegator_addr) : "",
            pagination: isSet(object.pagination) ? PageRequest.fromJSON(object.pagination) : undefined
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.delegator_addr !== "") {
            obj.delegator_addr = message.delegator_addr;
        }
        if (message.pagination !== undefined) {
            obj.pagination = PageRequest.toJSON(message.pagination);
        }
        return obj;
    },
    create(base) {
        return QueryDelegatorValidatorsRequest.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryDelegatorValidatorsRequest();
        message.delegator_addr = object.delegator_addr ?? "";
        message.pagination = object.pagination !== undefined && object.pagination !== null ? PageRequest.fromPartial(object.pagination) : undefined;
        return message;
    }
};
export const QueryDelegatorValidatorsResponse = {
    $type: "cosmos.staking.v1beta1.QueryDelegatorValidatorsResponse",
    encode(message, writer = new BinaryWriter()) {
        for (const v of message.validators) {
            Validator.encode(v, writer.uint32(10).fork()).join();
        }
        if (message.pagination !== undefined) {
            PageResponse.encode(message.pagination, writer.uint32(18).fork()).join();
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
                    message.validators.push(Validator.decode(reader, reader.uint32()));
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
            validators: globalThis.Array.isArray(object?.validators) ? object.validators.map((e) => Validator.fromJSON(e)) : [],
            pagination: isSet(object.pagination) ? PageResponse.fromJSON(object.pagination) : undefined
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.validators?.length) {
            obj.validators = message.validators.map((e) => Validator.toJSON(e));
        }
        if (message.pagination !== undefined) {
            obj.pagination = PageResponse.toJSON(message.pagination);
        }
        return obj;
    },
    create(base) {
        return QueryDelegatorValidatorsResponse.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryDelegatorValidatorsResponse();
        message.validators = object.validators?.map((e) => Validator.fromPartial(e)) || [];
        message.pagination = object.pagination !== undefined && object.pagination !== null ? PageResponse.fromPartial(object.pagination) : undefined;
        return message;
    }
};
export const QueryDelegatorValidatorRequest = {
    $type: "cosmos.staking.v1beta1.QueryDelegatorValidatorRequest",
    encode(message, writer = new BinaryWriter()) {
        if (message.delegator_addr !== "") {
            writer.uint32(10).string(message.delegator_addr);
        }
        if (message.validator_addr !== "") {
            writer.uint32(18).string(message.validator_addr);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
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
        return QueryDelegatorValidatorRequest.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryDelegatorValidatorRequest();
        message.delegator_addr = object.delegator_addr ?? "";
        message.validator_addr = object.validator_addr ?? "";
        return message;
    }
};
export const QueryDelegatorValidatorResponse = {
    $type: "cosmos.staking.v1beta1.QueryDelegatorValidatorResponse",
    encode(message, writer = new BinaryWriter()) {
        if (message.validator !== undefined) {
            Validator.encode(message.validator, writer.uint32(10).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseQueryDelegatorValidatorResponse();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.validator = Validator.decode(reader, reader.uint32());
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
        return { validator: isSet(object.validator) ? Validator.fromJSON(object.validator) : undefined };
    },
    toJSON(message) {
        const obj = {};
        if (message.validator !== undefined) {
            obj.validator = Validator.toJSON(message.validator);
        }
        return obj;
    },
    create(base) {
        return QueryDelegatorValidatorResponse.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryDelegatorValidatorResponse();
        message.validator = object.validator !== undefined && object.validator !== null ? Validator.fromPartial(object.validator) : undefined;
        return message;
    }
};
export const QueryHistoricalInfoRequest = {
    $type: "cosmos.staking.v1beta1.QueryHistoricalInfoRequest",
    encode(message, writer = new BinaryWriter()) {
        if (message.height !== 0) {
            writer.uint32(8).int64(message.height);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
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
        return QueryHistoricalInfoRequest.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryHistoricalInfoRequest();
        message.height = object.height ?? 0;
        return message;
    }
};
export const QueryHistoricalInfoResponse = {
    $type: "cosmos.staking.v1beta1.QueryHistoricalInfoResponse",
    encode(message, writer = new BinaryWriter()) {
        if (message.hist !== undefined) {
            HistoricalInfo.encode(message.hist, writer.uint32(10).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseQueryHistoricalInfoResponse();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.hist = HistoricalInfo.decode(reader, reader.uint32());
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
        return { hist: isSet(object.hist) ? HistoricalInfo.fromJSON(object.hist) : undefined };
    },
    toJSON(message) {
        const obj = {};
        if (message.hist !== undefined) {
            obj.hist = HistoricalInfo.toJSON(message.hist);
        }
        return obj;
    },
    create(base) {
        return QueryHistoricalInfoResponse.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryHistoricalInfoResponse();
        message.hist = object.hist !== undefined && object.hist !== null ? HistoricalInfo.fromPartial(object.hist) : undefined;
        return message;
    }
};
export const QueryPoolRequest = {
    $type: "cosmos.staking.v1beta1.QueryPoolRequest",
    encode(_, writer = new BinaryWriter()) {
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
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
        return QueryPoolRequest.fromPartial(base ?? {});
    },
    fromPartial(_) {
        const message = createBaseQueryPoolRequest();
        return message;
    }
};
export const QueryPoolResponse = {
    $type: "cosmos.staking.v1beta1.QueryPoolResponse",
    encode(message, writer = new BinaryWriter()) {
        if (message.pool !== undefined) {
            Pool.encode(message.pool, writer.uint32(10).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseQueryPoolResponse();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.pool = Pool.decode(reader, reader.uint32());
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
        return { pool: isSet(object.pool) ? Pool.fromJSON(object.pool) : undefined };
    },
    toJSON(message) {
        const obj = {};
        if (message.pool !== undefined) {
            obj.pool = Pool.toJSON(message.pool);
        }
        return obj;
    },
    create(base) {
        return QueryPoolResponse.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryPoolResponse();
        message.pool = object.pool !== undefined && object.pool !== null ? Pool.fromPartial(object.pool) : undefined;
        return message;
    }
};
export const QueryParamsRequest = {
    $type: "cosmos.staking.v1beta1.QueryParamsRequest",
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
    $type: "cosmos.staking.v1beta1.QueryParamsResponse",
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
export const registry = [
    ["/cosmos.staking.v1beta1.QueryValidatorsRequest", QueryValidatorsRequest],
    ["/cosmos.staking.v1beta1.QueryValidatorsResponse", QueryValidatorsResponse],
    ["/cosmos.staking.v1beta1.QueryValidatorRequest", QueryValidatorRequest],
    ["/cosmos.staking.v1beta1.QueryValidatorResponse", QueryValidatorResponse],
    ["/cosmos.staking.v1beta1.QueryValidatorDelegationsRequest", QueryValidatorDelegationsRequest],
    ["/cosmos.staking.v1beta1.QueryValidatorDelegationsResponse", QueryValidatorDelegationsResponse],
    ["/cosmos.staking.v1beta1.QueryValidatorUnbondingDelegationsRequest", QueryValidatorUnbondingDelegationsRequest],
    ["/cosmos.staking.v1beta1.QueryValidatorUnbondingDelegationsResponse", QueryValidatorUnbondingDelegationsResponse],
    ["/cosmos.staking.v1beta1.QueryDelegationRequest", QueryDelegationRequest],
    ["/cosmos.staking.v1beta1.QueryDelegationResponse", QueryDelegationResponse],
    ["/cosmos.staking.v1beta1.QueryUnbondingDelegationRequest", QueryUnbondingDelegationRequest],
    ["/cosmos.staking.v1beta1.QueryUnbondingDelegationResponse", QueryUnbondingDelegationResponse],
    ["/cosmos.staking.v1beta1.QueryDelegatorDelegationsRequest", QueryDelegatorDelegationsRequest],
    ["/cosmos.staking.v1beta1.QueryDelegatorDelegationsResponse", QueryDelegatorDelegationsResponse],
    ["/cosmos.staking.v1beta1.QueryDelegatorUnbondingDelegationsRequest", QueryDelegatorUnbondingDelegationsRequest],
    ["/cosmos.staking.v1beta1.QueryDelegatorUnbondingDelegationsResponse", QueryDelegatorUnbondingDelegationsResponse],
    ["/cosmos.staking.v1beta1.QueryRedelegationsRequest", QueryRedelegationsRequest],
    ["/cosmos.staking.v1beta1.QueryRedelegationsResponse", QueryRedelegationsResponse],
    ["/cosmos.staking.v1beta1.QueryDelegatorValidatorsRequest", QueryDelegatorValidatorsRequest],
    ["/cosmos.staking.v1beta1.QueryDelegatorValidatorsResponse", QueryDelegatorValidatorsResponse],
    ["/cosmos.staking.v1beta1.QueryDelegatorValidatorRequest", QueryDelegatorValidatorRequest],
    ["/cosmos.staking.v1beta1.QueryDelegatorValidatorResponse", QueryDelegatorValidatorResponse],
    ["/cosmos.staking.v1beta1.QueryHistoricalInfoRequest", QueryHistoricalInfoRequest],
    ["/cosmos.staking.v1beta1.QueryHistoricalInfoResponse", QueryHistoricalInfoResponse],
    ["/cosmos.staking.v1beta1.QueryPoolRequest", QueryPoolRequest],
    ["/cosmos.staking.v1beta1.QueryPoolResponse", QueryPoolResponse],
    ["/cosmos.staking.v1beta1.QueryParamsRequest", QueryParamsRequest],
    ["/cosmos.staking.v1beta1.QueryParamsResponse", QueryParamsResponse]
];
export const aminoConverters = {
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
