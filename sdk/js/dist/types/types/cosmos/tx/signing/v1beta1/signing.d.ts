import type { Any } from "../../../../google/protobuf/any";
import type { CompactBitArray } from "../../../crypto/multisig/v1beta1/multisig";
export declare enum SignMode {
    /**
     * SIGN_MODE_UNSPECIFIED - SIGN_MODE_UNSPECIFIED specifies an unknown signing mode and will be
     * rejected
     */
    SIGN_MODE_UNSPECIFIED = 0,
    /**
     * SIGN_MODE_DIRECT - SIGN_MODE_DIRECT specifies a signing mode which uses SignDoc and is
     * verified with raw bytes from Tx
     */
    SIGN_MODE_DIRECT = 1,
    /**
     * SIGN_MODE_TEXTUAL - SIGN_MODE_TEXTUAL is a future signing mode that will verify some
     * human-readable textual representation on top of the binary representation
     * from SIGN_MODE_DIRECT
     */
    SIGN_MODE_TEXTUAL = 2,
    /**
     * SIGN_MODE_LEGACY_AMINO_JSON - SIGN_MODE_LEGACY_AMINO_JSON is a backwards compatibility mode which uses
     * Amino JSON and will be removed in the future
     */
    SIGN_MODE_LEGACY_AMINO_JSON = 127,
    /**
     * SIGN_MODE_EIP_191 - SIGN_MODE_EIP_191 specifies the sign mode for EIP 191 signing on the Cosmos
     * SDK. Ref: https://eips.ethereum.org/EIPS/eip-191
     *
     * Currently, SIGN_MODE_EIP_191 is registered as a SignMode enum variant,
     * but is not implemented on the SDK by default. To enable EIP-191, you need
     * to pass a custom `TxConfig` that has an implementation of
     * `SignModeHandler` for EIP-191. The SDK may decide to fully support
     * EIP-191 in the future.
     *
     * Since: cosmos-sdk 0.45.2
     */
    SIGN_MODE_EIP_191 = 191,
    UNRECOGNIZED = -1
}
export interface SignatureDescriptors {
    /** signatures are the signature descriptors */
    signatures: SignatureDescriptor[];
}
export interface SignatureDescriptor {
    /** public_key is the public key of the signer */
    public_key?: Any;
    data?: SignatureDescriptorData;
    /**
     * sequence is the sequence of the account, which describes the
     * number of committed transactions signed by a given address. It is used to prevent
     * replay attacks.
     */
    sequence: number;
}
export interface SignatureDescriptorData {
    /** single represents a single signer */
    single?: SignatureDescriptorDataSingle;
    /** multi represents a multisig signer */
    multi?: SignatureDescriptorDataMulti;
}
export interface SignatureDescriptorDataSingle {
    /** mode is the signing mode of the single signer */
    mode: SignMode;
    /** signature is the raw signature bytes */
    signature: Uint8Array;
}
export interface SignatureDescriptorDataMulti {
    /** bitarray specifies which keys within the multisig are signing */
    bitarray?: CompactBitArray;
    /** signatures is the signatures of the multi-signature */
    signatures: SignatureDescriptorData[];
}
