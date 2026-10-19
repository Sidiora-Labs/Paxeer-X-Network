"use strict";
Object.defineProperty(exports, "__esModule", { value: true });
exports.aminoConverters = exports.registry = exports.GenesisState = void 0;
const wire_1 = require("@bufbuild/protobuf/wire");
const gov_1 = require("./gov");
exports.GenesisState = {
    $type: "cosmos.gov.v1beta1.GenesisState",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.starting_proposal_id !== 0) {
            writer.uint32(8).uint64(message.starting_proposal_id);
        }
        for (const v of message.deposits) {
            gov_1.Deposit.encode(v, writer.uint32(18).fork()).join();
        }
        for (const v of message.votes) {
            gov_1.Vote.encode(v, writer.uint32(26).fork()).join();
        }
        for (const v of message.proposals) {
            gov_1.Proposal.encode(v, writer.uint32(34).fork()).join();
        }
        if (message.deposit_params !== undefined) {
            gov_1.DepositParams.encode(message.deposit_params, writer.uint32(42).fork()).join();
        }
        if (message.voting_params !== undefined) {
            gov_1.VotingParams.encode(message.voting_params, writer.uint32(50).fork()).join();
        }
        if (message.tally_params !== undefined) {
            gov_1.TallyParams.encode(message.tally_params, writer.uint32(58).fork()).join();
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
                    if (tag !== 8) {
                        break;
                    }
                    message.starting_proposal_id = longToNumber(reader.uint64());
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.deposits.push(gov_1.Deposit.decode(reader, reader.uint32()));
                    continue;
                case 3:
                    if (tag !== 26) {
                        break;
                    }
                    message.votes.push(gov_1.Vote.decode(reader, reader.uint32()));
                    continue;
                case 4:
                    if (tag !== 34) {
                        break;
                    }
                    message.proposals.push(gov_1.Proposal.decode(reader, reader.uint32()));
                    continue;
                case 5:
                    if (tag !== 42) {
                        break;
                    }
                    message.deposit_params = gov_1.DepositParams.decode(reader, reader.uint32());
                    continue;
                case 6:
                    if (tag !== 50) {
                        break;
                    }
                    message.voting_params = gov_1.VotingParams.decode(reader, reader.uint32());
                    continue;
                case 7:
                    if (tag !== 58) {
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
            starting_proposal_id: isSet(object.starting_proposal_id) ? globalThis.Number(object.starting_proposal_id) : 0,
            deposits: globalThis.Array.isArray(object?.deposits) ? object.deposits.map((e) => gov_1.Deposit.fromJSON(e)) : [],
            votes: globalThis.Array.isArray(object?.votes) ? object.votes.map((e) => gov_1.Vote.fromJSON(e)) : [],
            proposals: globalThis.Array.isArray(object?.proposals) ? object.proposals.map((e) => gov_1.Proposal.fromJSON(e)) : [],
            deposit_params: isSet(object.deposit_params) ? gov_1.DepositParams.fromJSON(object.deposit_params) : undefined,
            voting_params: isSet(object.voting_params) ? gov_1.VotingParams.fromJSON(object.voting_params) : undefined,
            tally_params: isSet(object.tally_params) ? gov_1.TallyParams.fromJSON(object.tally_params) : undefined
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.starting_proposal_id !== 0) {
            obj.starting_proposal_id = Math.round(message.starting_proposal_id);
        }
        if (message.deposits?.length) {
            obj.deposits = message.deposits.map((e) => gov_1.Deposit.toJSON(e));
        }
        if (message.votes?.length) {
            obj.votes = message.votes.map((e) => gov_1.Vote.toJSON(e));
        }
        if (message.proposals?.length) {
            obj.proposals = message.proposals.map((e) => gov_1.Proposal.toJSON(e));
        }
        if (message.deposit_params !== undefined) {
            obj.deposit_params = gov_1.DepositParams.toJSON(message.deposit_params);
        }
        if (message.voting_params !== undefined) {
            obj.voting_params = gov_1.VotingParams.toJSON(message.voting_params);
        }
        if (message.tally_params !== undefined) {
            obj.tally_params = gov_1.TallyParams.toJSON(message.tally_params);
        }
        return obj;
    },
    create(base) {
        return exports.GenesisState.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseGenesisState();
        message.starting_proposal_id = object.starting_proposal_id ?? 0;
        message.deposits = object.deposits?.map((e) => gov_1.Deposit.fromPartial(e)) || [];
        message.votes = object.votes?.map((e) => gov_1.Vote.fromPartial(e)) || [];
        message.proposals = object.proposals?.map((e) => gov_1.Proposal.fromPartial(e)) || [];
        message.deposit_params =
            object.deposit_params !== undefined && object.deposit_params !== null ? gov_1.DepositParams.fromPartial(object.deposit_params) : undefined;
        message.voting_params = object.voting_params !== undefined && object.voting_params !== null ? gov_1.VotingParams.fromPartial(object.voting_params) : undefined;
        message.tally_params = object.tally_params !== undefined && object.tally_params !== null ? gov_1.TallyParams.fromPartial(object.tally_params) : undefined;
        return message;
    }
};
function createBaseGenesisState() {
    return {
        starting_proposal_id: 0,
        deposits: [],
        votes: [],
        proposals: [],
        deposit_params: undefined,
        voting_params: undefined,
        tally_params: undefined
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
exports.registry = [["/cosmos.gov.v1beta1.GenesisState", exports.GenesisState]];
exports.aminoConverters = {
    "/cosmos.gov.v1beta1.GenesisState": {
        aminoType: "cosmos-sdk/GenesisState",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    }
};
