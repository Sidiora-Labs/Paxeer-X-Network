"use strict";
Object.defineProperty(exports, "__esModule", { value: true });
exports.aminoConverters = exports.registry = exports.GenesisState = exports.ValidatorSlashEventRecord = exports.DelegatorStartingInfoRecord = exports.ValidatorCurrentRewardsRecord = exports.ValidatorHistoricalRewardsRecord = exports.ValidatorAccumulatedCommissionRecord = exports.ValidatorOutstandingRewardsRecord = exports.DelegatorWithdrawInfo = void 0;
const wire_1 = require("@bufbuild/protobuf/wire");
const coin_1 = require("../../base/v1beta1/coin");
const distribution_1 = require("./distribution");
exports.DelegatorWithdrawInfo = {
    $type: "cosmos.distribution.v1beta1.DelegatorWithdrawInfo",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.delegator_address !== "") {
            writer.uint32(10).string(message.delegator_address);
        }
        if (message.withdraw_address !== "") {
            writer.uint32(18).string(message.withdraw_address);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseDelegatorWithdrawInfo();
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
        return exports.DelegatorWithdrawInfo.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseDelegatorWithdrawInfo();
        message.delegator_address = object.delegator_address ?? "";
        message.withdraw_address = object.withdraw_address ?? "";
        return message;
    }
};
exports.ValidatorOutstandingRewardsRecord = {
    $type: "cosmos.distribution.v1beta1.ValidatorOutstandingRewardsRecord",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.validator_address !== "") {
            writer.uint32(10).string(message.validator_address);
        }
        for (const v of message.outstanding_rewards) {
            coin_1.DecCoin.encode(v, writer.uint32(18).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseValidatorOutstandingRewardsRecord();
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
                    if (tag !== 18) {
                        break;
                    }
                    message.outstanding_rewards.push(coin_1.DecCoin.decode(reader, reader.uint32()));
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
            outstanding_rewards: globalThis.Array.isArray(object?.outstanding_rewards) ? object.outstanding_rewards.map((e) => coin_1.DecCoin.fromJSON(e)) : []
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.validator_address !== "") {
            obj.validator_address = message.validator_address;
        }
        if (message.outstanding_rewards?.length) {
            obj.outstanding_rewards = message.outstanding_rewards.map((e) => coin_1.DecCoin.toJSON(e));
        }
        return obj;
    },
    create(base) {
        return exports.ValidatorOutstandingRewardsRecord.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseValidatorOutstandingRewardsRecord();
        message.validator_address = object.validator_address ?? "";
        message.outstanding_rewards = object.outstanding_rewards?.map((e) => coin_1.DecCoin.fromPartial(e)) || [];
        return message;
    }
};
exports.ValidatorAccumulatedCommissionRecord = {
    $type: "cosmos.distribution.v1beta1.ValidatorAccumulatedCommissionRecord",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.validator_address !== "") {
            writer.uint32(10).string(message.validator_address);
        }
        if (message.accumulated !== undefined) {
            distribution_1.ValidatorAccumulatedCommission.encode(message.accumulated, writer.uint32(18).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseValidatorAccumulatedCommissionRecord();
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
                    if (tag !== 18) {
                        break;
                    }
                    message.accumulated = distribution_1.ValidatorAccumulatedCommission.decode(reader, reader.uint32());
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
            accumulated: isSet(object.accumulated) ? distribution_1.ValidatorAccumulatedCommission.fromJSON(object.accumulated) : undefined
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.validator_address !== "") {
            obj.validator_address = message.validator_address;
        }
        if (message.accumulated !== undefined) {
            obj.accumulated = distribution_1.ValidatorAccumulatedCommission.toJSON(message.accumulated);
        }
        return obj;
    },
    create(base) {
        return exports.ValidatorAccumulatedCommissionRecord.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseValidatorAccumulatedCommissionRecord();
        message.validator_address = object.validator_address ?? "";
        message.accumulated =
            object.accumulated !== undefined && object.accumulated !== null ? distribution_1.ValidatorAccumulatedCommission.fromPartial(object.accumulated) : undefined;
        return message;
    }
};
exports.ValidatorHistoricalRewardsRecord = {
    $type: "cosmos.distribution.v1beta1.ValidatorHistoricalRewardsRecord",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.validator_address !== "") {
            writer.uint32(10).string(message.validator_address);
        }
        if (message.period !== 0) {
            writer.uint32(16).uint64(message.period);
        }
        if (message.rewards !== undefined) {
            distribution_1.ValidatorHistoricalRewards.encode(message.rewards, writer.uint32(26).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseValidatorHistoricalRewardsRecord();
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
                    message.period = longToNumber(reader.uint64());
                    continue;
                case 3:
                    if (tag !== 26) {
                        break;
                    }
                    message.rewards = distribution_1.ValidatorHistoricalRewards.decode(reader, reader.uint32());
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
            period: isSet(object.period) ? globalThis.Number(object.period) : 0,
            rewards: isSet(object.rewards) ? distribution_1.ValidatorHistoricalRewards.fromJSON(object.rewards) : undefined
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.validator_address !== "") {
            obj.validator_address = message.validator_address;
        }
        if (message.period !== 0) {
            obj.period = Math.round(message.period);
        }
        if (message.rewards !== undefined) {
            obj.rewards = distribution_1.ValidatorHistoricalRewards.toJSON(message.rewards);
        }
        return obj;
    },
    create(base) {
        return exports.ValidatorHistoricalRewardsRecord.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseValidatorHistoricalRewardsRecord();
        message.validator_address = object.validator_address ?? "";
        message.period = object.period ?? 0;
        message.rewards = object.rewards !== undefined && object.rewards !== null ? distribution_1.ValidatorHistoricalRewards.fromPartial(object.rewards) : undefined;
        return message;
    }
};
exports.ValidatorCurrentRewardsRecord = {
    $type: "cosmos.distribution.v1beta1.ValidatorCurrentRewardsRecord",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.validator_address !== "") {
            writer.uint32(10).string(message.validator_address);
        }
        if (message.rewards !== undefined) {
            distribution_1.ValidatorCurrentRewards.encode(message.rewards, writer.uint32(18).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseValidatorCurrentRewardsRecord();
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
                    if (tag !== 18) {
                        break;
                    }
                    message.rewards = distribution_1.ValidatorCurrentRewards.decode(reader, reader.uint32());
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
            rewards: isSet(object.rewards) ? distribution_1.ValidatorCurrentRewards.fromJSON(object.rewards) : undefined
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.validator_address !== "") {
            obj.validator_address = message.validator_address;
        }
        if (message.rewards !== undefined) {
            obj.rewards = distribution_1.ValidatorCurrentRewards.toJSON(message.rewards);
        }
        return obj;
    },
    create(base) {
        return exports.ValidatorCurrentRewardsRecord.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseValidatorCurrentRewardsRecord();
        message.validator_address = object.validator_address ?? "";
        message.rewards = object.rewards !== undefined && object.rewards !== null ? distribution_1.ValidatorCurrentRewards.fromPartial(object.rewards) : undefined;
        return message;
    }
};
exports.DelegatorStartingInfoRecord = {
    $type: "cosmos.distribution.v1beta1.DelegatorStartingInfoRecord",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.delegator_address !== "") {
            writer.uint32(10).string(message.delegator_address);
        }
        if (message.validator_address !== "") {
            writer.uint32(18).string(message.validator_address);
        }
        if (message.starting_info !== undefined) {
            distribution_1.DelegatorStartingInfo.encode(message.starting_info, writer.uint32(26).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseDelegatorStartingInfoRecord();
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
                    message.starting_info = distribution_1.DelegatorStartingInfo.decode(reader, reader.uint32());
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
            starting_info: isSet(object.starting_info) ? distribution_1.DelegatorStartingInfo.fromJSON(object.starting_info) : undefined
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
        if (message.starting_info !== undefined) {
            obj.starting_info = distribution_1.DelegatorStartingInfo.toJSON(message.starting_info);
        }
        return obj;
    },
    create(base) {
        return exports.DelegatorStartingInfoRecord.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseDelegatorStartingInfoRecord();
        message.delegator_address = object.delegator_address ?? "";
        message.validator_address = object.validator_address ?? "";
        message.starting_info =
            object.starting_info !== undefined && object.starting_info !== null ? distribution_1.DelegatorStartingInfo.fromPartial(object.starting_info) : undefined;
        return message;
    }
};
exports.ValidatorSlashEventRecord = {
    $type: "cosmos.distribution.v1beta1.ValidatorSlashEventRecord",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.validator_address !== "") {
            writer.uint32(10).string(message.validator_address);
        }
        if (message.height !== 0) {
            writer.uint32(16).uint64(message.height);
        }
        if (message.period !== 0) {
            writer.uint32(24).uint64(message.period);
        }
        if (message.validator_slash_event !== undefined) {
            distribution_1.ValidatorSlashEvent.encode(message.validator_slash_event, writer.uint32(34).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseValidatorSlashEventRecord();
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
                    message.height = longToNumber(reader.uint64());
                    continue;
                case 3:
                    if (tag !== 24) {
                        break;
                    }
                    message.period = longToNumber(reader.uint64());
                    continue;
                case 4:
                    if (tag !== 34) {
                        break;
                    }
                    message.validator_slash_event = distribution_1.ValidatorSlashEvent.decode(reader, reader.uint32());
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
            height: isSet(object.height) ? globalThis.Number(object.height) : 0,
            period: isSet(object.period) ? globalThis.Number(object.period) : 0,
            validator_slash_event: isSet(object.validator_slash_event) ? distribution_1.ValidatorSlashEvent.fromJSON(object.validator_slash_event) : undefined
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.validator_address !== "") {
            obj.validator_address = message.validator_address;
        }
        if (message.height !== 0) {
            obj.height = Math.round(message.height);
        }
        if (message.period !== 0) {
            obj.period = Math.round(message.period);
        }
        if (message.validator_slash_event !== undefined) {
            obj.validator_slash_event = distribution_1.ValidatorSlashEvent.toJSON(message.validator_slash_event);
        }
        return obj;
    },
    create(base) {
        return exports.ValidatorSlashEventRecord.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseValidatorSlashEventRecord();
        message.validator_address = object.validator_address ?? "";
        message.height = object.height ?? 0;
        message.period = object.period ?? 0;
        message.validator_slash_event =
            object.validator_slash_event !== undefined && object.validator_slash_event !== null
                ? distribution_1.ValidatorSlashEvent.fromPartial(object.validator_slash_event)
                : undefined;
        return message;
    }
};
exports.GenesisState = {
    $type: "cosmos.distribution.v1beta1.GenesisState",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.params !== undefined) {
            distribution_1.Params.encode(message.params, writer.uint32(10).fork()).join();
        }
        if (message.fee_pool !== undefined) {
            distribution_1.FeePool.encode(message.fee_pool, writer.uint32(18).fork()).join();
        }
        for (const v of message.delegator_withdraw_infos) {
            exports.DelegatorWithdrawInfo.encode(v, writer.uint32(26).fork()).join();
        }
        if (message.previous_proposer !== "") {
            writer.uint32(34).string(message.previous_proposer);
        }
        for (const v of message.outstanding_rewards) {
            exports.ValidatorOutstandingRewardsRecord.encode(v, writer.uint32(42).fork()).join();
        }
        for (const v of message.validator_accumulated_commissions) {
            exports.ValidatorAccumulatedCommissionRecord.encode(v, writer.uint32(50).fork()).join();
        }
        for (const v of message.validator_historical_rewards) {
            exports.ValidatorHistoricalRewardsRecord.encode(v, writer.uint32(58).fork()).join();
        }
        for (const v of message.validator_current_rewards) {
            exports.ValidatorCurrentRewardsRecord.encode(v, writer.uint32(66).fork()).join();
        }
        for (const v of message.delegator_starting_infos) {
            exports.DelegatorStartingInfoRecord.encode(v, writer.uint32(74).fork()).join();
        }
        for (const v of message.validator_slash_events) {
            exports.ValidatorSlashEventRecord.encode(v, writer.uint32(82).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseGenesisState();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.params = distribution_1.Params.decode(reader, reader.uint32());
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.fee_pool = distribution_1.FeePool.decode(reader, reader.uint32());
                    continue;
                case 3:
                    if (tag !== 26) {
                        break;
                    }
                    message.delegator_withdraw_infos.push(exports.DelegatorWithdrawInfo.decode(reader, reader.uint32()));
                    continue;
                case 4:
                    if (tag !== 34) {
                        break;
                    }
                    message.previous_proposer = reader.string();
                    continue;
                case 5:
                    if (tag !== 42) {
                        break;
                    }
                    message.outstanding_rewards.push(exports.ValidatorOutstandingRewardsRecord.decode(reader, reader.uint32()));
                    continue;
                case 6:
                    if (tag !== 50) {
                        break;
                    }
                    message.validator_accumulated_commissions.push(exports.ValidatorAccumulatedCommissionRecord.decode(reader, reader.uint32()));
                    continue;
                case 7:
                    if (tag !== 58) {
                        break;
                    }
                    message.validator_historical_rewards.push(exports.ValidatorHistoricalRewardsRecord.decode(reader, reader.uint32()));
                    continue;
                case 8:
                    if (tag !== 66) {
                        break;
                    }
                    message.validator_current_rewards.push(exports.ValidatorCurrentRewardsRecord.decode(reader, reader.uint32()));
                    continue;
                case 9:
                    if (tag !== 74) {
                        break;
                    }
                    message.delegator_starting_infos.push(exports.DelegatorStartingInfoRecord.decode(reader, reader.uint32()));
                    continue;
                case 10:
                    if (tag !== 82) {
                        break;
                    }
                    message.validator_slash_events.push(exports.ValidatorSlashEventRecord.decode(reader, reader.uint32()));
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
            params: isSet(object.params) ? distribution_1.Params.fromJSON(object.params) : undefined,
            fee_pool: isSet(object.fee_pool) ? distribution_1.FeePool.fromJSON(object.fee_pool) : undefined,
            delegator_withdraw_infos: globalThis.Array.isArray(object?.delegator_withdraw_infos)
                ? object.delegator_withdraw_infos.map((e) => exports.DelegatorWithdrawInfo.fromJSON(e))
                : [],
            previous_proposer: isSet(object.previous_proposer) ? globalThis.String(object.previous_proposer) : "",
            outstanding_rewards: globalThis.Array.isArray(object?.outstanding_rewards)
                ? object.outstanding_rewards.map((e) => exports.ValidatorOutstandingRewardsRecord.fromJSON(e))
                : [],
            validator_accumulated_commissions: globalThis.Array.isArray(object?.validator_accumulated_commissions)
                ? object.validator_accumulated_commissions.map((e) => exports.ValidatorAccumulatedCommissionRecord.fromJSON(e))
                : [],
            validator_historical_rewards: globalThis.Array.isArray(object?.validator_historical_rewards)
                ? object.validator_historical_rewards.map((e) => exports.ValidatorHistoricalRewardsRecord.fromJSON(e))
                : [],
            validator_current_rewards: globalThis.Array.isArray(object?.validator_current_rewards)
                ? object.validator_current_rewards.map((e) => exports.ValidatorCurrentRewardsRecord.fromJSON(e))
                : [],
            delegator_starting_infos: globalThis.Array.isArray(object?.delegator_starting_infos)
                ? object.delegator_starting_infos.map((e) => exports.DelegatorStartingInfoRecord.fromJSON(e))
                : [],
            validator_slash_events: globalThis.Array.isArray(object?.validator_slash_events)
                ? object.validator_slash_events.map((e) => exports.ValidatorSlashEventRecord.fromJSON(e))
                : []
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.params !== undefined) {
            obj.params = distribution_1.Params.toJSON(message.params);
        }
        if (message.fee_pool !== undefined) {
            obj.fee_pool = distribution_1.FeePool.toJSON(message.fee_pool);
        }
        if (message.delegator_withdraw_infos?.length) {
            obj.delegator_withdraw_infos = message.delegator_withdraw_infos.map((e) => exports.DelegatorWithdrawInfo.toJSON(e));
        }
        if (message.previous_proposer !== "") {
            obj.previous_proposer = message.previous_proposer;
        }
        if (message.outstanding_rewards?.length) {
            obj.outstanding_rewards = message.outstanding_rewards.map((e) => exports.ValidatorOutstandingRewardsRecord.toJSON(e));
        }
        if (message.validator_accumulated_commissions?.length) {
            obj.validator_accumulated_commissions = message.validator_accumulated_commissions.map((e) => exports.ValidatorAccumulatedCommissionRecord.toJSON(e));
        }
        if (message.validator_historical_rewards?.length) {
            obj.validator_historical_rewards = message.validator_historical_rewards.map((e) => exports.ValidatorHistoricalRewardsRecord.toJSON(e));
        }
        if (message.validator_current_rewards?.length) {
            obj.validator_current_rewards = message.validator_current_rewards.map((e) => exports.ValidatorCurrentRewardsRecord.toJSON(e));
        }
        if (message.delegator_starting_infos?.length) {
            obj.delegator_starting_infos = message.delegator_starting_infos.map((e) => exports.DelegatorStartingInfoRecord.toJSON(e));
        }
        if (message.validator_slash_events?.length) {
            obj.validator_slash_events = message.validator_slash_events.map((e) => exports.ValidatorSlashEventRecord.toJSON(e));
        }
        return obj;
    },
    create(base) {
        return exports.GenesisState.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseGenesisState();
        message.params = object.params !== undefined && object.params !== null ? distribution_1.Params.fromPartial(object.params) : undefined;
        message.fee_pool = object.fee_pool !== undefined && object.fee_pool !== null ? distribution_1.FeePool.fromPartial(object.fee_pool) : undefined;
        message.delegator_withdraw_infos = object.delegator_withdraw_infos?.map((e) => exports.DelegatorWithdrawInfo.fromPartial(e)) || [];
        message.previous_proposer = object.previous_proposer ?? "";
        message.outstanding_rewards = object.outstanding_rewards?.map((e) => exports.ValidatorOutstandingRewardsRecord.fromPartial(e)) || [];
        message.validator_accumulated_commissions = object.validator_accumulated_commissions?.map((e) => exports.ValidatorAccumulatedCommissionRecord.fromPartial(e)) || [];
        message.validator_historical_rewards = object.validator_historical_rewards?.map((e) => exports.ValidatorHistoricalRewardsRecord.fromPartial(e)) || [];
        message.validator_current_rewards = object.validator_current_rewards?.map((e) => exports.ValidatorCurrentRewardsRecord.fromPartial(e)) || [];
        message.delegator_starting_infos = object.delegator_starting_infos?.map((e) => exports.DelegatorStartingInfoRecord.fromPartial(e)) || [];
        message.validator_slash_events = object.validator_slash_events?.map((e) => exports.ValidatorSlashEventRecord.fromPartial(e)) || [];
        return message;
    }
};
function createBaseDelegatorWithdrawInfo() {
    return { delegator_address: "", withdraw_address: "" };
}
function createBaseValidatorOutstandingRewardsRecord() {
    return { validator_address: "", outstanding_rewards: [] };
}
function createBaseValidatorAccumulatedCommissionRecord() {
    return { validator_address: "", accumulated: undefined };
}
function createBaseValidatorHistoricalRewardsRecord() {
    return { validator_address: "", period: 0, rewards: undefined };
}
function createBaseValidatorCurrentRewardsRecord() {
    return { validator_address: "", rewards: undefined };
}
function createBaseDelegatorStartingInfoRecord() {
    return { delegator_address: "", validator_address: "", starting_info: undefined };
}
function createBaseValidatorSlashEventRecord() {
    return { validator_address: "", height: 0, period: 0, validator_slash_event: undefined };
}
function createBaseGenesisState() {
    return {
        params: undefined,
        fee_pool: undefined,
        delegator_withdraw_infos: [],
        previous_proposer: "",
        outstanding_rewards: [],
        validator_accumulated_commissions: [],
        validator_historical_rewards: [],
        validator_current_rewards: [],
        delegator_starting_infos: [],
        validator_slash_events: []
    };
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
    ["/cosmos.distribution.v1beta1.DelegatorWithdrawInfo", exports.DelegatorWithdrawInfo],
    ["/cosmos.distribution.v1beta1.ValidatorOutstandingRewardsRecord", exports.ValidatorOutstandingRewardsRecord],
    ["/cosmos.distribution.v1beta1.ValidatorAccumulatedCommissionRecord", exports.ValidatorAccumulatedCommissionRecord],
    ["/cosmos.distribution.v1beta1.ValidatorHistoricalRewardsRecord", exports.ValidatorHistoricalRewardsRecord],
    ["/cosmos.distribution.v1beta1.ValidatorCurrentRewardsRecord", exports.ValidatorCurrentRewardsRecord],
    ["/cosmos.distribution.v1beta1.DelegatorStartingInfoRecord", exports.DelegatorStartingInfoRecord],
    ["/cosmos.distribution.v1beta1.ValidatorSlashEventRecord", exports.ValidatorSlashEventRecord],
    ["/cosmos.distribution.v1beta1.GenesisState", exports.GenesisState]
];
exports.aminoConverters = {
    "/cosmos.distribution.v1beta1.DelegatorWithdrawInfo": {
        aminoType: "cosmos-sdk/DelegatorWithdrawInfo",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.distribution.v1beta1.ValidatorOutstandingRewardsRecord": {
        aminoType: "cosmos-sdk/ValidatorOutstandingRewardsRecord",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.distribution.v1beta1.ValidatorAccumulatedCommissionRecord": {
        aminoType: "cosmos-sdk/ValidatorAccumulatedCommissionRecord",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.distribution.v1beta1.ValidatorHistoricalRewardsRecord": {
        aminoType: "cosmos-sdk/ValidatorHistoricalRewardsRecord",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.distribution.v1beta1.ValidatorCurrentRewardsRecord": {
        aminoType: "cosmos-sdk/ValidatorCurrentRewardsRecord",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.distribution.v1beta1.DelegatorStartingInfoRecord": {
        aminoType: "cosmos-sdk/DelegatorStartingInfoRecord",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.distribution.v1beta1.ValidatorSlashEventRecord": {
        aminoType: "cosmos-sdk/ValidatorSlashEventRecord",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.distribution.v1beta1.GenesisState": {
        aminoType: "cosmos-sdk/GenesisState",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    }
};
