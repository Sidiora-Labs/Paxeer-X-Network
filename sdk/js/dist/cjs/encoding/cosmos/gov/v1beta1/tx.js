"use strict";
Object.defineProperty(exports, "__esModule", { value: true });
exports.aminoConverters = exports.registry = exports.MsgDepositResponse = exports.MsgDeposit = exports.MsgVoteWeightedResponse = exports.MsgVoteWeighted = exports.MsgVoteResponse = exports.MsgVote = exports.MsgSubmitProposalResponse = exports.MsgSubmitProposal = void 0;
const wire_1 = require("@bufbuild/protobuf/wire");
const any_1 = require("../../../google/protobuf/any");
const coin_1 = require("../../base/v1beta1/coin");
const gov_1 = require("./gov");
exports.MsgSubmitProposal = {
    $type: "cosmos.gov.v1beta1.MsgSubmitProposal",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.content !== undefined) {
            any_1.Any.encode(message.content, writer.uint32(10).fork()).join();
        }
        for (const v of message.initial_deposit) {
            coin_1.Coin.encode(v, writer.uint32(18).fork()).join();
        }
        if (message.proposer !== "") {
            writer.uint32(26).string(message.proposer);
        }
        if (message.is_expedited !== false) {
            writer.uint32(32).bool(message.is_expedited);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseMsgSubmitProposal();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.content = any_1.Any.decode(reader, reader.uint32());
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.initial_deposit.push(coin_1.Coin.decode(reader, reader.uint32()));
                    continue;
                case 3:
                    if (tag !== 26) {
                        break;
                    }
                    message.proposer = reader.string();
                    continue;
                case 4:
                    if (tag !== 32) {
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
            content: isSet(object.content) ? any_1.Any.fromJSON(object.content) : undefined,
            initial_deposit: globalThis.Array.isArray(object?.initial_deposit) ? object.initial_deposit.map((e) => coin_1.Coin.fromJSON(e)) : [],
            proposer: isSet(object.proposer) ? globalThis.String(object.proposer) : "",
            is_expedited: isSet(object.is_expedited) ? globalThis.Boolean(object.is_expedited) : false
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.content !== undefined) {
            obj.content = any_1.Any.toJSON(message.content);
        }
        if (message.initial_deposit?.length) {
            obj.initial_deposit = message.initial_deposit.map((e) => coin_1.Coin.toJSON(e));
        }
        if (message.proposer !== "") {
            obj.proposer = message.proposer;
        }
        if (message.is_expedited !== false) {
            obj.is_expedited = message.is_expedited;
        }
        return obj;
    },
    create(base) {
        return exports.MsgSubmitProposal.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseMsgSubmitProposal();
        message.content = object.content !== undefined && object.content !== null ? any_1.Any.fromPartial(object.content) : undefined;
        message.initial_deposit = object.initial_deposit?.map((e) => coin_1.Coin.fromPartial(e)) || [];
        message.proposer = object.proposer ?? "";
        message.is_expedited = object.is_expedited ?? false;
        return message;
    }
};
exports.MsgSubmitProposalResponse = {
    $type: "cosmos.gov.v1beta1.MsgSubmitProposalResponse",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.proposal_id !== 0) {
            writer.uint32(8).uint64(message.proposal_id);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseMsgSubmitProposalResponse();
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
        return exports.MsgSubmitProposalResponse.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseMsgSubmitProposalResponse();
        message.proposal_id = object.proposal_id ?? 0;
        return message;
    }
};
exports.MsgVote = {
    $type: "cosmos.gov.v1beta1.MsgVote",
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
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseMsgVote();
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
            option: isSet(object.option) ? (0, gov_1.voteOptionFromJSON)(object.option) : 0
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
            obj.option = (0, gov_1.voteOptionToJSON)(message.option);
        }
        return obj;
    },
    create(base) {
        return exports.MsgVote.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseMsgVote();
        message.proposal_id = object.proposal_id ?? 0;
        message.voter = object.voter ?? "";
        message.option = object.option ?? 0;
        return message;
    }
};
exports.MsgVoteResponse = {
    $type: "cosmos.gov.v1beta1.MsgVoteResponse",
    encode(_, writer = new wire_1.BinaryWriter()) {
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseMsgVoteResponse();
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
        return exports.MsgVoteResponse.fromPartial(base ?? {});
    },
    fromPartial(_) {
        const message = createBaseMsgVoteResponse();
        return message;
    }
};
exports.MsgVoteWeighted = {
    $type: "cosmos.gov.v1beta1.MsgVoteWeighted",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.proposal_id !== 0) {
            writer.uint32(8).uint64(message.proposal_id);
        }
        if (message.voter !== "") {
            writer.uint32(18).string(message.voter);
        }
        for (const v of message.options) {
            gov_1.WeightedVoteOption.encode(v, writer.uint32(26).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseMsgVoteWeighted();
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
                    if (tag !== 26) {
                        break;
                    }
                    message.options.push(gov_1.WeightedVoteOption.decode(reader, reader.uint32()));
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
            options: globalThis.Array.isArray(object?.options) ? object.options.map((e) => gov_1.WeightedVoteOption.fromJSON(e)) : []
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
        if (message.options?.length) {
            obj.options = message.options.map((e) => gov_1.WeightedVoteOption.toJSON(e));
        }
        return obj;
    },
    create(base) {
        return exports.MsgVoteWeighted.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseMsgVoteWeighted();
        message.proposal_id = object.proposal_id ?? 0;
        message.voter = object.voter ?? "";
        message.options = object.options?.map((e) => gov_1.WeightedVoteOption.fromPartial(e)) || [];
        return message;
    }
};
exports.MsgVoteWeightedResponse = {
    $type: "cosmos.gov.v1beta1.MsgVoteWeightedResponse",
    encode(_, writer = new wire_1.BinaryWriter()) {
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseMsgVoteWeightedResponse();
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
        return exports.MsgVoteWeightedResponse.fromPartial(base ?? {});
    },
    fromPartial(_) {
        const message = createBaseMsgVoteWeightedResponse();
        return message;
    }
};
exports.MsgDeposit = {
    $type: "cosmos.gov.v1beta1.MsgDeposit",
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
        const message = createBaseMsgDeposit();
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
        return exports.MsgDeposit.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseMsgDeposit();
        message.proposal_id = object.proposal_id ?? 0;
        message.depositor = object.depositor ?? "";
        message.amount = object.amount?.map((e) => coin_1.Coin.fromPartial(e)) || [];
        return message;
    }
};
exports.MsgDepositResponse = {
    $type: "cosmos.gov.v1beta1.MsgDepositResponse",
    encode(_, writer = new wire_1.BinaryWriter()) {
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseMsgDepositResponse();
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
        return exports.MsgDepositResponse.fromPartial(base ?? {});
    },
    fromPartial(_) {
        const message = createBaseMsgDepositResponse();
        return message;
    }
};
function createBaseMsgSubmitProposal() {
    return { content: undefined, initial_deposit: [], proposer: "", is_expedited: false };
}
function createBaseMsgSubmitProposalResponse() {
    return { proposal_id: 0 };
}
function createBaseMsgVote() {
    return { proposal_id: 0, voter: "", option: 0 };
}
function createBaseMsgVoteResponse() {
    return {};
}
function createBaseMsgVoteWeighted() {
    return { proposal_id: 0, voter: "", options: [] };
}
function createBaseMsgVoteWeightedResponse() {
    return {};
}
function createBaseMsgDeposit() {
    return { proposal_id: 0, depositor: "", amount: [] };
}
function createBaseMsgDepositResponse() {
    return {};
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
    ["/cosmos.gov.v1beta1.MsgSubmitProposal", exports.MsgSubmitProposal],
    ["/cosmos.gov.v1beta1.MsgSubmitProposalResponse", exports.MsgSubmitProposalResponse],
    ["/cosmos.gov.v1beta1.MsgVote", exports.MsgVote],
    ["/cosmos.gov.v1beta1.MsgVoteResponse", exports.MsgVoteResponse],
    ["/cosmos.gov.v1beta1.MsgVoteWeighted", exports.MsgVoteWeighted],
    ["/cosmos.gov.v1beta1.MsgVoteWeightedResponse", exports.MsgVoteWeightedResponse],
    ["/cosmos.gov.v1beta1.MsgDeposit", exports.MsgDeposit],
    ["/cosmos.gov.v1beta1.MsgDepositResponse", exports.MsgDepositResponse]
];
exports.aminoConverters = {
    "/cosmos.gov.v1beta1.MsgSubmitProposal": {
        aminoType: "cosmos-sdk/MsgSubmitProposal",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.gov.v1beta1.MsgSubmitProposalResponse": {
        aminoType: "cosmos-sdk/MsgSubmitProposalResponse",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.gov.v1beta1.MsgVote": {
        aminoType: "cosmos-sdk/MsgVote",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.gov.v1beta1.MsgVoteResponse": {
        aminoType: "cosmos-sdk/MsgVoteResponse",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.gov.v1beta1.MsgVoteWeighted": {
        aminoType: "cosmos-sdk/MsgVoteWeighted",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.gov.v1beta1.MsgVoteWeightedResponse": {
        aminoType: "cosmos-sdk/MsgVoteWeightedResponse",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.gov.v1beta1.MsgDeposit": {
        aminoType: "cosmos-sdk/MsgDeposit",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/cosmos.gov.v1beta1.MsgDepositResponse": {
        aminoType: "cosmos-sdk/MsgDepositResponse",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    }
};
