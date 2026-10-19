export interface MultiSignature {
    signatures: Uint8Array[];
}
export interface CompactBitArray {
    extra_bits_stored: number;
    elems: Uint8Array;
}
