// SPDX-License-Identifier: LicenseRef-Paxlabs-HyperPax-OS-Protocol
pragma solidity ^0.8.24;

/// @title IProtocolConfig
/// @notice Interface for the global protocol configuration contract
interface IProtocolConfig {
    // --- Errors ---
    error FeeOutOfRange();
    error ZeroAddress();

    // --- Events ---
    event ConfigUpdated(bytes32 indexed key, uint256 oldValue, uint256 newValue);

    // --- Views ---
    function usdlAddress() external view returns (address);
    function virtualUsdlDefault() external view returns (uint256);
    function virtualTokenDefault() external view returns (uint256);
    function minFeeBps() external view returns (uint256);
    function maxFeeBps() external view returns (uint256);
    function baseFeeBps() external view returns (uint256);
    function protocolFeeBps() external view returns (uint256);
    function feeDecayRate() external view returns (uint256);
    function volatilityWeight() external view returns (uint256);
    function concentrationWeight() external view returns (uint256);
    function creationFee() external view returns (uint256);

    // --- Setters (admin only) ---
    function setBaseFeeBps(uint256 newBaseFeeBps) external;
    function setProtocolFeeBps(uint256 newProtocolFeeBps) external;
    function setCreationFee(uint256 newCreationFee) external;
    function setFeeWeights(uint256 newDecayRate, uint256 newVolWeight, uint256 newConcWeight) external;
    function setVirtualDefaults(uint256 newVirtualUsdl, uint256 newVirtualToken) external;
}
