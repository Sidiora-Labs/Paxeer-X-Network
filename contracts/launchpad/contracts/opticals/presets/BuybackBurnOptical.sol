// SPDX-License-Identifier: LicenseRef-Paxlabs-HyperPax-OS-Protocol
pragma solidity ^0.8.24;

import "../BaseOptical.sol";
import "../../libraries/BitFlag.sol";
import "../../libraries/TransferHelper.sol";
import "../../interfaces/IFeeAccumulator.sol";
import "../../interfaces/ISidioraPool.sol";

/// @title BuybackBurnOptical
/// @notice Redirects a configurable portion of fee distributions to buy back tokens and burn them.
/// @dev Uses beforeFeeDistribution hook to redirect a percentage of fees.
///      The redirected USDL is accumulated and can be used to execute buyback+burn via
///      an external trigger. Immutable once deployed.
///
///      Flow: beforeFeeDistribution reduces the fee by buybackBps%, accumulating the
///      redirected portion. The owner can call executeBuybackBurn() to send the accumulated
///      USDL to the pool, execute a real swap, and retire output at the dead address.
contract BuybackBurnOptical is BaseOptical {
    error NothingToExecute();

    /// @notice Dead address for burning tokens
    address public constant DEAD_ADDRESS = 0x000000000000000000000000000000000000dEaD;

    /// @notice Percentage of fees redirected to buyback+burn (in bps, e.g., 2000 = 20%)
    uint256 public immutable buybackBps;

    /// @notice Accumulated USDL per pool for buyback execution
    mapping(address => uint256) public accumulatedUsdl;

    event BuybackAccumulated(address indexed pool, uint256 amount);
    event BuybackExecuted(address indexed pool, uint256 usdlUsed);

    /// @param _poolRegistry PoolRegistry address for pool validation
    /// @param _owner Deployer/owner address
    /// @param _buybackBps Percentage of fees to redirect (in bps, max 5000 = 50%)
    constructor(
        address _poolRegistry,
        address _owner,
        uint256 _buybackBps
    ) BaseOptical(_poolRegistry, _owner) {
        require(_owner != address(0), "Zero owner");
        require(_buybackBps <= 5000, "Max 50%");
        buybackBps = _buybackBps;
    }

    /// @inheritdoc IOptical
    function getFlags() external pure override returns (uint8) {
        return BitFlag.BEFORE_FEE_DISTRIBUTION;
    }

    /// @inheritdoc IOptical
    function beforeFeeDistribution(
        address pool,
        uint256 feeAmount
    ) external override onlyFeeAccumulator(pool) returns (uint256 adjustedFee) {
        if (buybackBps == 0) {
            return feeAmount;
        }

        uint256 buybackAmount = (feeAmount * buybackBps) / 10000;
        if (buybackAmount > 0) {
            accumulatedUsdl[pool] += buybackAmount;
            emit BuybackAccumulated(pool, buybackAmount);
        }

        return feeAmount - buybackAmount;
    }

    /// @notice Get the accumulated USDL available for buyback for a pool
    function getAccumulatedUsdl(address pool) external view returns (uint256) {
        return accumulatedUsdl[pool];
    }

    /// @notice Owner executes funded surplus buyback with an explicit minimum and deadline.
    /// @dev Trusted execution policy: spot reserves cannot provide a manipulation-resistant minimum.
    function executeBuybackBurn(address pool, uint256 minTokensOut, uint256 deadline)
        external onlyOwner returns (uint256 tokensBurned)
    {
        _poolMetadata(pool);
        require(minTokensOut > 0, "Slippage required");
        uint256 amount = accumulatedUsdl[pool];
        if (amount == 0) revert NothingToExecute();
        accumulatedUsdl[pool] = 0;
        (bool ok, bytes memory data) = pool.staticcall(abi.encodeWithSignature("feeAccumulator()"));
        require(ok && data.length == 32, "Invalid accumulator");
        address accumulator = abi.decode(data, (address));
        (ok, data) = accumulator.staticcall(abi.encodeWithSignature("usdlAddress()"));
        require(ok && data.length == 32, "Invalid USDL");
        address usdl = abi.decode(data, (address));
        IFeeAccumulator(accumulator).claimOpticalSurplus(pool, amount, address(this));
        TransferHelper.safeTransfer(usdl, pool, amount);
        tokensBurned = ISidioraPool(pool).swap(amount, minTokensOut, true, DEAD_ADDRESS, deadline);
        emit BuybackExecuted(pool, amount);
    }
}
