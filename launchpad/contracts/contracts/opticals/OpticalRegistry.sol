// SPDX-License-Identifier: LicenseRef-Paxlabs-HyperPax-OS-Protocol
pragma solidity ^0.8.24;

import "../base/Initializable.sol";
import "../base/UUPSUpgradeable.sol";
import "../base/AccessControl.sol";
import "../interfaces/IEventEmitter.sol";

/// @title OpticalRegistry
/// @notice Trust-signaling registry for optical plugins.
/// @dev UUPS proxy. Opticals do NOT require registration to function —
///      but unregistered opticals show as "unverified" to users/frontends.
///      Admin registers/deregisters opticals with metadata.
contract OpticalRegistry is Initializable, UUPSUpgradeable, AccessControl {
    error ZeroAddress();
    error AlreadyRegistered();
    error NotRegistered();

    struct OpticalMetadata {
        string name;
        string description;
        uint8 riskLevel;     // 1-5 scale
        string auditor;      // auditor name or "unaudited"
        uint256 registeredAt;
    }

    event OpticalRegistered(address indexed optical, string name, uint8 riskLevel, uint256 timestamp);
    event OpticalDeregistered(address indexed optical, uint256 timestamp);
    event OpticalMetadataUpdated(address indexed optical, uint256 timestamp);

    mapping(address => bool) private _approved;
    mapping(address => OpticalMetadata) private _metadata;
    address[] private _allOpticals;

    address public eventEmitter;
    mapping(address => bool) private _everSeen;
    mapping(address => bytes32) public approvedCodeHash;
    error InvalidOptical();
    bool public indexInitialized;
    uint256 public indexMigrationCursor;
    uint256 public indexMigrationWritten;

    struct IdentityReview {
        bool initialized;
        uint8 flags;
        uint256 reviewedAt;
        bytes32 evidenceDigest;
    }
    // Appended storage: populated proxies require explicit review renewal.
    mapping(address => IdentityReview) public identityReviews;
    event OpticalIdentityReviewed(address indexed optical, bytes32 codeHash, uint8 flags, bytes32 evidenceDigest, uint256 reviewedAt);
    error ReviewIdentityMismatch();

    /// @custom:oz-upgrades-unsafe-allow constructor
    constructor() {
        _disableInitializers();
    }

    function initialize(address _eventEmitter, address _admin) external initializer {
        if (_admin == address(0)) revert ZeroAddress();
        eventEmitter = _eventEmitter;
        _grantRole(DEFAULT_ADMIN_ROLE, _admin);
        indexInitialized = true;
    }

    /// @notice Register an optical as approved with metadata
    /// @param optical The optical contract address
    /// @param name Human-readable name
    /// @param description Description of the optical's behavior
    /// @param riskLevel Risk level 1-5 (1 = lowest)
    /// @param auditor Auditor name or "unaudited"
    function registerOptical(
        address optical,
        string calldata name,
        string calldata description,
        uint8 riskLevel,
        string calldata auditor
    ) external onlyRole(DEFAULT_ADMIN_ROLE) {
        if (!indexInitialized) revert InvalidOptical();
        if (optical == address(0)) revert ZeroAddress();
        uint8 flags = _validate(optical, riskLevel);
        if (_approved[optical]) revert AlreadyRegistered();

        _approved[optical] = true;
        _metadata[optical] = OpticalMetadata({
            name: name,
            description: description,
            riskLevel: riskLevel,
            auditor: auditor,
            registeredAt: block.timestamp
        });
        if (!_everSeen[optical]) {
            _everSeen[optical] = true;
            _allOpticals.push(optical);
        }
        _recordReview(optical, optical.codehash, flags, bytes32(0));

        emit OpticalRegistered(optical, name, riskLevel, block.timestamp);
    }

    /// @notice Deregister an optical (mark as not approved)
    /// @param optical The optical contract address to deregister
    function deregisterOptical(address optical) external onlyRole(DEFAULT_ADMIN_ROLE) {
        if (!_approved[optical]) revert NotRegistered();
        _approved[optical] = false;

        emit OpticalDeregistered(optical, block.timestamp);
    }

    /// @notice Update metadata for a registered optical
    function updateMetadata(
        address optical,
        string calldata name,
        string calldata description,
        uint8 riskLevel,
        string calldata auditor
    ) external onlyRole(DEFAULT_ADMIN_ROLE) {
        if (!_approved[optical]) revert NotRegistered();
        if (riskLevel < 1 || riskLevel > 5) revert InvalidOptical();

        _metadata[optical] = OpticalMetadata({
            name: name,
            description: description,
            riskLevel: riskLevel,
            auditor: auditor,
            registeredAt: _metadata[optical].registeredAt
        });

        emit OpticalMetadataUpdated(optical, block.timestamp);
    }

    /// @notice Check if an optical is registered and approved
    function isRegistered(address optical) external view returns (bool) {
        return _isReviewed(optical);
    }

    /// @notice Compatibility probe used by EventEmitter dynamic optical authorization.
    function isApproved(address optical) external view returns (bool) {
        return _isReviewed(optical);
    }

    /// @notice Get metadata for an optical
    function getOpticalMetadata(address optical) external view returns (OpticalMetadata memory) {
        return _metadata[optical];
    }

    /// @notice Get all registered opticals (paginated)
    function getAllOpticals(uint256 offset, uint256 limit) external view returns (address[] memory) {
        uint256 total = _allOpticals.length;
        if (offset >= total) {
            return new address[](0);
        }

        uint256 end = offset + (limit > total - offset ? total - offset : limit);

        uint256 resultLength = end - offset;
        address[] memory result = new address[](resultLength);
        for (uint256 i = 0; i < resultLength; i++) {
            result[i] = _allOpticals[offset + i];
        }
        return result;
    }

    /// @notice Get total count of ever-registered opticals
    function getOpticalCount() external view returns (uint256) {
        return _allOpticals.length;
    }

    /// @notice Upgrade-only bounded reconciliation of historical duplicate list entries.
    /// @dev Existing approval identity must be explicitly renewed through renewReview after review.
    function migrateIndex(uint256 limit) external onlyRole(DEFAULT_ADMIN_ROLE) {
        if (indexInitialized || limit == 0) revert InvalidOptical();
        uint256 cursor = indexMigrationCursor;
        uint256 end = cursor + (limit > _allOpticals.length - cursor ? _allOpticals.length - cursor : limit);
        uint256 written = indexMigrationWritten;
        for (; cursor < end; ++cursor) {
            address optical = _allOpticals[cursor];
            if (!_everSeen[optical]) {
                _everSeen[optical] = true;
                _allOpticals[written++] = optical;
            }
        }
        indexMigrationCursor = cursor;
        indexMigrationWritten = written;
        if (cursor == _allOpticals.length) {
            // Shorten only the logical list; future pushes overwrite abandoned duplicate slots.
            assembly { sstore(_allOpticals.slot, written) }
            indexInitialized = true;
        }
    }

    /// @notice Explicitly renew reviewed runtime identity after independent review.
    /// @dev Digest commits to external review evidence, including proxy implementation
    ///      identity when applicable. Runtime codehash cannot detect arbitrary proxy upgrades.
    function renewReview(address optical, bytes32 expectedCodeHash, uint8 expectedFlags, bytes32 evidenceDigest)
        external onlyRole(DEFAULT_ADMIN_ROLE)
    {
        if (!_approved[optical]) revert NotRegistered();
        if (evidenceDigest == bytes32(0)) revert InvalidOptical();
        (bool valid, uint8 flags) = _readFlags(optical);
        if (!valid || optical.codehash != expectedCodeHash || flags != expectedFlags) revert ReviewIdentityMismatch();
        _recordReview(optical, expectedCodeHash, flags, evidenceDigest);
    }

    function _recordReview(address optical, bytes32 codeHash, uint8 flags, bytes32 evidenceDigest) internal {
        approvedCodeHash[optical] = codeHash;
        identityReviews[optical] = IdentityReview(true, flags, block.timestamp, evidenceDigest);
        emit OpticalIdentityReviewed(optical, codeHash, flags, evidenceDigest, block.timestamp);
    }

    function _isReviewed(address optical) internal view returns (bool) {
        IdentityReview storage review = identityReviews[optical];
        if (!_approved[optical] || !review.initialized || approvedCodeHash[optical] != optical.codehash) return false;
        (bool valid, uint8 flags) = _readFlags(optical);
        return valid && flags == review.flags;
    }

    /// @dev Fixed gas and output copy bound hostile probes, including return-data bombs.
    ///      All built-in immutable opticals expose a cheap getFlags getter.
    function _readFlags(address optical) internal view returns (bool valid, uint8 flags) {
        if (optical.code.length == 0) return (false, 0);
        bytes4 selector = bytes4(keccak256("getFlags()"));
        uint256 value;
        bool success;
        uint256 size;
        assembly {
            let ptr := mload(0x40)
            mstore(ptr, selector)
            success := staticcall(30000, optical, ptr, 4, ptr, 32)
            size := returndatasize()
            value := mload(ptr)
        }
        valid = success && size == 32 && value <= 31;
        flags = valid ? uint8(value) : 0;
    }

    function _validate(address optical, uint8 riskLevel) internal view returns (uint8 flags) {
        bool valid;
        (valid, flags) = _readFlags(optical);
        if (!valid || riskLevel < 1 || riskLevel > 5) revert InvalidOptical();
    }

    function _authorizeUpgrade(address) internal override onlyRole(DEFAULT_ADMIN_ROLE) {}
}
