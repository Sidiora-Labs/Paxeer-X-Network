// SPDX-License-Identifier: LicenseRef-Paxlabs-HyperPax-OS-Protocol
pragma solidity ^0.8.27;
import "../WPAX9.sol";
import "../base/UUPSUpgradeable.sol";
import "../base/ERC721Base.sol";
import "../libraries/SidioraMath.sol";

contract PrimitiveWallet {
    WPAX9 public immutable wrapper;
    uint256 public received;
    bool public reject;
    bool public reenter;
    constructor(WPAX9 wrapper_) { wrapper = wrapper_; }
    function deposit() external payable { wrapper.deposit{value: msg.value}(); }
    function withdraw(uint256 amount, bool reject_, bool reenter_) external {
        reject = reject_;
        reenter = reenter_;
        wrapper.withdraw(amount);
    }
    receive() external payable {
        require(!reject, "receiver refused");
        received += msg.value;
        if (reenter) {
            reenter = false;
            wrapper.withdraw(wrapper.balanceOf(address(this)));
        }
    }
}
contract PrimitiveBadBeacon {
    address public implementation;
    constructor(address target) { implementation = target; }
}
contract PrimitiveDelegateWrapper {
    address private immutable target;
    constructor(address target_) { target = target_; }
    fallback() external payable {
        address t = target;
        assembly {
            calldatacopy(0, 0, calldatasize())
            let ok := delegatecall(gas(), t, 0, calldatasize(), 0, 0)
            returndatacopy(0, 0, returndatasize())
            if iszero(ok) { revert(0, returndatasize()) }
            return(0, returndatasize())
        }
    }
}
contract PrimitiveMigration is UUPSUpgradeable {
    error MigrationRefused(uint256 reason);
    function fail() external pure { revert MigrationRefused(42); }
    function _authorizeUpgrade(address) internal pure override {}
}
contract PrimitiveNFTReceiver is IERC721Receiver {
    error ReceiverRefused(uint256 token);
    address public forwardTo;
    bool public refuse;
    constructor(address recipient, bool refusal) { forwardTo = recipient; refuse = refusal; }
    function onERC721Received(address, address, uint256 tokenId, bytes calldata) external returns (bytes4) {
        if (refuse) revert ReceiverRefused(tokenId);
        if (forwardTo != address(0)) ERC721Base(msg.sender).transferFrom(address(this), forwardTo, tokenId);
        return IERC721Receiver.onERC721Received.selector;
    }
}
contract PrimitiveAbs {
    function abs(int256 value) external pure returns (uint256) { return SidioraMath.abs(value); }
}

contract PrimitiveMalformedToken {
    fallback() external {
        assembly { mstore(0, 2) return(0, 32) }
    }
}

interface IPrimitiveNativeVault {
    function withdrawNative(uint256 amount, address to) external;
}
contract PrimitiveVaultReceiver {
    IPrimitiveNativeVault public immutable vault;
    uint256 public received;
    bool public reject;
    bool public tryReenter;
    bool public reentrySucceeded;
    constructor(IPrimitiveNativeVault vault_) { vault = vault_; }
    function configure(bool reject_, bool reenter_) external { reject = reject_; tryReenter = reenter_; }
    receive() external payable {
        require(!reject, "wallet refuses native");
        received += msg.value;
        if (tryReenter) {
            (reentrySucceeded, ) = address(vault).call(abi.encodeCall(vault.withdrawNative, (1, address(this))));
        }
    }
}
