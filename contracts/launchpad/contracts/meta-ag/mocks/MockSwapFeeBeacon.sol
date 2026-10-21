// SPDX-License-Identifier: LicenseRef-Paxlabs-HyperPax-OS-Protocol
pragma solidity ^0.8.27;

/**
 * @title MockSwapFeeBeacon
 * @notice Minimal stand-in for the PECOR engine surface used by MetaAGQuoter.
 *         The quoter only ever reads `swapFeeBps()` via low-level staticcall,
 *         so this mock exposes that single function with a configurable value
 *         plus an optional revert switch to exercise the fail-soft path in
 *         `_getFeeBps`.
 * @dev Scope-isolated under `contracts/meta-ag/mocks/`. Do NOT use in
 *      production — this is for unit tests only.
 */
contract MockSwapFeeBeacon {
    error ForcedRevert();

    uint256 private _feeBps;
    bool private _shouldRevert;

    constructor(uint256 initialFeeBps_) {
        _feeBps = initialFeeBps_;
    }

    function setFeeBps(uint256 newFeeBps) external {
        _feeBps = newFeeBps;
    }

    function setRevert(bool flag) external {
        _shouldRevert = flag;
    }

    function swapFeeBps() external view returns (uint256) {
        if (_shouldRevert) revert ForcedRevert();
        return _feeBps;
    }
}
