// SPDX-License-Identifier: LicenseRef-Paxlabs-HyperPax-OS-Protocol
pragma solidity ^0.8.24;

import "../BaseOptical.sol";
import "../../libraries/BitFlag.sol";

/// @title MaxWalletOptical
/// @notice Enforces maximum token holding percentage per wallet after buy swaps.
/// @dev Uses the token transfer callback for pool outputs. Bytecode and cap immutable; owner may change exemptions.
///      Pool address is exempt (it holds the liquidity).
///      Only checks on buys — sells naturally reduce holdings.
contract MaxWalletOptical is BaseOptical {
    error MaxWalletExceeded();
    event ExemptionUpdated(address indexed account, bool isExempt);

    /// @notice Maximum wallet holding in basis points of total supply (e.g., 200 = 2%)
    uint256 public immutable maxWalletBps;

    /// @notice Addresses exempt from max wallet check
    mapping(address => bool) public exempt;

    /// @param _poolRegistry PoolRegistry address for pool validation
    /// @param _owner Deployer/owner address
    /// @param _maxWalletBps Maximum holding in bps of total supply
    constructor(
        address _poolRegistry,
        address _owner,
        uint256 _maxWalletBps
    ) BaseOptical(_poolRegistry, _owner) {
        require(_maxWalletBps > 0 && _maxWalletBps <= 10000, "Invalid cap");
        maxWalletBps = _maxWalletBps;
    }

    /// @notice Set an address as exempt from max wallet check
    /// @dev Only owner can set exemptions (e.g., pool address, treasury)
    function setExempt(address account, bool isExempt) external onlyOwner {
        exempt[account] = isExempt;
        emit ExemptionUpdated(account, isExempt);
    }

    /// @inheritdoc IOptical
    function getFlags() external pure override returns (uint8) {
        return BitFlag.BEFORE_TOKEN_TRANSFER;
    }

    /// @notice Buy-time cap: validate actual pool output recipient before the token balance changes.
    function beforeTokenTransfer(address pool, address from, address to, uint256 amount) external view {
        IPoolRegistry.PoolMetadata memory meta = _poolMetadata(pool);
        if (msg.sender != meta.token) revert NotPool();
        if (from != pool || to == pool || exempt[to]) return;
        (bool ok, bytes memory data) = meta.token.staticcall(abi.encodeWithSignature("balanceOf(address)", to));
        if (!ok || data.length != 32) revert NotPool();
        uint256 balance = abi.decode(data, (uint256));
        (ok, data) = meta.token.staticcall(abi.encodeWithSignature("totalSupply()"));
        if (!ok || data.length != 32) revert NotPool();
        uint256 cap = abi.decode(data, (uint256)) * maxWalletBps / 10000;
        if (balance + amount > cap) revert MaxWalletExceeded();
    }
}
