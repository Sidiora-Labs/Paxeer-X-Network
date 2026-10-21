// SPDX-License-Identifier: LicenseRef-Paxlabs-HyperPax-OS-Protocol
pragma solidity ^0.8.24;
import "../base/Initializable.sol";
import "../base/UUPSUpgradeable.sol";
import "../base/AccessControl.sol";
import "../base/ReentrancyGuard.sol";
/// @dev Exact legacy field order for exercising populated proxy migration; not production logic.
contract LegacyFeeAccumulatorHarness is Initializable, UUPSUpgradeable, AccessControl, ReentrancyGuard {
    address public protocolConfig;
    address public treasury;
    address public poolRegistry;
    address public eventEmitter;
    address public usdlAddress;
    mapping(address => uint256) private _accumulatedFees;
    uint256 private _protocolFeesPending;
    mapping(address => uint256) private _airdropBalance;
    mapping(address => uint256) private _airdropEpoch;
    mapping(address => mapping(uint256 => uint256)) private _airdropEpochAmount;
    mapping(address => mapping(address => mapping(uint256 => bool))) private _airdropClaimed;
    mapping(address => uint256) private _lpRewardsBalance;
    mapping(address => uint256) private _opticalSurplus;
    constructor() { _disableInitializers(); }
    function initialize(address registry, address usdl, address admin) external initializer {
        poolRegistry = registry; usdlAddress = usdl; _grantRole(DEFAULT_ADMIN_ROLE, admin); _initReentrancyGuard();
    }
    function seed(address pool, uint256 fees, uint256 airdrop, uint256 surplus) external onlyRole(DEFAULT_ADMIN_ROLE) {
        _accumulatedFees[pool] = fees; _airdropBalance[pool] = airdrop; _opticalSurplus[pool] = surplus;
        _airdropEpoch[pool] = 1; _airdropEpochAmount[pool][1] = airdrop;
    }
    function _authorizeUpgrade(address) internal override onlyRole(DEFAULT_ADMIN_ROLE) {}
}
