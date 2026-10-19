export interface Proof {
    total: number;
    index: number;
    leaf_hash: Uint8Array;
    aunts: Uint8Array[];
}
export interface ValueOp {
    /** Encoded in ProofOp.Key. */
    key: Uint8Array;
    /** To encode in ProofOp.Data */
    proof?: Proof;
}
export interface DominoOp {
    key: string;
    input: string;
    output: string;
}
export interface ProofOp {
    type: string;
    key: Uint8Array;
    data: Uint8Array;
}
export interface ProofOps {
    ops: ProofOp[];
}
