import { BinaryReader, BinaryWriter } from "@bufbuild/protobuf/wire";
import { Timestamp } from "../../google/protobuf/timestamp";
import { Proof } from "../crypto/proof";
import { Consensus } from "../version/types";
import { ValidatorSet } from "./validator";
import { BlockIDFlag, SignedMsgType } from "../../../types/tendermint/types";
export const PartSetHeader = {
    $type: "tendermint.types.PartSetHeader",
    encode(message, writer = new BinaryWriter()) {
        if (message.total !== 0) {
            writer.uint32(8).uint32(message.total);
        }
        if (message.hash.length !== 0) {
            writer.uint32(18).bytes(message.hash);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBasePartSetHeader();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 8) {
                        break;
                    }
                    message.total = reader.uint32();
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.hash = reader.bytes();
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
            total: isSet(object.total) ? globalThis.Number(object.total) : 0,
            hash: isSet(object.hash) ? bytesFromBase64(object.hash) : new Uint8Array(0)
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.total !== 0) {
            obj.total = Math.round(message.total);
        }
        if (message.hash.length !== 0) {
            obj.hash = base64FromBytes(message.hash);
        }
        return obj;
    },
    create(base) {
        return PartSetHeader.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBasePartSetHeader();
        message.total = object.total ?? 0;
        message.hash = object.hash ?? new Uint8Array(0);
        return message;
    }
};
export const Part = {
    $type: "tendermint.types.Part",
    encode(message, writer = new BinaryWriter()) {
        if (message.index !== 0) {
            writer.uint32(8).uint32(message.index);
        }
        if (message.bytes.length !== 0) {
            writer.uint32(18).bytes(message.bytes);
        }
        if (message.proof !== undefined) {
            Proof.encode(message.proof, writer.uint32(26).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBasePart();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 8) {
                        break;
                    }
                    message.index = reader.uint32();
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.bytes = reader.bytes();
                    continue;
                case 3:
                    if (tag !== 26) {
                        break;
                    }
                    message.proof = Proof.decode(reader, reader.uint32());
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
            index: isSet(object.index) ? globalThis.Number(object.index) : 0,
            bytes: isSet(object.bytes) ? bytesFromBase64(object.bytes) : new Uint8Array(0),
            proof: isSet(object.proof) ? Proof.fromJSON(object.proof) : undefined
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.index !== 0) {
            obj.index = Math.round(message.index);
        }
        if (message.bytes.length !== 0) {
            obj.bytes = base64FromBytes(message.bytes);
        }
        if (message.proof !== undefined) {
            obj.proof = Proof.toJSON(message.proof);
        }
        return obj;
    },
    create(base) {
        return Part.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBasePart();
        message.index = object.index ?? 0;
        message.bytes = object.bytes ?? new Uint8Array(0);
        message.proof = object.proof !== undefined && object.proof !== null ? Proof.fromPartial(object.proof) : undefined;
        return message;
    }
};
export const BlockID = {
    $type: "tendermint.types.BlockID",
    encode(message, writer = new BinaryWriter()) {
        if (message.hash.length !== 0) {
            writer.uint32(10).bytes(message.hash);
        }
        if (message.part_set_header !== undefined) {
            PartSetHeader.encode(message.part_set_header, writer.uint32(18).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseBlockID();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.hash = reader.bytes();
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.part_set_header = PartSetHeader.decode(reader, reader.uint32());
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
            hash: isSet(object.hash) ? bytesFromBase64(object.hash) : new Uint8Array(0),
            part_set_header: isSet(object.part_set_header) ? PartSetHeader.fromJSON(object.part_set_header) : undefined
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.hash.length !== 0) {
            obj.hash = base64FromBytes(message.hash);
        }
        if (message.part_set_header !== undefined) {
            obj.part_set_header = PartSetHeader.toJSON(message.part_set_header);
        }
        return obj;
    },
    create(base) {
        return BlockID.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseBlockID();
        message.hash = object.hash ?? new Uint8Array(0);
        message.part_set_header =
            object.part_set_header !== undefined && object.part_set_header !== null ? PartSetHeader.fromPartial(object.part_set_header) : undefined;
        return message;
    }
};
export const Header = {
    $type: "tendermint.types.Header",
    encode(message, writer = new BinaryWriter()) {
        if (message.version !== undefined) {
            Consensus.encode(message.version, writer.uint32(10).fork()).join();
        }
        if (message.chain_id !== "") {
            writer.uint32(18).string(message.chain_id);
        }
        if (message.height !== 0) {
            writer.uint32(24).int64(message.height);
        }
        if (message.time !== undefined) {
            Timestamp.encode(toTimestamp(message.time), writer.uint32(34).fork()).join();
        }
        if (message.last_block_id !== undefined) {
            BlockID.encode(message.last_block_id, writer.uint32(42).fork()).join();
        }
        if (message.last_commit_hash.length !== 0) {
            writer.uint32(50).bytes(message.last_commit_hash);
        }
        if (message.data_hash.length !== 0) {
            writer.uint32(58).bytes(message.data_hash);
        }
        if (message.validators_hash.length !== 0) {
            writer.uint32(66).bytes(message.validators_hash);
        }
        if (message.next_validators_hash.length !== 0) {
            writer.uint32(74).bytes(message.next_validators_hash);
        }
        if (message.consensus_hash.length !== 0) {
            writer.uint32(82).bytes(message.consensus_hash);
        }
        if (message.app_hash.length !== 0) {
            writer.uint32(90).bytes(message.app_hash);
        }
        if (message.last_results_hash.length !== 0) {
            writer.uint32(98).bytes(message.last_results_hash);
        }
        if (message.evidence_hash.length !== 0) {
            writer.uint32(106).bytes(message.evidence_hash);
        }
        if (message.proposer_address.length !== 0) {
            writer.uint32(114).bytes(message.proposer_address);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseHeader();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.version = Consensus.decode(reader, reader.uint32());
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.chain_id = reader.string();
                    continue;
                case 3:
                    if (tag !== 24) {
                        break;
                    }
                    message.height = longToNumber(reader.int64());
                    continue;
                case 4:
                    if (tag !== 34) {
                        break;
                    }
                    message.time = fromTimestamp(Timestamp.decode(reader, reader.uint32()));
                    continue;
                case 5:
                    if (tag !== 42) {
                        break;
                    }
                    message.last_block_id = BlockID.decode(reader, reader.uint32());
                    continue;
                case 6:
                    if (tag !== 50) {
                        break;
                    }
                    message.last_commit_hash = reader.bytes();
                    continue;
                case 7:
                    if (tag !== 58) {
                        break;
                    }
                    message.data_hash = reader.bytes();
                    continue;
                case 8:
                    if (tag !== 66) {
                        break;
                    }
                    message.validators_hash = reader.bytes();
                    continue;
                case 9:
                    if (tag !== 74) {
                        break;
                    }
                    message.next_validators_hash = reader.bytes();
                    continue;
                case 10:
                    if (tag !== 82) {
                        break;
                    }
                    message.consensus_hash = reader.bytes();
                    continue;
                case 11:
                    if (tag !== 90) {
                        break;
                    }
                    message.app_hash = reader.bytes();
                    continue;
                case 12:
                    if (tag !== 98) {
                        break;
                    }
                    message.last_results_hash = reader.bytes();
                    continue;
                case 13:
                    if (tag !== 106) {
                        break;
                    }
                    message.evidence_hash = reader.bytes();
                    continue;
                case 14:
                    if (tag !== 114) {
                        break;
                    }
                    message.proposer_address = reader.bytes();
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
            version: isSet(object.version) ? Consensus.fromJSON(object.version) : undefined,
            chain_id: isSet(object.chain_id) ? globalThis.String(object.chain_id) : "",
            height: isSet(object.height) ? globalThis.Number(object.height) : 0,
            time: isSet(object.time) ? fromJsonTimestamp(object.time) : undefined,
            last_block_id: isSet(object.last_block_id) ? BlockID.fromJSON(object.last_block_id) : undefined,
            last_commit_hash: isSet(object.last_commit_hash) ? bytesFromBase64(object.last_commit_hash) : new Uint8Array(0),
            data_hash: isSet(object.data_hash) ? bytesFromBase64(object.data_hash) : new Uint8Array(0),
            validators_hash: isSet(object.validators_hash) ? bytesFromBase64(object.validators_hash) : new Uint8Array(0),
            next_validators_hash: isSet(object.next_validators_hash) ? bytesFromBase64(object.next_validators_hash) : new Uint8Array(0),
            consensus_hash: isSet(object.consensus_hash) ? bytesFromBase64(object.consensus_hash) : new Uint8Array(0),
            app_hash: isSet(object.app_hash) ? bytesFromBase64(object.app_hash) : new Uint8Array(0),
            last_results_hash: isSet(object.last_results_hash) ? bytesFromBase64(object.last_results_hash) : new Uint8Array(0),
            evidence_hash: isSet(object.evidence_hash) ? bytesFromBase64(object.evidence_hash) : new Uint8Array(0),
            proposer_address: isSet(object.proposer_address) ? bytesFromBase64(object.proposer_address) : new Uint8Array(0)
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.version !== undefined) {
            obj.version = Consensus.toJSON(message.version);
        }
        if (message.chain_id !== "") {
            obj.chain_id = message.chain_id;
        }
        if (message.height !== 0) {
            obj.height = Math.round(message.height);
        }
        if (message.time !== undefined) {
            obj.time = message.time.toISOString();
        }
        if (message.last_block_id !== undefined) {
            obj.last_block_id = BlockID.toJSON(message.last_block_id);
        }
        if (message.last_commit_hash.length !== 0) {
            obj.last_commit_hash = base64FromBytes(message.last_commit_hash);
        }
        if (message.data_hash.length !== 0) {
            obj.data_hash = base64FromBytes(message.data_hash);
        }
        if (message.validators_hash.length !== 0) {
            obj.validators_hash = base64FromBytes(message.validators_hash);
        }
        if (message.next_validators_hash.length !== 0) {
            obj.next_validators_hash = base64FromBytes(message.next_validators_hash);
        }
        if (message.consensus_hash.length !== 0) {
            obj.consensus_hash = base64FromBytes(message.consensus_hash);
        }
        if (message.app_hash.length !== 0) {
            obj.app_hash = base64FromBytes(message.app_hash);
        }
        if (message.last_results_hash.length !== 0) {
            obj.last_results_hash = base64FromBytes(message.last_results_hash);
        }
        if (message.evidence_hash.length !== 0) {
            obj.evidence_hash = base64FromBytes(message.evidence_hash);
        }
        if (message.proposer_address.length !== 0) {
            obj.proposer_address = base64FromBytes(message.proposer_address);
        }
        return obj;
    },
    create(base) {
        return Header.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseHeader();
        message.version = object.version !== undefined && object.version !== null ? Consensus.fromPartial(object.version) : undefined;
        message.chain_id = object.chain_id ?? "";
        message.height = object.height ?? 0;
        message.time = object.time ?? undefined;
        message.last_block_id = object.last_block_id !== undefined && object.last_block_id !== null ? BlockID.fromPartial(object.last_block_id) : undefined;
        message.last_commit_hash = object.last_commit_hash ?? new Uint8Array(0);
        message.data_hash = object.data_hash ?? new Uint8Array(0);
        message.validators_hash = object.validators_hash ?? new Uint8Array(0);
        message.next_validators_hash = object.next_validators_hash ?? new Uint8Array(0);
        message.consensus_hash = object.consensus_hash ?? new Uint8Array(0);
        message.app_hash = object.app_hash ?? new Uint8Array(0);
        message.last_results_hash = object.last_results_hash ?? new Uint8Array(0);
        message.evidence_hash = object.evidence_hash ?? new Uint8Array(0);
        message.proposer_address = object.proposer_address ?? new Uint8Array(0);
        return message;
    }
};
export const Data = {
    $type: "tendermint.types.Data",
    encode(message, writer = new BinaryWriter()) {
        for (const v of message.txs) {
            writer.uint32(10).bytes(v);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseData();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.txs.push(reader.bytes());
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
        return { txs: globalThis.Array.isArray(object?.txs) ? object.txs.map((e) => bytesFromBase64(e)) : [] };
    },
    toJSON(message) {
        const obj = {};
        if (message.txs?.length) {
            obj.txs = message.txs.map((e) => base64FromBytes(e));
        }
        return obj;
    },
    create(base) {
        return Data.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseData();
        message.txs = object.txs?.map((e) => e) || [];
        return message;
    }
};
export const Vote = {
    $type: "tendermint.types.Vote",
    encode(message, writer = new BinaryWriter()) {
        if (message.type !== 0) {
            writer.uint32(8).int32(message.type);
        }
        if (message.height !== 0) {
            writer.uint32(16).int64(message.height);
        }
        if (message.round !== 0) {
            writer.uint32(24).int32(message.round);
        }
        if (message.block_id !== undefined) {
            BlockID.encode(message.block_id, writer.uint32(34).fork()).join();
        }
        if (message.timestamp !== undefined) {
            Timestamp.encode(toTimestamp(message.timestamp), writer.uint32(42).fork()).join();
        }
        if (message.validator_address.length !== 0) {
            writer.uint32(50).bytes(message.validator_address);
        }
        if (message.validator_index !== 0) {
            writer.uint32(56).int32(message.validator_index);
        }
        if (message.signature.length !== 0) {
            writer.uint32(66).bytes(message.signature);
        }
        if (message.extension.length !== 0) {
            writer.uint32(74).bytes(message.extension);
        }
        if (message.extension_signature.length !== 0) {
            writer.uint32(82).bytes(message.extension_signature);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseVote();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 8) {
                        break;
                    }
                    message.type = reader.int32();
                    continue;
                case 2:
                    if (tag !== 16) {
                        break;
                    }
                    message.height = longToNumber(reader.int64());
                    continue;
                case 3:
                    if (tag !== 24) {
                        break;
                    }
                    message.round = reader.int32();
                    continue;
                case 4:
                    if (tag !== 34) {
                        break;
                    }
                    message.block_id = BlockID.decode(reader, reader.uint32());
                    continue;
                case 5:
                    if (tag !== 42) {
                        break;
                    }
                    message.timestamp = fromTimestamp(Timestamp.decode(reader, reader.uint32()));
                    continue;
                case 6:
                    if (tag !== 50) {
                        break;
                    }
                    message.validator_address = reader.bytes();
                    continue;
                case 7:
                    if (tag !== 56) {
                        break;
                    }
                    message.validator_index = reader.int32();
                    continue;
                case 8:
                    if (tag !== 66) {
                        break;
                    }
                    message.signature = reader.bytes();
                    continue;
                case 9:
                    if (tag !== 74) {
                        break;
                    }
                    message.extension = reader.bytes();
                    continue;
                case 10:
                    if (tag !== 82) {
                        break;
                    }
                    message.extension_signature = reader.bytes();
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
            type: isSet(object.type) ? signedMsgTypeFromJSON(object.type) : 0,
            height: isSet(object.height) ? globalThis.Number(object.height) : 0,
            round: isSet(object.round) ? globalThis.Number(object.round) : 0,
            block_id: isSet(object.block_id) ? BlockID.fromJSON(object.block_id) : undefined,
            timestamp: isSet(object.timestamp) ? fromJsonTimestamp(object.timestamp) : undefined,
            validator_address: isSet(object.validator_address) ? bytesFromBase64(object.validator_address) : new Uint8Array(0),
            validator_index: isSet(object.validator_index) ? globalThis.Number(object.validator_index) : 0,
            signature: isSet(object.signature) ? bytesFromBase64(object.signature) : new Uint8Array(0),
            extension: isSet(object.extension) ? bytesFromBase64(object.extension) : new Uint8Array(0),
            extension_signature: isSet(object.extension_signature) ? bytesFromBase64(object.extension_signature) : new Uint8Array(0)
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.type !== 0) {
            obj.type = signedMsgTypeToJSON(message.type);
        }
        if (message.height !== 0) {
            obj.height = Math.round(message.height);
        }
        if (message.round !== 0) {
            obj.round = Math.round(message.round);
        }
        if (message.block_id !== undefined) {
            obj.block_id = BlockID.toJSON(message.block_id);
        }
        if (message.timestamp !== undefined) {
            obj.timestamp = message.timestamp.toISOString();
        }
        if (message.validator_address.length !== 0) {
            obj.validator_address = base64FromBytes(message.validator_address);
        }
        if (message.validator_index !== 0) {
            obj.validator_index = Math.round(message.validator_index);
        }
        if (message.signature.length !== 0) {
            obj.signature = base64FromBytes(message.signature);
        }
        if (message.extension.length !== 0) {
            obj.extension = base64FromBytes(message.extension);
        }
        if (message.extension_signature.length !== 0) {
            obj.extension_signature = base64FromBytes(message.extension_signature);
        }
        return obj;
    },
    create(base) {
        return Vote.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseVote();
        message.type = object.type ?? 0;
        message.height = object.height ?? 0;
        message.round = object.round ?? 0;
        message.block_id = object.block_id !== undefined && object.block_id !== null ? BlockID.fromPartial(object.block_id) : undefined;
        message.timestamp = object.timestamp ?? undefined;
        message.validator_address = object.validator_address ?? new Uint8Array(0);
        message.validator_index = object.validator_index ?? 0;
        message.signature = object.signature ?? new Uint8Array(0);
        message.extension = object.extension ?? new Uint8Array(0);
        message.extension_signature = object.extension_signature ?? new Uint8Array(0);
        return message;
    }
};
export const Commit = {
    $type: "tendermint.types.Commit",
    encode(message, writer = new BinaryWriter()) {
        if (message.height !== 0) {
            writer.uint32(8).int64(message.height);
        }
        if (message.round !== 0) {
            writer.uint32(16).int32(message.round);
        }
        if (message.block_id !== undefined) {
            BlockID.encode(message.block_id, writer.uint32(26).fork()).join();
        }
        for (const v of message.signatures) {
            CommitSig.encode(v, writer.uint32(34).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseCommit();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 8) {
                        break;
                    }
                    message.height = longToNumber(reader.int64());
                    continue;
                case 2:
                    if (tag !== 16) {
                        break;
                    }
                    message.round = reader.int32();
                    continue;
                case 3:
                    if (tag !== 26) {
                        break;
                    }
                    message.block_id = BlockID.decode(reader, reader.uint32());
                    continue;
                case 4:
                    if (tag !== 34) {
                        break;
                    }
                    message.signatures.push(CommitSig.decode(reader, reader.uint32()));
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
            height: isSet(object.height) ? globalThis.Number(object.height) : 0,
            round: isSet(object.round) ? globalThis.Number(object.round) : 0,
            block_id: isSet(object.block_id) ? BlockID.fromJSON(object.block_id) : undefined,
            signatures: globalThis.Array.isArray(object?.signatures) ? object.signatures.map((e) => CommitSig.fromJSON(e)) : []
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.height !== 0) {
            obj.height = Math.round(message.height);
        }
        if (message.round !== 0) {
            obj.round = Math.round(message.round);
        }
        if (message.block_id !== undefined) {
            obj.block_id = BlockID.toJSON(message.block_id);
        }
        if (message.signatures?.length) {
            obj.signatures = message.signatures.map((e) => CommitSig.toJSON(e));
        }
        return obj;
    },
    create(base) {
        return Commit.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseCommit();
        message.height = object.height ?? 0;
        message.round = object.round ?? 0;
        message.block_id = object.block_id !== undefined && object.block_id !== null ? BlockID.fromPartial(object.block_id) : undefined;
        message.signatures = object.signatures?.map((e) => CommitSig.fromPartial(e)) || [];
        return message;
    }
};
export const CommitSig = {
    $type: "tendermint.types.CommitSig",
    encode(message, writer = new BinaryWriter()) {
        if (message.block_id_flag !== 0) {
            writer.uint32(8).int32(message.block_id_flag);
        }
        if (message.validator_address.length !== 0) {
            writer.uint32(18).bytes(message.validator_address);
        }
        if (message.timestamp !== undefined) {
            Timestamp.encode(toTimestamp(message.timestamp), writer.uint32(26).fork()).join();
        }
        if (message.signature.length !== 0) {
            writer.uint32(34).bytes(message.signature);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseCommitSig();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 8) {
                        break;
                    }
                    message.block_id_flag = reader.int32();
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.validator_address = reader.bytes();
                    continue;
                case 3:
                    if (tag !== 26) {
                        break;
                    }
                    message.timestamp = fromTimestamp(Timestamp.decode(reader, reader.uint32()));
                    continue;
                case 4:
                    if (tag !== 34) {
                        break;
                    }
                    message.signature = reader.bytes();
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
            block_id_flag: isSet(object.block_id_flag) ? blockIDFlagFromJSON(object.block_id_flag) : 0,
            validator_address: isSet(object.validator_address) ? bytesFromBase64(object.validator_address) : new Uint8Array(0),
            timestamp: isSet(object.timestamp) ? fromJsonTimestamp(object.timestamp) : undefined,
            signature: isSet(object.signature) ? bytesFromBase64(object.signature) : new Uint8Array(0)
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.block_id_flag !== 0) {
            obj.block_id_flag = blockIDFlagToJSON(message.block_id_flag);
        }
        if (message.validator_address.length !== 0) {
            obj.validator_address = base64FromBytes(message.validator_address);
        }
        if (message.timestamp !== undefined) {
            obj.timestamp = message.timestamp.toISOString();
        }
        if (message.signature.length !== 0) {
            obj.signature = base64FromBytes(message.signature);
        }
        return obj;
    },
    create(base) {
        return CommitSig.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseCommitSig();
        message.block_id_flag = object.block_id_flag ?? 0;
        message.validator_address = object.validator_address ?? new Uint8Array(0);
        message.timestamp = object.timestamp ?? undefined;
        message.signature = object.signature ?? new Uint8Array(0);
        return message;
    }
};
export const ExtendedCommit = {
    $type: "tendermint.types.ExtendedCommit",
    encode(message, writer = new BinaryWriter()) {
        if (message.height !== 0) {
            writer.uint32(8).int64(message.height);
        }
        if (message.round !== 0) {
            writer.uint32(16).int32(message.round);
        }
        if (message.block_id !== undefined) {
            BlockID.encode(message.block_id, writer.uint32(26).fork()).join();
        }
        for (const v of message.extended_signatures) {
            ExtendedCommitSig.encode(v, writer.uint32(34).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseExtendedCommit();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 8) {
                        break;
                    }
                    message.height = longToNumber(reader.int64());
                    continue;
                case 2:
                    if (tag !== 16) {
                        break;
                    }
                    message.round = reader.int32();
                    continue;
                case 3:
                    if (tag !== 26) {
                        break;
                    }
                    message.block_id = BlockID.decode(reader, reader.uint32());
                    continue;
                case 4:
                    if (tag !== 34) {
                        break;
                    }
                    message.extended_signatures.push(ExtendedCommitSig.decode(reader, reader.uint32()));
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
            height: isSet(object.height) ? globalThis.Number(object.height) : 0,
            round: isSet(object.round) ? globalThis.Number(object.round) : 0,
            block_id: isSet(object.block_id) ? BlockID.fromJSON(object.block_id) : undefined,
            extended_signatures: globalThis.Array.isArray(object?.extended_signatures)
                ? object.extended_signatures.map((e) => ExtendedCommitSig.fromJSON(e))
                : []
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.height !== 0) {
            obj.height = Math.round(message.height);
        }
        if (message.round !== 0) {
            obj.round = Math.round(message.round);
        }
        if (message.block_id !== undefined) {
            obj.block_id = BlockID.toJSON(message.block_id);
        }
        if (message.extended_signatures?.length) {
            obj.extended_signatures = message.extended_signatures.map((e) => ExtendedCommitSig.toJSON(e));
        }
        return obj;
    },
    create(base) {
        return ExtendedCommit.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseExtendedCommit();
        message.height = object.height ?? 0;
        message.round = object.round ?? 0;
        message.block_id = object.block_id !== undefined && object.block_id !== null ? BlockID.fromPartial(object.block_id) : undefined;
        message.extended_signatures = object.extended_signatures?.map((e) => ExtendedCommitSig.fromPartial(e)) || [];
        return message;
    }
};
export const ExtendedCommitSig = {
    $type: "tendermint.types.ExtendedCommitSig",
    encode(message, writer = new BinaryWriter()) {
        if (message.block_id_flag !== 0) {
            writer.uint32(8).int32(message.block_id_flag);
        }
        if (message.validator_address.length !== 0) {
            writer.uint32(18).bytes(message.validator_address);
        }
        if (message.timestamp !== undefined) {
            Timestamp.encode(toTimestamp(message.timestamp), writer.uint32(26).fork()).join();
        }
        if (message.signature.length !== 0) {
            writer.uint32(34).bytes(message.signature);
        }
        if (message.extension.length !== 0) {
            writer.uint32(42).bytes(message.extension);
        }
        if (message.extension_signature.length !== 0) {
            writer.uint32(50).bytes(message.extension_signature);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseExtendedCommitSig();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 8) {
                        break;
                    }
                    message.block_id_flag = reader.int32();
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.validator_address = reader.bytes();
                    continue;
                case 3:
                    if (tag !== 26) {
                        break;
                    }
                    message.timestamp = fromTimestamp(Timestamp.decode(reader, reader.uint32()));
                    continue;
                case 4:
                    if (tag !== 34) {
                        break;
                    }
                    message.signature = reader.bytes();
                    continue;
                case 5:
                    if (tag !== 42) {
                        break;
                    }
                    message.extension = reader.bytes();
                    continue;
                case 6:
                    if (tag !== 50) {
                        break;
                    }
                    message.extension_signature = reader.bytes();
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
            block_id_flag: isSet(object.block_id_flag) ? blockIDFlagFromJSON(object.block_id_flag) : 0,
            validator_address: isSet(object.validator_address) ? bytesFromBase64(object.validator_address) : new Uint8Array(0),
            timestamp: isSet(object.timestamp) ? fromJsonTimestamp(object.timestamp) : undefined,
            signature: isSet(object.signature) ? bytesFromBase64(object.signature) : new Uint8Array(0),
            extension: isSet(object.extension) ? bytesFromBase64(object.extension) : new Uint8Array(0),
            extension_signature: isSet(object.extension_signature) ? bytesFromBase64(object.extension_signature) : new Uint8Array(0)
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.block_id_flag !== 0) {
            obj.block_id_flag = blockIDFlagToJSON(message.block_id_flag);
        }
        if (message.validator_address.length !== 0) {
            obj.validator_address = base64FromBytes(message.validator_address);
        }
        if (message.timestamp !== undefined) {
            obj.timestamp = message.timestamp.toISOString();
        }
        if (message.signature.length !== 0) {
            obj.signature = base64FromBytes(message.signature);
        }
        if (message.extension.length !== 0) {
            obj.extension = base64FromBytes(message.extension);
        }
        if (message.extension_signature.length !== 0) {
            obj.extension_signature = base64FromBytes(message.extension_signature);
        }
        return obj;
    },
    create(base) {
        return ExtendedCommitSig.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseExtendedCommitSig();
        message.block_id_flag = object.block_id_flag ?? 0;
        message.validator_address = object.validator_address ?? new Uint8Array(0);
        message.timestamp = object.timestamp ?? undefined;
        message.signature = object.signature ?? new Uint8Array(0);
        message.extension = object.extension ?? new Uint8Array(0);
        message.extension_signature = object.extension_signature ?? new Uint8Array(0);
        return message;
    }
};
export const Proposal = {
    $type: "tendermint.types.Proposal",
    encode(message, writer = new BinaryWriter()) {
        if (message.type !== 0) {
            writer.uint32(8).int32(message.type);
        }
        if (message.height !== 0) {
            writer.uint32(16).int64(message.height);
        }
        if (message.round !== 0) {
            writer.uint32(24).int32(message.round);
        }
        if (message.pol_round !== 0) {
            writer.uint32(32).int32(message.pol_round);
        }
        if (message.block_id !== undefined) {
            BlockID.encode(message.block_id, writer.uint32(42).fork()).join();
        }
        if (message.timestamp !== undefined) {
            Timestamp.encode(toTimestamp(message.timestamp), writer.uint32(50).fork()).join();
        }
        if (message.signature.length !== 0) {
            writer.uint32(58).bytes(message.signature);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseProposal();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 8) {
                        break;
                    }
                    message.type = reader.int32();
                    continue;
                case 2:
                    if (tag !== 16) {
                        break;
                    }
                    message.height = longToNumber(reader.int64());
                    continue;
                case 3:
                    if (tag !== 24) {
                        break;
                    }
                    message.round = reader.int32();
                    continue;
                case 4:
                    if (tag !== 32) {
                        break;
                    }
                    message.pol_round = reader.int32();
                    continue;
                case 5:
                    if (tag !== 42) {
                        break;
                    }
                    message.block_id = BlockID.decode(reader, reader.uint32());
                    continue;
                case 6:
                    if (tag !== 50) {
                        break;
                    }
                    message.timestamp = fromTimestamp(Timestamp.decode(reader, reader.uint32()));
                    continue;
                case 7:
                    if (tag !== 58) {
                        break;
                    }
                    message.signature = reader.bytes();
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
            type: isSet(object.type) ? signedMsgTypeFromJSON(object.type) : 0,
            height: isSet(object.height) ? globalThis.Number(object.height) : 0,
            round: isSet(object.round) ? globalThis.Number(object.round) : 0,
            pol_round: isSet(object.pol_round) ? globalThis.Number(object.pol_round) : 0,
            block_id: isSet(object.block_id) ? BlockID.fromJSON(object.block_id) : undefined,
            timestamp: isSet(object.timestamp) ? fromJsonTimestamp(object.timestamp) : undefined,
            signature: isSet(object.signature) ? bytesFromBase64(object.signature) : new Uint8Array(0)
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.type !== 0) {
            obj.type = signedMsgTypeToJSON(message.type);
        }
        if (message.height !== 0) {
            obj.height = Math.round(message.height);
        }
        if (message.round !== 0) {
            obj.round = Math.round(message.round);
        }
        if (message.pol_round !== 0) {
            obj.pol_round = Math.round(message.pol_round);
        }
        if (message.block_id !== undefined) {
            obj.block_id = BlockID.toJSON(message.block_id);
        }
        if (message.timestamp !== undefined) {
            obj.timestamp = message.timestamp.toISOString();
        }
        if (message.signature.length !== 0) {
            obj.signature = base64FromBytes(message.signature);
        }
        return obj;
    },
    create(base) {
        return Proposal.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseProposal();
        message.type = object.type ?? 0;
        message.height = object.height ?? 0;
        message.round = object.round ?? 0;
        message.pol_round = object.pol_round ?? 0;
        message.block_id = object.block_id !== undefined && object.block_id !== null ? BlockID.fromPartial(object.block_id) : undefined;
        message.timestamp = object.timestamp ?? undefined;
        message.signature = object.signature ?? new Uint8Array(0);
        return message;
    }
};
export const SignedHeader = {
    $type: "tendermint.types.SignedHeader",
    encode(message, writer = new BinaryWriter()) {
        if (message.header !== undefined) {
            Header.encode(message.header, writer.uint32(10).fork()).join();
        }
        if (message.commit !== undefined) {
            Commit.encode(message.commit, writer.uint32(18).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseSignedHeader();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.header = Header.decode(reader, reader.uint32());
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.commit = Commit.decode(reader, reader.uint32());
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
            header: isSet(object.header) ? Header.fromJSON(object.header) : undefined,
            commit: isSet(object.commit) ? Commit.fromJSON(object.commit) : undefined
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.header !== undefined) {
            obj.header = Header.toJSON(message.header);
        }
        if (message.commit !== undefined) {
            obj.commit = Commit.toJSON(message.commit);
        }
        return obj;
    },
    create(base) {
        return SignedHeader.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseSignedHeader();
        message.header = object.header !== undefined && object.header !== null ? Header.fromPartial(object.header) : undefined;
        message.commit = object.commit !== undefined && object.commit !== null ? Commit.fromPartial(object.commit) : undefined;
        return message;
    }
};
export const LightBlock = {
    $type: "tendermint.types.LightBlock",
    encode(message, writer = new BinaryWriter()) {
        if (message.signed_header !== undefined) {
            SignedHeader.encode(message.signed_header, writer.uint32(10).fork()).join();
        }
        if (message.validator_set !== undefined) {
            ValidatorSet.encode(message.validator_set, writer.uint32(18).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseLightBlock();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.signed_header = SignedHeader.decode(reader, reader.uint32());
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.validator_set = ValidatorSet.decode(reader, reader.uint32());
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
            signed_header: isSet(object.signed_header) ? SignedHeader.fromJSON(object.signed_header) : undefined,
            validator_set: isSet(object.validator_set) ? ValidatorSet.fromJSON(object.validator_set) : undefined
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.signed_header !== undefined) {
            obj.signed_header = SignedHeader.toJSON(message.signed_header);
        }
        if (message.validator_set !== undefined) {
            obj.validator_set = ValidatorSet.toJSON(message.validator_set);
        }
        return obj;
    },
    create(base) {
        return LightBlock.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseLightBlock();
        message.signed_header = object.signed_header !== undefined && object.signed_header !== null ? SignedHeader.fromPartial(object.signed_header) : undefined;
        message.validator_set = object.validator_set !== undefined && object.validator_set !== null ? ValidatorSet.fromPartial(object.validator_set) : undefined;
        return message;
    }
};
export const BlockMeta = {
    $type: "tendermint.types.BlockMeta",
    encode(message, writer = new BinaryWriter()) {
        if (message.block_id !== undefined) {
            BlockID.encode(message.block_id, writer.uint32(10).fork()).join();
        }
        if (message.block_size !== 0) {
            writer.uint32(16).int64(message.block_size);
        }
        if (message.header !== undefined) {
            Header.encode(message.header, writer.uint32(26).fork()).join();
        }
        if (message.num_txs !== 0) {
            writer.uint32(32).int64(message.num_txs);
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseBlockMeta();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.block_id = BlockID.decode(reader, reader.uint32());
                    continue;
                case 2:
                    if (tag !== 16) {
                        break;
                    }
                    message.block_size = longToNumber(reader.int64());
                    continue;
                case 3:
                    if (tag !== 26) {
                        break;
                    }
                    message.header = Header.decode(reader, reader.uint32());
                    continue;
                case 4:
                    if (tag !== 32) {
                        break;
                    }
                    message.num_txs = longToNumber(reader.int64());
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
            block_id: isSet(object.block_id) ? BlockID.fromJSON(object.block_id) : undefined,
            block_size: isSet(object.block_size) ? globalThis.Number(object.block_size) : 0,
            header: isSet(object.header) ? Header.fromJSON(object.header) : undefined,
            num_txs: isSet(object.num_txs) ? globalThis.Number(object.num_txs) : 0
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.block_id !== undefined) {
            obj.block_id = BlockID.toJSON(message.block_id);
        }
        if (message.block_size !== 0) {
            obj.block_size = Math.round(message.block_size);
        }
        if (message.header !== undefined) {
            obj.header = Header.toJSON(message.header);
        }
        if (message.num_txs !== 0) {
            obj.num_txs = Math.round(message.num_txs);
        }
        return obj;
    },
    create(base) {
        return BlockMeta.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseBlockMeta();
        message.block_id = object.block_id !== undefined && object.block_id !== null ? BlockID.fromPartial(object.block_id) : undefined;
        message.block_size = object.block_size ?? 0;
        message.header = object.header !== undefined && object.header !== null ? Header.fromPartial(object.header) : undefined;
        message.num_txs = object.num_txs ?? 0;
        return message;
    }
};
export const TxProof = {
    $type: "tendermint.types.TxProof",
    encode(message, writer = new BinaryWriter()) {
        if (message.root_hash.length !== 0) {
            writer.uint32(10).bytes(message.root_hash);
        }
        if (message.data.length !== 0) {
            writer.uint32(18).bytes(message.data);
        }
        if (message.proof !== undefined) {
            Proof.encode(message.proof, writer.uint32(26).fork()).join();
        }
        return writer;
    },
    decode(input, length) {
        const reader = input instanceof BinaryReader ? input : new BinaryReader(input);
        const end = length === undefined ? reader.len : reader.pos + length;
        const message = createBaseTxProof();
        while (reader.pos < end) {
            const tag = reader.uint32();
            switch (tag >>> 3) {
                case 1:
                    if (tag !== 10) {
                        break;
                    }
                    message.root_hash = reader.bytes();
                    continue;
                case 2:
                    if (tag !== 18) {
                        break;
                    }
                    message.data = reader.bytes();
                    continue;
                case 3:
                    if (tag !== 26) {
                        break;
                    }
                    message.proof = Proof.decode(reader, reader.uint32());
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
            root_hash: isSet(object.root_hash) ? bytesFromBase64(object.root_hash) : new Uint8Array(0),
            data: isSet(object.data) ? bytesFromBase64(object.data) : new Uint8Array(0),
            proof: isSet(object.proof) ? Proof.fromJSON(object.proof) : undefined
        };
    },
    toJSON(message) {
        const obj = {};
        if (message.root_hash.length !== 0) {
            obj.root_hash = base64FromBytes(message.root_hash);
        }
        if (message.data.length !== 0) {
            obj.data = base64FromBytes(message.data);
        }
        if (message.proof !== undefined) {
            obj.proof = Proof.toJSON(message.proof);
        }
        return obj;
    },
    create(base) {
        return TxProof.fromPartial(base ?? {});
    },
    fromPartial(object) {
        const message = createBaseTxProof();
        message.root_hash = object.root_hash ?? new Uint8Array(0);
        message.data = object.data ?? new Uint8Array(0);
        message.proof = object.proof !== undefined && object.proof !== null ? Proof.fromPartial(object.proof) : undefined;
        return message;
    }
};
export function blockIDFlagFromJSON(object) {
    switch (object) {
        case 0:
        case "BLOCK_ID_FLAG_UNKNOWN":
            return BlockIDFlag.BLOCK_ID_FLAG_UNKNOWN;
        case 1:
        case "BLOCK_ID_FLAG_ABSENT":
            return BlockIDFlag.BLOCK_ID_FLAG_ABSENT;
        case 2:
        case "BLOCK_ID_FLAG_COMMIT":
            return BlockIDFlag.BLOCK_ID_FLAG_COMMIT;
        case 3:
        case "BLOCK_ID_FLAG_NIL":
            return BlockIDFlag.BLOCK_ID_FLAG_NIL;
        case -1:
        case "UNRECOGNIZED":
        default:
            return BlockIDFlag.UNRECOGNIZED;
    }
}
export function blockIDFlagToJSON(object) {
    switch (object) {
        case BlockIDFlag.BLOCK_ID_FLAG_UNKNOWN:
            return "BLOCK_ID_FLAG_UNKNOWN";
        case BlockIDFlag.BLOCK_ID_FLAG_ABSENT:
            return "BLOCK_ID_FLAG_ABSENT";
        case BlockIDFlag.BLOCK_ID_FLAG_COMMIT:
            return "BLOCK_ID_FLAG_COMMIT";
        case BlockIDFlag.BLOCK_ID_FLAG_NIL:
            return "BLOCK_ID_FLAG_NIL";
        case BlockIDFlag.UNRECOGNIZED:
        default:
            return "UNRECOGNIZED";
    }
}
export function signedMsgTypeFromJSON(object) {
    switch (object) {
        case 0:
        case "SIGNED_MSG_TYPE_UNKNOWN":
            return SignedMsgType.SIGNED_MSG_TYPE_UNKNOWN;
        case 1:
        case "SIGNED_MSG_TYPE_PREVOTE":
            return SignedMsgType.SIGNED_MSG_TYPE_PREVOTE;
        case 2:
        case "SIGNED_MSG_TYPE_PRECOMMIT":
            return SignedMsgType.SIGNED_MSG_TYPE_PRECOMMIT;
        case 32:
        case "SIGNED_MSG_TYPE_PROPOSAL":
            return SignedMsgType.SIGNED_MSG_TYPE_PROPOSAL;
        case -1:
        case "UNRECOGNIZED":
        default:
            return SignedMsgType.UNRECOGNIZED;
    }
}
export function signedMsgTypeToJSON(object) {
    switch (object) {
        case SignedMsgType.SIGNED_MSG_TYPE_UNKNOWN:
            return "SIGNED_MSG_TYPE_UNKNOWN";
        case SignedMsgType.SIGNED_MSG_TYPE_PREVOTE:
            return "SIGNED_MSG_TYPE_PREVOTE";
        case SignedMsgType.SIGNED_MSG_TYPE_PRECOMMIT:
            return "SIGNED_MSG_TYPE_PRECOMMIT";
        case SignedMsgType.SIGNED_MSG_TYPE_PROPOSAL:
            return "SIGNED_MSG_TYPE_PROPOSAL";
        case SignedMsgType.UNRECOGNIZED:
        default:
            return "UNRECOGNIZED";
    }
}
function createBasePartSetHeader() {
    return { total: 0, hash: new Uint8Array(0) };
}
function createBasePart() {
    return { index: 0, bytes: new Uint8Array(0), proof: undefined };
}
function createBaseBlockID() {
    return { hash: new Uint8Array(0), part_set_header: undefined };
}
function createBaseHeader() {
    return {
        version: undefined,
        chain_id: "",
        height: 0,
        time: undefined,
        last_block_id: undefined,
        last_commit_hash: new Uint8Array(0),
        data_hash: new Uint8Array(0),
        validators_hash: new Uint8Array(0),
        next_validators_hash: new Uint8Array(0),
        consensus_hash: new Uint8Array(0),
        app_hash: new Uint8Array(0),
        last_results_hash: new Uint8Array(0),
        evidence_hash: new Uint8Array(0),
        proposer_address: new Uint8Array(0)
    };
}
function createBaseData() {
    return { txs: [] };
}
function createBaseVote() {
    return {
        type: 0,
        height: 0,
        round: 0,
        block_id: undefined,
        timestamp: undefined,
        validator_address: new Uint8Array(0),
        validator_index: 0,
        signature: new Uint8Array(0),
        extension: new Uint8Array(0),
        extension_signature: new Uint8Array(0)
    };
}
function createBaseCommit() {
    return { height: 0, round: 0, block_id: undefined, signatures: [] };
}
function createBaseCommitSig() {
    return { block_id_flag: 0, validator_address: new Uint8Array(0), timestamp: undefined, signature: new Uint8Array(0) };
}
function createBaseExtendedCommit() {
    return { height: 0, round: 0, block_id: undefined, extended_signatures: [] };
}
function createBaseExtendedCommitSig() {
    return {
        block_id_flag: 0,
        validator_address: new Uint8Array(0),
        timestamp: undefined,
        signature: new Uint8Array(0),
        extension: new Uint8Array(0),
        extension_signature: new Uint8Array(0)
    };
}
function createBaseProposal() {
    return {
        type: 0,
        height: 0,
        round: 0,
        pol_round: 0,
        block_id: undefined,
        timestamp: undefined,
        signature: new Uint8Array(0)
    };
}
function createBaseSignedHeader() {
    return { header: undefined, commit: undefined };
}
function createBaseLightBlock() {
    return { signed_header: undefined, validator_set: undefined };
}
function createBaseBlockMeta() {
    return { block_id: undefined, block_size: 0, header: undefined, num_txs: 0 };
}
function createBaseTxProof() {
    return { root_hash: new Uint8Array(0), data: new Uint8Array(0), proof: undefined };
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
        return fromTimestamp(Timestamp.fromJSON(o));
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
export const registry = [
    ["/tendermint.types.PartSetHeader", PartSetHeader],
    ["/tendermint.types.Part", Part],
    ["/tendermint.types.BlockID", BlockID],
    ["/tendermint.types.Header", Header],
    ["/tendermint.types.Data", Data],
    ["/tendermint.types.Vote", Vote],
    ["/tendermint.types.Commit", Commit],
    ["/tendermint.types.CommitSig", CommitSig],
    ["/tendermint.types.ExtendedCommit", ExtendedCommit],
    ["/tendermint.types.ExtendedCommitSig", ExtendedCommitSig],
    ["/tendermint.types.Proposal", Proposal],
    ["/tendermint.types.SignedHeader", SignedHeader],
    ["/tendermint.types.LightBlock", LightBlock],
    ["/tendermint.types.BlockMeta", BlockMeta],
    ["/tendermint.types.TxProof", TxProof]
];
export const aminoConverters = {
    "/tendermint.types.PartSetHeader": {
        aminoType: "tendermint.types.PartSetHeader",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/tendermint.types.Part": {
        aminoType: "tendermint.types.Part",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/tendermint.types.BlockID": {
        aminoType: "tendermint.types.BlockID",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/tendermint.types.Header": {
        aminoType: "tendermint.types.Header",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/tendermint.types.Data": {
        aminoType: "tendermint.types.Data",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/tendermint.types.Vote": {
        aminoType: "tendermint.types.Vote",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/tendermint.types.Commit": {
        aminoType: "tendermint.types.Commit",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/tendermint.types.CommitSig": {
        aminoType: "tendermint.types.CommitSig",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/tendermint.types.ExtendedCommit": {
        aminoType: "tendermint.types.ExtendedCommit",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/tendermint.types.ExtendedCommitSig": {
        aminoType: "tendermint.types.ExtendedCommitSig",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/tendermint.types.Proposal": {
        aminoType: "tendermint.types.Proposal",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/tendermint.types.SignedHeader": {
        aminoType: "tendermint.types.SignedHeader",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/tendermint.types.LightBlock": {
        aminoType: "tendermint.types.LightBlock",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/tendermint.types.BlockMeta": {
        aminoType: "tendermint.types.BlockMeta",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    },
    "/tendermint.types.TxProof": {
        aminoType: "tendermint.types.TxProof",
        toAmino: (message) => ({ ...message }),
        fromAmino: (object) => ({ ...object })
    }
};
