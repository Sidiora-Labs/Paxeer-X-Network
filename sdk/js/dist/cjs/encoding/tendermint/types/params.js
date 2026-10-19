"use strict";
Object.defineProperty(exports, "__esModule", { value: true });
exports.aminoConverters = exports.registry = exports.ABCIParams = exports.TimeoutParams = exports.SynchronyParams = exports.HashedParams = exports.VersionParams = exports.ValidatorParams = exports.EvidenceParams = exports.BlockParams = exports.ConsensusParams = void 0;
const wire_1 = require("@bufbuild/protobuf/wire");
const duration_1 = require("../../google/protobuf/duration");
exports.ConsensusParams = {
    $type: "tendermint.types.ConsensusParams",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.block !== undefined) {
            exports.BlockParams.encode(message.block, writer.uint32(10).fork()).join();
        }
        if (message.evidence !== undefined) {
            exports.EvidenceParams.encode(message.evidence, writer.uint32(18).fork()).join();
        }
        if (message.validator !== undefined) {
            exports.ValidatorParams.encode(message.validator, writer.uint32(26).fork()).join();
        }
        if (message.version !== undefined) {
            exports.VersionParams.encode(message.version, writer.uint32(34).fork()).join();
        }
        if (message.synchrony !== undefined) {
            exports.SynchronyParams.encode(message.synchrony, writer.uint32(42).fork()).join();
        }
        if (message.timeout !== undefined) {
            exports.TimeoutParams.encode(message.timeout, writer.uint32(50).fork()).join();
        }
        if (message.abci !== undefined) {
            exports.ABCIParams.encode(message.abci, writer.uint32(58).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseConsensusParams();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.block = exports.BlockParams.decode(reader, reader.uint32());
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.evidence = exports.EvidenceParams.decode(reader, reader.uint32());
                    continue;
                case 3:
                    if (tag !== 26) {
                        break;
                    }
                    message.validator = exports.ValidatorParams.decode(reader, reader.uint32());
                    continue;
                case 4:
                    if (tag !== 34) {
                        break;
                    }
                    message.version = exports.VersionParams.decode(reader, reader.uint32());
                    continue;
                case 5:
                    if (tag !== 42) {
                        break;
                    }
                    message.synchrony = exports.SynchronyParams.decode(reader, reader.uint32());
                    continue;
                case 6:
                    if (tag !== 50) {
                        break;
                    }
                    message.timeout = exports.TimeoutParams.decode(reader, reader.uint32());
                    continue;
                case 7:
                    if (tag !== 58) {
                        break;
                    }
                    message.abci = exports.ABCIParams.decode(reader, reader.uint32());
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
            block: isSet(object.block) ? exports.BlockParams.fromJSON(object.block) : undefined,
            evidence: isSet(object.evidence) ? exports.EvidenceParams.fromJSON(object.evidence) : undefined,
            validator: isSet(object.validator) ? exports.ValidatorParams.fromJSON(object.validator) : undefined,
            version: isSet(object.version) ? exports.VersionParams.fromJSON(object.version) : undefined,
            synchrony: isSet(object.synchrony) ? exports.SynchronyParams.fromJSON(object.synchrony) : undefined,
            timeout: isSet(object.timeout) ? exports.TimeoutParams.fromJSON(object.timeout) : undefined,
            abci: isSet(object.abci) ? exports.ABCIParams.fromJSON(object.abci) : undefined
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.block !== undefined) {
            obj.block = exports.BlockParams.toJSON(message.block);
        }
        if (message.evidence !== undefined) {
            obj.evidence = exports.EvidenceParams.toJSON(message.evidence);
        }
        if (message.validator !== undefined) {
            obj.validator = exports.ValidatorParams.toJSON(message.validator);
        }
        if (message.version !== undefined) {
            obj.version = exports.VersionParams.toJSON(message.version);
        }
        if (message.synchrony !== undefined) {
            obj.synchrony = exports.SynchronyParams.toJSON(message.synchrony);
        }
        if (message.timeout !== undefined) {
            obj.timeout = exports.TimeoutParams.toJSON(message.timeout);
        }
        if (message.abci !== undefined) {
            obj.abci = exports.ABCIParams.toJSON(message.abci);
        }
        return obj;
    },
    create(base) {
        return exports.ConsensusParams.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseConsensusParams();
        message.block = object.block !== undefined && object.block !== null ? exports.BlockParams.fromPartial(object.block) : undefined;
        message.evidence = object.evidence !== undefined && object.evidence !== null ? exports.EvidenceParams.fromPartial(object.evidence) : undefined;
        message.validator = object.validator !== undefined && object.validator !== null ? exports.ValidatorParams.fromPartial(object.validator) : undefined;
        message.version = object.version !== undefined && object.version !== null ? exports.VersionParams.fromPartial(object.version) : undefined;
        message.synchrony = object.synchrony !== undefined && object.synchrony !== null ? exports.SynchronyParams.fromPartial(object.synchrony) : undefined;
        message.timeout = object.timeout !== undefined && object.timeout !== null ? exports.TimeoutParams.fromPartial(object.timeout) : undefined;
        message.abci = object.abci !== undefined && object.abci !== null ? exports.ABCIParams.fromPartial(object.abci) : undefined;
        return message;
    }
};
exports.BlockParams = {
    $type: "tendermint.types.BlockParams",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.max_bytes !== 0) {
            writer.uint32(8).int64(message.max_bytes);
        }
        if (message.max_gas !== 0) {
            writer.uint32(16).int64(message.max_gas);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseBlockParams();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 8) {
                        break;
                    }
                    message.max_bytes = longToNumber(reader.int64());
                    continue;
                case 2:
                    if (tag !== 16) {
                        break;
                    }
                    message.max_gas = longToNumber(reader.int64());
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
            max_bytes: isSet(object.max_bytes) ? globalThis.Number(object.max_bytes) : 0,
            max_gas: isSet(object.max_gas) ? globalThis.Number(object.max_gas) : 0
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.max_bytes !== 0) {
            obj.max_bytes = Math.round(message.max_bytes);
        }
        if (message.max_gas !== 0) {
            obj.max_gas = Math.round(message.max_gas);
        }
        return obj;
    },
    create(base) {
        return exports.BlockParams.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseBlockParams();
        message.max_bytes = object.max_bytes ?? 0;
        message.max_gas = object.max_gas ?? 0;
        return message;
    }
};
exports.EvidenceParams = {
    $type: "tendermint.types.EvidenceParams",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.max_age_num_blocks !== 0) {
            writer.uint32(8).int64(message.max_age_num_blocks);
        }
        if (message.max_age_duration !== undefined) {
            duration_1.Duration.encode(message.max_age_duration, writer.uint32(18).fork()).join();
        }
        if (message.max_bytes !== 0) {
            writer.uint32(24).int64(message.max_bytes);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseEvidenceParams();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 8) {
                        break;
                    }
                    message.max_age_num_blocks = longToNumber(reader.int64());
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.max_age_duration = duration_1.Duration.decode(reader, reader.uint32());
                    continue;
                case 3:
                    if (tag !== 24) {
                        break;
                    }
                    message.max_bytes = longToNumber(reader.int64());
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
            max_age_num_blocks: isSet(object.max_age_num_blocks) ? globalThis.Number(object.max_age_num_blocks) : 0,
            max_age_duration: isSet(object.max_age_duration) ? duration_1.Duration.fromJSON(object.max_age_duration) : undefined,
            max_bytes: isSet(object.max_bytes) ? globalThis.Number(object.max_bytes) : 0
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.max_age_num_blocks !== 0) {
            obj.max_age_num_blocks = Math.round(message.max_age_num_blocks);
        }
        if (message.max_age_duration !== undefined) {
            obj.max_age_duration = duration_1.Duration.toJSON(message.max_age_duration);
        }
        if (message.max_bytes !== 0) {
            obj.max_bytes = Math.round(message.max_bytes);
        }
        return obj;
    },
    create(base) {
        return exports.EvidenceParams.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseEvidenceParams();
        message.max_age_num_blocks = object.max_age_num_blocks ?? 0;
        message.max_age_duration =
            object.max_age_duration !== undefined && object.max_age_duration !== null ? duration_1.Duration.fromPartial(object.max_age_duration) : undefined;
        message.max_bytes = object.max_bytes ?? 0;
        return message;
    }
};
exports.ValidatorParams = {
    $type: "tendermint.types.ValidatorParams",
    encode(message, writer = new wire_1.BinaryWriter()) {
        for (const v of message.pub_key_types) {
            writer.uint32(10).string(v);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseValidatorParams();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.pub_key_types.push(reader.string());
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
            pub_key_types: globalThis.Array.isArray(object?.pub_key_types) ? object.pub_key_types.map((e) => globalThis.String(e)) : []
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.pub_key_types?.length) {
            obj.pub_key_types = message.pub_key_types;
        }
        return obj;
    },
    create(base) {
        return exports.ValidatorParams.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseValidatorParams();
        message.pub_key_types = object.pub_key_types?.map((e) => e) || [];
        return message;
    }
};
exports.VersionParams = {
    $type: "tendermint.types.VersionParams",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.app_version !== 0) {
            writer.uint32(8).uint64(message.app_version);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseVersionParams();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 8) {
                        break;
                    }
                    message.app_version = longToNumber(reader.uint64());
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
        return { app_version: isSet(object.app_version) ? globalThis.Number(object.app_version) : 0 };
    },
    toJSON(message) {
        const obj = {};
        if (message.app_version !== 0) {
            obj.app_version = Math.round(message.app_version);
        }
        return obj;
    },
    create(base) {
        return exports.VersionParams.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseVersionParams();
        message.app_version = object.app_version ?? 0;
        return message;
    }
};
exports.HashedParams = {
    $type: "tendermint.types.HashedParams",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.block_max_bytes !== 0) {
            writer.uint32(8).int64(message.block_max_bytes);
        }
        if (message.block_max_gas !== 0) {
            writer.uint32(16).int64(message.block_max_gas);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseHashedParams();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 8) {
                        break;
                    }
                    message.block_max_bytes = longToNumber(reader.int64());
                    continue;
                case 2:
                    if (tag !== 16) {
                        break;
                    }
                    message.block_max_gas = longToNumber(reader.int64());
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
            block_max_bytes: isSet(object.block_max_bytes) ? globalThis.Number(object.block_max_bytes) : 0,
            block_max_gas: isSet(object.block_max_gas) ? globalThis.Number(object.block_max_gas) : 0
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.block_max_bytes !== 0) {
            obj.block_max_bytes = Math.round(message.block_max_bytes);
        }
        if (message.block_max_gas !== 0) {
            obj.block_max_gas = Math.round(message.block_max_gas);
        }
        return obj;
    },
    create(base) {
        return exports.HashedParams.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseHashedParams();
        message.block_max_bytes = object.block_max_bytes ?? 0;
        message.block_max_gas = object.block_max_gas ?? 0;
        return message;
    }
};
exports.SynchronyParams = {
    $type: "tendermint.types.SynchronyParams",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.message_delay !== undefined) {
            duration_1.Duration.encode(message.message_delay, writer.uint32(10).fork()).join();
        }
        if (message.precision !== undefined) {
            duration_1.Duration.encode(message.precision, writer.uint32(18).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseSynchronyParams();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.message_delay = duration_1.Duration.decode(reader, reader.uint32());
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.precision = duration_1.Duration.decode(reader, reader.uint32());
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
            message_delay: isSet(object.message_delay) ? duration_1.Duration.fromJSON(object.message_delay) : undefined,
            precision: isSet(object.precision) ? duration_1.Duration.fromJSON(object.precision) : undefined
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.message_delay !== undefined) {
            obj.message_delay = duration_1.Duration.toJSON(message.message_delay);
        }
        if (message.precision !== undefined) {
            obj.precision = duration_1.Duration.toJSON(message.precision);
        }
        return obj;
    },
    create(base) {
        return exports.SynchronyParams.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseSynchronyParams();
        message.message_delay = object.message_delay !== undefined && object.message_delay !== null ? duration_1.Duration.fromPartial(object.message_delay) : undefined;
        message.precision = object.precision !== undefined && object.precision !== null ? duration_1.Duration.fromPartial(object.precision) : undefined;
        return message;
    }
};
exports.TimeoutParams = {
    $type: "tendermint.types.TimeoutParams",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.propose !== undefined) {
            duration_1.Duration.encode(message.propose, writer.uint32(10).fork()).join();
        }
        if (message.propose_delta !== undefined) {
            duration_1.Duration.encode(message.propose_delta, writer.uint32(18).fork()).join();
        }
        if (message.vote !== undefined) {
            duration_1.Duration.encode(message.vote, writer.uint32(26).fork()).join();
        }
        if (message.vote_delta !== undefined) {
            duration_1.Duration.encode(message.vote_delta, writer.uint32(34).fork()).join();
        }
        if (message.commit !== undefined) {
            duration_1.Duration.encode(message.commit, writer.uint32(42).fork()).join();
        }
        if (message.bypass_commit_timeout !== false) {
            writer.uint32(48).bool(message.bypass_commit_timeout);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseTimeoutParams();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.propose = duration_1.Duration.decode(reader, reader.uint32());
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.propose_delta = duration_1.Duration.decode(reader, reader.uint32());
                    continue;
                case 3:
                    if (tag !== 26) {
                        break;
                    }
                    message.vote = duration_1.Duration.decode(reader, reader.uint32());
                    continue;
                case 4:
                    if (tag !== 34) {
                        break;
                    }
                    message.vote_delta = duration_1.Duration.decode(reader, reader.uint32());
                    continue;
                case 5:
                    if (tag !== 42) {
                        break;
                    }
                    message.commit = duration_1.Duration.decode(reader, reader.uint32());
                    continue;
                case 6:
                    if (tag !== 48) {
                        break;
                    }
                    message.bypass_commit_timeout = reader.bool();
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
            propose: isSet(object.propose) ? duration_1.Duration.fromJSON(object.propose) : undefined,
            propose_delta: isSet(object.propose_delta) ? duration_1.Duration.fromJSON(object.propose_delta) : undefined,
            vote: isSet(object.vote) ? duration_1.Duration.fromJSON(object.vote) : undefined,
            vote_delta: isSet(object.vote_delta) ? duration_1.Duration.fromJSON(object.vote_delta) : undefined,
            commit: isSet(object.commit) ? duration_1.Duration.fromJSON(object.commit) : undefined,
            bypass_commit_timeout: isSet(object.bypass_commit_timeout) ? globalThis.Boolean(object.bypass_commit_timeout) : false
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.propose !== undefined) {
            obj.propose = duration_1.Duration.toJSON(message.propose);
        }
        if (message.propose_delta !== undefined) {
            obj.propose_delta = duration_1.Duration.toJSON(message.propose_delta);
        }
        if (message.vote !== undefined) {
            obj.vote = duration_1.Duration.toJSON(message.vote);
        }
        if (message.vote_delta !== undefined) {
            obj.vote_delta = duration_1.Duration.toJSON(message.vote_delta);
        }
        if (message.commit !== undefined) {
            obj.commit = duration_1.Duration.toJSON(message.commit);
        }
        if (message.bypass_commit_timeout !== false) {
            obj.bypass_commit_timeout = message.bypass_commit_timeout;
        }
        return obj;
    },
    create(base) {
        return exports.TimeoutParams.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseTimeoutParams();
        message.propose = object.propose !== undefined && object.propose !== null ? duration_1.Duration.fromPartial(object.propose) : undefined;
        message.propose_delta = object.propose_delta !== undefined && object.propose_delta !== null ? duration_1.Duration.fromPartial(object.propose_delta) : undefined;
        message.vote = object.vote !== undefined && object.vote !== null ? duration_1.Duration.fromPartial(object.vote) : undefined;
        message.vote_delta = object.vote_delta !== undefined && object.vote_delta !== null ? duration_1.Duration.fromPartial(object.vote_delta) : undefined;
        message.commit = object.commit !== undefined && object.commit !== null ? duration_1.Duration.fromPartial(object.commit) : undefined;
        message.bypass_commit_timeout = object.bypass_commit_timeout ?? false;
        return message;
    }
};
exports.ABCIParams = {
    $type: "tendermint.types.ABCIParams",
    encode(message, writer = new wire_1.BinaryWriter()) {
        if (message.vote_extensions_enable_height !== 0) {
            writer.uint32(8).int64(message.vote_extensions_enable_height);
        }
        if (message.recheck_tx !== false) {
            writer.uint32(16).bool(message.recheck_tx);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof wire_1.BinaryReader ? input : new wire_1.BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseABCIParams();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 8) {
                        break;
                    }
                    message.vote_extensions_enable_height = longToNumber(reader.int64());
                    continue;
                case 2:
                    if (tag !== 16) {
                        break;
                    }
                    message.recheck_tx = reader.bool();
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
            vote_extensions_enable_height: isSet(object.vote_extensions_enable_height) ? globalThis.Number(object.vote_extensions_enable_height) : 0,
            recheck_tx: isSet(object.recheck_tx) ? globalThis.Boolean(object.recheck_tx) : false
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.vote_extensions_enable_height !== 0) {
            obj.vote_extensions_enable_height = Math.round(message.vote_extensions_enable_height);
        }
        if (message.recheck_tx !== false) {
            obj.recheck_tx = message.recheck_tx;
        }
        return obj;
    },
    create(base) {
        return exports.ABCIParams.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseABCIParams();
        message.vote_extensions_enable_height = object.vote_extensions_enable_height ?? 0;
        message.recheck_tx = object.recheck_tx ?? false;
        return message;
    }
};
function createBaseConsensusParams() {
    return {
        block: undefined,
        evidence: undefined,
        validator: undefined,
        version: undefined,
        synchrony: undefined,
        timeout: undefined,
        abci: undefined
    };
}
function createBaseBlockParams() {
    return { max_bytes: 0, max_gas: 0 };
}
function createBaseEvidenceParams() {
    return { max_age_num_blocks: 0, max_age_duration: undefined, max_bytes: 0 };
}
function createBaseValidatorParams() {
    return { pub_key_types: [] };
}
function createBaseVersionParams() {
    return { app_version: 0 };
}
function createBaseHashedParams() {
    return { block_max_bytes: 0, block_max_gas: 0 };
}
function createBaseSynchronyParams() {
    return { message_delay: undefined, precision: undefined };
}
function createBaseTimeoutParams() {
    return {
        propose: undefined,
        propose_delta: undefined,
        vote: undefined,
        vote_delta: undefined,
        commit: undefined,
        bypass_commit_timeout: false
    };
}
function createBaseABCIParams() {
    return { vote_extensions_enable_height: 0, recheck_tx: false };
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
    ["/tendermint.types.ConsensusParams", exports.ConsensusParams],
    ["/tendermint.types.BlockParams", exports.BlockParams],
    ["/tendermint.types.EvidenceParams", exports.EvidenceParams],
    ["/tendermint.types.ValidatorParams", exports.ValidatorParams],
    ["/tendermint.types.VersionParams", exports.VersionParams],
    ["/tendermint.types.HashedParams", exports.HashedParams],
    ["/tendermint.types.SynchronyParams", exports.SynchronyParams],
    ["/tendermint.types.TimeoutParams", exports.TimeoutParams],
    ["/tendermint.types.ABCIParams", exports.ABCIParams]
];
exports.aminoConverters = {
    "/tendermint.types.ConsensusParams": {
        aminoType: "tendermint.types.ConsensusParams",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/tendermint.types.BlockParams": {
        aminoType: "tendermint.types.BlockParams",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/tendermint.types.EvidenceParams": {
        aminoType: "tendermint.types.EvidenceParams",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/tendermint.types.ValidatorParams": {
        aminoType: "tendermint.types.ValidatorParams",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/tendermint.types.VersionParams": {
        aminoType: "tendermint.types.VersionParams",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/tendermint.types.HashedParams": {
        aminoType: "tendermint.types.HashedParams",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/tendermint.types.SynchronyParams": {
        aminoType: "tendermint.types.SynchronyParams",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/tendermint.types.TimeoutParams": {
        aminoType: "tendermint.types.TimeoutParams",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/tendermint.types.ABCIParams": {
        aminoType: "tendermint.types.ABCIParams",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    }
};
