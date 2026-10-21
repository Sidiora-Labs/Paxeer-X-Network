// SPDX-License-Identifier: LicenseRef-Paxlabs-HyperPax-OS-Protocol
pragma solidity ^0.8.24;

import "../base/Initializable.sol";
import "../base/UUPSUpgradeable.sol";
import "../base/AccessControl.sol";
import "../interfaces/IFeeAccumulator.sol";
import "./presets/LaunchpadOptical.sol";

/// @title LaunchpadOpticalFactory
/// @notice Self-service factory for deploying LaunchpadOptical instances.
///         Any creator can deploy their own vesting + capital-raise optical for their pool.
/// @dev UUPS proxy. Deploys immutable LaunchpadOptical contracts and auto-grants
///      OPTICAL_CLAIM_ROLE on FeeAccumulator so the new optical can claim surplus.
contract LaunchpadOpticalFactory is Initializable, UUPSUpgradeable, AccessControl {
    // ============ ERRORS ============

    error ZeroAddress();
    error ArrayTooLong();

    // ============ EVENTS ============

    event LaunchpadOpticalCreated(
        address indexed optical,
        address indexed creator,
        address teamClaimAddress,
        uint256 cliffDuration,
        uint256 vestingDuration,
        uint256 capitalRaiseBps,
        uint256 capitalRaiseDuration
    );

    // ============ STATE ============

    /// @notice PoolRegistry address (passed to each LaunchpadOptical)
    address public poolRegistry;

    /// @notice FeeAccumulator address (passed to each LaunchpadOptical + role granting)
    address public feeAccumulator;

    /// @notice OpticalRegistry for optional auto-registration
    address public opticalRegistry;

    /// @notice All deployed LaunchpadOptical instances
    address[] private _deployedOpticals;

    /// @notice Creator → their deployed opticals
    mapping(address => address[]) private _creatorOpticals;

    /// @custom:oz-upgrades-unsafe-allow constructor
    constructor() {
        _disableInitializers();
    }

    function initialize(
        address _poolRegistry,
        address _feeAccumulator,
        address _opticalRegistry,
        address _admin
    ) external initializer {
        if (_admin == address(0)) revert ZeroAddress();
        if (_feeAccumulator == address(0)) revert ZeroAddress();

        poolRegistry = _poolRegistry;
        feeAccumulator = _feeAccumulator;
        opticalRegistry = _opticalRegistry;

        _grantRole(DEFAULT_ADMIN_ROLE, _admin);
    }

    /// @notice Deploy a new LaunchpadOptical for a project
    /// @param teamWallets Additional team wallets to vest (creator is auto-vested)
    /// @param cliffDuration Hard lock period in seconds
    /// @param vestingDuration Linear unlock period after cliff in seconds
    /// @param capitalRaiseBps Fee percentage diverted to team (max 1000 = 10%)
    /// @param capitalRaiseDuration How long the capital raise fee is active in seconds
    /// @param teamClaimAddress Where accumulated USDL is claimed to
    /// @return optical The address of the newly deployed LaunchpadOptical
    function createLaunchpadOptical(
        address[] calldata teamWallets,
        uint256 cliffDuration,
        uint256 vestingDuration,
        uint256 capitalRaiseBps,
        uint256 capitalRaiseDuration,
        address teamClaimAddress
    ) external returns (address optical) {
        if (teamClaimAddress == address(0)) revert ZeroAddress();

        // Deploy new LaunchpadOptical (immutable, caller is the creator)
        LaunchpadOptical instance = new LaunchpadOptical(
            poolRegistry,
            msg.sender,         // owner
            msg.sender,         // creator (auto-vested)
            teamWallets,
            cliffDuration,
            vestingDuration,
            capitalRaiseBps,
            capitalRaiseDuration,
            teamClaimAddress,
            feeAccumulator
        );

        optical = address(instance);

        // Grant OPTICAL_CLAIM_ROLE on FeeAccumulator so the optical can claim surplus
        IFeeAccumulator(feeAccumulator).authorizeOptical(optical);

        // Track deployment
        _deployedOpticals.push(optical);
        _creatorOpticals[msg.sender].push(optical);

        emit LaunchpadOpticalCreated(
            optical,
            msg.sender,
            teamClaimAddress,
            cliffDuration,
            vestingDuration,
            capitalRaiseBps,
            capitalRaiseDuration
        );
    }

    // ============ VIEWS ============

    /// @notice Get all LaunchpadOpticals deployed by a specific creator
    function getOpticalsByCreator(address creator) external view returns (address[] memory) {
        return _creatorOpticals[creator];
    }

    /// @notice Get total number of deployed LaunchpadOpticals
    function getDeployedCount() external view returns (uint256) {
        return _deployedOpticals.length;
    }

    /// @notice Get paginated list of all deployed opticals
    function getAllOpticals(uint256 offset, uint256 limit) external view returns (address[] memory) {
        uint256 total = _deployedOpticals.length;
        if (offset >= total) return new address[](0);

        uint256 end = offset + (limit > total - offset ? total - offset : limit);

        uint256 len = end - offset;
        address[] memory result = new address[](len);
        for (uint256 i = 0; i < len; i++) {
            result[i] = _deployedOpticals[offset + i];
        }
        return result;
    }

    // ============ INTERNAL ============

    function _authorizeUpgrade(address) internal override onlyRole(DEFAULT_ADMIN_ROLE) {}
}
