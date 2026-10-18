// SPDX-License-Identifier: LicenseRef-Paxlabs-HyperPax-OS-Protocol
pragma solidity ^0.8.24;

import "../base/Initializable.sol";
import "../base/UUPSUpgradeable.sol";
import "../base/AccessControl.sol";
import "../interfaces/IProtocolConfig.sol";
import "../interfaces/IEventEmitter.sol";

/// @title ProtocolConfig
/// @notice Single source of truth for all global protocol parameters
/// @dev UUPS proxy. Only admin can update. Emits config changes via EventEmitter.
contract ProtocolConfig is IProtocolConfig, Initializable, UUPSUpgradeable, AccessControl {
    address public override usdlAddress;
    uint256 public override virtualUsdlDefault;
    uint256 public override virtualTokenDefault;
    uint256 public override minFeeBps;
    uint256 public override maxFeeBps;
    uint256 public override baseFeeBps;
    uint256 public override protocolFeeBps;
    uint256 public override feeDecayRate;
    uint256 public override volatilityWeight;
    uint256 public override concentrationWeight;
    uint256 public override creationFee;

    address public eventEmitter;

    /// @custom:oz-upgrades-unsafe-allow constructor
    constructor() {
        _disableInitializers();
    }

    function initialize(
        address _usdlAddress,
        address _eventEmitter,
        address _admin
    ) external initializer {
        if (_usdlAddress.code.length == 0) revert ZeroAddress();
        if (_eventEmitter != address(0) && _eventEmitter.code.length == 0) revert ZeroAddress();
        if (_admin == address(0)) revert ZeroAddress();

        usdlAddress = _usdlAddress;
        eventEmitter = _eventEmitter;

        virtualUsdlDefault = 10_000e6;
        virtualTokenDefault = 1_000_000_000e6;
        minFeeBps = 10;
        maxFeeBps = 300;
        baseFeeBps = 30;
        protocolFeeBps = 1000; // 10% of pool fees
        feeDecayRate = 500;
        volatilityWeight = 100;
        concentrationWeight = 0; // Disabled until pools provide an authenticated concentration metric.
        creationFee = 1e6; // 1 USDL (6 decimals), excluding network gas.

        _grantRole(DEFAULT_ADMIN_ROLE, _admin);
    }

    function setBaseFeeBps(uint256 newBaseFeeBps) external override onlyRole(DEFAULT_ADMIN_ROLE) {
        if (newBaseFeeBps < minFeeBps || newBaseFeeBps > maxFeeBps) revert FeeOutOfRange();
        uint256 old = baseFeeBps;
        baseFeeBps = newBaseFeeBps;
        _emitConfigUpdated("baseFeeBps", old, newBaseFeeBps);
    }

    function setProtocolFeeBps(uint256 newProtocolFeeBps) external override onlyRole(DEFAULT_ADMIN_ROLE) {
        if (newProtocolFeeBps > 5000) revert FeeOutOfRange(); // max 50%
        uint256 old = protocolFeeBps;
        protocolFeeBps = newProtocolFeeBps;
        _emitConfigUpdated("protocolFeeBps", old, newProtocolFeeBps);
    }

    function setCreationFee(uint256 newCreationFee) external override onlyRole(DEFAULT_ADMIN_ROLE) {
        uint256 old = creationFee;
        creationFee = newCreationFee;
        _emitConfigUpdated("creationFee", old, newCreationFee);
    }

    function setFeeWeights(
        uint256 newDecayRate,
        uint256 newVolWeight,
        uint256 newConcWeight
    ) external override onlyRole(DEFAULT_ADMIN_ROLE) {
        if (newDecayRate > 10_000 || newVolWeight > 10_000 || newConcWeight != 0) revert FeeOutOfRange();
        uint256 oldDecay = feeDecayRate;
        uint256 oldVol = volatilityWeight;
        uint256 oldConc = concentrationWeight;
        feeDecayRate = newDecayRate;
        volatilityWeight = newVolWeight;
        concentrationWeight = newConcWeight;
        _emitConfigUpdated("feeDecayRate", oldDecay, newDecayRate);
        _emitConfigUpdated("volatilityWeight", oldVol, newVolWeight);
        _emitConfigUpdated("concentrationWeight", oldConc, newConcWeight);
    }

    function setVirtualDefaults(
        uint256 newVirtualUsdl,
        uint256 newVirtualToken
    ) external override onlyRole(DEFAULT_ADMIN_ROLE) {
        if (newVirtualUsdl == 0 || newVirtualToken == 0 || newVirtualUsdl > type(uint112).max || newVirtualToken > type(uint112).max) revert FeeOutOfRange();
        uint256 oldUsdl = virtualUsdlDefault;
        uint256 oldToken = virtualTokenDefault;
        virtualUsdlDefault = newVirtualUsdl;
        virtualTokenDefault = newVirtualToken;
        _emitConfigUpdated("virtualUsdlDefault", oldUsdl, newVirtualUsdl);
        _emitConfigUpdated("virtualTokenDefault", oldToken, newVirtualToken);
    }

    function _emitConfigUpdated(string memory key, uint256 oldValue, uint256 newValue) private {
        if (eventEmitter != address(0)) {
            bytes32 keyHash = keccak256(bytes(key));
            IEventEmitter(eventEmitter).emitConfigUpdated(keyHash, oldValue, newValue);
        }
    }

    function _authorizeUpgrade(address) internal override onlyRole(DEFAULT_ADMIN_ROLE) {}
}
