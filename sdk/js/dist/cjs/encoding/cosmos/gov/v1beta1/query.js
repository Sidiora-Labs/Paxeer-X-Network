"use strict";
Object.defineProperty(exports, "__esModule", { value: true });
exports.aminoConverters = exports.registry = exports.QueryTallyResultResponse = exports.QueryTallyResultRequest = exports.QueryDepositsResponse = exports.QueryDepositsRequest = exports.QueryDepositResponse = exports.QueryDepositRequest = exports.QueryParamsResponse = exports.QueryParamsRequest = exports.QueryVotesResponse = exports.QueryVotesRequest = exports.QueryVoteResponse = exports.QueryVoteRequest = exports.QueryProposalsResponse = exports.QueryProposalsRequest = exports.QueryProposalResponse = exports.QueryProposalRequest = void 0;
const wire_1 = require("@bufbuild/protobuf/wire");
const pagination_1 = require("../../base/query/v1beta1/pagination");
const gov_1 = require("./gov");
exports.QueryProposalRequest = {
    $type: "cosmos.gov.v1beta1.QueryProposalRequest",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.proposal_id !== 0) {
            writer.uint32(8).uint64(message.proposal_id);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseQueryProposalRequest();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 8) {
                        break;
                    }
                    message.proposal_id = longToNumber(reader.uint64());
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
        return { proposal_id: isSet(object.proposal_id) ? globalThis.Number(object.proposal_id) : 0 };
    },
    toJSON(message) {
        const obj = {};
        if (message.proposal_id !== 0) {
            obj.proposal_id = Math.round(message.proposal_id);
        }
        return obj;
    },
    create(base) {
        return exports.QueryProposalRequest.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryProposalRequest();
        message.proposal_id = object.proposal_id ?? 0;
        return message;
    }
};
exports.QueryProposalResponse = {
    $type: "cosmos.gov.v1beta1.QueryProposalResponse",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.proposal !== undefined) {
            gov_1.Proposal.encode(message.proposal, writer.uint32(10).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseQueryProposalResponse();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.proposal = gov_1.Proposal.decode(reader, reader.uint32());
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
        return { proposal: isSet(object.proposal) ? gov_1.Proposal.fromJSON(object.proposal) : undefined };
    },
    toJSON(message) {
        const obj = {};
        if (message.proposal !== undefined) {
            obj.proposal = gov_1.Proposal.toJSON(message.proposal);
        }
        return obj;
    },
    create(base) {
        return exports.QueryProposalResponse.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryProposalResponse();
        message.proposal = object.proposal !== undefined && object.proposal !== null ? gov_1.Proposal.fromPartial(object.proposal) : undefined;
        return message;
    }
};
exports.QueryProposalsRequest = {
    $type: "cosmos.gov.v1beta1.QueryProposalsRequest",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.proposal_status !== 0) {
            writer.uint32(8).int32(message.proposal_status);
        }
        if (message.voter !== "") {
            writer.uint32(18).string(message.voter);
        }
        if (message.depositor !== "") {
            writer.uint32(26).string(message.depositor);
        }
        if (message.pagination !== undefined) {
            pagination_1.PageRequest.encode(message.pagination, writer.uint32(34).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseQueryProposalsRequest();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 8) {
                        break;
                    }
                    message.proposal_status = reader.int32();
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.voter = reader.string();
                    continue;
                case 3:
                    if (tag !== 26) {
                        break;
                    }
                    message.depositor = reader.string();
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
            proposal_status: isSet(object.proposal_status) ? (0, gov_1.proposalStatusFromJSON)(object.proposal_status) : 0,
            voter: isSet(object.voter) ? globalThis.String(object.voter) : "",
            depositor: isSet(object.depositor) ? globalThis.String(object.depositor) : "",
            pagination: isSet(object.pagination) ? pagination_1.PageRequest.fromJSON(object.pagination) : undefined
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.proposal_status !== 0) {
            obj.proposal_status = (0, gov_1.proposalStatusToJSON)(message.proposal_status);
        }
        if (message.voter !== "") {
            obj.voter = message.voter;
        }
        if (message.depositor !== "") {
            obj.depositor = message.depositor;
        }
        if (message.pagination !== undefined) {
            obj.pagination = pagination_1.PageRequest.toJSON(message.pagination);
        }
        return obj;
    },
    create(base) {
        return exports.QueryProposalsRequest.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryProposalsRequest();
        message.proposal_status = object.proposal_status ?? 0;
        message.voter = object.voter ?? "";
        message.depositor = object.depositor ?? "";
        message.pagination = object.pagination !== undefined && object.pagination !== null ? pagination_1.PageRequest.fromPartial(object.pagination) : undefined;
        return message;
    }
};
exports.QueryProposalsResponse = {
    $type: "cosmos.gov.v1beta1.QueryProposalsResponse",
    encode(message, writer = new wire_1.BinaryWriter()) {
        for (const v of message.proposals) {
            gov_1.Proposal.encode(v, writer.uint32(10).fork()).join();
        }
        if (message.pagination !== undefined) {
            pagination_1.PageResponse.encode(message.pagination, writer.uint32(18).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseQueryProposalsResponse();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.proposals.push(gov_1.Proposal.decode(reader, reader.uint32()));
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
            proposals: globalThis.Array.isArray(object?.proposals) ? object.proposals.map((e) => gov_1.Proposal.fromJSON(e)) : [],
            pagination: isSet(object.pagination) ? pagination_1.PageResponse.fromJSON(object.pagination) : undefined
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.proposals?.length) {
            obj.proposals = message.proposals.map((e) => gov_1.Proposal.toJSON(e));
        }
        if (message.pagination !== undefined) {
            obj.pagination = pagination_1.PageResponse.toJSON(message.pagination);
        }
        return obj;
    },
    create(base) {
        return exports.QueryProposalsResponse.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryProposalsResponse();
        message.proposals = object.proposals?.map((e) => gov_1.Proposal.fromPartial(e)) || [];
        message.pagination = object.pagination !== undefined && object.pagination !== null ? pagination_1.PageResponse.fromPartial(object.pagination) : undefined;
        return message;
    }
};
exports.QueryVoteRequest = {
    $type: "cosmos.gov.v1beta1.QueryVoteRequest",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.proposal_id !== 0) {
            writer.uint32(8).uint64(message.proposal_id);
        }
        if (message.voter !== "") {
            writer.uint32(18).string(message.voter);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseQueryVoteRequest();
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
            voter: isSet(object.voter) ? globalThis.String(object.voter) : ""
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
        return obj;
    },
    create(base) {
        return exports.QueryVoteRequest.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryVoteRequest();
        message.proposal_id = object.proposal_id ?? 0;
        message.voter = object.voter ?? "";
        return message;
    }
};
exports.QueryVoteResponse = {
    $type: "cosmos.gov.v1beta1.QueryVoteResponse",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.vote !== undefined) {
            gov_1.Vote.encode(message.vote, writer.uint32(10).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseQueryVoteResponse();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.vote = gov_1.Vote.decode(reader, reader.uint32());
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
        return { vote: isSet(object.vote) ? gov_1.Vote.fromJSON(object.vote) : undefined };
    },
    toJSON(message) {
        const obj = {};
        if (message.vote !== undefined) {
            obj.vote = gov_1.Vote.toJSON(message.vote);
        }
        return obj;
    },
    create(base) {
        return exports.QueryVoteResponse.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryVoteResponse();
        message.vote = object.vote !== undefined && object.vote !== null ? gov_1.Vote.fromPartial(object.vote) : undefined;
        return message;
    }
};
exports.QueryVotesRequest = {
    $type: "cosmos.gov.v1beta1.QueryVotesRequest",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.proposal_id !== 0) {
            writer.uint32(8).uint64(message.proposal_id);
        }
        if (message.pagination !== undefined) {
            pagination_1.PageRequest.encode(message.pagination, writer.uint32(18).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseQueryVotesRequest();
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
            proposal_id: isSet(object.proposal_id) ? globalThis.Number(object.proposal_id) : 0,
            pagination: isSet(object.pagination) ? pagination_1.PageRequest.fromJSON(object.pagination) : undefined
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.proposal_id !== 0) {
            obj.proposal_id = Math.round(message.proposal_id);
        }
        if (message.pagination !== undefined) {
            obj.pagination = pagination_1.PageRequest.toJSON(message.pagination);
        }
        return obj;
    },
    create(base) {
        return exports.QueryVotesRequest.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryVotesRequest();
        message.proposal_id = object.proposal_id ?? 0;
        message.pagination = object.pagination !== undefined && object.pagination !== null ? pagination_1.PageRequest.fromPartial(object.pagination) : undefined;
        return message;
    }
};
exports.QueryVotesResponse = {
    $type: "cosmos.gov.v1beta1.QueryVotesResponse",
    encode(message, writer = new wire_1.BinaryWriter()) {
        for (const v of message.votes) {
            gov_1.Vote.encode(v, writer.uint32(10).fork()).join();
        }
        if (message.pagination !== undefined) {
            pagination_1.PageResponse.encode(message.pagination, writer.uint32(18).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseQueryVotesResponse();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.votes.push(gov_1.Vote.decode(reader, reader.uint32()));
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
            votes: globalThis.Array.isArray(object?.votes) ? object.votes.map((e) => gov_1.Vote.fromJSON(e)) : [],
            pagination: isSet(object.pagination) ? pagination_1.PageResponse.fromJSON(object.pagination) : undefined
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.votes?.length) {
            obj.votes = message.votes.map((e) => gov_1.Vote.toJSON(e));
        }
        if (message.pagination !== undefined) {
            obj.pagination = pagination_1.PageResponse.toJSON(message.pagination);
        }
        return obj;
    },
    create(base) {
        return exports.QueryVotesResponse.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryVotesResponse();
        message.votes = object.votes?.map((e) => gov_1.Vote.fromPartial(e)) || [];
        message.pagination = object.pagination !== undefined && object.pagination !== null ? pagination_1.PageResponse.fromPartial(object.pagination) : undefined;
        return message;
    }
};
exports.QueryParamsRequest = {
    $type: "cosmos.gov.v1beta1.QueryParamsRequest",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.params_type !== "") {
            writer.uint32(10).string(message.params_type);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseQueryParamsRequest();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.params_type = reader.string();
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
        return { params_type: isSet(object.params_type) ? globalThis.String(object.params_type) : "" };
    },
    toJSON(message) {
        const obj = {};
        if (message.params_type !== "") {
            obj.params_type = message.params_type;
        }
        return obj;
    },
    create(base) {
        return exports.QueryParamsRequest.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryParamsRequest();
        message.params_type = object.params_type ?? "";
        return message;
    }
};
exports.QueryParamsResponse = {
    $type: "cosmos.gov.v1beta1.QueryParamsResponse",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.voting_params !== undefined) {
            gov_1.VotingParams.encode(message.voting_params, writer.uint32(10).fork()).join();
        }
        if (message.deposit_params !== undefined) {
            gov_1.DepositParams.encode(message.deposit_params, writer.uint32(18).fork()).join();
        }
        if (message.tally_params !== undefined) {
            gov_1.TallyParams.encode(message.tally_params, writer.uint32(26).fork()).join();
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
                    message.voting_params = gov_1.VotingParams.decode(reader, reader.uint32());
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.deposit_params = gov_1.DepositParams.decode(reader, reader.uint32());
                    continue;
                case 3:
                    if (tag !== 26) {
                        break;
                    }
                    message.tally_params = gov_1.TallyParams.decode(reader, reader.uint32());
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
            voting_params: isSet(object.voting_params) ? gov_1.VotingParams.fromJSON(object.voting_params) : undefined,
            deposit_params: isSet(object.deposit_params) ? gov_1.DepositParams.fromJSON(object.deposit_params) : undefined,
            tally_params: isSet(object.tally_params) ? gov_1.TallyParams.fromJSON(object.tally_params) : undefined
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.voting_params !== undefined) {
            obj.voting_params = gov_1.VotingParams.toJSON(message.voting_params);
        }
        if (message.deposit_params !== undefined) {
            obj.deposit_params = gov_1.DepositParams.toJSON(message.deposit_params);
        }
        if (message.tally_params !== undefined) {
            obj.tally_params = gov_1.TallyParams.toJSON(message.tally_params);
        }
        return obj;
    },
    create(base) {
        return exports.QueryParamsResponse.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryParamsResponse();
        message.voting_params = object.voting_params !== undefined && object.voting_params !== null ? gov_1.VotingParams.fromPartial(object.voting_params) : undefined;
        message.deposit_params =
            object.deposit_params !== undefined && object.deposit_params !== null ? gov_1.DepositParams.fromPartial(object.deposit_params) : undefined;
        message.tally_params = object.tally_params !== undefined && object.tally_params !== null ? gov_1.TallyParams.fromPartial(object.tally_params) : undefined;
        return message;
    }
};
exports.QueryDepositRequest = {
    $type: "cosmos.gov.v1beta1.QueryDepositRequest",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.proposal_id !== 0) {
            writer.uint32(8).uint64(message.proposal_id);
        }
        if (message.depositor !== "") {
            writer.uint32(18).string(message.depositor);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseQueryDepositRequest();
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
            depositor: isSet(object.depositor) ? globalThis.String(object.depositor) : ""
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
        return obj;
    },
    create(base) {
        return exports.QueryDepositRequest.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryDepositRequest();
        message.proposal_id = object.proposal_id ?? 0;
        message.depositor = object.depositor ?? "";
        return message;
    }
};
exports.QueryDepositResponse = {
    $type: "cosmos.gov.v1beta1.QueryDepositResponse",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.deposit !== undefined) {
            gov_1.Deposit.encode(message.deposit, writer.uint32(10).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseQueryDepositResponse();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.deposit = gov_1.Deposit.decode(reader, reader.uint32());
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
        return { deposit: isSet(object.deposit) ? gov_1.Deposit.fromJSON(object.deposit) : undefined };
    },
    toJSON(message) {
        const obj = {};
        if (message.deposit !== undefined) {
            obj.deposit = gov_1.Deposit.toJSON(message.deposit);
        }
        return obj;
    },
    create(base) {
        return exports.QueryDepositResponse.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryDepositResponse();
        message.deposit = object.deposit !== undefined && object.deposit !== null ? gov_1.Deposit.fromPartial(object.deposit) : undefined;
        return message;
    }
};
exports.QueryDepositsRequest = {
    $type: "cosmos.gov.v1beta1.QueryDepositsRequest",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.proposal_id !== 0) {
            writer.uint32(8).uint64(message.proposal_id);
        }
        if (message.pagination !== undefined) {
            pagination_1.PageRequest.encode(message.pagination, writer.uint32(18).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseQueryDepositsRequest();
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
            proposal_id: isSet(object.proposal_id) ? globalThis.Number(object.proposal_id) : 0,
            pagination: isSet(object.pagination) ? pagination_1.PageRequest.fromJSON(object.pagination) : undefined
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.proposal_id !== 0) {
            obj.proposal_id = Math.round(message.proposal_id);
        }
        if (message.pagination !== undefined) {
            obj.pagination = pagination_1.PageRequest.toJSON(message.pagination);
        }
        return obj;
    },
    create(base) {
        return exports.QueryDepositsRequest.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryDepositsRequest();
        message.proposal_id = object.proposal_id ?? 0;
        message.pagination = object.pagination !== undefined && object.pagination !== null ? pagination_1.PageRequest.fromPartial(object.pagination) : undefined;
        return message;
    }
};
exports.QueryDepositsResponse = {
    $type: "cosmos.gov.v1beta1.QueryDepositsResponse",
    encode(message, writer = new wire_1.BinaryWriter()) {
        for (const v of message.deposits) {
            gov_1.Deposit.encode(v, writer.uint32(10).fork()).join();
        }
        if (message.pagination !== undefined) {
            pagination_1.PageResponse.encode(message.pagination, writer.uint32(18).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseQueryDepositsResponse();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.deposits.push(gov_1.Deposit.decode(reader, reader.uint32()));
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
            deposits: globalThis.Array.isArray(object?.deposits) ? object.deposits.map((e) => gov_1.Deposit.fromJSON(e)) : [],
            pagination: isSet(object.pagination) ? pagination_1.PageResponse.fromJSON(object.pagination) : undefined
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.deposits?.length) {
            obj.deposits = message.deposits.map((e) => gov_1.Deposit.toJSON(e));
        }
        if (message.pagination !== undefined) {
            obj.pagination = pagination_1.PageResponse.toJSON(message.pagination);
        }
        return obj;
    },
    create(base) {
        return exports.QueryDepositsResponse.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryDepositsResponse();
        message.deposits = object.deposits?.map((e) => gov_1.Deposit.fromPartial(e)) || [];
        message.pagination = object.pagination !== undefined && object.pagination !== null ? pagination_1.PageResponse.fromPartial(object.pagination) : undefined;
        return message;
    }
};
exports.QueryTallyResultRequest = {
    $type: "cosmos.gov.v1beta1.QueryTallyResultRequest",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.proposal_id !== 0) {
            writer.uint32(8).uint64(message.proposal_id);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseQueryTallyResultRequest();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 8) {
                        break;
                    }
                    message.proposal_id = longToNumber(reader.uint64());
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
        return { proposal_id: isSet(object.proposal_id) ? globalThis.Number(object.proposal_id) : 0 };
    },
    toJSON(message) {
        const obj = {};
        if (message.proposal_id !== 0) {
            obj.proposal_id = Math.round(message.proposal_id);
        }
        return obj;
    },
    create(base) {
        return exports.QueryTallyResultRequest.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryTallyResultRequest();
        message.proposal_id = object.proposal_id ?? 0;
        return message;
    }
};
exports.QueryTallyResultResponse = {
    $type: "cosmos.gov.v1beta1.QueryTallyResultResponse",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.tally !== undefined) {
            gov_1.TallyResult.encode(message.tally, writer.uint32(10).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseQueryTallyResultResponse();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.tally = gov_1.TallyResult.decode(reader, reader.uint32());
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
        return { tally: isSet(object.tally) ? gov_1.TallyResult.fromJSON(object.tally) : undefined };
    },
    toJSON(message) {
        const obj = {};
        if (message.tally !== undefined) {
            obj.tally = gov_1.TallyResult.toJSON(message.tally);
        }
        return obj;
    },
    create(base) {
        return exports.QueryTallyResultResponse.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryTallyResultResponse();
        message.tally = object.tally !== undefined && object.tally !== null ? gov_1.TallyResult.fromPartial(object.tally) : undefined;
        return message;
    }
};
function createBaseQueryProposalRequest() {
    return { proposal_id: 0 };
}
function createBaseQueryProposalResponse() {
    return { proposal: undefined };
}
function createBaseQueryProposalsRequest() {
    return { proposal_status: 0, voter: "", depositor: "", pagination: undefined };
}
function createBaseQueryProposalsResponse() {
    return { proposals: [], pagination: undefined };
}
function createBaseQueryVoteRequest() {
    return { proposal_id: 0, voter: "" };
}
function createBaseQueryVoteResponse() {
    return { vote: undefined };
}
function createBaseQueryVotesRequest() {
    return { proposal_id: 0, pagination: undefined };
}
function createBaseQueryVotesResponse() {
    return { votes: [], pagination: undefined };
}
function createBaseQueryParamsRequest() {
    return { params_type: "" };
}
function createBaseQueryParamsResponse() {
    return { voting_params: undefined, deposit_params: undefined, tally_params: undefined };
}
function createBaseQueryDepositRequest() {
    return { proposal_id: 0, depositor: "" };
}
function createBaseQueryDepositResponse() {
    return { deposit: undefined };
}
function createBaseQueryDepositsRequest() {
    return { proposal_id: 0, pagination: undefined };
}
function createBaseQueryDepositsResponse() {
    return { deposits: [], pagination: undefined };
}
function createBaseQueryTallyResultRequest() {
    return { proposal_id: 0 };
}
function createBaseQueryTallyResultResponse() {
    return { tally: undefined };
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
    ["/cosmos.gov.v1beta1.QueryProposalRequest", exports.QueryProposalRequest],
    ["/cosmos.gov.v1beta1.QueryProposalResponse", exports.QueryProposalResponse],
    ["/cosmos.gov.v1beta1.QueryProposalsRequest", exports.QueryProposalsRequest],
    ["/cosmos.gov.v1beta1.QueryProposalsResponse", exports.QueryProposalsResponse],
    ["/cosmos.gov.v1beta1.QueryVoteRequest", exports.QueryVoteRequest],
    ["/cosmos.gov.v1beta1.QueryVoteResponse", exports.QueryVoteResponse],
    ["/cosmos.gov.v1beta1.QueryVotesRequest", exports.QueryVotesRequest],
    ["/cosmos.gov.v1beta1.QueryVotesResponse", exports.QueryVotesResponse],
    ["/cosmos.gov.v1beta1.QueryParamsRequest", exports.QueryParamsRequest],
    ["/cosmos.gov.v1beta1.QueryParamsResponse", exports.QueryParamsResponse],
    ["/cosmos.gov.v1beta1.QueryDepositRequest", exports.QueryDepositRequest],
    ["/cosmos.gov.v1beta1.QueryDepositResponse", exports.QueryDepositResponse],
    ["/cosmos.gov.v1beta1.QueryDepositsRequest", exports.QueryDepositsRequest],
    ["/cosmos.gov.v1beta1.QueryDepositsResponse", exports.QueryDepositsResponse],
    ["/cosmos.gov.v1beta1.QueryTallyResultRequest", exports.QueryTallyResultRequest],
    ["/cosmos.gov.v1beta1.QueryTallyResultResponse", exports.QueryTallyResultResponse]
];
exports.aminoConverters = {
    "/cosmos.gov.v1beta1.QueryProposalRequest": {
        aminoType: "cosmos-sdk/QueryProposalRequest",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.gov.v1beta1.QueryProposalResponse": {
        aminoType: "cosmos-sdk/QueryProposalResponse",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.gov.v1beta1.QueryProposalsRequest": {
        aminoType: "cosmos-sdk/QueryProposalsRequest",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.gov.v1beta1.QueryProposalsResponse": {
        aminoType: "cosmos-sdk/QueryProposalsResponse",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.gov.v1beta1.QueryVoteRequest": {
        aminoType: "cosmos-sdk/QueryVoteRequest",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.gov.v1beta1.QueryVoteResponse": {
        aminoType: "cosmos-sdk/QueryVoteResponse",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.gov.v1beta1.QueryVotesRequest": {
        aminoType: "cosmos-sdk/QueryVotesRequest",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.gov.v1beta1.QueryVotesResponse": {
        aminoType: "cosmos-sdk/QueryVotesResponse",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.gov.v1beta1.QueryParamsRequest": {
        aminoType: "cosmos-sdk/QueryParamsRequest",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.gov.v1beta1.QueryParamsResponse": {
        aminoType: "cosmos-sdk/QueryParamsResponse",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.gov.v1beta1.QueryDepositRequest": {
        aminoType: "cosmos-sdk/QueryDepositRequest",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.gov.v1beta1.QueryDepositResponse": {
        aminoType: "cosmos-sdk/QueryDepositResponse",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.gov.v1beta1.QueryDepositsRequest": {
        aminoType: "cosmos-sdk/QueryDepositsRequest",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.gov.v1beta1.QueryDepositsResponse": {
        aminoType: "cosmos-sdk/QueryDepositsResponse",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.gov.v1beta1.QueryTallyResultRequest": {
        aminoType: "cosmos-sdk/QueryTallyResultRequest",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.gov.v1beta1.QueryTallyResultResponse": {
        aminoType: "cosmos-sdk/QueryTallyResultResponse",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    }
};
