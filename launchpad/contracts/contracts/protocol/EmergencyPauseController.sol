// SPDX-License-Identifier: LicenseRef-Paxlabs-HyperPax-OS-Protocol
pragma solidity ^0.8.24;

import {AccessControl} from "../base/AccessControl.sol";
import {ReentrancyGuard} from "../base/ReentrancyGuard.sol";

/// @notice Incident responders can halt allowlisted endpoints. Recovery and
/// target/guardian management remain with the Timelock administrator.
/// @dev No arbitrary calldata, upgrades, custody, or unpause entry point.
contract EmergencyPauseController is AccessControl, ReentrancyGuard {
    bytes32 public constant INCIDENT_GUARDIAN_ROLE = keccak256("INCIDENT_GUARDIAN_ROLE");
    uint256 public constant MAX_TARGETS = 32;
    uint256 public constant TARGET_CALL_GAS = 150_000;
    uint256 public constant STATUS_CALL_GAS = 30_000;
    bytes4 private constant STATUS_SELECTOR = bytes4(keccak256("emergencyPaused()"));
    bytes4 private constant PAUSE_SELECTOR = bytes4(keccak256("pause()"));
    mapping(address => bool) public allowedTargets;

    enum PauseStatus { Paused, AlreadyPaused, NotAllowed, InvalidStatus, CallFailed, PostconditionFailed }
    error InvalidTarget();
    error InvalidBatchLength();
    error InsufficientIncidentGas();
    event TargetUpdated(address indexed target, bool allowed);
    event IncidentTargetResult(bytes32 indexed incidentId, address indexed target, PauseStatus status);
    event IncidentProcessed(bytes32 indexed incidentId, address indexed responder, uint256 targetCount);

    constructor(address admin, address guardian, address[] memory initialTargets) {
        _grantRole(DEFAULT_ADMIN_ROLE, admin);
        _grantRole(INCIDENT_GUARDIAN_ROLE, guardian);
        if (initialTargets.length > MAX_TARGETS) revert InvalidBatchLength();
        for (uint256 i; i < initialTargets.length; ++i) _setTarget(initialTargets[i], true);
    }

    function setTarget(address target, bool allowed) external onlyRole(DEFAULT_ADMIN_ROLE) {
        _setTarget(target, allowed);
    }

    function _setTarget(address target, bool allowed) private {
        if (target == address(0) || (allowed && target.code.length == 0)) revert InvalidTarget();
        allowedTargets[target] = allowed;
        emit TargetUpdated(target, allowed);
    }

    function pauseTargets(address[] calldata targets, bytes32 incidentId)
        external onlyRole(INCIDENT_GUARDIAN_ROLE) nonReentrant
    {
        uint256 length = targets.length;
        if (length == 0 || length > MAX_TARGETS) revert InvalidBatchLength();
        // Reserve the full bounded worst-case budget up front. A caller-provided
        // undersized gas limit must not silently leave late targets untreated.
        if (gasleft() < length * 250_000 + 50_000) revert InsufficientIncidentGas();
        for (uint256 i; i < length; ++i) {
            address target = targets[i];
            PauseStatus result;
            if (!allowedTargets[target]) result = PauseStatus.NotAllowed;
            else {
                (bool valid, bool paused) = _status(target);
                if (!valid) result = PauseStatus.InvalidStatus;
                else if (paused) result = PauseStatus.AlreadyPaused;
                else if (!_callPause(target)) result = PauseStatus.CallFailed;
                else {
                    (valid, paused) = _status(target);
                    result = valid && paused ? PauseStatus.Paused : PauseStatus.PostconditionFailed;
                }
            }
            emit IncidentTargetResult(incidentId, target, result);
        }
        emit IncidentProcessed(incidentId, msg.sender, length);
    }

    function _status(address target) private view returns (bool valid, bool paused) {
        bytes4 selector = STATUS_SELECTOR;
        uint256 value;
        uint256 size;
        uint256 budget = STATUS_CALL_GAS;
        assembly {
            let ptr := mload(0x40)
            mstore(ptr, selector)
            valid := staticcall(budget, target, ptr, 4, ptr, 32)
            size := returndatasize()
            value := mload(ptr)
        }
        valid = valid && size == 32 && value <= 1;
        paused = value == 1;
    }

    function _callPause(address target) private returns (bool success) {
        bytes4 selector = PAUSE_SELECTOR;
        uint256 budget = TARGET_CALL_GAS;
        // Output length zero prevents a target's return/revert payload from
        // allocating unbounded memory in the incident controller.
        assembly {
            let ptr := mload(0x40)
            mstore(ptr, selector)
            success := call(budget, target, 0, ptr, 4, 0, 0)
        }
    }
}
