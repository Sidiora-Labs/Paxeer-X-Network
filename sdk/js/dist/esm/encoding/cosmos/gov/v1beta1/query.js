import { BinaryReader, BinaryWriter } from "@bufbuild/protobuf/wire";
import { PageRequest, PageResponse } from "../../base/query/v1beta1/pagination";
import { Deposit, DepositParams, Proposal, TallyParams, TallyResult, Vote, VotingParams, proposalStatusFromJSON, proposalStatusToJSON } from "./gov";
export const QueryProposalRequest = {
    $type: "cosmos.gov.v1beta1.QueryProposalRequest",
    encode(message, writer = new BinaryWriter()) {
        if (message.proposal_id !== 0) {
            writer.uint32(8).uint64(message.proposal_id);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
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
        return QueryProposalRequest.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryProposalRequest();
        message.proposal_id = object.proposal_id ?? 0;
        return message;
    }
};
export const QueryProposalResponse = {
    $type: "cosmos.gov.v1beta1.QueryProposalResponse",
    encode(message, writer = new BinaryWriter()) {
        if (message.proposal !== undefined) {
            Proposal.encode(message.proposal, writer.uint32(10).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseQueryProposalResponse();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.proposal = Proposal.decode(reader, reader.uint32());
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
        return { proposal: isSet(object.proposal) ? Proposal.fromJSON(object.proposal) : undefined };
    },
    toJSON(message) {
        const obj = {};
        if (message.proposal !== undefined) {
            obj.proposal = Proposal.toJSON(message.proposal);
        }
        return obj;
    },
    create(base) {
        return QueryProposalResponse.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryProposalResponse();
        message.proposal = object.proposal !== undefined && object.proposal !== null ? Proposal.fromPartial(object.proposal) : undefined;
        return message;
    }
};
export const QueryProposalsRequest = {
    $type: "cosmos.gov.v1beta1.QueryProposalsRequest",
    encode(message, writer = new BinaryWriter()) {
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
            PageRequest.encode(message.pagination, writer.uint32(34).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
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
            proposal_status: isSet(object.proposal_status) ? proposalStatusFromJSON(object.proposal_status) : 0,
            voter: isSet(object.voter) ? globalThis.String(object.voter) : "",
            depositor: isSet(object.depositor) ? globalThis.String(object.depositor) : "",
            pagination: isSet(object.pagination) ? PageRequest.fromJSON(object.pagination) : undefined
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.proposal_status !== 0) {
            obj.proposal_status = proposalStatusToJSON(message.proposal_status);
        }
        if (message.voter !== "") {
            obj.voter = message.voter;
        }
        if (message.depositor !== "") {
            obj.depositor = message.depositor;
        }
        if (message.pagination !== undefined) {
            obj.pagination = PageRequest.toJSON(message.pagination);
        }
        return obj;
    },
    create(base) {
        return QueryProposalsRequest.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryProposalsRequest();
        message.proposal_status = object.proposal_status ?? 0;
        message.voter = object.voter ?? "";
        message.depositor = object.depositor ?? "";
        message.pagination = object.pagination !== undefined && object.pagination !== null ? PageRequest.fromPartial(object.pagination) : undefined;
        return message;
    }
};
export const QueryProposalsResponse = {
    $type: "cosmos.gov.v1beta1.QueryProposalsResponse",
    encode(message, writer = new BinaryWriter()) {
        for (const v of message.proposals) {
            Proposal.encode(v, writer.uint32(10).fork()).join();
        }
        if (message.pagination !== undefined) {
            PageResponse.encode(message.pagination, writer.uint32(18).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseQueryProposalsResponse();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.proposals.push(Proposal.decode(reader, reader.uint32()));
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
            proposals: globalThis.Array.isArray(object?.proposals) ? object.proposals.map((e) => Proposal.fromJSON(e)) : [],
            pagination: isSet(object.pagination) ? PageResponse.fromJSON(object.pagination) : undefined
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.proposals?.length) {
            obj.proposals = message.proposals.map((e) => Proposal.toJSON(e));
        }
        if (message.pagination !== undefined) {
            obj.pagination = PageResponse.toJSON(message.pagination);
        }
        return obj;
    },
    create(base) {
        return QueryProposalsResponse.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryProposalsResponse();
        message.proposals = object.proposals?.map((e) => Proposal.fromPartial(e)) || [];
        message.pagination = object.pagination !== undefined && object.pagination !== null ? PageResponse.fromPartial(object.pagination) : undefined;
        return message;
    }
};
export const QueryVoteRequest = {
    $type: "cosmos.gov.v1beta1.QueryVoteRequest",
    encode(message, writer = new BinaryWriter()) {
        if (message.proposal_id !== 0) {
            writer.uint32(8).uint64(message.proposal_id);
        }
        if (message.voter !== "") {
            writer.uint32(18).string(message.voter);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
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
        return QueryVoteRequest.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryVoteRequest();
        message.proposal_id = object.proposal_id ?? 0;
        message.voter = object.voter ?? "";
        return message;
    }
};
export const QueryVoteResponse = {
    $type: "cosmos.gov.v1beta1.QueryVoteResponse",
    encode(message, writer = new BinaryWriter()) {
        if (message.vote !== undefined) {
            Vote.encode(message.vote, writer.uint32(10).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseQueryVoteResponse();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.vote = Vote.decode(reader, reader.uint32());
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
        return { vote: isSet(object.vote) ? Vote.fromJSON(object.vote) : undefined };
    },
    toJSON(message) {
        const obj = {};
        if (message.vote !== undefined) {
            obj.vote = Vote.toJSON(message.vote);
        }
        return obj;
    },
    create(base) {
        return QueryVoteResponse.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryVoteResponse();
        message.vote = object.vote !== undefined && object.vote !== null ? Vote.fromPartial(object.vote) : undefined;
        return message;
    }
};
export const QueryVotesRequest = {
    $type: "cosmos.gov.v1beta1.QueryVotesRequest",
    encode(message, writer = new BinaryWriter()) {
        if (message.proposal_id !== 0) {
            writer.uint32(8).uint64(message.proposal_id);
        }
        if (message.pagination !== undefined) {
            PageRequest.encode(message.pagination, writer.uint32(18).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
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
            proposal_id: isSet(object.proposal_id) ? globalThis.Number(object.proposal_id) : 0,
            pagination: isSet(object.pagination) ? PageRequest.fromJSON(object.pagination) : undefined
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.proposal_id !== 0) {
            obj.proposal_id = Math.round(message.proposal_id);
        }
        if (message.pagination !== undefined) {
            obj.pagination = PageRequest.toJSON(message.pagination);
        }
        return obj;
    },
    create(base) {
        return QueryVotesRequest.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryVotesRequest();
        message.proposal_id = object.proposal_id ?? 0;
        message.pagination = object.pagination !== undefined && object.pagination !== null ? PageRequest.fromPartial(object.pagination) : undefined;
        return message;
    }
};
export const QueryVotesResponse = {
    $type: "cosmos.gov.v1beta1.QueryVotesResponse",
    encode(message, writer = new BinaryWriter()) {
        for (const v of message.votes) {
            Vote.encode(v, writer.uint32(10).fork()).join();
        }
        if (message.pagination !== undefined) {
            PageResponse.encode(message.pagination, writer.uint32(18).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseQueryVotesResponse();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.votes.push(Vote.decode(reader, reader.uint32()));
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
            votes: globalThis.Array.isArray(object?.votes) ? object.votes.map((e) => Vote.fromJSON(e)) : [],
            pagination: isSet(object.pagination) ? PageResponse.fromJSON(object.pagination) : undefined
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.votes?.length) {
            obj.votes = message.votes.map((e) => Vote.toJSON(e));
        }
        if (message.pagination !== undefined) {
            obj.pagination = PageResponse.toJSON(message.pagination);
        }
        return obj;
    },
    create(base) {
        return QueryVotesResponse.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryVotesResponse();
        message.votes = object.votes?.map((e) => Vote.fromPartial(e)) || [];
        message.pagination = object.pagination !== undefined && object.pagination !== null ? PageResponse.fromPartial(object.pagination) : undefined;
        return message;
    }
};
export const QueryParamsRequest = {
    $type: "cosmos.gov.v1beta1.QueryParamsRequest",
    encode(message, writer = new BinaryWriter()) {
        if (message.params_type !== "") {
            writer.uint32(10).string(message.params_type);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
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
        return QueryParamsRequest.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryParamsRequest();
        message.params_type = object.params_type ?? "";
        return message;
    }
};
export const QueryParamsResponse = {
    $type: "cosmos.gov.v1beta1.QueryParamsResponse",
    encode(message, writer = new BinaryWriter()) {
        if (message.voting_params !== undefined) {
            VotingParams.encode(message.voting_params, writer.uint32(10).fork()).join();
        }
        if (message.deposit_params !== undefined) {
            DepositParams.encode(message.deposit_params, writer.uint32(18).fork()).join();
        }
        if (message.tally_params !== undefined) {
            TallyParams.encode(message.tally_params, writer.uint32(26).fork()).join();
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
                    message.voting_params = VotingParams.decode(reader, reader.uint32());
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.deposit_params = DepositParams.decode(reader, reader.uint32());
                    continue;
                case 3:
                    if (tag !== 26) {
                        break;
                    }
                    message.tally_params = TallyParams.decode(reader, reader.uint32());
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
            voting_params: isSet(object.voting_params) ? VotingParams.fromJSON(object.voting_params) : undefined,
            deposit_params: isSet(object.deposit_params) ? DepositParams.fromJSON(object.deposit_params) : undefined,
            tally_params: isSet(object.tally_params) ? TallyParams.fromJSON(object.tally_params) : undefined
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.voting_params !== undefined) {
            obj.voting_params = VotingParams.toJSON(message.voting_params);
        }
        if (message.deposit_params !== undefined) {
            obj.deposit_params = DepositParams.toJSON(message.deposit_params);
        }
        if (message.tally_params !== undefined) {
            obj.tally_params = TallyParams.toJSON(message.tally_params);
        }
        return obj;
    },
    create(base) {
        return QueryParamsResponse.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryParamsResponse();
        message.voting_params = object.voting_params !== undefined && object.voting_params !== null ? VotingParams.fromPartial(object.voting_params) : undefined;
        message.deposit_params =
            object.deposit_params !== undefined && object.deposit_params !== null ? DepositParams.fromPartial(object.deposit_params) : undefined;
        message.tally_params = object.tally_params !== undefined && object.tally_params !== null ? TallyParams.fromPartial(object.tally_params) : undefined;
        return message;
    }
};
export const QueryDepositRequest = {
    $type: "cosmos.gov.v1beta1.QueryDepositRequest",
    encode(message, writer = new BinaryWriter()) {
        if (message.proposal_id !== 0) {
            writer.uint32(8).uint64(message.proposal_id);
        }
        if (message.depositor !== "") {
            writer.uint32(18).string(message.depositor);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
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
        return QueryDepositRequest.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryDepositRequest();
        message.proposal_id = object.proposal_id ?? 0;
        message.depositor = object.depositor ?? "";
        return message;
    }
};
export const QueryDepositResponse = {
    $type: "cosmos.gov.v1beta1.QueryDepositResponse",
    encode(message, writer = new BinaryWriter()) {
        if (message.deposit !== undefined) {
            Deposit.encode(message.deposit, writer.uint32(10).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseQueryDepositResponse();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.deposit = Deposit.decode(reader, reader.uint32());
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
        return { deposit: isSet(object.deposit) ? Deposit.fromJSON(object.deposit) : undefined };
    },
    toJSON(message) {
        const obj = {};
        if (message.deposit !== undefined) {
            obj.deposit = Deposit.toJSON(message.deposit);
        }
        return obj;
    },
    create(base) {
        return QueryDepositResponse.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryDepositResponse();
        message.deposit = object.deposit !== undefined && object.deposit !== null ? Deposit.fromPartial(object.deposit) : undefined;
        return message;
    }
};
export const QueryDepositsRequest = {
    $type: "cosmos.gov.v1beta1.QueryDepositsRequest",
    encode(message, writer = new BinaryWriter()) {
        if (message.proposal_id !== 0) {
            writer.uint32(8).uint64(message.proposal_id);
        }
        if (message.pagination !== undefined) {
            PageRequest.encode(message.pagination, writer.uint32(18).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
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
            proposal_id: isSet(object.proposal_id) ? globalThis.Number(object.proposal_id) : 0,
            pagination: isSet(object.pagination) ? PageRequest.fromJSON(object.pagination) : undefined
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.proposal_id !== 0) {
            obj.proposal_id = Math.round(message.proposal_id);
        }
        if (message.pagination !== undefined) {
            obj.pagination = PageRequest.toJSON(message.pagination);
        }
        return obj;
    },
    create(base) {
        return QueryDepositsRequest.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryDepositsRequest();
        message.proposal_id = object.proposal_id ?? 0;
        message.pagination = object.pagination !== undefined && object.pagination !== null ? PageRequest.fromPartial(object.pagination) : undefined;
        return message;
    }
};
export const QueryDepositsResponse = {
    $type: "cosmos.gov.v1beta1.QueryDepositsResponse",
    encode(message, writer = new BinaryWriter()) {
        for (const v of message.deposits) {
            Deposit.encode(v, writer.uint32(10).fork()).join();
        }
        if (message.pagination !== undefined) {
            PageResponse.encode(message.pagination, writer.uint32(18).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseQueryDepositsResponse();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.deposits.push(Deposit.decode(reader, reader.uint32()));
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
            deposits: globalThis.Array.isArray(object?.deposits) ? object.deposits.map((e) => Deposit.fromJSON(e)) : [],
            pagination: isSet(object.pagination) ? PageResponse.fromJSON(object.pagination) : undefined
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.deposits?.length) {
            obj.deposits = message.deposits.map((e) => Deposit.toJSON(e));
        }
        if (message.pagination !== undefined) {
            obj.pagination = PageResponse.toJSON(message.pagination);
        }
        return obj;
    },
    create(base) {
        return QueryDepositsResponse.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryDepositsResponse();
        message.deposits = object.deposits?.map((e) => Deposit.fromPartial(e)) || [];
        message.pagination = object.pagination !== undefined && object.pagination !== null ? PageResponse.fromPartial(object.pagination) : undefined;
        return message;
    }
};
export const QueryTallyResultRequest = {
    $type: "cosmos.gov.v1beta1.QueryTallyResultRequest",
    encode(message, writer = new BinaryWriter()) {
        if (message.proposal_id !== 0) {
            writer.uint32(8).uint64(message.proposal_id);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
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
        return QueryTallyResultRequest.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryTallyResultRequest();
        message.proposal_id = object.proposal_id ?? 0;
        return message;
    }
};
export const QueryTallyResultResponse = {
    $type: "cosmos.gov.v1beta1.QueryTallyResultResponse",
    encode(message, writer = new BinaryWriter()) {
        if (message.tally !== undefined) {
            TallyResult.encode(message.tally, writer.uint32(10).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseQueryTallyResultResponse();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.tally = TallyResult.decode(reader, reader.uint32());
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
        return { tally: isSet(object.tally) ? TallyResult.fromJSON(object.tally) : undefined };
    },
    toJSON(message) {
        const obj = {};
        if (message.tally !== undefined) {
            obj.tally = TallyResult.toJSON(message.tally);
        }
        return obj;
    },
    create(base) {
        return QueryTallyResultResponse.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseQueryTallyResultResponse();
        message.tally = object.tally !== undefined && object.tally !== null ? TallyResult.fromPartial(object.tally) : undefined;
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
export const registry = [
    ["/cosmos.gov.v1beta1.QueryProposalRequest", QueryProposalRequest],
    ["/cosmos.gov.v1beta1.QueryProposalResponse", QueryProposalResponse],
    ["/cosmos.gov.v1beta1.QueryProposalsRequest", QueryProposalsRequest],
    ["/cosmos.gov.v1beta1.QueryProposalsResponse", QueryProposalsResponse],
    ["/cosmos.gov.v1beta1.QueryVoteRequest", QueryVoteRequest],
    ["/cosmos.gov.v1beta1.QueryVoteResponse", QueryVoteResponse],
    ["/cosmos.gov.v1beta1.QueryVotesRequest", QueryVotesRequest],
    ["/cosmos.gov.v1beta1.QueryVotesResponse", QueryVotesResponse],
    ["/cosmos.gov.v1beta1.QueryParamsRequest", QueryParamsRequest],
    ["/cosmos.gov.v1beta1.QueryParamsResponse", QueryParamsResponse],
    ["/cosmos.gov.v1beta1.QueryDepositRequest", QueryDepositRequest],
    ["/cosmos.gov.v1beta1.QueryDepositResponse", QueryDepositResponse],
    ["/cosmos.gov.v1beta1.QueryDepositsRequest", QueryDepositsRequest],
    ["/cosmos.gov.v1beta1.QueryDepositsResponse", QueryDepositsResponse],
    ["/cosmos.gov.v1beta1.QueryTallyResultRequest", QueryTallyResultRequest],
    ["/cosmos.gov.v1beta1.QueryTallyResultResponse", QueryTallyResultResponse]
];
export const aminoConverters = {
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
