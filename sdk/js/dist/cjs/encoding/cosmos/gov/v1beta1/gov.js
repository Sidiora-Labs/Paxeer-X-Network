"use strict";
Object.defineProperty(exports, "__esModule", { value: true });
exports.aminoConverters = exports.registry = exports.TallyParams = exports.VotingParams = exports.DepositParams = exports.Vote = exports.TallyResult = exports.Proposal = exports.Deposit = exports.TextProposal = exports.WeightedVoteOption = void 0;
exports.voteOptionFromJSON = voteOptionFromJSON;
exports.voteOptionToJSON = voteOptionToJSON;
exports.proposalStatusFromJSON = proposalStatusFromJSON;
exports.proposalStatusToJSON = proposalStatusToJSON;
const wire_1 = require("@bufbuild/protobuf/wire");
const any_1 = require("../../../google/protobuf/any");
const duration_1 = require("../../../google/protobuf/duration");
const timestamp_1 = require("../../../google/protobuf/timestamp");
const coin_1 = require("../../base/v1beta1/coin");
const v1beta1_1 = require("../../../../types/cosmos/gov/v1beta1");
exports.WeightedVoteOption = {
    $type: "cosmos.gov.v1beta1.WeightedVoteOption",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.option !== 0) {
            writer.uint32(8).int32(message.option);
        }
        if (message.weight !== "") {
            writer.uint32(18).string(message.weight);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseWeightedVoteOption();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 8) {
                        break;
                    }
                    message.option = reader.int32();
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.weight = reader.string();
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
            option: isSet(object.option) ? voteOptionFromJSON(object.option) : 0,
            weight: isSet(object.weight) ? globalThis.String(object.weight) : ""
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.option !== 0) {
            obj.option = voteOptionToJSON(message.option);
        }
        if (message.weight !== "") {
            obj.weight = message.weight;
        }
        return obj;
    },
    create(base) {
        return exports.WeightedVoteOption.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseWeightedVoteOption();
        message.option = object.option ?? 0;
        message.weight = object.weight ?? "";
        return message;
    }
};
exports.TextProposal = {
    $type: "cosmos.gov.v1beta1.TextProposal",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.title !== "") {
            writer.uint32(10).string(message.title);
        }
        if (message.description !== "") {
            writer.uint32(18).string(message.description);
        }
        if (message.is_expedited !== false) {
            writer.uint32(24).bool(message.is_expedited);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseTextProposal();
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
                    if (tag !== 24) {
                        break;
                    }
                    message.is_expedited = reader.bool();
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
            is_expedited: isSet(object.is_expedited) ? globalThis.Boolean(object.is_expedited) : false
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
        if (message.is_expedited !== false) {
            obj.is_expedited = message.is_expedited;
        }
        return obj;
    },
    create(base) {
        return exports.TextProposal.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseTextProposal();
        message.title = object.title ?? "";
        message.description = object.description ?? "";
        message.is_expedited = object.is_expedited ?? false;
        return message;
    }
};
exports.Deposit = {
    $type: "cosmos.gov.v1beta1.Deposit",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.proposal_id !== 0) {
            writer.uint32(8).uint64(message.proposal_id);
        }
        if (message.depositor !== "") {
            writer.uint32(18).string(message.depositor);
        }
        for (const v of message.amount) {
            coin_1.Coin.encode(v, writer.uint32(26).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseDeposit();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 8) {
                        break;
                    }
                    message.proposal_id = longToNumber(reader.uint64());
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.depositor = reader.string();
                    continue;
                case 3:
                    if (tag !== 26) {
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
            proposal_id: isSet(object.proposal_id) ? globalThis.Number(object.proposal_id) : 0,
            depositor: isSet(object.depositor) ? globalThis.String(object.depositor) : "",
            amount: globalThis.Array.isArray(object?.amount) ? object.amount.map((e) => coin_1.Coin.fromJSON(e)) : []
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.proposal_id !== 0) {
            obj.proposal_id = Math.round(message.proposal_id);
        }
        if (message.depositor !== "") {
            obj.depositor = message.depositor;
        }
        if (message.amount?.length) {
            obj.amount = message.amount.map((e) => coin_1.Coin.toJSON(e));
        }
        return obj;
    },
    create(base) {
        return exports.Deposit.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseDeposit();
        message.proposal_id = object.proposal_id ?? 0;
        message.depositor = object.depositor ?? "";
        message.amount = object.amount?.map((e) => coin_1.Coin.fromPartial(e)) || [];
        return message;
    }
};
exports.Proposal = {
    $type: "cosmos.gov.v1beta1.Proposal",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.proposal_id !== 0) {
            writer.uint32(8).uint64(message.proposal_id);
        }
        if (message.content !== undefined) {
            any_1.Any.encode(message.content, writer.uint32(18).fork()).join();
        }
        if (message.status !== 0) {
            writer.uint32(24).int32(message.status);
        }
        if (message.final_tally_result !== undefined) {
            exports.TallyResult.encode(message.final_tally_result, writer.uint32(34).fork()).join();
        }
        if (message.submit_time !== undefined) {
            timestamp_1.Timestamp.encode(toTimestamp(message.submit_time), writer.uint32(42).fork()).join();
        }
        if (message.deposit_end_time !== undefined) {
            timestamp_1.Timestamp.encode(toTimestamp(message.deposit_end_time), writer.uint32(50).fork()).join();
        }
        for (const v of message.total_deposit) {
            coin_1.Coin.encode(v, writer.uint32(58).fork()).join();
        }
        if (message.voting_start_time !== undefined) {
            timestamp_1.Timestamp.encode(toTimestamp(message.voting_start_time), writer.uint32(66).fork()).join();
        }
        if (message.voting_end_time !== undefined) {
            timestamp_1.Timestamp.encode(toTimestamp(message.voting_end_time), writer.uint32(74).fork()).join();
        }
        if (message.is_expedited !== false) {
            writer.uint32(80).bool(message.is_expedited);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseProposal();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 8) {
                        break;
                    }
                    message.proposal_id = longToNumber(reader.uint64());
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.content = any_1.Any.decode(reader, reader.uint32());
                    continue;
                case 3:
                    if (tag !== 24) {
                        break;
                    }
                    message.status = reader.int32();
                    continue;
                case 4:
                    if (tag !== 34) {
                        break;
                    }
                    message.final_tally_result = exports.TallyResult.decode(reader, reader.uint32());
                    continue;
                case 5:
                    if (tag !== 42) {
                        break;
                    }
                    message.submit_time = fromTimestamp(timestamp_1.Timestamp.decode(reader, reader.uint32()));
                    continue;
                case 6:
                    if (tag !== 50) {
                        break;
                    }
                    message.deposit_end_time = fromTimestamp(timestamp_1.Timestamp.decode(reader, reader.uint32()));
                    continue;
                case 7:
                    if (tag !== 58) {
                        break;
                    }
                    message.total_deposit.push(coin_1.Coin.decode(reader, reader.uint32()));
                    continue;
                case 8:
                    if (tag !== 66) {
                        break;
                    }
                    message.voting_start_time = fromTimestamp(timestamp_1.Timestamp.decode(reader, reader.uint32()));
                    continue;
                case 9:
                    if (tag !== 74) {
                        break;
                    }
                    message.voting_end_time = fromTimestamp(timestamp_1.Timestamp.decode(reader, reader.uint32()));
                    continue;
                case 10:
                    if (tag !== 80) {
                        break;
                    }
                    message.is_expedited = reader.bool();
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
            proposal_id: isSet(object.proposal_id) ? globalThis.Number(object.proposal_id) : 0,
            content: isSet(object.content) ? any_1.Any.fromJSON(object.content) : undefined,
            status: isSet(object.status) ? proposalStatusFromJSON(object.status) : 0,
            final_tally_result: isSet(object.final_tally_result) ? exports.TallyResult.fromJSON(object.final_tally_result) : undefined,
            submit_time: isSet(object.submit_time) ? fromJsonTimestamp(object.submit_time) : undefined,
            deposit_end_time: isSet(object.deposit_end_time) ? fromJsonTimestamp(object.deposit_end_time) : undefined,
            total_deposit: globalThis.Array.isArray(object?.total_deposit) ? object.total_deposit.map((e) => coin_1.Coin.fromJSON(e)) : [],
            voting_start_time: isSet(object.voting_start_time) ? fromJsonTimestamp(object.voting_start_time) : undefined,
            voting_end_time: isSet(object.voting_end_time) ? fromJsonTimestamp(object.voting_end_time) : undefined,
            is_expedited: isSet(object.is_expedited) ? globalThis.Boolean(object.is_expedited) : false
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.proposal_id !== 0) {
            obj.proposal_id = Math.round(message.proposal_id);
        }
        if (message.content !== undefined) {
            obj.content = any_1.Any.toJSON(message.content);
        }
        if (message.status !== 0) {
            obj.status = proposalStatusToJSON(message.status);
        }
        if (message.final_tally_result !== undefined) {
            obj.final_tally_result = exports.TallyResult.toJSON(message.final_tally_result);
        }
        if (message.submit_time !== undefined) {
            obj.submit_time = message.submit_time.toISOString();
        }
        if (message.deposit_end_time !== undefined) {
            obj.deposit_end_time = message.deposit_end_time.toISOString();
        }
        if (message.total_deposit?.length) {
            obj.total_deposit = message.total_deposit.map((e) => coin_1.Coin.toJSON(e));
        }
        if (message.voting_start_time !== undefined) {
            obj.voting_start_time = message.voting_start_time.toISOString();
        }
        if (message.voting_end_time !== undefined) {
            obj.voting_end_time = message.voting_end_time.toISOString();
        }
        if (message.is_expedited !== false) {
            obj.is_expedited = message.is_expedited;
        }
        return obj;
    },
    create(base) {
        return exports.Proposal.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseProposal();
        message.proposal_id = object.proposal_id ?? 0;
        message.content = object.content !== undefined && object.content !== null ? any_1.Any.fromPartial(object.content) : undefined;
        message.status = object.status ?? 0;
        message.final_tally_result =
            object.final_tally_result !== undefined && object.final_tally_result !== null ? exports.TallyResult.fromPartial(object.final_tally_result) : undefined;
        message.submit_time = object.submit_time ?? undefined;
        message.deposit_end_time = object.deposit_end_time ?? undefined;
        message.total_deposit = object.total_deposit?.map((e) => coin_1.Coin.fromPartial(e)) || [];
        message.voting_start_time = object.voting_start_time ?? undefined;
        message.voting_end_time = object.voting_end_time ?? undefined;
        message.is_expedited = object.is_expedited ?? false;
        return message;
    }
};
exports.TallyResult = {
    $type: "cosmos.gov.v1beta1.TallyResult",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.yes !== "") {
            writer.uint32(10).string(message.yes);
        }
        if (message.abstain !== "") {
            writer.uint32(18).string(message.abstain);
        }
        if (message.no !== "") {
            writer.uint32(26).string(message.no);
        }
        if (message.no_with_veto !== "") {
            writer.uint32(34).string(message.no_with_veto);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseTallyResult();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.yes = reader.string();
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.abstain = reader.string();
                    continue;
                case 3:
                    if (tag !== 26) {
                        break;
                    }
                    message.no = reader.string();
                    continue;
                case 4:
                    if (tag !== 34) {
                        break;
                    }
                    message.no_with_veto = reader.string();
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
            yes: isSet(object.yes) ? globalThis.String(object.yes) : "",
            abstain: isSet(object.abstain) ? globalThis.String(object.abstain) : "",
            no: isSet(object.no) ? globalThis.String(object.no) : "",
            no_with_veto: isSet(object.no_with_veto) ? globalThis.String(object.no_with_veto) : ""
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.yes !== "") {
            obj.yes = message.yes;
        }
        if (message.abstain !== "") {
            obj.abstain = message.abstain;
        }
        if (message.no !== "") {
            obj.no = message.no;
        }
        if (message.no_with_veto !== "") {
            obj.no_with_veto = message.no_with_veto;
        }
        return obj;
    },
    create(base) {
        return exports.TallyResult.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseTallyResult();
        message.yes = object.yes ?? "";
        message.abstain = object.abstain ?? "";
        message.no = object.no ?? "";
        message.no_with_veto = object.no_with_veto ?? "";
        return message;
    }
};
exports.Vote = {
    $type: "cosmos.gov.v1beta1.Vote",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.proposal_id !== 0) {
            writer.uint32(8).uint64(message.proposal_id);
        }
        if (message.voter !== "") {
            writer.uint32(18).string(message.voter);
        }
        if (message.option !== 0) {
            writer.uint32(24).int32(message.option);
        }
        for (const v of message.options) {
            exports.WeightedVoteOption.encode(v, writer.uint32(34).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseVote();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 8) {
                        break;
                    }
                    message.proposal_id = longToNumber(reader.uint64());
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.voter = reader.string();
                    continue;
                case 3:
                    if (tag !== 24) {
                        break;
                    }
                    message.option = reader.int32();
                    continue;
                case 4:
                    if (tag !== 34) {
                        break;
                    }
                    message.options.push(exports.WeightedVoteOption.decode(reader, reader.uint32()));
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
            proposal_id: isSet(object.proposal_id) ? globalThis.Number(object.proposal_id) : 0,
            voter: isSet(object.voter) ? globalThis.String(object.voter) : "",
            option: isSet(object.option) ? voteOptionFromJSON(object.option) : 0,
            options: globalThis.Array.isArray(object?.options) ? object.options.map((e) => exports.WeightedVoteOption.fromJSON(e)) : []
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.proposal_id !== 0) {
            obj.proposal_id = Math.round(message.proposal_id);
        }
        if (message.voter !== "") {
            obj.voter = message.voter;
        }
        if (message.option !== 0) {
            obj.option = voteOptionToJSON(message.option);
        }
        if (message.options?.length) {
            obj.options = message.options.map((e) => exports.WeightedVoteOption.toJSON(e));
        }
        return obj;
    },
    create(base) {
        return exports.Vote.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseVote();
        message.proposal_id = object.proposal_id ?? 0;
        message.voter = object.voter ?? "";
        message.option = object.option ?? 0;
        message.options = object.options?.map((e) => exports.WeightedVoteOption.fromPartial(e)) || [];
        return message;
    }
};
exports.DepositParams = {
    $type: "cosmos.gov.v1beta1.DepositParams",
    encode(message, writer = new wire_1.BinaryWriter()) {
        for (const v of message.min_deposit) {
            coin_1.Coin.encode(v, writer.uint32(10).fork()).join();
        }
        if (message.max_deposit_period !== undefined) {
            duration_1.Duration.encode(message.max_deposit_period, writer.uint32(18).fork()).join();
        }
        for (const v of message.min_expedited_deposit) {
            coin_1.Coin.encode(v, writer.uint32(26).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseDepositParams();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.min_deposit.push(coin_1.Coin.decode(reader, reader.uint32()));
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.max_deposit_period = duration_1.Duration.decode(reader, reader.uint32());
                    continue;
                case 3:
                    if (tag !== 26) {
                        break;
                    }
                    message.min_expedited_deposit.push(coin_1.Coin.decode(reader, reader.uint32()));
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
            min_deposit: globalThis.Array.isArray(object?.min_deposit) ? object.min_deposit.map((e) => coin_1.Coin.fromJSON(e)) : [],
            max_deposit_period: isSet(object.max_deposit_period) ? duration_1.Duration.fromJSON(object.max_deposit_period) : undefined,
            min_expedited_deposit: globalThis.Array.isArray(object?.min_expedited_deposit) ? object.min_expedited_deposit.map((e) => coin_1.Coin.fromJSON(e)) : []
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.min_deposit?.length) {
            obj.min_deposit = message.min_deposit.map((e) => coin_1.Coin.toJSON(e));
        }
        if (message.max_deposit_period !== undefined) {
            obj.max_deposit_period = duration_1.Duration.toJSON(message.max_deposit_period);
        }
        if (message.min_expedited_deposit?.length) {
            obj.min_expedited_deposit = message.min_expedited_deposit.map((e) => coin_1.Coin.toJSON(e));
        }
        return obj;
    },
    create(base) {
        return exports.DepositParams.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseDepositParams();
        message.min_deposit = object.min_deposit?.map((e) => coin_1.Coin.fromPartial(e)) || [];
        message.max_deposit_period =
            object.max_deposit_period !== undefined && object.max_deposit_period !== null ? duration_1.Duration.fromPartial(object.max_deposit_period) : undefined;
        message.min_expedited_deposit = object.min_expedited_deposit?.map((e) => coin_1.Coin.fromPartial(e)) || [];
        return message;
    }
};
exports.VotingParams = {
    $type: "cosmos.gov.v1beta1.VotingParams",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.voting_period !== undefined) {
            duration_1.Duration.encode(message.voting_period, writer.uint32(10).fork()).join();
        }
        if (message.expedited_voting_period !== undefined) {
            duration_1.Duration.encode(message.expedited_voting_period, writer.uint32(18).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseVotingParams();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.voting_period = duration_1.Duration.decode(reader, reader.uint32());
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.expedited_voting_period = duration_1.Duration.decode(reader, reader.uint32());
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
            voting_period: isSet(object.voting_period) ? duration_1.Duration.fromJSON(object.voting_period) : undefined,
            expedited_voting_period: isSet(object.expedited_voting_period) ? duration_1.Duration.fromJSON(object.expedited_voting_period) : undefined
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.voting_period !== undefined) {
            obj.voting_period = duration_1.Duration.toJSON(message.voting_period);
        }
        if (message.expedited_voting_period !== undefined) {
            obj.expedited_voting_period = duration_1.Duration.toJSON(message.expedited_voting_period);
        }
        return obj;
    },
    create(base) {
        return exports.VotingParams.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseVotingParams();
        message.voting_period = object.voting_period !== undefined && object.voting_period !== null ? duration_1.Duration.fromPartial(object.voting_period) : undefined;
        message.expedited_voting_period =
            object.expedited_voting_period !== undefined && object.expedited_voting_period !== null
                ? duration_1.Duration.fromPartial(object.expedited_voting_period)
                : undefined;
        return message;
    }
};
exports.TallyParams = {
    $type: "cosmos.gov.v1beta1.TallyParams",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.quorum.length !== 0) {
            writer.uint32(10).bytes(message.quorum);
        }
        if (message.threshold.length !== 0) {
            writer.uint32(18).bytes(message.threshold);
        }
        if (message.veto_threshold.length !== 0) {
            writer.uint32(26).bytes(message.veto_threshold);
        }
        if (message.expedited_quorum.length !== 0) {
            writer.uint32(34).bytes(message.expedited_quorum);
        }
        if (message.expedited_threshold.length !== 0) {
            writer.uint32(42).bytes(message.expedited_threshold);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseTallyParams();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.quorum = reader.bytes();
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.threshold = reader.bytes();
                    continue;
                case 3:
                    if (tag !== 26) {
                        break;
                    }
                    message.veto_threshold = reader.bytes();
                    continue;
                case 4:
                    if (tag !== 34) {
                        break;
                    }
                    message.expedited_quorum = reader.bytes();
                    continue;
                case 5:
                    if (tag !== 42) {
                        break;
                    }
                    message.expedited_threshold = reader.bytes();
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
            quorum: isSet(object.quorum) ? bytesFromBase64(object.quorum) : new Uint8Array(0),
            threshold: isSet(object.threshold) ? bytesFromBase64(object.threshold) : new Uint8Array(0),
            veto_threshold: isSet(object.veto_threshold) ? bytesFromBase64(object.veto_threshold) : new Uint8Array(0),
            expedited_quorum: isSet(object.expedited_quorum) ? bytesFromBase64(object.expedited_quorum) : new Uint8Array(0),
            expedited_threshold: isSet(object.expedited_threshold) ? bytesFromBase64(object.expedited_threshold) : new Uint8Array(0)
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.quorum.length !== 0) {
            obj.quorum = base64FromBytes(message.quorum);
        }
        if (message.threshold.length !== 0) {
            obj.threshold = base64FromBytes(message.threshold);
        }
        if (message.veto_threshold.length !== 0) {
            obj.veto_threshold = base64FromBytes(message.veto_threshold);
        }
        if (message.expedited_quorum.length !== 0) {
            obj.expedited_quorum = base64FromBytes(message.expedited_quorum);
        }
        if (message.expedited_threshold.length !== 0) {
            obj.expedited_threshold = base64FromBytes(message.expedited_threshold);
        }
        return obj;
    },
    create(base) {
        return exports.TallyParams.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseTallyParams();
        message.quorum = object.quorum ?? new Uint8Array(0);
        message.threshold = object.threshold ?? new Uint8Array(0);
        message.veto_threshold = object.veto_threshold ?? new Uint8Array(0);
        message.expedited_quorum = object.expedited_quorum ?? new Uint8Array(0);
        message.expedited_threshold = object.expedited_threshold ?? new Uint8Array(0);
        return message;
    }
};
function voteOptionFromJSON(object) {
    switch (object) {
        case 0:
        case "VOTE_OPTION_UNSPECIFIED":
            return v1beta1_1.VoteOption.VOTE_OPTION_UNSPECIFIED;
        case 1:
        case "VOTE_OPTION_YES":
            return v1beta1_1.VoteOption.VOTE_OPTION_YES;
        case 2:
        case "VOTE_OPTION_ABSTAIN":
            return v1beta1_1.VoteOption.VOTE_OPTION_ABSTAIN;
        case 3:
        case "VOTE_OPTION_NO":
            return v1beta1_1.VoteOption.VOTE_OPTION_NO;
        case 4:
        case "VOTE_OPTION_NO_WITH_VETO":
            return v1beta1_1.VoteOption.VOTE_OPTION_NO_WITH_VETO;
        case -1:
        case "UNRECOGNIZED":
        default:
            return v1beta1_1.VoteOption.UNRECOGNIZED;
    }
}
function voteOptionToJSON(object) {
    switch (object) {
        case v1beta1_1.VoteOption.VOTE_OPTION_UNSPECIFIED:
            return "VOTE_OPTION_UNSPECIFIED";
        case v1beta1_1.VoteOption.VOTE_OPTION_YES:
            return "VOTE_OPTION_YES";
        case v1beta1_1.VoteOption.VOTE_OPTION_ABSTAIN:
            return "VOTE_OPTION_ABSTAIN";
        case v1beta1_1.VoteOption.VOTE_OPTION_NO:
            return "VOTE_OPTION_NO";
        case v1beta1_1.VoteOption.VOTE_OPTION_NO_WITH_VETO:
            return "VOTE_OPTION_NO_WITH_VETO";
        case v1beta1_1.VoteOption.UNRECOGNIZED:
        default:
            return "UNRECOGNIZED";
    }
}
function proposalStatusFromJSON(object) {
    switch (object) {
        case 0:
        case "PROPOSAL_STATUS_UNSPECIFIED":
            return v1beta1_1.ProposalStatus.PROPOSAL_STATUS_UNSPECIFIED;
        case 1:
        case "PROPOSAL_STATUS_DEPOSIT_PERIOD":
            return v1beta1_1.ProposalStatus.PROPOSAL_STATUS_DEPOSIT_PERIOD;
        case 2:
        case "PROPOSAL_STATUS_VOTING_PERIOD":
            return v1beta1_1.ProposalStatus.PROPOSAL_STATUS_VOTING_PERIOD;
        case 3:
        case "PROPOSAL_STATUS_PASSED":
            return v1beta1_1.ProposalStatus.PROPOSAL_STATUS_PASSED;
        case 4:
        case "PROPOSAL_STATUS_REJECTED":
            return v1beta1_1.ProposalStatus.PROPOSAL_STATUS_REJECTED;
        case 5:
        case "PROPOSAL_STATUS_FAILED":
            return v1beta1_1.ProposalStatus.PROPOSAL_STATUS_FAILED;
        case -1:
        case "UNRECOGNIZED":
        default:
            return v1beta1_1.ProposalStatus.UNRECOGNIZED;
    }
}
function proposalStatusToJSON(object) {
    switch (object) {
        case v1beta1_1.ProposalStatus.PROPOSAL_STATUS_UNSPECIFIED:
            return "PROPOSAL_STATUS_UNSPECIFIED";
        case v1beta1_1.ProposalStatus.PROPOSAL_STATUS_DEPOSIT_PERIOD:
            return "PROPOSAL_STATUS_DEPOSIT_PERIOD";
        case v1beta1_1.ProposalStatus.PROPOSAL_STATUS_VOTING_PERIOD:
            return "PROPOSAL_STATUS_VOTING_PERIOD";
        case v1beta1_1.ProposalStatus.PROPOSAL_STATUS_PASSED:
            return "PROPOSAL_STATUS_PASSED";
        case v1beta1_1.ProposalStatus.PROPOSAL_STATUS_REJECTED:
            return "PROPOSAL_STATUS_REJECTED";
        case v1beta1_1.ProposalStatus.PROPOSAL_STATUS_FAILED:
            return "PROPOSAL_STATUS_FAILED";
        case v1beta1_1.ProposalStatus.UNRECOGNIZED:
        default:
            return "UNRECOGNIZED";
    }
}
function createBaseWeightedVoteOption() {
    return { option: 0, weight: "" };
}
function createBaseTextProposal() {
    return { title: "", description: "", is_expedited: false };
}
function createBaseDeposit() {
    return { proposal_id: 0, depositor: "", amount: [] };
}
function createBaseProposal() {
    return {
        proposal_id: 0,
        content: undefined,
        status: 0,
        final_tally_result: undefined,
        submit_time: undefined,
        deposit_end_time: undefined,
        total_deposit: [],
        voting_start_time: undefined,
        voting_end_time: undefined,
        is_expedited: false
    };
}
function createBaseTallyResult() {
    return { yes: "", abstain: "", no: "", no_with_veto: "" };
}
function createBaseVote() {
    return { proposal_id: 0, voter: "", option: 0, options: [] };
}
function createBaseDepositParams() {
    return { min_deposit: [], max_deposit_period: undefined, min_expedited_deposit: [] };
}
function createBaseVotingParams() {
    return { voting_period: undefined, expedited_voting_period: undefined };
}
function createBaseTallyParams() {
    return {
        quorum: new Uint8Array(0),
        threshold: new Uint8Array(0),
        veto_threshold: new Uint8Array(0),
        expedited_quorum: new Uint8Array(0),
        expedited_threshold: new Uint8Array(0)
    };
}
function bytesFromBase64(b64) {
    if (globalThis.Buffer) {
        return Uint8Array.from(globalThis.Buffer.from(b64, "base64"));
    }
    else {
        const bin = globalThis.atob(b64);
        const arr = new Uint8Array(bin.length);
        for (let i = 0; i < bin.length; ++i) {
            arr[i] = bin.charCodeAt(i);
        }
        return arr;
    }
}
function base64FromBytes(arr) {
    if (globalThis.Buffer) {
        return globalThis.Buffer.from(arr).toString("base64");
    }
    else {
        const bin = [];
        arr.forEach((byte) => {
            bin.push(globalThis.String.fromCharCode(byte));
        });
        return globalThis.btoa(bin.join(""));
    }
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
    ["/cosmos.gov.v1beta1.WeightedVoteOption", exports.WeightedVoteOption],
    ["/cosmos.gov.v1beta1.TextProposal", exports.TextProposal],
    ["/cosmos.gov.v1beta1.Deposit", exports.Deposit],
    ["/cosmos.gov.v1beta1.Proposal", exports.Proposal],
    ["/cosmos.gov.v1beta1.TallyResult", exports.TallyResult],
    ["/cosmos.gov.v1beta1.Vote", exports.Vote],
    ["/cosmos.gov.v1beta1.DepositParams", exports.DepositParams],
    ["/cosmos.gov.v1beta1.VotingParams", exports.VotingParams],
    ["/cosmos.gov.v1beta1.TallyParams", exports.TallyParams]
];
exports.aminoConverters = {
    "/cosmos.gov.v1beta1.WeightedVoteOption": {
        aminoType: "cosmos-sdk/WeightedVoteOption",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.gov.v1beta1.TextProposal": {
        aminoType: "cosmos-sdk/TextProposal",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.gov.v1beta1.Deposit": {
        aminoType: "cosmos-sdk/Deposit",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.gov.v1beta1.Proposal": {
        aminoType: "cosmos-sdk/Proposal",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.gov.v1beta1.TallyResult": {
        aminoType: "cosmos-sdk/TallyResult",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.gov.v1beta1.Vote": {
        aminoType: "cosmos-sdk/Vote",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.gov.v1beta1.DepositParams": {
        aminoType: "cosmos-sdk/DepositParams",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.gov.v1beta1.VotingParams": {
        aminoType: "cosmos-sdk/VotingParams",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.gov.v1beta1.TallyParams": {
        aminoType: "cosmos-sdk/TallyParams",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    }
};
