import type { GeneratedType } from "@cosmjs/proto-signing";
import type { DominoOp as DominoOp_type, ProofOp as ProofOp_type, ProofOps as ProofOps_type, Proof as Proof_type, ValueOp as ValueOp_type } from "../../../types/tendermint/crypto";
import type { MessageFns } from "../../common";
export interface Proof extends Proof_type {
}
export interface ValueOp extends ValueOp_type {
}
export interface DominoOp extends DominoOp_type {
}
export interface ProofOp extends ProofOp_type {
}
export interface ProofOps extends ProofOps_type {
}
export declare const Proof: MessageFns<Proof, "tendermint.crypto.Proof">;
export declare const ValueOp: MessageFns<ValueOp, "tendermint.crypto.ValueOp">;
export declare const DominoOp: MessageFns<DominoOp, "tendermint.crypto.DominoOp">;
export declare const ProofOp: MessageFns<ProofOp, "tendermint.crypto.ProofOp">;
export declare const ProofOps: MessageFns<ProofOps, "tendermint.crypto.ProofOps">;
export declare const registry: Array<[string, GeneratedType]>;
export declare const aminoConverters: {
    "/tendermint.crypto.Proof": {
        aminoType: string;
        toAmino: (message: Proof) => {
            total: number;
            index: number;
            leaf_hash: Uint8Array;
            aunts: Uint8Array[];
        };
        fromAmino: (object: Proof) => {
            total: number;
            index: number;
            leaf_hash: Uint8Array;
            aunts: Uint8Array[];
        };
    };
    "/tendermint.crypto.ValueOp": {
        aminoType: string;
        toAmino: (message: ValueOp) => {
            key: Uint8Array;
            proof?: Proof_type;
        };
        fromAmino: (object: ValueOp) => {
            key: Uint8Array;
            proof?: Proof_type;
        };
    };
    "/tendermint.crypto.DominoOp": {
        aminoType: string;
        toAmino: (message: DominoOp) => {
            key: string;
            input: string;
            output: string;
        };
        fromAmino: (object: DominoOp) => {
            key: string;
            input: string;
            output: string;
        };
    };
    "/tendermint.crypto.ProofOp": {
        aminoType: string;
        toAmino: (message: ProofOp) => {
            type: string;
            key: Uint8Array;
            data: Uint8Array;
        };
        fromAmino: (object: ProofOp) => {
            type: string;
            key: Uint8Array;
            data: Uint8Array;
        };
    };
    "/tendermint.crypto.ProofOps": {
        aminoType: string;
        toAmino: (message: ProofOps) => {
            ops: ProofOp_type[];
        };
        fromAmino: (object: ProofOps) => {
            ops: ProofOp_type[];
        };
    };
};
