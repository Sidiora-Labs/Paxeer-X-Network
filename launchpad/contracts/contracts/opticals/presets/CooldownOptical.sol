// SPDX-License-Identifier: LicenseRef-Paxlabs-HyperPax-OS-Protocol
pragma solidity ^0.8.24;

import "../BaseOptical.sol";
import "../../libraries/BitFlag.sol";

/// @title CooldownOptical
/// @notice Enforces minimum time between trades per wallet (anti-bot).
/// @dev Uses beforeSwap hook. Tracks last trade timestamp per pool per wallet.
///      Configurable cooldown period. Independent per wallet.
///      Immutable once deployed.
contract CooldownOptical is BaseOptical {
    error CooldownActive();

    /// @notice Minimum seconds between trades for any given wallet
    uint256 public immutable cooldownSeconds;

    /// @notice Last trade timestamp per pool per wallet: pool => wallet => timestamp
    mapping(address => mapping(address => uint256)) public lastTradeTime;

    /// @param _poolRegistry PoolRegistry address for pool validation
    /// @param _owner Deployer/owner address
    /// @param _cooldownSeconds Minimum seconds between trades per wallet
    constructor(
        address _poolRegistry,
        address _owner,
        uint256 _cooldownSeconds
    ) BaseOptical(_poolRegistry, _owner) {
        cooldownSeconds = _cooldownSeconds;
    }

    /// @inheritdoc IOptical
    function getFlags() external pure override returns (uint8) {
        return BitFlag.BEFORE_SWAP;
    }

    /// @inheritdoc IOptical
    function beforeSwap(
        address pool,
        address sender,
        bool, /* isBuy */
        uint256 /* amountIn */
    ) external override onlyAttachedPool(pool) returns (bool proceed, int256 amountDelta) {
        uint256 lastTrade = lastTradeTime[pool][sender];

        if (lastTrade != 0 && block.timestamp < lastTrade + cooldownSeconds) {
            return (false, 0);
        }

        // Record this trade time
        lastTradeTime[pool][sender] = block.timestamp;

        return (true, 0);
    }
}
