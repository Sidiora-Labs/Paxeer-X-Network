// SPDX-License-Identifier: MIT
pragma solidity ^0.8.30;
// Local chain125 cryptographic fixture. No production custody.
contract TestWallet {
    address public immutable signer;
    bool public revoked;
    constructor(address signer_) { signer = signer_; }
    function revoke() external { require(msg.sender == signer); revoked = true; }
    function isValidSignature(bytes32 digest, bytes calldata signature) external view returns (bytes4) {
        if (revoked || signature.length != 65) return 0xffffffff;
        bytes32 r; bytes32 s; uint8 v;
        assembly { r := calldataload(signature.offset) s := calldataload(add(signature.offset, 32)) v := byte(0,calldataload(add(signature.offset,64))) }
        if (v < 27) v += 27;
        return ecrecover(digest,v,r,s)==signer ? bytes4(0x1626ba7e) : bytes4(0xffffffff);
    }
}
