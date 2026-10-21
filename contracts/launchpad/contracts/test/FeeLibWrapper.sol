// SPDX-License-Identifier: LicenseRef-Paxlabs-HyperPax-OS-Protocol
pragma solidity ^0.8.24;

import "../libraries/FeeLib.sol";

/// @notice Wrapper to expose FeeLib library functions for testing
contract FeeLibWrapper {
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
    ) external pure returns (uint256) {
        return FeeLib.calculateDynamicFee(
            baseFee, minFee, maxFee, feeDecayRate,
            volatilityWeight, concentrationWeight,
            poolAgeSeconds, volatility, topHolderBps
        );
    }

    function calculateAgeFactor(uint256 feeDecayRate, uint256 poolAgeSeconds) external pure returns (uint256) {
        return FeeLib.calculateAgeFactor(feeDecayRate, poolAgeSeconds);
    }

    function calculateVolatilityFactor(uint256 volatilityWeight, uint256 volatility) external pure returns (uint256) {
        return FeeLib.calculateVolatilityFactor(volatilityWeight, volatility);
    }

    function calculateConcentrationFactor(uint256 concentrationWeight, uint256 topHolderBps) external pure returns (uint256) {
        return FeeLib.calculateConcentrationFactor(concentrationWeight, topHolderBps);
    }

    function calculateVolatility(uint256[8] memory snapshots, uint256 count) external pure returns (uint256) {
        return FeeLib.calculateVolatility(snapshots, count);
    }
}
