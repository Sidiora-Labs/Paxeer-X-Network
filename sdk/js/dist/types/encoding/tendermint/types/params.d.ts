import type { GeneratedType } from "@cosmjs/proto-signing";
import type { ABCIParams as ABCIParams_type, BlockParams as BlockParams_type, ConsensusParams as ConsensusParams_type, EvidenceParams as EvidenceParams_type, HashedParams as HashedParams_type, SynchronyParams as SynchronyParams_type, TimeoutParams as TimeoutParams_type, ValidatorParams as ValidatorParams_type, VersionParams as VersionParams_type } from "../../../types/tendermint/types";
import type { MessageFns } from "../../common";
export interface ConsensusParams extends ConsensusParams_type {
}
export interface BlockParams extends BlockParams_type {
}
export interface EvidenceParams extends EvidenceParams_type {
}
export interface ValidatorParams extends ValidatorParams_type {
}
export interface VersionParams extends VersionParams_type {
}
export interface HashedParams extends HashedParams_type {
}
export interface SynchronyParams extends SynchronyParams_type {
}
export interface TimeoutParams extends TimeoutParams_type {
}
export interface ABCIParams extends ABCIParams_type {
}
export declare const ConsensusParams: MessageFns<ConsensusParams, "tendermint.types.ConsensusParams">;
export declare const BlockParams: MessageFns<BlockParams, "tendermint.types.BlockParams">;
export declare const EvidenceParams: MessageFns<EvidenceParams, "tendermint.types.EvidenceParams">;
export declare const ValidatorParams: MessageFns<ValidatorParams, "tendermint.types.ValidatorParams">;
export declare const VersionParams: MessageFns<VersionParams, "tendermint.types.VersionParams">;
export declare const HashedParams: MessageFns<HashedParams, "tendermint.types.HashedParams">;
export declare const SynchronyParams: MessageFns<SynchronyParams, "tendermint.types.SynchronyParams">;
export declare const TimeoutParams: MessageFns<TimeoutParams, "tendermint.types.TimeoutParams">;
export declare const ABCIParams: MessageFns<ABCIParams, "tendermint.types.ABCIParams">;
export declare const registry: Array<[string, GeneratedType]>;
export declare const aminoConverters: {
    "/tendermint.types.ConsensusParams": {
        aminoType: string;
        toAmino: (message: ConsensusParams) => {
            block?: BlockParams_type;
            evidence?: EvidenceParams_type;
            validator?: ValidatorParams_type;
            version?: VersionParams_type;
            synchrony?: SynchronyParams_type;
            timeout?: TimeoutParams_type;
            abci?: ABCIParams_type;
        };
        fromAmino: (object: ConsensusParams) => {
            block?: BlockParams_type;
            evidence?: EvidenceParams_type;
            validator?: ValidatorParams_type;
            version?: VersionParams_type;
            synchrony?: SynchronyParams_type;
            timeout?: TimeoutParams_type;
            abci?: ABCIParams_type;
        };
    };
    "/tendermint.types.BlockParams": {
        aminoType: string;
        toAmino: (message: BlockParams) => {
            max_bytes: number;
            max_gas: number;
        };
        fromAmino: (object: BlockParams) => {
            max_bytes: number;
            max_gas: number;
        };
    };
    "/tendermint.types.EvidenceParams": {
        aminoType: string;
        toAmino: (message: EvidenceParams) => {
            max_age_num_blocks: number;
            max_age_duration?: import("../../../types/google/protobuf").Duration;
            max_bytes: number;
        };
        fromAmino: (object: EvidenceParams) => {
            max_age_num_blocks: number;
            max_age_duration?: import("../../../types/google/protobuf").Duration;
            max_bytes: number;
        };
    };
    "/tendermint.types.ValidatorParams": {
        aminoType: string;
        toAmino: (message: ValidatorParams) => {
            pub_key_types: string[];
        };
        fromAmino: (object: ValidatorParams) => {
            pub_key_types: string[];
        };
    };
    "/tendermint.types.VersionParams": {
        aminoType: string;
        toAmino: (message: VersionParams) => {
            app_version: number;
        };
        fromAmino: (object: VersionParams) => {
            app_version: number;
        };
    };
    "/tendermint.types.HashedParams": {
        aminoType: string;
        toAmino: (message: HashedParams) => {
            block_max_bytes: number;
            block_max_gas: number;
        };
        fromAmino: (object: HashedParams) => {
            block_max_bytes: number;
            block_max_gas: number;
        };
    };
    "/tendermint.types.SynchronyParams": {
        aminoType: string;
        toAmino: (message: SynchronyParams) => {
            message_delay?: import("../../../types/google/protobuf").Duration;
            precision?: import("../../../types/google/protobuf").Duration;
        };
        fromAmino: (object: SynchronyParams) => {
            message_delay?: import("../../../types/google/protobuf").Duration;
            precision?: import("../../../types/google/protobuf").Duration;
        };
    };
    "/tendermint.types.TimeoutParams": {
        aminoType: string;
        toAmino: (message: TimeoutParams) => {
            propose?: import("../../../types/google/protobuf").Duration;
            propose_delta?: import("../../../types/google/protobuf").Duration;
            vote?: import("../../../types/google/protobuf").Duration;
            vote_delta?: import("../../../types/google/protobuf").Duration;
            commit?: import("../../../types/google/protobuf").Duration;
            bypass_commit_timeout: boolean;
        };
        fromAmino: (object: TimeoutParams) => {
            propose?: import("../../../types/google/protobuf").Duration;
            propose_delta?: import("../../../types/google/protobuf").Duration;
            vote?: import("../../../types/google/protobuf").Duration;
            vote_delta?: import("../../../types/google/protobuf").Duration;
            commit?: import("../../../types/google/protobuf").Duration;
            bypass_commit_timeout: boolean;
        };
    };
    "/tendermint.types.ABCIParams": {
        aminoType: string;
        toAmino: (message: ABCIParams) => {
            vote_extensions_enable_height: number;
            recheck_tx: boolean;
        };
        fromAmino: (object: ABCIParams) => {
            vote_extensions_enable_height: number;
            recheck_tx: boolean;
        };
    };
};
