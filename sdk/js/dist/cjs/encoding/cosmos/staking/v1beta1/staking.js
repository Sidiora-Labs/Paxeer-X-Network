"use strict";
Object.defineProperty(exports, "__esModule", { value: true });
exports.aminoConverters = exports.registry = exports.Pool = exports.RedelegationResponse = exports.RedelegationEntryResponse = exports.DelegationResponse = exports.Params = exports.Redelegation = exports.RedelegationEntry = exports.UnbondingDelegationEntry = exports.UnbondingDelegation = exports.Delegation = exports.DVVTriplets = exports.DVVTriplet = exports.DVPairs = exports.DVPair = exports.ValAddresses = exports.Validator = exports.Description = exports.Commission = exports.CommissionRates = exports.HistoricalInfo = void 0;
exports.bondStatusFromJSON = bondStatusFromJSON;
exports.bondStatusToJSON = bondStatusToJSON;
const wire_1 = require("@bufbuild/protobuf/wire");
const any_1 = require("../../../google/protobuf/any");
const duration_1 = require("../../../google/protobuf/duration");
const timestamp_1 = require("../../../google/protobuf/timestamp");
const types_1 = require("../../../tendermint/types/types");
const coin_1 = require("../../base/v1beta1/coin");
const v1beta1_1 = require("../../../../types/cosmos/staking/v1beta1");
exports.HistoricalInfo = {
    $type: "cosmos.staking.v1beta1.HistoricalInfo",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.header !== undefined) {
            types_1.Header.encode(message.header, writer.uint32(10).fork()).join();
        }
        for (const v of message.valset) {
            exports.Validator.encode(v, writer.uint32(18).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseHistoricalInfo();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.header = types_1.Header.decode(reader, reader.uint32());
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.valset.push(exports.Validator.decode(reader, reader.uint32()));
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
            header: isSet(object.header) ? types_1.Header.fromJSON(object.header) : undefined,
            valset: globalThis.Array.isArray(object?.valset) ? object.valset.map((e) => exports.Validator.fromJSON(e)) : []
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.header !== undefined) {
            obj.header = types_1.Header.toJSON(message.header);
        }
        if (message.valset?.length) {
            obj.valset = message.valset.map((e) => exports.Validator.toJSON(e));
        }
        return obj;
    },
    create(base) {
        return exports.HistoricalInfo.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseHistoricalInfo();
        message.header = object.header !== undefined && object.header !== null ? types_1.Header.fromPartial(object.header) : undefined;
        message.valset = object.valset?.map((e) => exports.Validator.fromPartial(e)) || [];
        return message;
    }
};
exports.CommissionRates = {
    $type: "cosmos.staking.v1beta1.CommissionRates",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.rate !== "") {
            writer.uint32(10).string(message.rate);
        }
        if (message.max_rate !== "") {
            writer.uint32(18).string(message.max_rate);
        }
        if (message.max_change_rate !== "") {
            writer.uint32(26).string(message.max_change_rate);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseCommissionRates();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.rate = reader.string();
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.max_rate = reader.string();
                    continue;
                case 3:
                    if (tag !== 26) {
                        break;
                    }
                    message.max_change_rate = reader.string();
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
            rate: isSet(object.rate) ? globalThis.String(object.rate) : "",
            max_rate: isSet(object.max_rate) ? globalThis.String(object.max_rate) : "",
            max_change_rate: isSet(object.max_change_rate) ? globalThis.String(object.max_change_rate) : ""
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.rate !== "") {
            obj.rate = message.rate;
        }
        if (message.max_rate !== "") {
            obj.max_rate = message.max_rate;
        }
        if (message.max_change_rate !== "") {
            obj.max_change_rate = message.max_change_rate;
        }
        return obj;
    },
    create(base) {
        return exports.CommissionRates.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseCommissionRates();
        message.rate = object.rate ?? "";
        message.max_rate = object.max_rate ?? "";
        message.max_change_rate = object.max_change_rate ?? "";
        return message;
    }
};
exports.Commission = {
    $type: "cosmos.staking.v1beta1.Commission",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.commission_rates !== undefined) {
            exports.CommissionRates.encode(message.commission_rates, writer.uint32(10).fork()).join();
        }
        if (message.update_time !== undefined) {
            timestamp_1.Timestamp.encode(toTimestamp(message.update_time), writer.uint32(18).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseCommission();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.commission_rates = exports.CommissionRates.decode(reader, reader.uint32());
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.update_time = fromTimestamp(timestamp_1.Timestamp.decode(reader, reader.uint32()));
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
            commission_rates: isSet(object.commission_rates) ? exports.CommissionRates.fromJSON(object.commission_rates) : undefined,
            update_time: isSet(object.update_time) ? fromJsonTimestamp(object.update_time) : undefined
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.commission_rates !== undefined) {
            obj.commission_rates = exports.CommissionRates.toJSON(message.commission_rates);
        }
        if (message.update_time !== undefined) {
            obj.update_time = message.update_time.toISOString();
        }
        return obj;
    },
    create(base) {
        return exports.Commission.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseCommission();
        message.commission_rates =
            object.commission_rates !== undefined && object.commission_rates !== null ? exports.CommissionRates.fromPartial(object.commission_rates) : undefined;
        message.update_time = object.update_time ?? undefined;
        return message;
    }
};
exports.Description = {
    $type: "cosmos.staking.v1beta1.Description",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.moniker !== "") {
            writer.uint32(10).string(message.moniker);
        }
        if (message.identity !== "") {
            writer.uint32(18).string(message.identity);
        }
        if (message.website !== "") {
            writer.uint32(26).string(message.website);
        }
        if (message.security_contact !== "") {
            writer.uint32(34).string(message.security_contact);
        }
        if (message.details !== "") {
            writer.uint32(42).string(message.details);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseDescription();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.moniker = reader.string();
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.identity = reader.string();
                    continue;
                case 3:
                    if (tag !== 26) {
                        break;
                    }
                    message.website = reader.string();
                    continue;
                case 4:
                    if (tag !== 34) {
                        break;
                    }
                    message.security_contact = reader.string();
                    continue;
                case 5:
                    if (tag !== 42) {
                        break;
                    }
                    message.details = reader.string();
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
            moniker: isSet(object.moniker) ? globalThis.String(object.moniker) : "",
            identity: isSet(object.identity) ? globalThis.String(object.identity) : "",
            website: isSet(object.website) ? globalThis.String(object.website) : "",
            security_contact: isSet(object.security_contact) ? globalThis.String(object.security_contact) : "",
            details: isSet(object.details) ? globalThis.String(object.details) : ""
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.moniker !== "") {
            obj.moniker = message.moniker;
        }
        if (message.identity !== "") {
            obj.identity = message.identity;
        }
        if (message.website !== "") {
            obj.website = message.website;
        }
        if (message.security_contact !== "") {
            obj.security_contact = message.security_contact;
        }
        if (message.details !== "") {
            obj.details = message.details;
        }
        return obj;
    },
    create(base) {
        return exports.Description.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseDescription();
        message.moniker = object.moniker ?? "";
        message.identity = object.identity ?? "";
        message.website = object.website ?? "";
        message.security_contact = object.security_contact ?? "";
        message.details = object.details ?? "";
        return message;
    }
};
exports.Validator = {
    $type: "cosmos.staking.v1beta1.Validator",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.operator_address !== "") {
            writer.uint32(10).string(message.operator_address);
        }
        if (message.consensus_pubkey !== undefined) {
            any_1.Any.encode(message.consensus_pubkey, writer.uint32(18).fork()).join();
        }
        if (message.jailed !== false) {
            writer.uint32(24).bool(message.jailed);
        }
        if (message.status !== 0) {
            writer.uint32(32).int32(message.status);
        }
        if (message.tokens !== "") {
            writer.uint32(42).string(message.tokens);
        }
        if (message.delegator_shares !== "") {
            writer.uint32(50).string(message.delegator_shares);
        }
        if (message.description !== undefined) {
            exports.Description.encode(message.description, writer.uint32(58).fork()).join();
        }
        if (message.unbonding_height !== 0) {
            writer.uint32(64).int64(message.unbonding_height);
        }
        if (message.unbonding_time !== undefined) {
            timestamp_1.Timestamp.encode(toTimestamp(message.unbonding_time), writer.uint32(74).fork()).join();
        }
        if (message.commission !== undefined) {
            exports.Commission.encode(message.commission, writer.uint32(82).fork()).join();
        }
        if (message.min_self_delegation !== "") {
            writer.uint32(90).string(message.min_self_delegation);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseValidator();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.operator_address = reader.string();
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.consensus_pubkey = any_1.Any.decode(reader, reader.uint32());
                    continue;
                case 3:
                    if (tag !== 24) {
                        break;
                    }
                    message.jailed = reader.bool();
                    continue;
                case 4:
                    if (tag !== 32) {
                        break;
                    }
                    message.status = reader.int32();
                    continue;
                case 5:
                    if (tag !== 42) {
                        break;
                    }
                    message.tokens = reader.string();
                    continue;
                case 6:
                    if (tag !== 50) {
                        break;
                    }
                    message.delegator_shares = reader.string();
                    continue;
                case 7:
                    if (tag !== 58) {
                        break;
                    }
                    message.description = exports.Description.decode(reader, reader.uint32());
                    continue;
                case 8:
                    if (tag !== 64) {
                        break;
                    }
                    message.unbonding_height = longToNumber(reader.int64());
                    continue;
                case 9:
                    if (tag !== 74) {
                        break;
                    }
                    message.unbonding_time = fromTimestamp(timestamp_1.Timestamp.decode(reader, reader.uint32()));
                    continue;
                case 10:
                    if (tag !== 82) {
                        break;
                    }
                    message.commission = exports.Commission.decode(reader, reader.uint32());
                    continue;
                case 11:
                    if (tag !== 90) {
                        break;
                    }
                    message.min_self_delegation = reader.string();
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
            operator_address: isSet(object.operator_address) ? globalThis.String(object.operator_address) : "",
            consensus_pubkey: isSet(object.consensus_pubkey) ? any_1.Any.fromJSON(object.consensus_pubkey) : undefined,
            jailed: isSet(object.jailed) ? globalThis.Boolean(object.jailed) : false,
            status: isSet(object.status) ? bondStatusFromJSON(object.status) : 0,
            tokens: isSet(object.tokens) ? globalThis.String(object.tokens) : "",
            delegator_shares: isSet(object.delegator_shares) ? globalThis.String(object.delegator_shares) : "",
            description: isSet(object.description) ? exports.Description.fromJSON(object.description) : undefined,
            unbonding_height: isSet(object.unbonding_height) ? globalThis.Number(object.unbonding_height) : 0,
            unbonding_time: isSet(object.unbonding_time) ? fromJsonTimestamp(object.unbonding_time) : undefined,
            commission: isSet(object.commission) ? exports.Commission.fromJSON(object.commission) : undefined,
            min_self_delegation: isSet(object.min_self_delegation) ? globalThis.String(object.min_self_delegation) : ""
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.operator_address !== "") {
            obj.operator_address = message.operator_address;
        }
        if (message.consensus_pubkey !== undefined) {
            obj.consensus_pubkey = any_1.Any.toJSON(message.consensus_pubkey);
        }
        if (message.jailed !== false) {
            obj.jailed = message.jailed;
        }
        if (message.status !== 0) {
            obj.status = bondStatusToJSON(message.status);
        }
        if (message.tokens !== "") {
            obj.tokens = message.tokens;
        }
        if (message.delegator_shares !== "") {
            obj.delegator_shares = message.delegator_shares;
        }
        if (message.description !== undefined) {
            obj.description = exports.Description.toJSON(message.description);
        }
        if (message.unbonding_height !== 0) {
            obj.unbonding_height = Math.round(message.unbonding_height);
        }
        if (message.unbonding_time !== undefined) {
            obj.unbonding_time = message.unbonding_time.toISOString();
        }
        if (message.commission !== undefined) {
            obj.commission = exports.Commission.toJSON(message.commission);
        }
        if (message.min_self_delegation !== "") {
            obj.min_self_delegation = message.min_self_delegation;
        }
        return obj;
    },
    create(base) {
        return exports.Validator.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseValidator();
        message.operator_address = object.operator_address ?? "";
        message.consensus_pubkey = object.consensus_pubkey !== undefined && object.consensus_pubkey !== null ? any_1.Any.fromPartial(object.consensus_pubkey) : undefined;
        message.jailed = object.jailed ?? false;
        message.status = object.status ?? 0;
        message.tokens = object.tokens ?? "";
        message.delegator_shares = object.delegator_shares ?? "";
        message.description = object.description !== undefined && object.description !== null ? exports.Description.fromPartial(object.description) : undefined;
        message.unbonding_height = object.unbonding_height ?? 0;
        message.unbonding_time = object.unbonding_time ?? undefined;
        message.commission = object.commission !== undefined && object.commission !== null ? exports.Commission.fromPartial(object.commission) : undefined;
        message.min_self_delegation = object.min_self_delegation ?? "";
        return message;
    }
};
exports.ValAddresses = {
    $type: "cosmos.staking.v1beta1.ValAddresses",
    encode(message, writer = new wire_1.BinaryWriter()) {
        for (const v of message.addresses) {
            writer.uint32(10).string(v);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseValAddresses();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.addresses.push(reader.string());
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
            addresses: globalThis.Array.isArray(object?.addresses) ? object.addresses.map((e) => globalThis.String(e)) : []
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.addresses?.length) {
            obj.addresses = message.addresses;
        }
        return obj;
    },
    create(base) {
        return exports.ValAddresses.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseValAddresses();
        message.addresses = object.addresses?.map((e) => e) || [];
        return message;
    }
};
exports.DVPair = {
    $type: "cosmos.staking.v1beta1.DVPair",
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
        const message = createBaseDVPair();
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
        return exports.DVPair.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseDVPair();
        message.delegator_address = object.delegator_address ?? "";
        message.validator_address = object.validator_address ?? "";
        return message;
    }
};
exports.DVPairs = {
    $type: "cosmos.staking.v1beta1.DVPairs",
    encode(message, writer = new wire_1.BinaryWriter()) {
        for (const v of message.pairs) {
            exports.DVPair.encode(v, writer.uint32(10).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseDVPairs();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.pairs.push(exports.DVPair.decode(reader, reader.uint32()));
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
        return { pairs: globalThis.Array.isArray(object?.pairs) ? object.pairs.map((e) => exports.DVPair.fromJSON(e)) : [] };
    },
    toJSON(message) {
        const obj = {};
        if (message.pairs?.length) {
            obj.pairs = message.pairs.map((e) => exports.DVPair.toJSON(e));
        }
        return obj;
    },
    create(base) {
        return exports.DVPairs.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseDVPairs();
        message.pairs = object.pairs?.map((e) => exports.DVPair.fromPartial(e)) || [];
        return message;
    }
};
exports.DVVTriplet = {
    $type: "cosmos.staking.v1beta1.DVVTriplet",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.delegator_address !== "") {
            writer.uint32(10).string(message.delegator_address);
        }
        if (message.validator_src_address !== "") {
            writer.uint32(18).string(message.validator_src_address);
        }
        if (message.validator_dst_address !== "") {
            writer.uint32(26).string(message.validator_dst_address);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseDVVTriplet();
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
                    message.validator_src_address = reader.string();
                    continue;
                case 3:
                    if (tag !== 26) {
                        break;
                    }
                    message.validator_dst_address = reader.string();
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
            validator_src_address: isSet(object.validator_src_address) ? globalThis.String(object.validator_src_address) : "",
            validator_dst_address: isSet(object.validator_dst_address) ? globalThis.String(object.validator_dst_address) : ""
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.delegator_address !== "") {
            obj.delegator_address = message.delegator_address;
        }
        if (message.validator_src_address !== "") {
            obj.validator_src_address = message.validator_src_address;
        }
        if (message.validator_dst_address !== "") {
            obj.validator_dst_address = message.validator_dst_address;
        }
        return obj;
    },
    create(base) {
        return exports.DVVTriplet.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseDVVTriplet();
        message.delegator_address = object.delegator_address ?? "";
        message.validator_src_address = object.validator_src_address ?? "";
        message.validator_dst_address = object.validator_dst_address ?? "";
        return message;
    }
};
exports.DVVTriplets = {
    $type: "cosmos.staking.v1beta1.DVVTriplets",
    encode(message, writer = new wire_1.BinaryWriter()) {
        for (const v of message.triplets) {
            exports.DVVTriplet.encode(v, writer.uint32(10).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseDVVTriplets();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.triplets.push(exports.DVVTriplet.decode(reader, reader.uint32()));
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
            triplets: globalThis.Array.isArray(object?.triplets) ? object.triplets.map((e) => exports.DVVTriplet.fromJSON(e)) : []
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.triplets?.length) {
            obj.triplets = message.triplets.map((e) => exports.DVVTriplet.toJSON(e));
        }
        return obj;
    },
    create(base) {
        return exports.DVVTriplets.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseDVVTriplets();
        message.triplets = object.triplets?.map((e) => exports.DVVTriplet.fromPartial(e)) || [];
        return message;
    }
};
exports.Delegation = {
    $type: "cosmos.staking.v1beta1.Delegation",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.delegator_address !== "") {
            writer.uint32(10).string(message.delegator_address);
        }
        if (message.validator_address !== "") {
            writer.uint32(18).string(message.validator_address);
        }
        if (message.shares !== "") {
            writer.uint32(26).string(message.shares);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseDelegation();
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
                case 3:
                    if (tag !== 26) {
                        break;
                    }
                    message.shares = reader.string();
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
            validator_address: isSet(object.validator_address) ? globalThis.String(object.validator_address) : "",
            shares: isSet(object.shares) ? globalThis.String(object.shares) : ""
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
        if (message.shares !== "") {
            obj.shares = message.shares;
        }
        return obj;
    },
    create(base) {
        return exports.Delegation.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseDelegation();
        message.delegator_address = object.delegator_address ?? "";
        message.validator_address = object.validator_address ?? "";
        message.shares = object.shares ?? "";
        return message;
    }
};
exports.UnbondingDelegation = {
    $type: "cosmos.staking.v1beta1.UnbondingDelegation",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.delegator_address !== "") {
            writer.uint32(10).string(message.delegator_address);
        }
        if (message.validator_address !== "") {
            writer.uint32(18).string(message.validator_address);
        }
        for (const v of message.entries) {
            exports.UnbondingDelegationEntry.encode(v, writer.uint32(26).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseUnbondingDelegation();
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
                case 3:
                    if (tag !== 26) {
                        break;
                    }
                    message.entries.push(exports.UnbondingDelegationEntry.decode(reader, reader.uint32()));
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
            validator_address: isSet(object.validator_address) ? globalThis.String(object.validator_address) : "",
            entries: globalThis.Array.isArray(object?.entries) ? object.entries.map((e) => exports.UnbondingDelegationEntry.fromJSON(e)) : []
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
        if (message.entries?.length) {
            obj.entries = message.entries.map((e) => exports.UnbondingDelegationEntry.toJSON(e));
        }
        return obj;
    },
    create(base) {
        return exports.UnbondingDelegation.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseUnbondingDelegation();
        message.delegator_address = object.delegator_address ?? "";
        message.validator_address = object.validator_address ?? "";
        message.entries = object.entries?.map((e) => exports.UnbondingDelegationEntry.fromPartial(e)) || [];
        return message;
    }
};
exports.UnbondingDelegationEntry = {
    $type: "cosmos.staking.v1beta1.UnbondingDelegationEntry",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.creation_height !== 0) {
            writer.uint32(8).int64(message.creation_height);
        }
        if (message.completion_time !== undefined) {
            timestamp_1.Timestamp.encode(toTimestamp(message.completion_time), writer.uint32(18).fork()).join();
        }
        if (message.initial_balance !== "") {
            writer.uint32(26).string(message.initial_balance);
        }
        if (message.balance !== "") {
            writer.uint32(34).string(message.balance);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseUnbondingDelegationEntry();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 8) {
                        break;
                    }
                    message.creation_height = longToNumber(reader.int64());
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.completion_time = fromTimestamp(timestamp_1.Timestamp.decode(reader, reader.uint32()));
                    continue;
                case 3:
                    if (tag !== 26) {
                        break;
                    }
                    message.initial_balance = reader.string();
                    continue;
                case 4:
                    if (tag !== 34) {
                        break;
                    }
                    message.balance = reader.string();
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
            creation_height: isSet(object.creation_height) ? globalThis.Number(object.creation_height) : 0,
            completion_time: isSet(object.completion_time) ? fromJsonTimestamp(object.completion_time) : undefined,
            initial_balance: isSet(object.initial_balance) ? globalThis.String(object.initial_balance) : "",
            balance: isSet(object.balance) ? globalThis.String(object.balance) : ""
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.creation_height !== 0) {
            obj.creation_height = Math.round(message.creation_height);
        }
        if (message.completion_time !== undefined) {
            obj.completion_time = message.completion_time.toISOString();
        }
        if (message.initial_balance !== "") {
            obj.initial_balance = message.initial_balance;
        }
        if (message.balance !== "") {
            obj.balance = message.balance;
        }
        return obj;
    },
    create(base) {
        return exports.UnbondingDelegationEntry.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseUnbondingDelegationEntry();
        message.creation_height = object.creation_height ?? 0;
        message.completion_time = object.completion_time ?? undefined;
        message.initial_balance = object.initial_balance ?? "";
        message.balance = object.balance ?? "";
        return message;
    }
};
exports.RedelegationEntry = {
    $type: "cosmos.staking.v1beta1.RedelegationEntry",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.creation_height !== 0) {
            writer.uint32(8).int64(message.creation_height);
        }
        if (message.completion_time !== undefined) {
            timestamp_1.Timestamp.encode(toTimestamp(message.completion_time), writer.uint32(18).fork()).join();
        }
        if (message.initial_balance !== "") {
            writer.uint32(26).string(message.initial_balance);
        }
        if (message.shares_dst !== "") {
            writer.uint32(34).string(message.shares_dst);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseRedelegationEntry();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 8) {
                        break;
                    }
                    message.creation_height = longToNumber(reader.int64());
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.completion_time = fromTimestamp(timestamp_1.Timestamp.decode(reader, reader.uint32()));
                    continue;
                case 3:
                    if (tag !== 26) {
                        break;
                    }
                    message.initial_balance = reader.string();
                    continue;
                case 4:
                    if (tag !== 34) {
                        break;
                    }
                    message.shares_dst = reader.string();
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
            creation_height: isSet(object.creation_height) ? globalThis.Number(object.creation_height) : 0,
            completion_time: isSet(object.completion_time) ? fromJsonTimestamp(object.completion_time) : undefined,
            initial_balance: isSet(object.initial_balance) ? globalThis.String(object.initial_balance) : "",
            shares_dst: isSet(object.shares_dst) ? globalThis.String(object.shares_dst) : ""
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.creation_height !== 0) {
            obj.creation_height = Math.round(message.creation_height);
        }
        if (message.completion_time !== undefined) {
            obj.completion_time = message.completion_time.toISOString();
        }
        if (message.initial_balance !== "") {
            obj.initial_balance = message.initial_balance;
        }
        if (message.shares_dst !== "") {
            obj.shares_dst = message.shares_dst;
        }
        return obj;
    },
    create(base) {
        return exports.RedelegationEntry.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseRedelegationEntry();
        message.creation_height = object.creation_height ?? 0;
        message.completion_time = object.completion_time ?? undefined;
        message.initial_balance = object.initial_balance ?? "";
        message.shares_dst = object.shares_dst ?? "";
        return message;
    }
};
exports.Redelegation = {
    $type: "cosmos.staking.v1beta1.Redelegation",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.delegator_address !== "") {
            writer.uint32(10).string(message.delegator_address);
        }
        if (message.validator_src_address !== "") {
            writer.uint32(18).string(message.validator_src_address);
        }
        if (message.validator_dst_address !== "") {
            writer.uint32(26).string(message.validator_dst_address);
        }
        for (const v of message.entries) {
            exports.RedelegationEntry.encode(v, writer.uint32(34).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseRedelegation();
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
                    message.validator_src_address = reader.string();
                    continue;
                case 3:
                    if (tag !== 26) {
                        break;
                    }
                    message.validator_dst_address = reader.string();
                    continue;
                case 4:
                    if (tag !== 34) {
                        break;
                    }
                    message.entries.push(exports.RedelegationEntry.decode(reader, reader.uint32()));
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
            validator_src_address: isSet(object.validator_src_address) ? globalThis.String(object.validator_src_address) : "",
            validator_dst_address: isSet(object.validator_dst_address) ? globalThis.String(object.validator_dst_address) : "",
            entries: globalThis.Array.isArray(object?.entries) ? object.entries.map((e) => exports.RedelegationEntry.fromJSON(e)) : []
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.delegator_address !== "") {
            obj.delegator_address = message.delegator_address;
        }
        if (message.validator_src_address !== "") {
            obj.validator_src_address = message.validator_src_address;
        }
        if (message.validator_dst_address !== "") {
            obj.validator_dst_address = message.validator_dst_address;
        }
        if (message.entries?.length) {
            obj.entries = message.entries.map((e) => exports.RedelegationEntry.toJSON(e));
        }
        return obj;
    },
    create(base) {
        return exports.Redelegation.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseRedelegation();
        message.delegator_address = object.delegator_address ?? "";
        message.validator_src_address = object.validator_src_address ?? "";
        message.validator_dst_address = object.validator_dst_address ?? "";
        message.entries = object.entries?.map((e) => exports.RedelegationEntry.fromPartial(e)) || [];
        return message;
    }
};
exports.Params = {
    $type: "cosmos.staking.v1beta1.Params",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.unbonding_time !== undefined) {
            duration_1.Duration.encode(message.unbonding_time, writer.uint32(10).fork()).join();
        }
        if (message.max_validators !== 0) {
            writer.uint32(16).uint32(message.max_validators);
        }
        if (message.max_entries !== 0) {
            writer.uint32(24).uint32(message.max_entries);
        }
        if (message.historical_entries !== 0) {
            writer.uint32(32).uint32(message.historical_entries);
        }
        if (message.bond_denom !== "") {
            writer.uint32(42).string(message.bond_denom);
        }
        if (message.min_commission_rate !== "") {
            writer.uint32(50).string(message.min_commission_rate);
        }
        if (message.max_voting_power_ratio !== "") {
            writer.uint32(58).string(message.max_voting_power_ratio);
        }
        if (message.max_voting_power_enforcement_threshold !== "") {
            writer.uint32(66).string(message.max_voting_power_enforcement_threshold);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseParams();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.unbonding_time = duration_1.Duration.decode(reader, reader.uint32());
                    continue;
                case 2:
                    if (tag !== 16) {
                        break;
                    }
                    message.max_validators = reader.uint32();
                    continue;
                case 3:
                    if (tag !== 24) {
                        break;
                    }
                    message.max_entries = reader.uint32();
                    continue;
                case 4:
                    if (tag !== 32) {
                        break;
                    }
                    message.historical_entries = reader.uint32();
                    continue;
                case 5:
                    if (tag !== 42) {
                        break;
                    }
                    message.bond_denom = reader.string();
                    continue;
                case 6:
                    if (tag !== 50) {
                        break;
                    }
                    message.min_commission_rate = reader.string();
                    continue;
                case 7:
                    if (tag !== 58) {
                        break;
                    }
                    message.max_voting_power_ratio = reader.string();
                    continue;
                case 8:
                    if (tag !== 66) {
                        break;
                    }
                    message.max_voting_power_enforcement_threshold = reader.string();
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
            unbonding_time: isSet(object.unbonding_time) ? duration_1.Duration.fromJSON(object.unbonding_time) : undefined,
            max_validators: isSet(object.max_validators) ? globalThis.Number(object.max_validators) : 0,
            max_entries: isSet(object.max_entries) ? globalThis.Number(object.max_entries) : 0,
            historical_entries: isSet(object.historical_entries) ? globalThis.Number(object.historical_entries) : 0,
            bond_denom: isSet(object.bond_denom) ? globalThis.String(object.bond_denom) : "",
            min_commission_rate: isSet(object.min_commission_rate) ? globalThis.String(object.min_commission_rate) : "",
            max_voting_power_ratio: isSet(object.max_voting_power_ratio) ? globalThis.String(object.max_voting_power_ratio) : "",
            max_voting_power_enforcement_threshold: isSet(object.max_voting_power_enforcement_threshold)
                ? globalThis.String(object.max_voting_power_enforcement_threshold)
                : ""
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.unbonding_time !== undefined) {
            obj.unbonding_time = duration_1.Duration.toJSON(message.unbonding_time);
        }
        if (message.max_validators !== 0) {
            obj.max_validators = Math.round(message.max_validators);
        }
        if (message.max_entries !== 0) {
            obj.max_entries = Math.round(message.max_entries);
        }
        if (message.historical_entries !== 0) {
            obj.historical_entries = Math.round(message.historical_entries);
        }
        if (message.bond_denom !== "") {
            obj.bond_denom = message.bond_denom;
        }
        if (message.min_commission_rate !== "") {
            obj.min_commission_rate = message.min_commission_rate;
        }
        if (message.max_voting_power_ratio !== "") {
            obj.max_voting_power_ratio = message.max_voting_power_ratio;
        }
        if (message.max_voting_power_enforcement_threshold !== "") {
            obj.max_voting_power_enforcement_threshold = message.max_voting_power_enforcement_threshold;
        }
        return obj;
    },
    create(base) {
        return exports.Params.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseParams();
        message.unbonding_time = object.unbonding_time !== undefined && object.unbonding_time !== null ? duration_1.Duration.fromPartial(object.unbonding_time) : undefined;
        message.max_validators = object.max_validators ?? 0;
        message.max_entries = object.max_entries ?? 0;
        message.historical_entries = object.historical_entries ?? 0;
        message.bond_denom = object.bond_denom ?? "";
        message.min_commission_rate = object.min_commission_rate ?? "";
        message.max_voting_power_ratio = object.max_voting_power_ratio ?? "";
        message.max_voting_power_enforcement_threshold = object.max_voting_power_enforcement_threshold ?? "";
        return message;
    }
};
exports.DelegationResponse = {
    $type: "cosmos.staking.v1beta1.DelegationResponse",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.delegation !== undefined) {
            exports.Delegation.encode(message.delegation, writer.uint32(10).fork()).join();
        }
        if (message.balance !== undefined) {
            coin_1.Coin.encode(message.balance, writer.uint32(18).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseDelegationResponse();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.delegation = exports.Delegation.decode(reader, reader.uint32());
                    continue;
                case 2:
                    if (tag !== 18) {
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
        return {
            delegation: isSet(object.delegation) ? exports.Delegation.fromJSON(object.delegation) : undefined,
            balance: isSet(object.balance) ? coin_1.Coin.fromJSON(object.balance) : undefined
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.delegation !== undefined) {
            obj.delegation = exports.Delegation.toJSON(message.delegation);
        }
        if (message.balance !== undefined) {
            obj.balance = coin_1.Coin.toJSON(message.balance);
        }
        return obj;
    },
    create(base) {
        return exports.DelegationResponse.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseDelegationResponse();
        message.delegation = object.delegation !== undefined && object.delegation !== null ? exports.Delegation.fromPartial(object.delegation) : undefined;
        message.balance = object.balance !== undefined && object.balance !== null ? coin_1.Coin.fromPartial(object.balance) : undefined;
        return message;
    }
};
exports.RedelegationEntryResponse = {
    $type: "cosmos.staking.v1beta1.RedelegationEntryResponse",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.redelegation_entry !== undefined) {
            exports.RedelegationEntry.encode(message.redelegation_entry, writer.uint32(10).fork()).join();
        }
        if (message.balance !== "") {
            writer.uint32(34).string(message.balance);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseRedelegationEntryResponse();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.redelegation_entry = exports.RedelegationEntry.decode(reader, reader.uint32());
                    continue;
                case 4:
                    if (tag !== 34) {
                        break;
                    }
                    message.balance = reader.string();
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
            redelegation_entry: isSet(object.redelegation_entry) ? exports.RedelegationEntry.fromJSON(object.redelegation_entry) : undefined,
            balance: isSet(object.balance) ? globalThis.String(object.balance) : ""
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.redelegation_entry !== undefined) {
            obj.redelegation_entry = exports.RedelegationEntry.toJSON(message.redelegation_entry);
        }
        if (message.balance !== "") {
            obj.balance = message.balance;
        }
        return obj;
    },
    create(base) {
        return exports.RedelegationEntryResponse.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseRedelegationEntryResponse();
        message.redelegation_entry =
            object.redelegation_entry !== undefined && object.redelegation_entry !== null ? exports.RedelegationEntry.fromPartial(object.redelegation_entry) : undefined;
        message.balance = object.balance ?? "";
        return message;
    }
};
exports.RedelegationResponse = {
    $type: "cosmos.staking.v1beta1.RedelegationResponse",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.redelegation !== undefined) {
            exports.Redelegation.encode(message.redelegation, writer.uint32(10).fork()).join();
        }
        for (const v of message.entries) {
            exports.RedelegationEntryResponse.encode(v, writer.uint32(18).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseRedelegationResponse();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.redelegation = exports.Redelegation.decode(reader, reader.uint32());
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.entries.push(exports.RedelegationEntryResponse.decode(reader, reader.uint32()));
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
            redelegation: isSet(object.redelegation) ? exports.Redelegation.fromJSON(object.redelegation) : undefined,
            entries: globalThis.Array.isArray(object?.entries) ? object.entries.map((e) => exports.RedelegationEntryResponse.fromJSON(e)) : []
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.redelegation !== undefined) {
            obj.redelegation = exports.Redelegation.toJSON(message.redelegation);
        }
        if (message.entries?.length) {
            obj.entries = message.entries.map((e) => exports.RedelegationEntryResponse.toJSON(e));
        }
        return obj;
    },
    create(base) {
        return exports.RedelegationResponse.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseRedelegationResponse();
        message.redelegation = object.redelegation !== undefined && object.redelegation !== null ? exports.Redelegation.fromPartial(object.redelegation) : undefined;
        message.entries = object.entries?.map((e) => exports.RedelegationEntryResponse.fromPartial(e)) || [];
        return message;
    }
};
exports.Pool = {
    $type: "cosmos.staking.v1beta1.Pool",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.not_bonded_tokens !== "") {
            writer.uint32(10).string(message.not_bonded_tokens);
        }
        if (message.bonded_tokens !== "") {
            writer.uint32(18).string(message.bonded_tokens);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBasePool();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.not_bonded_tokens = reader.string();
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.bonded_tokens = reader.string();
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
            not_bonded_tokens: isSet(object.not_bonded_tokens) ? globalThis.String(object.not_bonded_tokens) : "",
            bonded_tokens: isSet(object.bonded_tokens) ? globalThis.String(object.bonded_tokens) : ""
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.not_bonded_tokens !== "") {
            obj.not_bonded_tokens = message.not_bonded_tokens;
        }
        if (message.bonded_tokens !== "") {
            obj.bonded_tokens = message.bonded_tokens;
        }
        return obj;
    },
    create(base) {
        return exports.Pool.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBasePool();
        message.not_bonded_tokens = object.not_bonded_tokens ?? "";
        message.bonded_tokens = object.bonded_tokens ?? "";
        return message;
    }
};
function bondStatusFromJSON(object) {
    switch (object) {
        case 0:
        case "BOND_STATUS_UNSPECIFIED":
            return v1beta1_1.BondStatus.BOND_STATUS_UNSPECIFIED;
        case 1:
        case "BOND_STATUS_UNBONDED":
            return v1beta1_1.BondStatus.BOND_STATUS_UNBONDED;
        case 2:
        case "BOND_STATUS_UNBONDING":
            return v1beta1_1.BondStatus.BOND_STATUS_UNBONDING;
        case 3:
        case "BOND_STATUS_BONDED":
            return v1beta1_1.BondStatus.BOND_STATUS_BONDED;
        case -1:
        case "UNRECOGNIZED":
        default:
            return v1beta1_1.BondStatus.UNRECOGNIZED;
    }
}
function bondStatusToJSON(object) {
    switch (object) {
        case v1beta1_1.BondStatus.BOND_STATUS_UNSPECIFIED:
            return "BOND_STATUS_UNSPECIFIED";
        case v1beta1_1.BondStatus.BOND_STATUS_UNBONDED:
            return "BOND_STATUS_UNBONDED";
        case v1beta1_1.BondStatus.BOND_STATUS_UNBONDING:
            return "BOND_STATUS_UNBONDING";
        case v1beta1_1.BondStatus.BOND_STATUS_BONDED:
            return "BOND_STATUS_BONDED";
        case v1beta1_1.BondStatus.UNRECOGNIZED:
        default:
            return "UNRECOGNIZED";
    }
}
function createBaseHistoricalInfo() {
    return { header: undefined, valset: [] };
}
function createBaseCommissionRates() {
    return { rate: "", max_rate: "", max_change_rate: "" };
}
function createBaseCommission() {
    return { commission_rates: undefined, update_time: undefined };
}
function createBaseDescription() {
    return { moniker: "", identity: "", website: "", security_contact: "", details: "" };
}
function createBaseValidator() {
    return {
        operator_address: "",
        consensus_pubkey: undefined,
        jailed: false,
        status: 0,
        tokens: "",
        delegator_shares: "",
        description: undefined,
        unbonding_height: 0,
        unbonding_time: undefined,
        commission: undefined,
        min_self_delegation: ""
    };
}
function createBaseValAddresses() {
    return { addresses: [] };
}
function createBaseDVPair() {
    return { delegator_address: "", validator_address: "" };
}
function createBaseDVPairs() {
    return { pairs: [] };
}
function createBaseDVVTriplet() {
    return { delegator_address: "", validator_src_address: "", validator_dst_address: "" };
}
function createBaseDVVTriplets() {
    return { triplets: [] };
}
function createBaseDelegation() {
    return { delegator_address: "", validator_address: "", shares: "" };
}
function createBaseUnbondingDelegation() {
    return { delegator_address: "", validator_address: "", entries: [] };
}
function createBaseUnbondingDelegationEntry() {
    return { creation_height: 0, completion_time: undefined, initial_balance: "", balance: "" };
}
function createBaseRedelegationEntry() {
    return { creation_height: 0, completion_time: undefined, initial_balance: "", shares_dst: "" };
}
function createBaseRedelegation() {
    return { delegator_address: "", validator_src_address: "", validator_dst_address: "", entries: [] };
}
function createBaseParams() {
    return {
        unbonding_time: undefined,
        max_validators: 0,
        max_entries: 0,
        historical_entries: 0,
        bond_denom: "",
        min_commission_rate: "",
        max_voting_power_ratio: "",
        max_voting_power_enforcement_threshold: ""
    };
}
function createBaseDelegationResponse() {
    return { delegation: undefined, balance: undefined };
}
function createBaseRedelegationEntryResponse() {
    return { redelegation_entry: undefined, balance: "" };
}
function createBaseRedelegationResponse() {
    return { redelegation: undefined, entries: [] };
}
function createBasePool() {
    return { not_bonded_tokens: "", bonded_tokens: "" };
}
function toTimestamp(date) {
    const seconds = Math.trunc(date.getTime() / 1000);
    const nanos = (date.getTime() % 1000) * 1000000;
    return { seconds, nanos };
}
function fromTimestamp(t) {
    let millis = (t.seconds || 0) * 1000;
    millis += (t.nanos || 0) / 1000000;
    return new globalThis.Date(millis);
}
function fromJsonTimestamp(o) {
    if (o instanceof globalThis.Date) {
        return o;
    }
    else if (typeof o === "string") {
        return new globalThis.Date(o);
    }
    else {
        return fromTimestamp(timestamp_1.Timestamp.fromJSON(o));
    }
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
    ["/cosmos.staking.v1beta1.HistoricalInfo", exports.HistoricalInfo],
    ["/cosmos.staking.v1beta1.CommissionRates", exports.CommissionRates],
    ["/cosmos.staking.v1beta1.Commission", exports.Commission],
    ["/cosmos.staking.v1beta1.Description", exports.Description],
    ["/cosmos.staking.v1beta1.Validator", exports.Validator],
    ["/cosmos.staking.v1beta1.ValAddresses", exports.ValAddresses],
    ["/cosmos.staking.v1beta1.DVPair", exports.DVPair],
    ["/cosmos.staking.v1beta1.DVPairs", exports.DVPairs],
    ["/cosmos.staking.v1beta1.DVVTriplet", exports.DVVTriplet],
    ["/cosmos.staking.v1beta1.DVVTriplets", exports.DVVTriplets],
    ["/cosmos.staking.v1beta1.Delegation", exports.Delegation],
    ["/cosmos.staking.v1beta1.UnbondingDelegation", exports.UnbondingDelegation],
    ["/cosmos.staking.v1beta1.UnbondingDelegationEntry", exports.UnbondingDelegationEntry],
    ["/cosmos.staking.v1beta1.RedelegationEntry", exports.RedelegationEntry],
    ["/cosmos.staking.v1beta1.Redelegation", exports.Redelegation],
    ["/cosmos.staking.v1beta1.Params", exports.Params],
    ["/cosmos.staking.v1beta1.DelegationResponse", exports.DelegationResponse],
    ["/cosmos.staking.v1beta1.RedelegationEntryResponse", exports.RedelegationEntryResponse],
    ["/cosmos.staking.v1beta1.RedelegationResponse", exports.RedelegationResponse],
    ["/cosmos.staking.v1beta1.Pool", exports.Pool]
];
exports.aminoConverters = {
    "/cosmos.staking.v1beta1.HistoricalInfo": {
        aminoType: "cosmos-sdk/HistoricalInfo",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.staking.v1beta1.CommissionRates": {
        aminoType: "cosmos-sdk/CommissionRates",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.staking.v1beta1.Commission": {
        aminoType: "cosmos-sdk/Commission",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.staking.v1beta1.Description": {
        aminoType: "cosmos-sdk/Description",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.staking.v1beta1.Validator": {
        aminoType: "cosmos-sdk/Validator",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.staking.v1beta1.ValAddresses": {
        aminoType: "cosmos-sdk/ValAddresses",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.staking.v1beta1.DVPair": {
        aminoType: "cosmos-sdk/DVPair",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.staking.v1beta1.DVPairs": {
        aminoType: "cosmos-sdk/DVPairs",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.staking.v1beta1.DVVTriplet": {
        aminoType: "cosmos-sdk/DVVTriplet",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.staking.v1beta1.DVVTriplets": {
        aminoType: "cosmos-sdk/DVVTriplets",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.staking.v1beta1.Delegation": {
        aminoType: "cosmos-sdk/Delegation",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.staking.v1beta1.UnbondingDelegation": {
        aminoType: "cosmos-sdk/UnbondingDelegation",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.staking.v1beta1.UnbondingDelegationEntry": {
        aminoType: "cosmos-sdk/UnbondingDelegationEntry",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.staking.v1beta1.RedelegationEntry": {
        aminoType: "cosmos-sdk/RedelegationEntry",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.staking.v1beta1.Redelegation": {
        aminoType: "cosmos-sdk/Redelegation",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.staking.v1beta1.Params": {
        aminoType: "cosmos-sdk/Params",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.staking.v1beta1.DelegationResponse": {
        aminoType: "cosmos-sdk/DelegationResponse",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.staking.v1beta1.RedelegationEntryResponse": {
        aminoType: "cosmos-sdk/RedelegationEntryResponse",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.staking.v1beta1.RedelegationResponse": {
        aminoType: "cosmos-sdk/RedelegationResponse",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.staking.v1beta1.Pool": {
        aminoType: "cosmos-sdk/Pool",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    }
};
