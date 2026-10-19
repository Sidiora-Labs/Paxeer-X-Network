import { BinaryReader, BinaryWriter } from "@bufbuild/protobuf/wire";
import { PageRequest, PageResponse } from "../../base/query/v1beta1/pagination";
import { Coin } from "../../base/v1beta1/coin";
import { Metadata, Params } from "./bank";
export const QueryBalanceRequest = {
    $type: "cosmos.bank.v1beta1.QueryBalanceRequest",
    encode(message, writer = new BinaryWriter()) {
        if (message.address !== "") {
            writer.uint32(10).string(message.address);
        }
        if (message.denom !== "") {
            writer.uint32(18).string(message.denom);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
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
        return QueryBalanceRequest.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryBalanceRequest();
        message.address = object.address ?? "";
        message.denom = object.denom ?? "";
        return message;
    }
};
export const QueryBalanceResponse = {
    $type: "cosmos.bank.v1beta1.QueryBalanceResponse",
    encode(message, writer = new BinaryWriter()) {
        if (message.balance !== undefined) {
            Coin.encode(message.balance, writer.uint32(10).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseQueryBalanceResponse();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.balance = Coin.decode(reader, reader.uint32());
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
        return { balance: isSet(object.balance) ? Coin.fromJSON(object.balance) : undefined };
    },
    toJSON(message) {
        const obj = {};
        if (message.balance !== undefined) {
            obj.balance = Coin.toJSON(message.balance);
        }
        return obj;
    },
    create(base) {
        return QueryBalanceResponse.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryBalanceResponse();
        message.balance = object.balance !== undefined && object.balance !== null ? Coin.fromPartial(object.balance) : undefined;
        return message;
    }
};
export const QueryAllBalancesRequest = {
    $type: "cosmos.bank.v1beta1.QueryAllBalancesRequest",
    encode(message, writer = new BinaryWriter()) {
        if (message.address !== "") {
            writer.uint32(10).string(message.address);
        }
        if (message.pagination !== undefined) {
            PageRequest.encode(message.pagination, writer.uint32(18).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
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
            address: isSet(object.address) ? globalThis.String(object.address) : "",
            pagination: isSet(object.pagination) ? PageRequest.fromJSON(object.pagination) : undefined
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.address !== "") {
            obj.address = message.address;
        }
        if (message.pagination !== undefined) {
            obj.pagination = PageRequest.toJSON(message.pagination);
        }
        return obj;
    },
    create(base) {
        return QueryAllBalancesRequest.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryAllBalancesRequest();
        message.address = object.address ?? "";
        message.pagination = object.pagination !== undefined && object.pagination !== null ? PageRequest.fromPartial(object.pagination) : undefined;
        return message;
    }
};
export const QueryAllBalancesResponse = {
    $type: "cosmos.bank.v1beta1.QueryAllBalancesResponse",
    encode(message, writer = new BinaryWriter()) {
        for (const v of message.balances) {
            Coin.encode(v, writer.uint32(10).fork()).join();
        }
        if (message.pagination !== undefined) {
            PageResponse.encode(message.pagination, writer.uint32(18).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseQueryAllBalancesResponse();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.balances.push(Coin.decode(reader, reader.uint32()));
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
            balances: globalThis.Array.isArray(object?.balances) ? object.balances.map((e) => Coin.fromJSON(e)) : [],
            pagination: isSet(object.pagination) ? PageResponse.fromJSON(object.pagination) : undefined
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.balances?.length) {
            obj.balances = message.balances.map((e) => Coin.toJSON(e));
        }
        if (message.pagination !== undefined) {
            obj.pagination = PageResponse.toJSON(message.pagination);
        }
        return obj;
    },
    create(base) {
        return QueryAllBalancesResponse.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryAllBalancesResponse();
        message.balances = object.balances?.map((e) => Coin.fromPartial(e)) || [];
        message.pagination = object.pagination !== undefined && object.pagination !== null ? PageResponse.fromPartial(object.pagination) : undefined;
        return message;
    }
};
export const QuerySpendableBalancesRequest = {
    $type: "cosmos.bank.v1beta1.QuerySpendableBalancesRequest",
    encode(message, writer = new BinaryWriter()) {
        if (message.address !== "") {
            writer.uint32(10).string(message.address);
        }
        if (message.pagination !== undefined) {
            PageRequest.encode(message.pagination, writer.uint32(18).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
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
            address: isSet(object.address) ? globalThis.String(object.address) : "",
            pagination: isSet(object.pagination) ? PageRequest.fromJSON(object.pagination) : undefined
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.address !== "") {
            obj.address = message.address;
        }
        if (message.pagination !== undefined) {
            obj.pagination = PageRequest.toJSON(message.pagination);
        }
        return obj;
    },
    create(base) {
        return QuerySpendableBalancesRequest.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQuerySpendableBalancesRequest();
        message.address = object.address ?? "";
        message.pagination = object.pagination !== undefined && object.pagination !== null ? PageRequest.fromPartial(object.pagination) : undefined;
        return message;
    }
};
export const QuerySpendableBalancesResponse = {
    $type: "cosmos.bank.v1beta1.QuerySpendableBalancesResponse",
    encode(message, writer = new BinaryWriter()) {
        for (const v of message.balances) {
            Coin.encode(v, writer.uint32(10).fork()).join();
        }
        if (message.pagination !== undefined) {
            PageResponse.encode(message.pagination, writer.uint32(18).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseQuerySpendableBalancesResponse();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.balances.push(Coin.decode(reader, reader.uint32()));
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
            balances: globalThis.Array.isArray(object?.balances) ? object.balances.map((e) => Coin.fromJSON(e)) : [],
            pagination: isSet(object.pagination) ? PageResponse.fromJSON(object.pagination) : undefined
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.balances?.length) {
            obj.balances = message.balances.map((e) => Coin.toJSON(e));
        }
        if (message.pagination !== undefined) {
            obj.pagination = PageResponse.toJSON(message.pagination);
        }
        return obj;
    },
    create(base) {
        return QuerySpendableBalancesResponse.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQuerySpendableBalancesResponse();
        message.balances = object.balances?.map((e) => Coin.fromPartial(e)) || [];
        message.pagination = object.pagination !== undefined && object.pagination !== null ? PageResponse.fromPartial(object.pagination) : undefined;
        return message;
    }
};
export const QueryTotalSupplyRequest = {
    $type: "cosmos.bank.v1beta1.QueryTotalSupplyRequest",
    encode(message, writer = new BinaryWriter()) {
        if (message.pagination !== undefined) {
            PageRequest.encode(message.pagination, writer.uint32(10).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseQueryTotalSupplyRequest();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
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
        return { pagination: isSet(object.pagination) ? PageRequest.fromJSON(object.pagination) : undefined };
    },
    toJSON(message) {
        const obj = {};
        if (message.pagination !== undefined) {
            obj.pagination = PageRequest.toJSON(message.pagination);
        }
        return obj;
    },
    create(base) {
        return QueryTotalSupplyRequest.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryTotalSupplyRequest();
        message.pagination = object.pagination !== undefined && object.pagination !== null ? PageRequest.fromPartial(object.pagination) : undefined;
        return message;
    }
};
export const QueryTotalSupplyResponse = {
    $type: "cosmos.bank.v1beta1.QueryTotalSupplyResponse",
    encode(message, writer = new BinaryWriter()) {
        for (const v of message.supply) {
            Coin.encode(v, writer.uint32(10).fork()).join();
        }
        if (message.pagination !== undefined) {
            PageResponse.encode(message.pagination, writer.uint32(18).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseQueryTotalSupplyResponse();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.supply.push(Coin.decode(reader, reader.uint32()));
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
            supply: globalThis.Array.isArray(object?.supply) ? object.supply.map((e) => Coin.fromJSON(e)) : [],
            pagination: isSet(object.pagination) ? PageResponse.fromJSON(object.pagination) : undefined
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.supply?.length) {
            obj.supply = message.supply.map((e) => Coin.toJSON(e));
        }
        if (message.pagination !== undefined) {
            obj.pagination = PageResponse.toJSON(message.pagination);
        }
        return obj;
    },
    create(base) {
        return QueryTotalSupplyResponse.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryTotalSupplyResponse();
        message.supply = object.supply?.map((e) => Coin.fromPartial(e)) || [];
        message.pagination = object.pagination !== undefined && object.pagination !== null ? PageResponse.fromPartial(object.pagination) : undefined;
        return message;
    }
};
export const QuerySupplyOfRequest = {
    $type: "cosmos.bank.v1beta1.QuerySupplyOfRequest",
    encode(message, writer = new BinaryWriter()) {
        if (message.denom !== "") {
            writer.uint32(10).string(message.denom);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
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
        return QuerySupplyOfRequest.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQuerySupplyOfRequest();
        message.denom = object.denom ?? "";
        return message;
    }
};
export const QuerySupplyOfResponse = {
    $type: "cosmos.bank.v1beta1.QuerySupplyOfResponse",
    encode(message, writer = new BinaryWriter()) {
        if (message.amount !== undefined) {
            Coin.encode(message.amount, writer.uint32(10).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseQuerySupplyOfResponse();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.amount = Coin.decode(reader, reader.uint32());
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
        return { amount: isSet(object.amount) ? Coin.fromJSON(object.amount) : undefined };
    },
    toJSON(message) {
        const obj = {};
        if (message.amount !== undefined) {
            obj.amount = Coin.toJSON(message.amount);
        }
        return obj;
    },
    create(base) {
        return QuerySupplyOfResponse.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQuerySupplyOfResponse();
        message.amount = object.amount !== undefined && object.amount !== null ? Coin.fromPartial(object.amount) : undefined;
        return message;
    }
};
export const QueryParamsRequest = {
    $type: "cosmos.bank.v1beta1.QueryParamsRequest",
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
    $type: "cosmos.bank.v1beta1.QueryParamsResponse",
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
export const QueryDenomsMetadataRequest = {
    $type: "cosmos.bank.v1beta1.QueryDenomsMetadataRequest",
    encode(message, writer = new BinaryWriter()) {
        if (message.pagination !== undefined) {
            PageRequest.encode(message.pagination, writer.uint32(10).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseQueryDenomsMetadataRequest();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
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
        return { pagination: isSet(object.pagination) ? PageRequest.fromJSON(object.pagination) : undefined };
    },
    toJSON(message) {
        const obj = {};
        if (message.pagination !== undefined) {
            obj.pagination = PageRequest.toJSON(message.pagination);
        }
        return obj;
    },
    create(base) {
        return QueryDenomsMetadataRequest.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryDenomsMetadataRequest();
        message.pagination = object.pagination !== undefined && object.pagination !== null ? PageRequest.fromPartial(object.pagination) : undefined;
        return message;
    }
};
export const QueryDenomsMetadataResponse = {
    $type: "cosmos.bank.v1beta1.QueryDenomsMetadataResponse",
    encode(message, writer = new BinaryWriter()) {
        for (const v of message.metadatas) {
            Metadata.encode(v, writer.uint32(10).fork()).join();
        }
        if (message.pagination !== undefined) {
            PageResponse.encode(message.pagination, writer.uint32(18).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseQueryDenomsMetadataResponse();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.metadatas.push(Metadata.decode(reader, reader.uint32()));
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
            metadatas: globalThis.Array.isArray(object?.metadatas) ? object.metadatas.map((e) => Metadata.fromJSON(e)) : [],
            pagination: isSet(object.pagination) ? PageResponse.fromJSON(object.pagination) : undefined
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.metadatas?.length) {
            obj.metadatas = message.metadatas.map((e) => Metadata.toJSON(e));
        }
        if (message.pagination !== undefined) {
            obj.pagination = PageResponse.toJSON(message.pagination);
        }
        return obj;
    },
    create(base) {
        return QueryDenomsMetadataResponse.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryDenomsMetadataResponse();
        message.metadatas = object.metadatas?.map((e) => Metadata.fromPartial(e)) || [];
        message.pagination = object.pagination !== undefined && object.pagination !== null ? PageResponse.fromPartial(object.pagination) : undefined;
        return message;
    }
};
export const QueryDenomMetadataRequest = {
    $type: "cosmos.bank.v1beta1.QueryDenomMetadataRequest",
    encode(message, writer = new BinaryWriter()) {
        if (message.denom !== "") {
            writer.uint32(10).string(message.denom);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
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
        return QueryDenomMetadataRequest.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryDenomMetadataRequest();
        message.denom = object.denom ?? "";
        return message;
    }
};
export const QueryDenomMetadataResponse = {
    $type: "cosmos.bank.v1beta1.QueryDenomMetadataResponse",
    encode(message, writer = new BinaryWriter()) {
        if (message.metadata !== undefined) {
            Metadata.encode(message.metadata, writer.uint32(10).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseQueryDenomMetadataResponse();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.metadata = Metadata.decode(reader, reader.uint32());
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
        return { metadata: isSet(object.metadata) ? Metadata.fromJSON(object.metadata) : undefined };
    },
    toJSON(message) {
        const obj = {};
        if (message.metadata !== undefined) {
            obj.metadata = Metadata.toJSON(message.metadata);
        }
        return obj;
    },
    create(base) {
        return QueryDenomMetadataResponse.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryDenomMetadataResponse();
        message.metadata = object.metadata !== undefined && object.metadata !== null ? Metadata.fromPartial(object.metadata) : undefined;
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
export const registry = [
    ["/cosmos.bank.v1beta1.QueryBalanceRequest", QueryBalanceRequest],
    ["/cosmos.bank.v1beta1.QueryBalanceResponse", QueryBalanceResponse],
    ["/cosmos.bank.v1beta1.QueryAllBalancesRequest", QueryAllBalancesRequest],
    ["/cosmos.bank.v1beta1.QueryAllBalancesResponse", QueryAllBalancesResponse],
    ["/cosmos.bank.v1beta1.QuerySpendableBalancesRequest", QuerySpendableBalancesRequest],
    ["/cosmos.bank.v1beta1.QuerySpendableBalancesResponse", QuerySpendableBalancesResponse],
    ["/cosmos.bank.v1beta1.QueryTotalSupplyRequest", QueryTotalSupplyRequest],
    ["/cosmos.bank.v1beta1.QueryTotalSupplyResponse", QueryTotalSupplyResponse],
    ["/cosmos.bank.v1beta1.QuerySupplyOfRequest", QuerySupplyOfRequest],
    ["/cosmos.bank.v1beta1.QuerySupplyOfResponse", QuerySupplyOfResponse],
    ["/cosmos.bank.v1beta1.QueryParamsRequest", QueryParamsRequest],
    ["/cosmos.bank.v1beta1.QueryParamsResponse", QueryParamsResponse],
    ["/cosmos.bank.v1beta1.QueryDenomsMetadataRequest", QueryDenomsMetadataRequest],
    ["/cosmos.bank.v1beta1.QueryDenomsMetadataResponse", QueryDenomsMetadataResponse],
    ["/cosmos.bank.v1beta1.QueryDenomMetadataRequest", QueryDenomMetadataRequest],
    ["/cosmos.bank.v1beta1.QueryDenomMetadataResponse", QueryDenomMetadataResponse]
];
export const aminoConverters = {
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
