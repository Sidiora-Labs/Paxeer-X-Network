import type { GeneratedType } from "@cosmjs/proto-signing";
import type { BlockID as BlockID_type, BlockMeta as BlockMeta_type, CommitSig as CommitSig_type, Commit as Commit_type, Data as Data_type, ExtendedCommitSig as ExtendedCommitSig_type, ExtendedCommit as ExtendedCommit_type, Header as Header_type, LightBlock as LightBlock_type, PartSetHeader as PartSetHeader_type, Part as Part_type, Proposal as Proposal_type, SignedHeader as SignedHeader_type, TxProof as TxProof_type, Vote as Vote_type } from "../../../types/tendermint/types";
import { BlockIDFlag, SignedMsgType } from "../../../types/tendermint/types";
import type { MessageFns } from "../../common";
export interface PartSetHeader extends PartSetHeader_type {
}
export interface Part extends Part_type {
}
export interface BlockID extends BlockID_type {
}
export interface Header extends Header_type {
}
export interface Data extends Data_type {
}
export interface Vote extends Vote_type {
}
export interface Commit extends Commit_type {
}
export interface CommitSig extends CommitSig_type {
}
export interface ExtendedCommit extends ExtendedCommit_type {
}
export interface ExtendedCommitSig extends ExtendedCommitSig_type {
}
export interface Proposal extends Proposal_type {
}
export interface SignedHeader extends SignedHeader_type {
}
export interface LightBlock extends LightBlock_type {
}
export interface BlockMeta extends BlockMeta_type {
}
export interface TxProof extends TxProof_type {
}
export declare const PartSetHeader: MessageFns<PartSetHeader, "tendermint.types.PartSetHeader">;
export declare const Part: MessageFns<Part, "tendermint.types.Part">;
export declare const BlockID: MessageFns<BlockID, "tendermint.types.BlockID">;
export declare const Header: MessageFns<Header, "tendermint.types.Header">;
export declare const Data: MessageFns<Data, "tendermint.types.Data">;
export declare const Vote: MessageFns<Vote, "tendermint.types.Vote">;
export declare const Commit: MessageFns<Commit, "tendermint.types.Commit">;
export declare const CommitSig: MessageFns<CommitSig, "tendermint.types.CommitSig">;
export declare const ExtendedCommit: MessageFns<ExtendedCommit, "tendermint.types.ExtendedCommit">;
export declare const ExtendedCommitSig: MessageFns<ExtendedCommitSig, "tendermint.types.ExtendedCommitSig">;
export declare const Proposal: MessageFns<Proposal, "tendermint.types.Proposal">;
export declare const SignedHeader: MessageFns<SignedHeader, "tendermint.types.SignedHeader">;
export declare const LightBlock: MessageFns<LightBlock, "tendermint.types.LightBlock">;
export declare const BlockMeta: MessageFns<BlockMeta, "tendermint.types.BlockMeta">;
export declare const TxProof: MessageFns<TxProof, "tendermint.types.TxProof">;
export declare function blockIDFlagFromJSON(object: any): BlockIDFlag;
export declare function blockIDFlagToJSON(object: BlockIDFlag): string;
export declare function signedMsgTypeFromJSON(object: any): SignedMsgType;
export declare function signedMsgTypeToJSON(object: SignedMsgType): string;
export declare const registry: Array<[string, GeneratedType]>;
export declare const aminoConverters: {
    "/tendermint.types.PartSetHeader": {
        aminoType: string;
        toAmino: (message: PartSetHeader) => {
            total: number;
            hash: Uint8Array;
        };
        fromAmino: (object: PartSetHeader) => {
            total: number;
            hash: Uint8Array;
        };
    };
    "/tendermint.types.Part": {
        aminoType: string;
        toAmino: (message: Part) => {
            index: number;
            bytes: Uint8Array;
            proof?: import("../../../types/tendermint/crypto").Proof;
        };
        fromAmino: (object: Part) => {
            index: number;
            bytes: Uint8Array;
            proof?: import("../../../types/tendermint/crypto").Proof;
        };
    };
    "/tendermint.types.BlockID": {
        aminoType: string;
        toAmino: (message: BlockID) => {
            hash: Uint8Array;
            part_set_header?: PartSetHeader_type;
        };
        fromAmino: (object: BlockID) => {
            hash: Uint8Array;
            part_set_header?: PartSetHeader_type;
        };
    };
    "/tendermint.types.Header": {
        aminoType: string;
        toAmino: (message: Header) => {
            version?: import("../../../types/tendermint/version").Consensus;
            chain_id: string;
            height: number;
            time?: Date;
            last_block_id?: BlockID_type;
            last_commit_hash: Uint8Array;
            data_hash: Uint8Array;
            validators_hash: Uint8Array;
            next_validators_hash: Uint8Array;
            consensus_hash: Uint8Array;
            app_hash: Uint8Array;
            last_results_hash: Uint8Array;
            evidence_hash: Uint8Array;
            proposer_address: Uint8Array;
        };
        fromAmino: (object: Header) => {
            version?: import("../../../types/tendermint/version").Consensus;
            chain_id: string;
            height: number;
            time?: Date;
            last_block_id?: BlockID_type;
            last_commit_hash: Uint8Array;
            data_hash: Uint8Array;
            validators_hash: Uint8Array;
            next_validators_hash: Uint8Array;
            consensus_hash: Uint8Array;
            app_hash: Uint8Array;
            last_results_hash: Uint8Array;
            evidence_hash: Uint8Array;
            proposer_address: Uint8Array;
        };
    };
    "/tendermint.types.Data": {
        aminoType: string;
        toAmino: (message: Data) => {
            txs: Uint8Array[];
        };
        fromAmino: (object: Data) => {
            txs: Uint8Array[];
        };
    };
    "/tendermint.types.Vote": {
        aminoType: string;
        toAmino: (message: Vote) => {
            type: SignedMsgType;
            height: number;
            round: number;
            block_id?: BlockID_type;
            timestamp?: Date;
            validator_address: Uint8Array;
            validator_index: number;
            signature: Uint8Array;
            extension: Uint8Array;
            extension_signature: Uint8Array;
        };
        fromAmino: (object: Vote) => {
            type: SignedMsgType;
            height: number;
            round: number;
            block_id?: BlockID_type;
            timestamp?: Date;
            validator_address: Uint8Array;
            validator_index: number;
            signature: Uint8Array;
            extension: Uint8Array;
            extension_signature: Uint8Array;
        };
    };
    "/tendermint.types.Commit": {
        aminoType: string;
        toAmino: (message: Commit) => {
            height: number;
            round: number;
            block_id?: BlockID_type;
            signatures: CommitSig_type[];
        };
        fromAmino: (object: Commit) => {
            height: number;
            round: number;
            block_id?: BlockID_type;
            signatures: CommitSig_type[];
        };
    };
    "/tendermint.types.CommitSig": {
        aminoType: string;
        toAmino: (message: CommitSig) => {
            block_id_flag: BlockIDFlag;
            validator_address: Uint8Array;
            timestamp?: Date;
            signature: Uint8Array;
        };
        fromAmino: (object: CommitSig) => {
            block_id_flag: BlockIDFlag;
            validator_address: Uint8Array;
            timestamp?: Date;
            signature: Uint8Array;
        };
    };
    "/tendermint.types.ExtendedCommit": {
        aminoType: string;
        toAmino: (message: ExtendedCommit) => {
            height: number;
            round: number;
            block_id?: BlockID_type;
            extended_signatures: ExtendedCommitSig_type[];
        };
        fromAmino: (object: ExtendedCommit) => {
            height: number;
            round: number;
            block_id?: BlockID_type;
            extended_signatures: ExtendedCommitSig_type[];
        };
    };
    "/tendermint.types.ExtendedCommitSig": {
        aminoType: string;
        toAmino: (message: ExtendedCommitSig) => {
            block_id_flag: BlockIDFlag;
            validator_address: Uint8Array;
            timestamp?: Date;
            signature: Uint8Array;
            extension: Uint8Array;
            extension_signature: Uint8Array;
        };
        fromAmino: (object: ExtendedCommitSig) => {
            block_id_flag: BlockIDFlag;
            validator_address: Uint8Array;
            timestamp?: Date;
            signature: Uint8Array;
            extension: Uint8Array;
            extension_signature: Uint8Array;
        };
    };
    "/tendermint.types.Proposal": {
        aminoType: string;
        toAmino: (message: Proposal) => {
            type: SignedMsgType;
            height: number;
            round: number;
            pol_round: number;
            block_id?: BlockID_type;
            timestamp?: Date;
            signature: Uint8Array;
        };
        fromAmino: (object: Proposal) => {
            type: SignedMsgType;
            height: number;
            round: number;
            pol_round: number;
            block_id?: BlockID_type;
            timestamp?: Date;
            signature: Uint8Array;
        };
    };
    "/tendermint.types.SignedHeader": {
        aminoType: string;
        toAmino: (message: SignedHeader) => {
            header?: Header_type;
            commit?: Commit_type;
        };
        fromAmino: (object: SignedHeader) => {
            header?: Header_type;
            commit?: Commit_type;
        };
    };
    "/tendermint.types.LightBlock": {
        aminoType: string;
        toAmino: (message: LightBlock) => {
            signed_header?: SignedHeader_type;
            validator_set?: import("../../../types/tendermint/types").ValidatorSet;
        };
        fromAmino: (object: LightBlock) => {
            signed_header?: SignedHeader_type;
            validator_set?: import("../../../types/tendermint/types").ValidatorSet;
        };
    };
    "/tendermint.types.BlockMeta": {
        aminoType: string;
        toAmino: (message: BlockMeta) => {
            block_id?: BlockID_type;
            block_size: number;
            header?: Header_type;
            num_txs: number;
        };
        fromAmino: (object: BlockMeta) => {
            block_id?: BlockID_type;
            block_size: number;
            header?: Header_type;
            num_txs: number;
        };
    };
    "/tendermint.types.TxProof": {
        aminoType: string;
        toAmino: (message: TxProof) => {
            root_hash: Uint8Array;
            data: Uint8Array;
            proof?: import("../../../types/tendermint/crypto").Proof;
        };
        fromAmino: (object: TxProof) => {
            root_hash: Uint8Array;
            data: Uint8Array;
            proof?: import("../../../types/tendermint/crypto").Proof;
        };
    };
};
