"use strict";
Object.defineProperty(exports, "__esModule", { value: true });
exports.aminoConverters = exports.registry = exports.CommunityPoolSpendProposalWithDeposit = exports.DelegationDelegatorReward = exports.DelegatorStartingInfo = exports.CommunityPoolSpendProposal = exports.FeePool = exports.ValidatorSlashEvents = exports.ValidatorSlashEvent = exports.ValidatorOutstandingRewards = exports.ValidatorAccumulatedCommission = exports.ValidatorCurrentRewards = exports.ValidatorHistoricalRewards = exports.Params = void 0;
const wire_1 = require("@bufbuild/protobuf/wire");
const coin_1 = require("../../base/v1beta1/coin");
exports.Params = {
    $type: "cosmos.distribution.v1beta1.Params",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.community_tax !== "") {
            writer.uint32(10).string(message.community_tax);
        }
        if (message.base_proposer_reward !== "") {
            writer.uint32(18).string(message.base_proposer_reward);
        }
        if (message.bonus_proposer_reward !== "") {
            writer.uint32(26).string(message.bonus_proposer_reward);
        }
        if (message.withdraw_addr_enabled !== false) {
            writer.uint32(32).bool(message.withdraw_addr_enabled);
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
                    message.community_tax = reader.string();
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.base_proposer_reward = reader.string();
                    continue;
                case 3:
                    if (tag !== 26) {
                        break;
                    }
                    message.bonus_proposer_reward = reader.string();
                    continue;
                case 4:
                    if (tag !== 32) {
                        break;
                    }
                    message.withdraw_addr_enabled = reader.bool();
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
            community_tax: isSet(object.community_tax) ? globalThis.String(object.community_tax) : "",
            base_proposer_reward: isSet(object.base_proposer_reward) ? globalThis.String(object.base_proposer_reward) : "",
            bonus_proposer_reward: isSet(object.bonus_proposer_reward) ? globalThis.String(object.bonus_proposer_reward) : "",
            withdraw_addr_enabled: isSet(object.withdraw_addr_enabled) ? globalThis.Boolean(object.withdraw_addr_enabled) : false
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.community_tax !== "") {
            obj.community_tax = message.community_tax;
        }
        if (message.base_proposer_reward !== "") {
            obj.base_proposer_reward = message.base_proposer_reward;
        }
        if (message.bonus_proposer_reward !== "") {
            obj.bonus_proposer_reward = message.bonus_proposer_reward;
        }
        if (message.withdraw_addr_enabled !== false) {
            obj.withdraw_addr_enabled = message.withdraw_addr_enabled;
        }
        return obj;
    },
    create(base) {
        return exports.Params.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseParams();
        message.community_tax = object.community_tax ?? "";
        message.base_proposer_reward = object.base_proposer_reward ?? "";
        message.bonus_proposer_reward = object.bonus_proposer_reward ?? "";
        message.withdraw_addr_enabled = object.withdraw_addr_enabled ?? false;
        return message;
    }
};
exports.ValidatorHistoricalRewards = {
    $type: "cosmos.distribution.v1beta1.ValidatorHistoricalRewards",
    encode(message, writer = new wire_1.BinaryWriter()) {
        for (const v of message.cumulative_reward_ratio) {
            coin_1.DecCoin.encode(v, writer.uint32(10).fork()).join();
        }
        if (message.reference_count !== 0) {
            writer.uint32(16).uint32(message.reference_count);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseValidatorHistoricalRewards();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.cumulative_reward_ratio.push(coin_1.DecCoin.decode(reader, reader.uint32()));
                    continue;
                case 2:
                    if (tag !== 16) {
                        break;
                    }
                    message.reference_count = reader.uint32();
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
            cumulative_reward_ratio: globalThis.Array.isArray(object?.cumulative_reward_ratio)
                ? object.cumulative_reward_ratio.map((e) => coin_1.DecCoin.fromJSON(e))
                : [],
            reference_count: isSet(object.reference_count) ? globalThis.Number(object.reference_count) : 0
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.cumulative_reward_ratio?.length) {
            obj.cumulative_reward_ratio = message.cumulative_reward_ratio.map((e) => coin_1.DecCoin.toJSON(e));
        }
        if (message.reference_count !== 0) {
            obj.reference_count = Math.round(message.reference_count);
        }
        return obj;
    },
    create(base) {
        return exports.ValidatorHistoricalRewards.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseValidatorHistoricalRewards();
        message.cumulative_reward_ratio = object.cumulative_reward_ratio?.map((e) => coin_1.DecCoin.fromPartial(e)) || [];
        message.reference_count = object.reference_count ?? 0;
        return message;
    }
};
exports.ValidatorCurrentRewards = {
    $type: "cosmos.distribution.v1beta1.ValidatorCurrentRewards",
    encode(message, writer = new wire_1.BinaryWriter()) {
        for (const v of message.rewards) {
            coin_1.DecCoin.encode(v, writer.uint32(10).fork()).join();
        }
        if (message.period !== 0) {
            writer.uint32(16).uint64(message.period);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseValidatorCurrentRewards();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.rewards.push(coin_1.DecCoin.decode(reader, reader.uint32()));
                    continue;
                case 2:
                    if (tag !== 16) {
                        break;
                    }
                    message.period = longToNumber(reader.uint64());
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
            rewards: globalThis.Array.isArray(object?.rewards) ? object.rewards.map((e) => coin_1.DecCoin.fromJSON(e)) : [],
            period: isSet(object.period) ? globalThis.Number(object.period) : 0
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.rewards?.length) {
            obj.rewards = message.rewards.map((e) => coin_1.DecCoin.toJSON(e));
        }
        if (message.period !== 0) {
            obj.period = Math.round(message.period);
        }
        return obj;
    },
    create(base) {
        return exports.ValidatorCurrentRewards.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseValidatorCurrentRewards();
        message.rewards = object.rewards?.map((e) => coin_1.DecCoin.fromPartial(e)) || [];
        message.period = object.period ?? 0;
        return message;
    }
};
exports.ValidatorAccumulatedCommission = {
    $type: "cosmos.distribution.v1beta1.ValidatorAccumulatedCommission",
    encode(message, writer = new wire_1.BinaryWriter()) {
        for (const v of message.commission) {
            coin_1.DecCoin.encode(v, writer.uint32(10).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseValidatorAccumulatedCommission();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.commission.push(coin_1.DecCoin.decode(reader, reader.uint32()));
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
            commission: globalThis.Array.isArray(object?.commission) ? object.commission.map((e) => coin_1.DecCoin.fromJSON(e)) : []
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.commission?.length) {
            obj.commission = message.commission.map((e) => coin_1.DecCoin.toJSON(e));
        }
        return obj;
    },
    create(base) {
        return exports.ValidatorAccumulatedCommission.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseValidatorAccumulatedCommission();
        message.commission = object.commission?.map((e) => coin_1.DecCoin.fromPartial(e)) || [];
        return message;
    }
};
exports.ValidatorOutstandingRewards = {
    $type: "cosmos.distribution.v1beta1.ValidatorOutstandingRewards",
    encode(message, writer = new wire_1.BinaryWriter()) {
        for (const v of message.rewards) {
            coin_1.DecCoin.encode(v, writer.uint32(10).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseValidatorOutstandingRewards();
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
        return exports.ValidatorOutstandingRewards.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseValidatorOutstandingRewards();
        message.rewards = object.rewards?.map((e) => coin_1.DecCoin.fromPartial(e)) || [];
        return message;
    }
};
exports.ValidatorSlashEvent = {
    $type: "cosmos.distribution.v1beta1.ValidatorSlashEvent",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.validator_period !== 0) {
            writer.uint32(8).uint64(message.validator_period);
        }
        if (message.fraction !== "") {
            writer.uint32(18).string(message.fraction);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseValidatorSlashEvent();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 8) {
                        break;
                    }
                    message.validator_period = longToNumber(reader.uint64());
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.fraction = reader.string();
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
            validator_period: isSet(object.validator_period) ? globalThis.Number(object.validator_period) : 0,
            fraction: isSet(object.fraction) ? globalThis.String(object.fraction) : ""
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.validator_period !== 0) {
            obj.validator_period = Math.round(message.validator_period);
        }
        if (message.fraction !== "") {
            obj.fraction = message.fraction;
        }
        return obj;
    },
    create(base) {
        return exports.ValidatorSlashEvent.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseValidatorSlashEvent();
        message.validator_period = object.validator_period ?? 0;
        message.fraction = object.fraction ?? "";
        return message;
    }
};
exports.ValidatorSlashEvents = {
    $type: "cosmos.distribution.v1beta1.ValidatorSlashEvents",
    encode(message, writer = new wire_1.BinaryWriter()) {
        for (const v of message.validator_slash_events) {
            exports.ValidatorSlashEvent.encode(v, writer.uint32(10).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseValidatorSlashEvents();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.validator_slash_events.push(exports.ValidatorSlashEvent.decode(reader, reader.uint32()));
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
            validator_slash_events: globalThis.Array.isArray(object?.validator_slash_events)
                ? object.validator_slash_events.map((e) => exports.ValidatorSlashEvent.fromJSON(e))
                : []
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.validator_slash_events?.length) {
            obj.validator_slash_events = message.validator_slash_events.map((e) => exports.ValidatorSlashEvent.toJSON(e));
        }
        return obj;
    },
    create(base) {
        return exports.ValidatorSlashEvents.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseValidatorSlashEvents();
        message.validator_slash_events = object.validator_slash_events?.map((e) => exports.ValidatorSlashEvent.fromPartial(e)) || [];
        return message;
    }
};
exports.FeePool = {
    $type: "cosmos.distribution.v1beta1.FeePool",
    encode(message, writer = new wire_1.BinaryWriter()) {
        for (const v of message.community_pool) {
            coin_1.DecCoin.encode(v, writer.uint32(10).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseFeePool();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.community_pool.push(coin_1.DecCoin.decode(reader, reader.uint32()));
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
            community_pool: globalThis.Array.isArray(object?.community_pool) ? object.community_pool.map((e) => coin_1.DecCoin.fromJSON(e)) : []
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.community_pool?.length) {
            obj.community_pool = message.community_pool.map((e) => coin_1.DecCoin.toJSON(e));
        }
        return obj;
    },
    create(base) {
        return exports.FeePool.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseFeePool();
        message.community_pool = object.community_pool?.map((e) => coin_1.DecCoin.fromPartial(e)) || [];
        return message;
    }
};
exports.CommunityPoolSpendProposal = {
    $type: "cosmos.distribution.v1beta1.CommunityPoolSpendProposal",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.title !== "") {
            writer.uint32(10).string(message.title);
        }
        if (message.description !== "") {
            writer.uint32(18).string(message.description);
        }
        if (message.recipient !== "") {
            writer.uint32(26).string(message.recipient);
        }
        for (const v of message.amount) {
            coin_1.Coin.encode(v, writer.uint32(34).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseCommunityPoolSpendProposal();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.title = reader.string();
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.description = reader.string();
                    continue;
                case 3:
                    if (tag !== 26) {
                        break;
                    }
                    message.recipient = reader.string();
                    continue;
                case 4:
                    if (tag !== 34) {
                        break;
                    }
                    message.amount.push(coin_1.Coin.decode(reader, reader.uint32()));
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
            title: isSet(object.title) ? globalThis.String(object.title) : "",
            description: isSet(object.description) ? globalThis.String(object.description) : "",
            recipient: isSet(object.recipient) ? globalThis.String(object.recipient) : "",
            amount: globalThis.Array.isArray(object?.amount) ? object.amount.map((e) => coin_1.Coin.fromJSON(e)) : []
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.title !== "") {
            obj.title = message.title;
        }
        if (message.description !== "") {
            obj.description = message.description;
        }
        if (message.recipient !== "") {
            obj.recipient = message.recipient;
        }
        if (message.amount?.length) {
            obj.amount = message.amount.map((e) => coin_1.Coin.toJSON(e));
        }
        return obj;
    },
    create(base) {
        return exports.CommunityPoolSpendProposal.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseCommunityPoolSpendProposal();
        message.title = object.title ?? "";
        message.description = object.description ?? "";
        message.recipient = object.recipient ?? "";
        message.amount = object.amount?.map((e) => coin_1.Coin.fromPartial(e)) || [];
        return message;
    }
};
exports.DelegatorStartingInfo = {
    $type: "cosmos.distribution.v1beta1.DelegatorStartingInfo",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.previous_period !== 0) {
            writer.uint32(8).uint64(message.previous_period);
        }
        if (message.stake !== "") {
            writer.uint32(18).string(message.stake);
        }
        if (message.height !== 0) {
            writer.uint32(24).uint64(message.height);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseDelegatorStartingInfo();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 8) {
                        break;
                    }
                    message.previous_period = longToNumber(reader.uint64());
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.stake = reader.string();
                    continue;
                case 3:
                    if (tag !== 24) {
                        break;
                    }
                    message.height = longToNumber(reader.uint64());
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
            previous_period: isSet(object.previous_period) ? globalThis.Number(object.previous_period) : 0,
            stake: isSet(object.stake) ? globalThis.String(object.stake) : "",
            height: isSet(object.height) ? globalThis.Number(object.height) : 0
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.previous_period !== 0) {
            obj.previous_period = Math.round(message.previous_period);
        }
        if (message.stake !== "") {
            obj.stake = message.stake;
        }
        if (message.height !== 0) {
            obj.height = Math.round(message.height);
        }
        return obj;
    },
    create(base) {
        return exports.DelegatorStartingInfo.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseDelegatorStartingInfo();
        message.previous_period = object.previous_period ?? 0;
        message.stake = object.stake ?? "";
        message.height = object.height ?? 0;
        return message;
    }
};
exports.DelegationDelegatorReward = {
    $type: "cosmos.distribution.v1beta1.DelegationDelegatorReward",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.validator_address !== "") {
            writer.uint32(10).string(message.validator_address);
        }
        for (const v of message.reward) {
            coin_1.DecCoin.encode(v, writer.uint32(18).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseDelegationDelegatorReward();
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
                    message.reward.push(coin_1.DecCoin.decode(reader, reader.uint32()));
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
            reward: globalThis.Array.isArray(object?.reward) ? object.reward.map((e) => coin_1.DecCoin.fromJSON(e)) : []
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.validator_address !== "") {
            obj.validator_address = message.validator_address;
        }
        if (message.reward?.length) {
            obj.reward = message.reward.map((e) => coin_1.DecCoin.toJSON(e));
        }
        return obj;
    },
    create(base) {
        return exports.DelegationDelegatorReward.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseDelegationDelegatorReward();
        message.validator_address = object.validator_address ?? "";
        message.reward = object.reward?.map((e) => coin_1.DecCoin.fromPartial(e)) || [];
        return message;
    }
};
exports.CommunityPoolSpendProposalWithDeposit = {
    $type: "cosmos.distribution.v1beta1.CommunityPoolSpendProposalWithDeposit",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.title !== "") {
            writer.uint32(10).string(message.title);
        }
        if (message.description !== "") {
            writer.uint32(18).string(message.description);
        }
        if (message.recipient !== "") {
            writer.uint32(26).string(message.recipient);
        }
        if (message.amount !== "") {
            writer.uint32(34).string(message.amount);
        }
        if (message.deposit !== "") {
            writer.uint32(42).string(message.deposit);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseCommunityPoolSpendProposalWithDeposit();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.title = reader.string();
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.description = reader.string();
                    continue;
                case 3:
                    if (tag !== 26) {
                        break;
                    }
                    message.recipient = reader.string();
                    continue;
                case 4:
                    if (tag !== 34) {
                        break;
                    }
                    message.amount = reader.string();
                    continue;
                case 5:
                    if (tag !== 42) {
                        break;
                    }
                    message.deposit = reader.string();
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
            title: isSet(object.title) ? globalThis.String(object.title) : "",
            description: isSet(object.description) ? globalThis.String(object.description) : "",
            recipient: isSet(object.recipient) ? globalThis.String(object.recipient) : "",
            amount: isSet(object.amount) ? globalThis.String(object.amount) : "",
            deposit: isSet(object.deposit) ? globalThis.String(object.deposit) : ""
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.title !== "") {
            obj.title = message.title;
        }
        if (message.description !== "") {
            obj.description = message.description;
        }
        if (message.recipient !== "") {
            obj.recipient = message.recipient;
        }
        if (message.amount !== "") {
            obj.amount = message.amount;
        }
        if (message.deposit !== "") {
            obj.deposit = message.deposit;
        }
        return obj;
    },
    create(base) {
        return exports.CommunityPoolSpendProposalWithDeposit.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseCommunityPoolSpendProposalWithDeposit();
        message.title = object.title ?? "";
        message.description = object.description ?? "";
        message.recipient = object.recipient ?? "";
        message.amount = object.amount ?? "";
        message.deposit = object.deposit ?? "";
        return message;
    }
};
function createBaseParams() {
    return { community_tax: "", base_proposer_reward: "", bonus_proposer_reward: "", withdraw_addr_enabled: false };
}
function createBaseValidatorHistoricalRewards() {
    return { cumulative_reward_ratio: [], reference_count: 0 };
}
function createBaseValidatorCurrentRewards() {
    return { rewards: [], period: 0 };
}
function createBaseValidatorAccumulatedCommission() {
    return { commission: [] };
}
function createBaseValidatorOutstandingRewards() {
    return { rewards: [] };
}
function createBaseValidatorSlashEvent() {
    return { validator_period: 0, fraction: "" };
}
function createBaseValidatorSlashEvents() {
    return { validator_slash_events: [] };
}
function createBaseFeePool() {
    return { community_pool: [] };
}
function createBaseCommunityPoolSpendProposal() {
    return { title: "", description: "", recipient: "", amount: [] };
}
function createBaseDelegatorStartingInfo() {
    return { previous_period: 0, stake: "", height: 0 };
}
function createBaseDelegationDelegatorReward() {
    return { validator_address: "", reward: [] };
}
function createBaseCommunityPoolSpendProposalWithDeposit() {
    return { title: "", description: "", recipient: "", amount: "", deposit: "" };
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
    ["/cosmos.distribution.v1beta1.Params", exports.Params],
    ["/cosmos.distribution.v1beta1.ValidatorHistoricalRewards", exports.ValidatorHistoricalRewards],
    ["/cosmos.distribution.v1beta1.ValidatorCurrentRewards", exports.ValidatorCurrentRewards],
    ["/cosmos.distribution.v1beta1.ValidatorAccumulatedCommission", exports.ValidatorAccumulatedCommission],
    ["/cosmos.distribution.v1beta1.ValidatorOutstandingRewards", exports.ValidatorOutstandingRewards],
    ["/cosmos.distribution.v1beta1.ValidatorSlashEvent", exports.ValidatorSlashEvent],
    ["/cosmos.distribution.v1beta1.ValidatorSlashEvents", exports.ValidatorSlashEvents],
    ["/cosmos.distribution.v1beta1.FeePool", exports.FeePool],
    ["/cosmos.distribution.v1beta1.CommunityPoolSpendProposal", exports.CommunityPoolSpendProposal],
    ["/cosmos.distribution.v1beta1.DelegatorStartingInfo", exports.DelegatorStartingInfo],
    ["/cosmos.distribution.v1beta1.DelegationDelegatorReward", exports.DelegationDelegatorReward],
    ["/cosmos.distribution.v1beta1.CommunityPoolSpendProposalWithDeposit", exports.CommunityPoolSpendProposalWithDeposit]
];
exports.aminoConverters = {
    "/cosmos.distribution.v1beta1.Params": {
        aminoType: "cosmos-sdk/Params",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.distribution.v1beta1.ValidatorHistoricalRewards": {
        aminoType: "cosmos-sdk/ValidatorHistoricalRewards",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.distribution.v1beta1.ValidatorCurrentRewards": {
        aminoType: "cosmos-sdk/ValidatorCurrentRewards",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.distribution.v1beta1.ValidatorAccumulatedCommission": {
        aminoType: "cosmos-sdk/ValidatorAccumulatedCommission",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.distribution.v1beta1.ValidatorOutstandingRewards": {
        aminoType: "cosmos-sdk/ValidatorOutstandingRewards",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.distribution.v1beta1.ValidatorSlashEvent": {
        aminoType: "cosmos-sdk/ValidatorSlashEvent",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.distribution.v1beta1.ValidatorSlashEvents": {
        aminoType: "cosmos-sdk/ValidatorSlashEvents",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.distribution.v1beta1.FeePool": {
        aminoType: "cosmos-sdk/FeePool",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.distribution.v1beta1.CommunityPoolSpendProposal": {
        aminoType: "cosmos-sdk/CommunityPoolSpendProposal",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.distribution.v1beta1.DelegatorStartingInfo": {
        aminoType: "cosmos-sdk/DelegatorStartingInfo",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.distribution.v1beta1.DelegationDelegatorReward": {
        aminoType: "cosmos-sdk/DelegationDelegatorReward",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.distribution.v1beta1.CommunityPoolSpendProposalWithDeposit": {
        aminoType: "cosmos-sdk/CommunityPoolSpendProposalWithDeposit",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    }
};
