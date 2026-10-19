"use strict";
Object.defineProperty(exports, "__esModule", { value: true });
exports.SignMode = void 0;
var SignMode;
(function (SignMode) {
    /**
     * SIGN_MODE_UNSPECIFIED - SIGN_MODE_UNSPECIFIED specifies an unknown signing mode and will be
     * rejected
     */
    SignMode[SignMode["SIGN_MODE_UNSPECIFIED"] = 0] = "SIGN_MODE_UNSPECIFIED";
    /**
     * SIGN_MODE_DIRECT - SIGN_MODE_DIRECT specifies a signing mode which uses SignDoc and is
     * verified with raw bytes from Tx
     */
    SignMode[SignMode["SIGN_MODE_DIRECT"] = 1] = "SIGN_MODE_DIRECT";
    /**
     * SIGN_MODE_TEXTUAL - SIGN_MODE_TEXTUAL is a future signing mode that will verify some
     * human-readable textual representation on top of the binary representation
     * from SIGN_MODE_DIRECT
     */
    SignMode[SignMode["SIGN_MODE_TEXTUAL"] = 2] = "SIGN_MODE_TEXTUAL";
    /**
     * SIGN_MODE_LEGACY_AMINO_JSON - SIGN_MODE_LEGACY_AMINO_JSON is a backwards compatibility mode which uses
     * Amino JSON and will be removed in the future
     */
    SignMode[SignMode["SIGN_MODE_LEGACY_AMINO_JSON"] = 127] = "SIGN_MODE_LEGACY_AMINO_JSON";
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
    SignMode[SignMode["SIGN_MODE_EIP_191"] = 191] = "SIGN_MODE_EIP_191";
    SignMode[SignMode["UNRECOGNIZED"] = -1] = "UNRECOGNIZED";
})(SignMode || (exports.SignMode = SignMode = {}));
