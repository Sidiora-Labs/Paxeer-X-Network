// SPDX-License-Identifier: LicenseRef-Paxlabs-HyperPax-OS-Protocol
pragma solidity ^0.8.24;

import "../BaseOptical.sol";
import "../../libraries/BitFlag.sol";

/// @title AntiSnipeOptical
/// @notice Blocks buys exceeding maxBuyBps of effective USDL reserves in the first N blocks after pool creation.
/// @dev Uses beforeSwap hook only. Immutable once deployed.
///      After the protection period, all buys are allowed regardless of size.
///      Sells are never affected by this optical.
contract AntiSnipeOptical is BaseOptical {
    error ProtectionActive();

    /// @notice Maximum buy percentage in basis points (e.g., 100 = 1%)
    uint256 public immutable maxBuyBps;

    /// @notice Number of blocks after pool creation during which protection is active
    uint256 public immutable protectionBlocks;

    /// @notice Mapping of pool address to the block number at which it was created
    mapping(address => uint256) public poolCreationBlock;

    /// @param _poolRegistry PoolRegistry address for pool validation
    /// @param _owner Deployer/owner address
    /// @param _maxBuyBps Maximum buy size in bps of token supply (e.g., 100 = 1%)
    /// @param _protectionBlocks Number of blocks the protection lasts
    constructor(
        address _poolRegistry,
        address _owner,
        uint256 _maxBuyBps,
        uint256 _protectionBlocks
    ) BaseOptical(_poolRegistry, _owner) {
        require(_maxBuyBps > 0 && _maxBuyBps <= 10000, "Invalid cap");
        maxBuyBps = _maxBuyBps;
        protectionBlocks = _protectionBlocks;
    }

    /// @notice Register a pool's creation block (called once when pool is created)
    /// @dev Anyone can call this, but it only records the first time for each pool
    function registerPool(address pool) external {
        if (poolCreationBlock[pool] == 0) {
            poolCreationBlock[pool] = _poolMetadata(pool).createdBlock;
        }
    }

    /// @inheritdoc IOptical
    function getFlags() external pure override returns (uint8) {
        return BitFlag.BEFORE_SWAP;
    }

    /// @inheritdoc IOptical
    function beforeSwap(
        address pool,
        address, /* sender */
        bool isBuy,
        uint256 amountIn
    ) external override onlyAttachedPool(pool) returns (bool proceed, int256 amountDelta) {
        // Sells are never affected
        if (!isBuy) {
            return (true, 0);
        }

        // If pool not registered or protection period expired, allow
        uint256 creationBlock = _poolMetadata(pool).createdBlock;
        if (creationBlock == 0 || block.number > creationBlock + protectionBlocks) {
            return (true, 0);
        }

        // During protection: check buy size against max allowed
        // We read the token reserve from the pool to calculate max buy amount in USDL terms
        (uint256 virtualUsdl, uint256 realUsdl, uint256 tokenRes) = _getPoolReserves(pool);
        if (tokenRes == 0) {
            return (true, 0);
        }

        // Max buy is maxBuyBps of the effective USDL reserve
        // This limits how much USDL can be used in a single buy
        
        uint256 effectiveUsdl = virtualUsdl + realUsdl;
        uint256 maxBuyAmount = (effectiveUsdl * maxBuyBps) / 10000;

        if (amountIn > maxBuyAmount) {
            return (false, 0);
        }

        return (true, 0);
    }
}
