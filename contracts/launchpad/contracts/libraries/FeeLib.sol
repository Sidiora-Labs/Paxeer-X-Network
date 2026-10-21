// SPDX-License-Identifier: LicenseRef-Paxlabs-HyperPax-OS-Protocol
pragma solidity ^0.8.24;

import "./SidioraMath.sol";

/// @title FeeLib
/// @notice Dynamic fee calculation for Sidiora pools
/// @dev feeBps = baseFee + ageFactor + volatilityFactor + concentrationFactor
///      Clamped to [minFeeBps, maxFeeBps]
library FeeLib {
    /// @notice Calculates the dynamic fee for a swap
    /// @param baseFee Base fee in basis points
    /// @param minFee Minimum fee in basis points
    /// @param maxFee Maximum fee in basis points
    /// @param feeDecayRate Controls how fast age factor decays
    /// @param volatilityWeight Weight of volatility component
    /// @param concentrationWeight Weight of concentration component
    /// @param poolAgeSeconds Age of pool in seconds
    /// @param volatility RMS relative price movement (parts per million)
    /// @param topHolderBps Top holder's percentage in basis points (0-10000)
    /// @return feeBps The calculated fee in basis points
    function calculateDynamicFee(
        uint256 baseFee,
        uint256 minFee,
        uint256 maxFee,
        uint256 feeDecayRate,
        uint256 volatilityWeight,
        uint256 concentrationWeight,
        uint256 poolAgeSeconds,
        uint256 volatility,
        uint256 topHolderBps
    ) internal pure returns (uint256 feeBps) {
        require(minFee <= maxFee && maxFee < 10000, "invalid fee bounds");
        if (baseFee >= maxFee) return maxFee;
        feeBps = baseFee;
        uint256 remaining = maxFee - feeBps;
        uint256 age = calculateAgeFactor(feeDecayRate, poolAgeSeconds);
        if (age >= remaining) return maxFee;
        feeBps += age;
        remaining = maxFee - feeBps;
        // Compare before multiplying. Components above the remaining budget saturate safely.
        if (volatility != 0 && volatilityWeight != 0) {
            uint256 threshold = SidioraMath.mulDiv(remaining, 1e6, volatility);
            if (volatilityWeight > threshold) return maxFee;
            feeBps += calculateVolatilityFactor(volatilityWeight, volatility);
        }
        remaining = maxFee - feeBps;
        if (topHolderBps != 0 && concentrationWeight != 0) {
            uint256 threshold = SidioraMath.mulDiv(remaining, 10000, topHolderBps);
            if (concentrationWeight > threshold) return maxFee;
            feeBps += calculateConcentrationFactor(concentrationWeight, topHolderBps);
        }
        if (feeBps < minFee) feeBps = minFee;
    }

    /// @notice Age factor: higher fee for younger pools, decays over time
    /// @dev ageFactor = feeDecayRate / (1 + poolAgeInHours)
    /// @param feeDecayRate Decay rate parameter (in bps)
    /// @param poolAgeSeconds Pool age in seconds
    /// @return factor Age-based fee component in basis points
    function calculateAgeFactor(
        uint256 feeDecayRate,
        uint256 poolAgeSeconds
    ) internal pure returns (uint256 factor) {
        uint256 poolAgeHours = poolAgeSeconds / 3600;
        factor = feeDecayRate / (1 + poolAgeHours);
    }

    /// @notice Volatility factor: higher fee when price swings are large
    /// @dev volatilityFactor = volatilityWeight * volatility / 1e6
    /// @param volatilityWeight Weight parameter
    /// @param volatility RMS relative price movement scaled by 1e6
    /// @return factor Volatility-based fee component in basis points
    function calculateVolatilityFactor(
        uint256 volatilityWeight,
        uint256 volatility
    ) internal pure returns (uint256 factor) {
        if (volatility == 0) return 0;
        factor = SidioraMath.mulDiv(volatilityWeight, volatility, 1e6);
    }

    /// @notice Concentration factor: higher fee when whale dominates
    /// @dev concentrationFactor = concentrationWeight * topHolderBps / 10000
    /// @param concentrationWeight Weight parameter
    /// @param topHolderBps Top holder's share in basis points
    /// @return factor Concentration-based fee component in basis points
    function calculateConcentrationFactor(
        uint256 concentrationWeight,
        uint256 topHolderBps
    ) internal pure returns (uint256 factor) {
        if (topHolderBps == 0) return 0;
        factor = SidioraMath.mulDiv(concentrationWeight, topHolderBps, 10000);
    }

    /// @notice RMS relative price movement, in parts per million, across chronological observations.
    /// @dev Each absolute return is capped at 100%. Squared returns retain oscillations and
    ///      prevent price-unit dependence. The bounded eight-observation calculation cannot overflow.
    function calculateVolatility(uint256[8] memory snapshots, uint256 count)
        internal pure returns (uint256 volatility)
    {
        require(count <= 8, "invalid snapshot count");
        if (count < 2) return 0;
        uint256 sumSquares;
        uint256 changes;
        for (uint256 i = 1; i < count; ++i) {
            uint256 previous = snapshots[i - 1];
            uint256 current = snapshots[i];
            uint256 difference = current > previous ? current - previous : previous - current;
            uint256 movement;
            if (previous == 0) movement = current == 0 ? 0 : 1e6;
            else movement = difference >= previous ? 1e6 : SidioraMath.mulDiv(difference, 1e6, previous);
            sumSquares += movement * movement;
            ++changes;
        }
        volatility = SidioraMath.sqrt(sumSquares / changes);
    }
}
