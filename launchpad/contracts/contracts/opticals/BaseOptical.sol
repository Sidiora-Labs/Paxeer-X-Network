// SPDX-License-Identifier: LicenseRef-Paxlabs-HyperPax-OS-Protocol
pragma solidity ^0.8.24;

import "./interfaces/IOptical.sol";
import "../interfaces/IPoolRegistry.sol";

/// @title BaseOptical
/// @notice Abstract base contract for building custom opticals.
/// @dev Provides default no-op implementations for all 4 hooks.
///      Inherit this to build custom opticals — override only the hooks you need.
///      Each optical is IMMUTABLE once deployed (audited once, deployed once).
abstract contract BaseOptical is IOptical {
    error NotPool();
    error NotOwner();

    /// @notice The PoolRegistry address for validating pool callers
    address public immutable poolRegistry;

    /// @notice The deployer/owner of this optical (for configuration)
    address public immutable owner;

    constructor(address _poolRegistry, address _owner) {
        poolRegistry = _poolRegistry;
        owner = _owner;
    }

    /// @dev Modifier to restrict calls to registered pools only.
    ///      Validates caller is a pool registered in PoolRegistry.
    modifier onlyPool() {
        if (!_isRegisteredPool(msg.sender)) revert NotPool();
        _;
    }

    /// @dev Modifier to restrict calls to the optical owner.
    modifier onlyOwner() {
        if (msg.sender != owner) revert NotOwner();
        _;
    }

    // ============ DEFAULT NO-OP HOOKS ============

    /// @inheritdoc IOptical
    function beforeSwap(
        address pool, /* pool */
        address, /* sender */
        bool, /* isBuy */
        uint256 /* amountIn */
    ) external virtual override onlyAttachedPool(pool) returns (bool proceed, int256 amountDelta) {
        return (true, 0);
    }

    /// @inheritdoc IOptical
    function afterSwap(
        address pool, /* pool */
        address, /* sender */
        bool, /* isBuy */
        uint256, /* amountIn */
        uint256 /* amountOut */
    ) external virtual override onlyAttachedPool(pool) returns (bytes4) {
        return IOptical.afterSwap.selector;
    }

    /// @inheritdoc IOptical
    function beforeFeeDistribution(
        address pool, /* pool */
        uint256 feeAmount
    ) external virtual override onlyFeeAccumulator(pool) returns (uint256 adjustedFee) {
        return feeAmount;
    }

    /// @inheritdoc IOptical
    function afterFeeDistribution(
        address pool, /* pool */
        uint256 /* feeAmount */
    ) external virtual override onlyFeeAccumulator(pool) returns (bytes4) {
        return IOptical.afterFeeDistribution.selector;
    }

    /// @inheritdoc IOptical
    function getFlags() external view virtual override returns (uint8) {
        return 0;
    }

    // ============ INTERNAL HELPERS ============

    modifier onlyAttachedPool(address pool) {
        if (msg.sender != pool || !_isRegisteredPool(pool)) revert NotPool();
        _;
    }

    modifier onlyFeeAccumulator(address pool) {
        if (!_isRegisteredPool(pool)) revert NotPool();
        (bool ok, bytes memory data) = pool.staticcall(abi.encodeWithSignature("feeAccumulator()"));
        if (!ok || data.length != 32 || abi.decode(data, (address)) != msg.sender) revert NotPool();
        _;
    }

    function _isRegisteredPool(address pool) internal view returns (bool) {
        if (poolRegistry == address(0)) return false;
        IPoolRegistry.PoolMetadata memory meta = IPoolRegistry(poolRegistry).getPoolMetadata(pool);
        return meta.creator != address(0) && meta.optical == address(this);
    }

    function _poolMetadata(address pool) internal view returns (IPoolRegistry.PoolMetadata memory) {
        if (!_isRegisteredPool(pool)) revert NotPool();
        return IPoolRegistry(poolRegistry).getPoolMetadata(pool);
    }

    /// @dev Helper to read pool reserves for opticals that need pricing data.
    function _getPoolReserves(address pool) internal view returns (
        uint256 virtualUsdl,
        uint256 realUsdl,
        uint256 tokenRes
    ) {
        (bool success, bytes memory data) = pool.staticcall(
            abi.encodeWithSignature("getReserves()")
        );
        if (!success || data.length < 96) revert NotPool();
        (virtualUsdl, realUsdl, tokenRes) = abi.decode(data, (uint256, uint256, uint256));
    }
}
