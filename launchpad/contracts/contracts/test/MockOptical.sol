// SPDX-License-Identifier: LicenseRef-Paxlabs-HyperPax-OS-Protocol
pragma solidity ^0.8.24;

import "../opticals/BaseOptical.sol";

/// @title MockOptical
/// @notice Test mock that inherits BaseOptical with configurable behavior
contract MockOptical is BaseOptical {
    // No flags set — all hooks are no-ops by default
    constructor(address _poolRegistry, address _owner)
        BaseOptical(_poolRegistry, _owner)
    {}
}

/// @title MockOpticalWithFlags
/// @notice Test mock with configurable flags and hook behavior
contract MockOpticalWithFlags is BaseOptical {
    uint8 private _flags;

    bool public beforeSwapRejectNext;
    int256 public beforeSwapDeltaNext;
    bool public beforeSwapCalled;
    bool public afterSwapCalled;
    bool public beforeFeeDistCalled;
    bool public afterFeeDistCalled;

    // Track call args for verification
    address public lastPool;
    address public lastSender;
    bool public lastIsBuy;
    uint256 public lastAmountIn;
    uint256 public lastAmountOut;
    uint256 public lastFeeAmount;

    constructor(address _poolRegistry, address _owner, uint8 flags_)
        BaseOptical(_poolRegistry, _owner)
    {
        _flags = flags_;
    }

    function setFlags(uint8 flags_) external {
        _flags = flags_;
    }

    function setBeforeSwapReject(bool reject) external {
        beforeSwapRejectNext = reject;
    }

    function setBeforeSwapDelta(int256 delta) external {
        beforeSwapDeltaNext = delta;
    }

    function resetCallTrackers() external {
        beforeSwapCalled = false;
        afterSwapCalled = false;
        beforeFeeDistCalled = false;
        afterFeeDistCalled = false;
    }

    function getFlags() external view override returns (uint8) {
        return _flags;
    }

    function beforeSwap(
        address pool,
        address sender,
        bool isBuy,
        uint256 amountIn
    ) external override returns (bool proceed, int256 amountDelta) {
        beforeSwapCalled = true;
        lastPool = pool;
        lastSender = sender;
        lastIsBuy = isBuy;
        lastAmountIn = amountIn;

        if (beforeSwapRejectNext) {
            return (false, 0);
        }
        return (true, beforeSwapDeltaNext);
    }

    function afterSwap(
        address pool,
        address sender,
        bool isBuy,
        uint256 amountIn,
        uint256 amountOut
    ) external override returns (bytes4) {
        afterSwapCalled = true;
        lastPool = pool;
        lastSender = sender;
        lastIsBuy = isBuy;
        lastAmountIn = amountIn;
        lastAmountOut = amountOut;
        return IOptical.afterSwap.selector;
    }

    function beforeFeeDistribution(
        address pool,
        uint256 feeAmount
    ) external override returns (uint256 adjustedFee) {
        beforeFeeDistCalled = true;
        lastPool = pool;
        lastFeeAmount = feeAmount;
        return feeAmount;
    }

    function afterFeeDistribution(
        address pool,
        uint256 feeAmount
    ) external override returns (bytes4) {
        afterFeeDistCalled = true;
        lastPool = pool;
        lastFeeAmount = feeAmount;
        return IOptical.afterFeeDistribution.selector;
    }
}

/// @title MockRevertingOptical
/// @notice An optical that reverts on any hook call
contract MockRevertingOptical is BaseOptical {
    uint8 private _flags;

    constructor(address _poolRegistry, address _owner, uint8 flags_)
        BaseOptical(_poolRegistry, _owner)
    {
        _flags = flags_;
    }

    function getFlags() external view override returns (uint8) {
        return _flags;
    }

    function beforeSwap(address, address, bool, uint256) external pure override returns (bool, int256) {
        revert("MockRevertingOptical: revert");
    }

    function afterSwap(address, address, bool, uint256, uint256) external pure override returns (bytes4) {
        revert("MockRevertingOptical: revert");
    }
}
