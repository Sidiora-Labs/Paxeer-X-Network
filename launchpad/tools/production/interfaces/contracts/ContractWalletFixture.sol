// SPDX-License-Identifier: MIT
pragma solidity ^0.8.24;

/// @notice Isolated interface fixture; not a production wallet implementation.
contract ContractWalletFixture {
    address public immutable signer;
    constructor(address authorizedSigner) { require(authorizedSigner != address(0)); signer = authorizedSigner; }
    function isValidSignature(bytes32 digest, bytes calldata signature) external view returns (bytes4) {
        if (signature.length != 65) return 0xffffffff;
        bytes32 r; bytes32 s; uint8 v;
        assembly { r := calldataload(signature.offset) s := calldataload(add(signature.offset, 32)) v := byte(0, calldataload(add(signature.offset, 64))) }
        if (uint256(s) > 0x7fffffffffffffffffffffffffffffff5d576e7357a4501ddfe92f46681b20a0 || (v != 27 && v != 28)) return 0xffffffff;
        return ecrecover(digest, v, r, s) == signer ? bytes4(0x1626ba7e) : bytes4(0xffffffff);
    }
}
