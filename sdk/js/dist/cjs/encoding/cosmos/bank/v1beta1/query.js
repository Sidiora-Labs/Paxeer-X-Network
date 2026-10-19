"use strict";
Object.defineProperty(exports, "__esModule", { value: true });
exports.aminoConverters = exports.registry = exports.QueryDenomMetadataResponse = exports.QueryDenomMetadataRequest = exports.QueryDenomsMetadataResponse = exports.QueryDenomsMetadataRequest = exports.QueryParamsResponse = exports.QueryParamsRequest = exports.QuerySupplyOfResponse = exports.QuerySupplyOfRequest = exports.QueryTotalSupplyResponse = exports.QueryTotalSupplyRequest = exports.QuerySpendableBalancesResponse = exports.QuerySpendableBalancesRequest = exports.QueryAllBalancesResponse = exports.QueryAllBalancesRequest = exports.QueryBalanceResponse = exports.QueryBalanceRequest = void 0;
const wire_1 = require("@bufbuild/protobuf/wire");
const pagination_1 = require("../../base/query/v1beta1/pagination");
const coin_1 = require("../../base/v1beta1/coin");
const bank_1 = require("./bank");
exports.QueryBalanceRequest = {
    $type: "cosmos.bank.v1beta1.QueryBalanceRequest",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.address !== "") {
            writer.uint32(10).string(message.address);
        }
        if (message.denom !== "") {
            writer.uint32(18).string(message.denom);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseQueryBalanceRequest();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.address = reader.string();
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.denom = reader.string();
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
            address: isSet(object.address) ? globalThis.String(object.address) : "",
            denom: isSet(object.denom) ? globalThis.String(object.denom) : ""
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.address !== "") {
            obj.address = message.address;
        }
        if (message.denom !== "") {
            obj.denom = message.denom;
        }
        return obj;
    },
    create(base) {
        return exports.QueryBalanceRequest.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryBalanceRequest();
        message.address = object.address ?? "";
        message.denom = object.denom ?? "";
        return message;
    }
};
exports.QueryBalanceResponse = {
    $type: "cosmos.bank.v1beta1.QueryBalanceResponse",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.balance !== undefined) {
            coin_1.Coin.encode(message.balance, writer.uint32(10).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseQueryBalanceResponse();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.balance = coin_1.Coin.decode(reader, reader.uint32());
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
        return { balance: isSet(object.balance) ? coin_1.Coin.fromJSON(object.balance) : undefined };
    },
    toJSON(message) {
        const obj = {};
        if (message.balance !== undefined) {
            obj.balance = coin_1.Coin.toJSON(message.balance);
        }
        return obj;
    },
    create(base) {
        return exports.QueryBalanceResponse.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryBalanceResponse();
        message.balance = object.balance !== undefined && object.balance !== null ? coin_1.Coin.fromPartial(object.balance) : undefined;
        return message;
    }
};
exports.QueryAllBalancesRequest = {
    $type: "cosmos.bank.v1beta1.QueryAllBalancesRequest",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.address !== "") {
            writer.uint32(10).string(message.address);
        }
        if (message.pagination !== undefined) {
            pagination_1.PageRequest.encode(message.pagination, writer.uint32(18).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseQueryAllBalancesRequest();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.address = reader.string();
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
            address: isSet(object.address) ? globalThis.String(object.address) : "",
            pagination: isSet(object.pagination) ? pagination_1.PageRequest.fromJSON(object.pagination) : undefined
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.address !== "") {
            obj.address = message.address;
        }
        if (message.pagination !== undefined) {
            obj.pagination = pagination_1.PageRequest.toJSON(message.pagination);
        }
        return obj;
    },
    create(base) {
        return exports.QueryAllBalancesRequest.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryAllBalancesRequest();
        message.address = object.address ?? "";
        message.pagination = object.pagination !== undefined && object.pagination !== null ? pagination_1.PageRequest.fromPartial(object.pagination) : undefined;
        return message;
    }
};
exports.QueryAllBalancesResponse = {
    $type: "cosmos.bank.v1beta1.QueryAllBalancesResponse",
    encode(message, writer = new wire_1.BinaryWriter()) {
        for (const v of message.balances) {
            coin_1.Coin.encode(v, writer.uint32(10).fork()).join();
        }
        if (message.pagination !== undefined) {
            pagination_1.PageResponse.encode(message.pagination, writer.uint32(18).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseQueryAllBalancesResponse();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.balances.push(coin_1.Coin.decode(reader, reader.uint32()));
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
            balances: globalThis.Array.isArray(object?.balances) ? object.balances.map((e) => coin_1.Coin.fromJSON(e)) : [],
            pagination: isSet(object.pagination) ? pagination_1.PageResponse.fromJSON(object.pagination) : undefined
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.balances?.length) {
            obj.balances = message.balances.map((e) => coin_1.Coin.toJSON(e));
        }
        if (message.pagination !== undefined) {
            obj.pagination = pagination_1.PageResponse.toJSON(message.pagination);
        }
        return obj;
    },
    create(base) {
        return exports.QueryAllBalancesResponse.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryAllBalancesResponse();
        message.balances = object.balances?.map((e) => coin_1.Coin.fromPartial(e)) || [];
        message.pagination = object.pagination !== undefined && object.pagination !== null ? pagination_1.PageResponse.fromPartial(object.pagination) : undefined;
        return message;
    }
};
exports.QuerySpendableBalancesRequest = {
    $type: "cosmos.bank.v1beta1.QuerySpendableBalancesRequest",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.address !== "") {
            writer.uint32(10).string(message.address);
        }
        if (message.pagination !== undefined) {
            pagination_1.PageRequest.encode(message.pagination, writer.uint32(18).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseQuerySpendableBalancesRequest();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.address = reader.string();
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
            address: isSet(object.address) ? globalThis.String(object.address) : "",
            pagination: isSet(object.pagination) ? pagination_1.PageRequest.fromJSON(object.pagination) : undefined
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.address !== "") {
            obj.address = message.address;
        }
        if (message.pagination !== undefined) {
            obj.pagination = pagination_1.PageRequest.toJSON(message.pagination);
        }
        return obj;
    },
    create(base) {
        return exports.QuerySpendableBalancesRequest.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQuerySpendableBalancesRequest();
        message.address = object.address ?? "";
        message.pagination = object.pagination !== undefined && object.pagination !== null ? pagination_1.PageRequest.fromPartial(object.pagination) : undefined;
        return message;
    }
};
exports.QuerySpendableBalancesResponse = {
    $type: "cosmos.bank.v1beta1.QuerySpendableBalancesResponse",
    encode(message, writer = new wire_1.BinaryWriter()) {
        for (const v of message.balances) {
            coin_1.Coin.encode(v, writer.uint32(10).fork()).join();
        }
        if (message.pagination !== undefined) {
            pagination_1.PageResponse.encode(message.pagination, writer.uint32(18).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseQuerySpendableBalancesResponse();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.balances.push(coin_1.Coin.decode(reader, reader.uint32()));
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
            balances: globalThis.Array.isArray(object?.balances) ? object.balances.map((e) => coin_1.Coin.fromJSON(e)) : [],
            pagination: isSet(object.pagination) ? pagination_1.PageResponse.fromJSON(object.pagination) : undefined
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.balances?.length) {
            obj.balances = message.balances.map((e) => coin_1.Coin.toJSON(e));
        }
        if (message.pagination !== undefined) {
            obj.pagination = pagination_1.PageResponse.toJSON(message.pagination);
        }
        return obj;
    },
    create(base) {
        return exports.QuerySpendableBalancesResponse.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQuerySpendableBalancesResponse();
        message.balances = object.balances?.map((e) => coin_1.Coin.fromPartial(e)) || [];
        message.pagination = object.pagination !== undefined && object.pagination !== null ? pagination_1.PageResponse.fromPartial(object.pagination) : undefined;
        return message;
    }
};
exports.QueryTotalSupplyRequest = {
    $type: "cosmos.bank.v1beta1.QueryTotalSupplyRequest",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.pagination !== undefined) {
            pagination_1.PageRequest.encode(message.pagination, writer.uint32(10).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseQueryTotalSupplyRequest();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
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
        return { pagination: isSet(object.pagination) ? pagination_1.PageRequest.fromJSON(object.pagination) : undefined };
    },
    toJSON(message) {
        const obj = {};
        if (message.pagination !== undefined) {
            obj.pagination = pagination_1.PageRequest.toJSON(message.pagination);
        }
        return obj;
    },
    create(base) {
        return exports.QueryTotalSupplyRequest.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryTotalSupplyRequest();
        message.pagination = object.pagination !== undefined && object.pagination !== null ? pagination_1.PageRequest.fromPartial(object.pagination) : undefined;
        return message;
    }
};
exports.QueryTotalSupplyResponse = {
    $type: "cosmos.bank.v1beta1.QueryTotalSupplyResponse",
    encode(message, writer = new wire_1.BinaryWriter()) {
        for (const v of message.supply) {
            coin_1.Coin.encode(v, writer.uint32(10).fork()).join();
        }
        if (message.pagination !== undefined) {
            pagination_1.PageResponse.encode(message.pagination, writer.uint32(18).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseQueryTotalSupplyResponse();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.supply.push(coin_1.Coin.decode(reader, reader.uint32()));
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
            supply: globalThis.Array.isArray(object?.supply) ? object.supply.map((e) => coin_1.Coin.fromJSON(e)) : [],
            pagination: isSet(object.pagination) ? pagination_1.PageResponse.fromJSON(object.pagination) : undefined
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.supply?.length) {
            obj.supply = message.supply.map((e) => coin_1.Coin.toJSON(e));
        }
        if (message.pagination !== undefined) {
            obj.pagination = pagination_1.PageResponse.toJSON(message.pagination);
        }
        return obj;
    },
    create(base) {
        return exports.QueryTotalSupplyResponse.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryTotalSupplyResponse();
        message.supply = object.supply?.map((e) => coin_1.Coin.fromPartial(e)) || [];
        message.pagination = object.pagination !== undefined && object.pagination !== null ? pagination_1.PageResponse.fromPartial(object.pagination) : undefined;
        return message;
    }
};
exports.QuerySupplyOfRequest = {
    $type: "cosmos.bank.v1beta1.QuerySupplyOfRequest",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.denom !== "") {
            writer.uint32(10).string(message.denom);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseQuerySupplyOfRequest();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.denom = reader.string();
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
        return { denom: isSet(object.denom) ? globalThis.String(object.denom) : "" };
    },
    toJSON(message) {
        const obj = {};
        if (message.denom !== "") {
            obj.denom = message.denom;
        }
        return obj;
    },
    create(base) {
        return exports.QuerySupplyOfRequest.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQuerySupplyOfRequest();
        message.denom = object.denom ?? "";
        return message;
    }
};
exports.QuerySupplyOfResponse = {
    $type: "cosmos.bank.v1beta1.QuerySupplyOfResponse",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.amount !== undefined) {
            coin_1.Coin.encode(message.amount, writer.uint32(10).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseQuerySupplyOfResponse();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.amount = coin_1.Coin.decode(reader, reader.uint32());
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
        return { amount: isSet(object.amount) ? coin_1.Coin.fromJSON(object.amount) : undefined };
    },
    toJSON(message) {
        const obj = {};
        if (message.amount !== undefined) {
            obj.amount = coin_1.Coin.toJSON(message.amount);
        }
        return obj;
    },
    create(base) {
        return exports.QuerySupplyOfResponse.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQuerySupplyOfResponse();
        message.amount = object.amount !== undefined && object.amount !== null ? coin_1.Coin.fromPartial(object.amount) : undefined;
        return message;
    }
};
exports.QueryParamsRequest = {
    $type: "cosmos.bank.v1beta1.QueryParamsRequest",
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
    $type: "cosmos.bank.v1beta1.QueryParamsResponse",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.params !== undefined) {
            bank_1.Params.encode(message.params, writer.uint32(10).fork()).join();
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
                    message.params = bank_1.Params.decode(reader, reader.uint32());
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
        return { params: isSet(object.params) ? bank_1.Params.fromJSON(object.params) : undefined };
    },
    toJSON(message) {
        const obj = {};
        if (message.params !== undefined) {
            obj.params = bank_1.Params.toJSON(message.params);
        }
        return obj;
    },
    create(base) {
        return exports.QueryParamsResponse.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryParamsResponse();
        message.params = object.params !== undefined && object.params !== null ? bank_1.Params.fromPartial(object.params) : undefined;
        return message;
    }
};
exports.QueryDenomsMetadataRequest = {
    $type: "cosmos.bank.v1beta1.QueryDenomsMetadataRequest",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.pagination !== undefined) {
            pagination_1.PageRequest.encode(message.pagination, writer.uint32(10).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseQueryDenomsMetadataRequest();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
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
        return { pagination: isSet(object.pagination) ? pagination_1.PageRequest.fromJSON(object.pagination) : undefined };
    },
    toJSON(message) {
        const obj = {};
        if (message.pagination !== undefined) {
            obj.pagination = pagination_1.PageRequest.toJSON(message.pagination);
        }
        return obj;
    },
    create(base) {
        return exports.QueryDenomsMetadataRequest.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryDenomsMetadataRequest();
        message.pagination = object.pagination !== undefined && object.pagination !== null ? pagination_1.PageRequest.fromPartial(object.pagination) : undefined;
        return message;
    }
};
exports.QueryDenomsMetadataResponse = {
    $type: "cosmos.bank.v1beta1.QueryDenomsMetadataResponse",
    encode(message, writer = new wire_1.BinaryWriter()) {
        for (const v of message.metadatas) {
            bank_1.Metadata.encode(v, writer.uint32(10).fork()).join();
        }
        if (message.pagination !== undefined) {
            pagination_1.PageResponse.encode(message.pagination, writer.uint32(18).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseQueryDenomsMetadataResponse();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.metadatas.push(bank_1.Metadata.decode(reader, reader.uint32()));
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
            metadatas: globalThis.Array.isArray(object?.metadatas) ? object.metadatas.map((e) => bank_1.Metadata.fromJSON(e)) : [],
            pagination: isSet(object.pagination) ? pagination_1.PageResponse.fromJSON(object.pagination) : undefined
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.metadatas?.length) {
            obj.metadatas = message.metadatas.map((e) => bank_1.Metadata.toJSON(e));
        }
        if (message.pagination !== undefined) {
            obj.pagination = pagination_1.PageResponse.toJSON(message.pagination);
        }
        return obj;
    },
    create(base) {
        return exports.QueryDenomsMetadataResponse.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryDenomsMetadataResponse();
        message.metadatas = object.metadatas?.map((e) => bank_1.Metadata.fromPartial(e)) || [];
        message.pagination = object.pagination !== undefined && object.pagination !== null ? pagination_1.PageResponse.fromPartial(object.pagination) : undefined;
        return message;
    }
};
exports.QueryDenomMetadataRequest = {
    $type: "cosmos.bank.v1beta1.QueryDenomMetadataRequest",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.denom !== "") {
            writer.uint32(10).string(message.denom);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseQueryDenomMetadataRequest();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.denom = reader.string();
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
        return { denom: isSet(object.denom) ? globalThis.String(object.denom) : "" };
    },
    toJSON(message) {
        const obj = {};
        if (message.denom !== "") {
            obj.denom = message.denom;
        }
        return obj;
    },
    create(base) {
        return exports.QueryDenomMetadataRequest.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryDenomMetadataRequest();
        message.denom = object.denom ?? "";
        return message;
    }
};
exports.QueryDenomMetadataResponse = {
    $type: "cosmos.bank.v1beta1.QueryDenomMetadataResponse",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.metadata !== undefined) {
            bank_1.Metadata.encode(message.metadata, writer.uint32(10).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseQueryDenomMetadataResponse();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.metadata = bank_1.Metadata.decode(reader, reader.uint32());
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
        return { metadata: isSet(object.metadata) ? bank_1.Metadata.fromJSON(object.metadata) : undefined };
    },
    toJSON(message) {
        const obj = {};
        if (message.metadata !== undefined) {
            obj.metadata = bank_1.Metadata.toJSON(message.metadata);
        }
        return obj;
    },
    create(base) {
        return exports.QueryDenomMetadataResponse.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryDenomMetadataResponse();
        message.metadata = object.metadata !== undefined && object.metadata !== null ? bank_1.Metadata.fromPartial(object.metadata) : undefined;
        return message;
    }
};
function createBaseQueryBalanceRequest() {
    return { address: "", denom: "" };
}
function createBaseQueryBalanceResponse() {
    return { balance: undefined };
}
function createBaseQueryAllBalancesRequest() {
    return { address: "", pagination: undefined };
}
function createBaseQueryAllBalancesResponse() {
    return { balances: [], pagination: undefined };
}
function createBaseQuerySpendableBalancesRequest() {
    return { address: "", pagination: undefined };
}
function createBaseQuerySpendableBalancesResponse() {
    return { balances: [], pagination: undefined };
}
function createBaseQueryTotalSupplyRequest() {
    return { pagination: undefined };
}
function createBaseQueryTotalSupplyResponse() {
    return { supply: [], pagination: undefined };
}
function createBaseQuerySupplyOfRequest() {
    return { denom: "" };
}
function createBaseQuerySupplyOfResponse() {
    return { amount: undefined };
}
function createBaseQueryParamsRequest() {
    return {};
}
function createBaseQueryParamsResponse() {
    return { params: undefined };
}
function createBaseQueryDenomsMetadataRequest() {
    return { pagination: undefined };
}
function createBaseQueryDenomsMetadataResponse() {
    return { metadatas: [], pagination: undefined };
}
function createBaseQueryDenomMetadataRequest() {
    return { denom: "" };
}
function createBaseQueryDenomMetadataResponse() {
    return { metadata: undefined };
}
function isSet(value) {
    return value !== null && value !== undefined;
}
exports.registry = [
    ["/cosmos.bank.v1beta1.QueryBalanceRequest", exports.QueryBalanceRequest],
    ["/cosmos.bank.v1beta1.QueryBalanceResponse", exports.QueryBalanceResponse],
    ["/cosmos.bank.v1beta1.QueryAllBalancesRequest", exports.QueryAllBalancesRequest],
    ["/cosmos.bank.v1beta1.QueryAllBalancesResponse", exports.QueryAllBalancesResponse],
    ["/cosmos.bank.v1beta1.QuerySpendableBalancesRequest", exports.QuerySpendableBalancesRequest],
    ["/cosmos.bank.v1beta1.QuerySpendableBalancesResponse", exports.QuerySpendableBalancesResponse],
    ["/cosmos.bank.v1beta1.QueryTotalSupplyRequest", exports.QueryTotalSupplyRequest],
    ["/cosmos.bank.v1beta1.QueryTotalSupplyResponse", exports.QueryTotalSupplyResponse],
    ["/cosmos.bank.v1beta1.QuerySupplyOfRequest", exports.QuerySupplyOfRequest],
    ["/cosmos.bank.v1beta1.QuerySupplyOfResponse", exports.QuerySupplyOfResponse],
    ["/cosmos.bank.v1beta1.QueryParamsRequest", exports.QueryParamsRequest],
    ["/cosmos.bank.v1beta1.QueryParamsResponse", exports.QueryParamsResponse],
    ["/cosmos.bank.v1beta1.QueryDenomsMetadataRequest", exports.QueryDenomsMetadataRequest],
    ["/cosmos.bank.v1beta1.QueryDenomsMetadataResponse", exports.QueryDenomsMetadataResponse],
    ["/cosmos.bank.v1beta1.QueryDenomMetadataRequest", exports.QueryDenomMetadataRequest],
    ["/cosmos.bank.v1beta1.QueryDenomMetadataResponse", exports.QueryDenomMetadataResponse]
];
exports.aminoConverters = {
    "/cosmos.bank.v1beta1.QueryBalanceRequest": {
        aminoType: "cosmos-sdk/QueryBalanceRequest",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.bank.v1beta1.QueryBalanceResponse": {
        aminoType: "cosmos-sdk/QueryBalanceResponse",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.bank.v1beta1.QueryAllBalancesRequest": {
        aminoType: "cosmos-sdk/QueryAllBalancesRequest",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.bank.v1beta1.QueryAllBalancesResponse": {
        aminoType: "cosmos-sdk/QueryAllBalancesResponse",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.bank.v1beta1.QuerySpendableBalancesRequest": {
        aminoType: "cosmos-sdk/QuerySpendableBalancesRequest",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.bank.v1beta1.QuerySpendableBalancesResponse": {
        aminoType: "cosmos-sdk/QuerySpendableBalancesResponse",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.bank.v1beta1.QueryTotalSupplyRequest": {
        aminoType: "cosmos-sdk/QueryTotalSupplyRequest",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.bank.v1beta1.QueryTotalSupplyResponse": {
        aminoType: "cosmos-sdk/QueryTotalSupplyResponse",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.bank.v1beta1.QuerySupplyOfRequest": {
        aminoType: "cosmos-sdk/QuerySupplyOfRequest",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.bank.v1beta1.QuerySupplyOfResponse": {
        aminoType: "cosmos-sdk/QuerySupplyOfResponse",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.bank.v1beta1.QueryParamsRequest": {
        aminoType: "cosmos-sdk/QueryParamsRequest",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.bank.v1beta1.QueryParamsResponse": {
        aminoType: "cosmos-sdk/QueryParamsResponse",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.bank.v1beta1.QueryDenomsMetadataRequest": {
        aminoType: "cosmos-sdk/QueryDenomsMetadataRequest",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.bank.v1beta1.QueryDenomsMetadataResponse": {
        aminoType: "cosmos-sdk/QueryDenomsMetadataResponse",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.bank.v1beta1.QueryDenomMetadataRequest": {
        aminoType: "cosmos-sdk/QueryDenomMetadataRequest",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.bank.v1beta1.QueryDenomMetadataResponse": {
        aminoType: "cosmos-sdk/QueryDenomMetadataResponse",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    }
};
