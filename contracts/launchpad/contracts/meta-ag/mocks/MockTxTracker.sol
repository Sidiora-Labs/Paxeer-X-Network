// SPDX-License-Identifier: LicenseRef-Paxlabs-HyperPax-OS-Protocol
pragma solidity ^0.8.27;

/**
 * @title MockTxTracker
 * @notice Deliberately-minimal stub for {ITransactionTracker} used by
 *         {PECORVault} tests. Phase 3 exercises the vault's tracker wiring
 *         (setTransactionTracker + event + zero-address accepted at
 *         bootstrap); the full tracker surface lands in Phase 7 (Task 7.2).
 * @dev The vault never reads back from the tracker during Phase 3, so this
 *      mock exposes no-op recorders. It simply captures the last call on
 *      each hook to keep future-phase compatibility trivial.
 */
contract MockTxTracker {
    address public lastCaller;
    bytes public lastPayload;
    uint256 public callCount;

    event Recorded(address indexed caller, bytes payload);

    /// @dev Generic catch-all recorder used by later phases; kept optional.
    function record(bytes calldata payload) external {
        lastCaller = msg.sender;
        lastPayload = payload;
        unchecked {
            callCount += 1;
        }
        emit Recorded(msg.sender, payload);
    }
}
