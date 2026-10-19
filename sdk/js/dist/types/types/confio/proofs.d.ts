export declare enum HashOp {
    /** NO_HASH - NO_HASH is the default if no data passed. Note this is an illegal argument some places. */
    NO_HASH = 0,
    SHA256 = 1,
    SHA512 = 2,
    KECCAK = 3,
    RIPEMD160 = 4,
    /** BITCOIN - ripemd160(sha256(x)) */
    BITCOIN = 5,
    UNRECOGNIZED = -1
}
export declare enum LengthOp {
    /** NO_PREFIX - NO_PREFIX don't include any length info */
    NO_PREFIX = 0,
    /** VAR_PROTO - VAR_PROTO uses protobuf (and go-amino) varint encoding of the length */
    VAR_PROTO = 1,
    /** VAR_RLP - VAR_RLP uses rlp int encoding of the length */
    VAR_RLP = 2,
    /** FIXED32_BIG - FIXED32_BIG uses big-endian encoding of the length as a 32 bit integer */
    FIXED32_BIG = 3,
    /** FIXED32_LITTLE - FIXED32_LITTLE uses little-endian encoding of the length as a 32 bit integer */
    FIXED32_LITTLE = 4,
    /** FIXED64_BIG - FIXED64_BIG uses big-endian encoding of the length as a 64 bit integer */
    FIXED64_BIG = 5,
    /** FIXED64_LITTLE - FIXED64_LITTLE uses little-endian encoding of the length as a 64 bit integer */
    FIXED64_LITTLE = 6,
    /** REQUIRE_32_BYTES - REQUIRE_32_BYTES is like NONE, but will fail if the input is not exactly 32 bytes (sha256 output) */
    REQUIRE_32_BYTES = 7,
    /** REQUIRE_64_BYTES - REQUIRE_64_BYTES is like NONE, but will fail if the input is not exactly 64 bytes (sha512 output) */
    REQUIRE_64_BYTES = 8,
    UNRECOGNIZED = -1
}
export interface ExistenceProof {
    key: Uint8Array;
    value: Uint8Array;
    leaf?: LeafOp;
    path: InnerOp[];
}
export interface NonExistenceProof {
    /** TODO: remove this as unnecessary??? we prove a range */
    key: Uint8Array;
    left?: ExistenceProof;
    right?: ExistenceProof;
}
export interface CommitmentProof {
    exist?: ExistenceProof;
    nonexist?: NonExistenceProof;
    batch?: BatchProof;
    compressed?: CompressedBatchProof;
}
export interface LeafOp {
    hash: HashOp;
    prehash_key: HashOp;
    prehash_value: HashOp;
    length: LengthOp;
    /**
     * prefix is a fixed bytes that may optionally be included at the beginning to differentiate
     * a leaf node from an inner node.
     */
    prefix: Uint8Array;
}
export interface InnerOp {
    hash: HashOp;
    prefix: Uint8Array;
    suffix: Uint8Array;
}
export interface ProofSpec {
    /**
     * any field in the ExistenceProof must be the same as in this spec.
     * except Prefix, which is just the first bytes of prefix (spec can be longer)
     */
    leaf_spec?: LeafOp;
    inner_spec?: InnerSpec;
    /** max_depth (if > 0) is the maximum number of InnerOps allowed (mainly for fixed-depth tries) */
    max_depth: number;
    /** min_depth (if > 0) is the minimum number of InnerOps allowed (mainly for fixed-depth tries) */
    min_depth: number;
}
export interface InnerSpec {
    /**
     * Child order is the ordering of the children node, must count from 0
     * iavl tree is [0, 1] (left then right)
     * merk is [0, 2, 1] (left, right, here)
     */
    child_order: number[];
    child_size: number;
    min_prefix_length: number;
    max_prefix_length: number;
    /** empty child is the prehash image that is used when one child is nil (eg. 20 bytes of 0) */
    empty_child: Uint8Array;
    /** hash is the algorithm that must be used for each InnerOp */
    hash: HashOp;
}
export interface BatchProof {
    entries: BatchEntry[];
}
export interface BatchEntry {
    exist?: ExistenceProof;
    nonexist?: NonExistenceProof;
}
export interface CompressedBatchProof {
    entries: CompressedBatchEntry[];
    lookup_inners: InnerOp[];
}
export interface CompressedBatchEntry {
    exist?: CompressedExistenceProof;
    nonexist?: CompressedNonExistenceProof;
}
export interface CompressedExistenceProof {
    key: Uint8Array;
    value: Uint8Array;
    leaf?: LeafOp;
    /** these are indexes into the lookup_inners table in CompressedBatchProof */
    path: number[];
}
export interface CompressedNonExistenceProof {
    /** TODO: remove this as unnecessary??? we prove a range */
    key: Uint8Array;
    left?: CompressedExistenceProof;
    right?: CompressedExistenceProof;
}
