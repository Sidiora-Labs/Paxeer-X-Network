"use strict";
Object.defineProperty(exports, "__esModule", { value: true });
exports.aminoConverters = exports.registry = exports.EvidenceList = exports.LightClientAttackEvidence = exports.DuplicateVoteEvidence = exports.Evidence = void 0;
const wire_1 = require("@bufbuild/protobuf/wire");
const timestamp_1 = require("../../google/protobuf/timestamp");
const types_1 = require("./types");
const validator_1 = require("./validator");
exports.Evidence = {
    $type: "tendermint.types.Evidence",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.duplicate_vote_evidence !== undefined) {
            exports.DuplicateVoteEvidence.encode(message.duplicate_vote_evidence, writer.uint32(10).fork()).join();
        }
        if (message.light_client_attack_evidence !== undefined) {
            exports.LightClientAttackEvidence.encode(message.light_client_attack_evidence, writer.uint32(18).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseEvidence();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.duplicate_vote_evidence = exports.DuplicateVoteEvidence.decode(reader, reader.uint32());
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.light_client_attack_evidence = exports.LightClientAttackEvidence.decode(reader, reader.uint32());
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
            duplicate_vote_evidence: isSet(object.duplicate_vote_evidence) ? exports.DuplicateVoteEvidence.fromJSON(object.duplicate_vote_evidence) : undefined,
            light_client_attack_evidence: isSet(object.light_client_attack_evidence)
                ? exports.LightClientAttackEvidence.fromJSON(object.light_client_attack_evidence)
                : undefined
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.duplicate_vote_evidence !== undefined) {
            obj.duplicate_vote_evidence = exports.DuplicateVoteEvidence.toJSON(message.duplicate_vote_evidence);
        }
        if (message.light_client_attack_evidence !== undefined) {
            obj.light_client_attack_evidence = exports.LightClientAttackEvidence.toJSON(message.light_client_attack_evidence);
        }
        return obj;
    },
    create(base) {
        return exports.Evidence.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseEvidence();
        message.duplicate_vote_evidence =
            object.duplicate_vote_evidence !== undefined && object.duplicate_vote_evidence !== null
                ? exports.DuplicateVoteEvidence.fromPartial(object.duplicate_vote_evidence)
                : undefined;
        message.light_client_attack_evidence =
            object.light_client_attack_evidence !== undefined && object.light_client_attack_evidence !== null
                ? exports.LightClientAttackEvidence.fromPartial(object.light_client_attack_evidence)
                : undefined;
        return message;
    }
};
exports.DuplicateVoteEvidence = {
    $type: "tendermint.types.DuplicateVoteEvidence",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.vote_a !== undefined) {
            types_1.Vote.encode(message.vote_a, writer.uint32(10).fork()).join();
        }
        if (message.vote_b !== undefined) {
            types_1.Vote.encode(message.vote_b, writer.uint32(18).fork()).join();
        }
        if (message.total_voting_power !== 0) {
            writer.uint32(24).int64(message.total_voting_power);
        }
        if (message.validator_power !== 0) {
            writer.uint32(32).int64(message.validator_power);
        }
        if (message.timestamp !== undefined) {
            timestamp_1.Timestamp.encode(toTimestamp(message.timestamp), writer.uint32(42).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseDuplicateVoteEvidence();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.vote_a = types_1.Vote.decode(reader, reader.uint32());
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.vote_b = types_1.Vote.decode(reader, reader.uint32());
                    continue;
                case 3:
                    if (tag !== 24) {
                        break;
                    }
                    message.total_voting_power = longToNumber(reader.int64());
                    continue;
                case 4:
                    if (tag !== 32) {
                        break;
                    }
                    message.validator_power = longToNumber(reader.int64());
                    continue;
                case 5:
                    if (tag !== 42) {
                        break;
                    }
                    message.timestamp = fromTimestamp(timestamp_1.Timestamp.decode(reader, reader.uint32()));
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
            vote_a: isSet(object.vote_a) ? types_1.Vote.fromJSON(object.vote_a) : undefined,
            vote_b: isSet(object.vote_b) ? types_1.Vote.fromJSON(object.vote_b) : undefined,
            total_voting_power: isSet(object.total_voting_power) ? globalThis.Number(object.total_voting_power) : 0,
            validator_power: isSet(object.validator_power) ? globalThis.Number(object.validator_power) : 0,
            timestamp: isSet(object.timestamp) ? fromJsonTimestamp(object.timestamp) : undefined
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.vote_a !== undefined) {
            obj.vote_a = types_1.Vote.toJSON(message.vote_a);
        }
        if (message.vote_b !== undefined) {
            obj.vote_b = types_1.Vote.toJSON(message.vote_b);
        }
        if (message.total_voting_power !== 0) {
            obj.total_voting_power = Math.round(message.total_voting_power);
        }
        if (message.validator_power !== 0) {
            obj.validator_power = Math.round(message.validator_power);
        }
        if (message.timestamp !== undefined) {
            obj.timestamp = message.timestamp.toISOString();
        }
        return obj;
    },
    create(base) {
        return exports.DuplicateVoteEvidence.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseDuplicateVoteEvidence();
        message.vote_a = object.vote_a !== undefined && object.vote_a !== null ? types_1.Vote.fromPartial(object.vote_a) : undefined;
        message.vote_b = object.vote_b !== undefined && object.vote_b !== null ? types_1.Vote.fromPartial(object.vote_b) : undefined;
        message.total_voting_power = object.total_voting_power ?? 0;
        message.validator_power = object.validator_power ?? 0;
        message.timestamp = object.timestamp ?? undefined;
        return message;
    }
};
exports.LightClientAttackEvidence = {
    $type: "tendermint.types.LightClientAttackEvidence",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.conflicting_block !== undefined) {
            types_1.LightBlock.encode(message.conflicting_block, writer.uint32(10).fork()).join();
        }
        if (message.common_height !== 0) {
            writer.uint32(16).int64(message.common_height);
        }
        for (const v of message.byzantine_validators) {
            validator_1.Validator.encode(v, writer.uint32(26).fork()).join();
        }
        if (message.total_voting_power !== 0) {
            writer.uint32(32).int64(message.total_voting_power);
        }
        if (message.timestamp !== undefined) {
            timestamp_1.Timestamp.encode(toTimestamp(message.timestamp), writer.uint32(42).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseLightClientAttackEvidence();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.conflicting_block = types_1.LightBlock.decode(reader, reader.uint32());
                    continue;
                case 2:
                    if (tag !== 16) {
                        break;
                    }
                    message.common_height = longToNumber(reader.int64());
                    continue;
                case 3:
                    if (tag !== 26) {
                        break;
                    }
                    message.byzantine_validators.push(validator_1.Validator.decode(reader, reader.uint32()));
                    continue;
                case 4:
                    if (tag !== 32) {
                        break;
                    }
                    message.total_voting_power = longToNumber(reader.int64());
                    continue;
                case 5:
                    if (tag !== 42) {
                        break;
                    }
                    message.timestamp = fromTimestamp(timestamp_1.Timestamp.decode(reader, reader.uint32()));
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
            conflicting_block: isSet(object.conflicting_block) ? types_1.LightBlock.fromJSON(object.conflicting_block) : undefined,
            common_height: isSet(object.common_height) ? globalThis.Number(object.common_height) : 0,
            byzantine_validators: globalThis.Array.isArray(object?.byzantine_validators) ? object.byzantine_validators.map((e) => validator_1.Validator.fromJSON(e)) : [],
            total_voting_power: isSet(object.total_voting_power) ? globalThis.Number(object.total_voting_power) : 0,
            timestamp: isSet(object.timestamp) ? fromJsonTimestamp(object.timestamp) : undefined
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.conflicting_block !== undefined) {
            obj.conflicting_block = types_1.LightBlock.toJSON(message.conflicting_block);
        }
        if (message.common_height !== 0) {
            obj.common_height = Math.round(message.common_height);
        }
        if (message.byzantine_validators?.length) {
            obj.byzantine_validators = message.byzantine_validators.map((e) => validator_1.Validator.toJSON(e));
        }
        if (message.total_voting_power !== 0) {
            obj.total_voting_power = Math.round(message.total_voting_power);
        }
        if (message.timestamp !== undefined) {
            obj.timestamp = message.timestamp.toISOString();
        }
        return obj;
    },
    create(base) {
        return exports.LightClientAttackEvidence.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseLightClientAttackEvidence();
        message.conflicting_block =
            object.conflicting_block !== undefined && object.conflicting_block !== null ? types_1.LightBlock.fromPartial(object.conflicting_block) : undefined;
        message.common_height = object.common_height ?? 0;
        message.byzantine_validators = object.byzantine_validators?.map((e) => validator_1.Validator.fromPartial(e)) || [];
        message.total_voting_power = object.total_voting_power ?? 0;
        message.timestamp = object.timestamp ?? undefined;
        return message;
    }
};
exports.EvidenceList = {
    $type: "tendermint.types.EvidenceList",
    encode(message, writer = new wire_1.BinaryWriter()) {
        for (const v of message.evidence) {
            exports.Evidence.encode(v, writer.uint32(10).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseEvidenceList();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.evidence.push(exports.Evidence.decode(reader, reader.uint32()));
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
            evidence: globalThis.Array.isArray(object?.evidence) ? object.evidence.map((e) => exports.Evidence.fromJSON(e)) : []
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.evidence?.length) {
            obj.evidence = message.evidence.map((e) => exports.Evidence.toJSON(e));
        }
        return obj;
    },
    create(base) {
        return exports.EvidenceList.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseEvidenceList();
        message.evidence = object.evidence?.map((e) => exports.Evidence.fromPartial(e)) || [];
        return message;
    }
};
function createBaseEvidence() {
    return { duplicate_vote_evidence: undefined, light_client_attack_evidence: undefined };
}
function createBaseDuplicateVoteEvidence() {
    return { vote_a: undefined, vote_b: undefined, total_voting_power: 0, validator_power: 0, timestamp: undefined };
}
function createBaseLightClientAttackEvidence() {
    return {
        conflicting_block: undefined,
        common_height: 0,
        byzantine_validators: [],
        total_voting_power: 0,
        timestamp: undefined
    };
}
function createBaseEvidenceList() {
    return { evidence: [] };
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
    ["/tendermint.types.Evidence", exports.Evidence],
    ["/tendermint.types.DuplicateVoteEvidence", exports.DuplicateVoteEvidence],
    ["/tendermint.types.LightClientAttackEvidence", exports.LightClientAttackEvidence],
    ["/tendermint.types.EvidenceList", exports.EvidenceList]
];
exports.aminoConverters = {
    "/tendermint.types.Evidence": {
        aminoType: "tendermint.types.Evidence",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/tendermint.types.DuplicateVoteEvidence": {
        aminoType: "tendermint.types.DuplicateVoteEvidence",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/tendermint.types.LightClientAttackEvidence": {
        aminoType: "tendermint.types.LightClientAttackEvidence",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/tendermint.types.EvidenceList": {
        aminoType: "tendermint.types.EvidenceList",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    }
};
