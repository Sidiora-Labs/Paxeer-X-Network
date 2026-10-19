import type { GeneratedType } from "@cosmjs/proto-signing";
import type { BatchEntry as BatchEntry_type, BatchProof as BatchProof_type, CommitmentProof as CommitmentProof_type, CompressedBatchEntry as CompressedBatchEntry_type, CompressedBatchProof as CompressedBatchProof_type, CompressedExistenceProof as CompressedExistenceProof_type, CompressedNonExistenceProof as CompressedNonExistenceProof_type, ExistenceProof as ExistenceProof_type, InnerOp as InnerOp_type, InnerSpec as InnerSpec_type, LeafOp as LeafOp_type, NonExistenceProof as NonExistenceProof_type, ProofSpec as ProofSpec_type } from "../../types/confio";
import { HashOp, LengthOp } from "../../types/confio";
import type { MessageFns } from "../common";
export interface ExistenceProof extends ExistenceProof_type {
}
export interface NonExistenceProof extends NonExistenceProof_type {
}
export interface CommitmentProof extends CommitmentProof_type {
}
export interface LeafOp extends LeafOp_type {
}
export interface InnerOp extends InnerOp_type {
}
export interface ProofSpec extends ProofSpec_type {
}
export interface InnerSpec extends InnerSpec_type {
}
export interface BatchProof extends BatchProof_type {
}
export interface BatchEntry extends BatchEntry_type {
}
export interface CompressedBatchProof extends CompressedBatchProof_type {
}
export interface CompressedBatchEntry extends CompressedBatchEntry_type {
}
export interface CompressedExistenceProof extends CompressedExistenceProof_type {
}
export interface CompressedNonExistenceProof extends CompressedNonExistenceProof_type {
}
export declare const ExistenceProof: MessageFns<ExistenceProof, "ics23.ExistenceProof">;
export declare const NonExistenceProof: MessageFns<NonExistenceProof, "ics23.NonExistenceProof">;
export declare const CommitmentProof: MessageFns<CommitmentProof, "ics23.CommitmentProof">;
export declare const LeafOp: MessageFns<LeafOp, "ics23.LeafOp">;
export declare const InnerOp: MessageFns<InnerOp, "ics23.InnerOp">;
export declare const ProofSpec: MessageFns<ProofSpec, "ics23.ProofSpec">;
export declare const InnerSpec: MessageFns<InnerSpec, "ics23.InnerSpec">;
export declare const BatchProof: MessageFns<BatchProof, "ics23.BatchProof">;
export declare const BatchEntry: MessageFns<BatchEntry, "ics23.BatchEntry">;
export declare const CompressedBatchProof: MessageFns<CompressedBatchProof, "ics23.CompressedBatchProof">;
export declare const CompressedBatchEntry: MessageFns<CompressedBatchEntry, "ics23.CompressedBatchEntry">;
export declare const CompressedExistenceProof: MessageFns<CompressedExistenceProof, "ics23.CompressedExistenceProof">;
export declare const CompressedNonExistenceProof: MessageFns<CompressedNonExistenceProof, "ics23.CompressedNonExistenceProof">;
export declare function hashOpFromJSON(object: any): HashOp;
export declare function hashOpToJSON(object: HashOp): string;
export declare function lengthOpFromJSON(object: any): LengthOp;
export declare function lengthOpToJSON(object: LengthOp): string;
export declare const registry: Array<[string, GeneratedType]>;
export declare const aminoConverters: {
    "/ics23.ExistenceProof": {
        aminoType: string;
        toAmino: (message: ExistenceProof) => {
            key: Uint8Array;
            value: Uint8Array;
            leaf?: LeafOp_type;
            path: InnerOp_type[];
        };
        fromAmino: (object: ExistenceProof) => {
            key: Uint8Array;
            value: Uint8Array;
            leaf?: LeafOp_type;
            path: InnerOp_type[];
        };
    };
    "/ics23.NonExistenceProof": {
        aminoType: string;
        toAmino: (message: NonExistenceProof) => {
            key: Uint8Array;
            left?: ExistenceProof_type;
            right?: ExistenceProof_type;
        };
        fromAmino: (object: NonExistenceProof) => {
            key: Uint8Array;
            left?: ExistenceProof_type;
            right?: ExistenceProof_type;
        };
    };
    "/ics23.CommitmentProof": {
        aminoType: string;
        toAmino: (message: CommitmentProof) => {
            exist?: ExistenceProof_type;
            nonexist?: NonExistenceProof_type;
            batch?: BatchProof_type;
            compressed?: CompressedBatchProof_type;
        };
        fromAmino: (object: CommitmentProof) => {
            exist?: ExistenceProof_type;
            nonexist?: NonExistenceProof_type;
            batch?: BatchProof_type;
            compressed?: CompressedBatchProof_type;
        };
    };
    "/ics23.LeafOp": {
        aminoType: string;
        toAmino: (message: LeafOp) => {
            hash: HashOp;
            prehash_key: HashOp;
            prehash_value: HashOp;
            length: LengthOp;
            prefix: Uint8Array;
        };
        fromAmino: (object: LeafOp) => {
            hash: HashOp;
            prehash_key: HashOp;
            prehash_value: HashOp;
            length: LengthOp;
            prefix: Uint8Array;
        };
    };
    "/ics23.InnerOp": {
        aminoType: string;
        toAmino: (message: InnerOp) => {
            hash: HashOp;
            prefix: Uint8Array;
            suffix: Uint8Array;
        };
        fromAmino: (object: InnerOp) => {
            hash: HashOp;
            prefix: Uint8Array;
            suffix: Uint8Array;
        };
    };
    "/ics23.ProofSpec": {
        aminoType: string;
        toAmino: (message: ProofSpec) => {
            leaf_spec?: LeafOp_type;
            inner_spec?: InnerSpec_type;
            max_depth: number;
            min_depth: number;
        };
        fromAmino: (object: ProofSpec) => {
            leaf_spec?: LeafOp_type;
            inner_spec?: InnerSpec_type;
            max_depth: number;
            min_depth: number;
        };
    };
    "/ics23.InnerSpec": {
        aminoType: string;
        toAmino: (message: InnerSpec) => {
            child_order: number[];
            child_size: number;
            min_prefix_length: number;
            max_prefix_length: number;
            empty_child: Uint8Array;
            hash: HashOp;
        };
        fromAmino: (object: InnerSpec) => {
            child_order: number[];
            child_size: number;
            min_prefix_length: number;
            max_prefix_length: number;
            empty_child: Uint8Array;
            hash: HashOp;
        };
    };
    "/ics23.BatchProof": {
        aminoType: string;
        toAmino: (message: BatchProof) => {
            entries: BatchEntry_type[];
        };
        fromAmino: (object: BatchProof) => {
            entries: BatchEntry_type[];
        };
    };
    "/ics23.BatchEntry": {
        aminoType: string;
        toAmino: (message: BatchEntry) => {
            exist?: ExistenceProof_type;
            nonexist?: NonExistenceProof_type;
        };
        fromAmino: (object: BatchEntry) => {
            exist?: ExistenceProof_type;
            nonexist?: NonExistenceProof_type;
        };
    };
    "/ics23.CompressedBatchProof": {
        aminoType: string;
        toAmino: (message: CompressedBatchProof) => {
            entries: CompressedBatchEntry_type[];
            lookup_inners: InnerOp_type[];
        };
        fromAmino: (object: CompressedBatchProof) => {
            entries: CompressedBatchEntry_type[];
            lookup_inners: InnerOp_type[];
        };
    };
    "/ics23.CompressedBatchEntry": {
        aminoType: string;
        toAmino: (message: CompressedBatchEntry) => {
            exist?: CompressedExistenceProof_type;
            nonexist?: CompressedNonExistenceProof_type;
        };
        fromAmino: (object: CompressedBatchEntry) => {
            exist?: CompressedExistenceProof_type;
            nonexist?: CompressedNonExistenceProof_type;
        };
    };
    "/ics23.CompressedExistenceProof": {
        aminoType: string;
        toAmino: (message: CompressedExistenceProof) => {
            key: Uint8Array;
            value: Uint8Array;
            leaf?: LeafOp_type;
            path: number[];
        };
        fromAmino: (object: CompressedExistenceProof) => {
            key: Uint8Array;
            value: Uint8Array;
            leaf?: LeafOp_type;
            path: number[];
        };
    };
    "/ics23.CompressedNonExistenceProof": {
        aminoType: string;
        toAmino: (message: CompressedNonExistenceProof) => {
            key: Uint8Array;
            left?: CompressedExistenceProof_type;
            right?: CompressedExistenceProof_type;
        };
        fromAmino: (object: CompressedNonExistenceProof) => {
            key: Uint8Array;
            left?: CompressedExistenceProof_type;
            right?: CompressedExistenceProof_type;
        };
    };
};
