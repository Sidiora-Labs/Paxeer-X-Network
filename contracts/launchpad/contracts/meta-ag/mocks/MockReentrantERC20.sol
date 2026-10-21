// SPDX-License-Identifier: LicenseRef-Paxlabs-HyperPax-OS-Protocol
pragma solidity ^0.8.27;

/**
 * @title MockReentrantERC20
 * @notice Malicious ERC20 that re-enters a configurable target on transfer.
 * @dev Used by {PECORVault} tests to prove `nonReentrant` guards fire on
 *      emergencyWithdraw / pullTokens / pushTokens / deposit. Re-entry is
 *      opt-in so the same mock can double as a plain ERC20 where desired.
 */
contract MockReentrantERC20 {
    string public name;
    string public symbol;
    uint8 public decimals;
    uint256 public totalSupply;

    mapping(address => uint256) public balanceOf;
    mapping(address => mapping(address => uint256)) public allowance;

    address public reentryTarget;
    bytes public reentryCalldata;
    bool public reentryArmed;

    // Post-mortem state captured on the most recent reentry attempt. Tests
    // use this to assert the exact selector (e.g. ReentrancyGuardReentrantCall)
    // that was raised inside the nested call, because the outer caller
    // (vault -> TransferHelper.safeTransfer) wraps token-level reverts with
    // TransferFailed() and the inner selector would otherwise be unobservable
    // from the outside. See PECORVault.test.js > ReentrancyGuard.
    bool public lastReentryOk;
    bytes public lastReentryRet;
    bool public lastReentryRecorded;

    event Transfer(address indexed from, address indexed to, uint256 value);
    event Approval(address indexed owner, address indexed spender, uint256 value);
    event ReentryArmed(address target, bytes payload);
    event ReentryAttempted(bool ok, bytes ret);

    constructor(string memory name_, string memory symbol_, uint8 decimals_) {
        name = name_;
        symbol = symbol_;
        decimals = decimals_;
    }

    function mint(address to, uint256 amount) external {
        totalSupply += amount;
        balanceOf[to] += amount;
        emit Transfer(address(0), to, amount);
    }

    function armReentry(address target, bytes calldata payload) external {
        reentryTarget = target;
        reentryCalldata = payload;
        reentryArmed = true;
        emit ReentryArmed(target, payload);
    }

    function disarmReentry() external {
        reentryArmed = false;
    }

    function approve(address spender, uint256 amount) external returns (bool) {
        allowance[msg.sender][spender] = amount;
        emit Approval(msg.sender, spender, amount);
        return true;
    }

    function transfer(address to, uint256 amount) external returns (bool) {
        _transfer(msg.sender, to, amount);
        _maybeReenter();
        return true;
    }

    function transferFrom(address from, address to, uint256 amount) external returns (bool) {
        uint256 current = allowance[from][msg.sender];
        require(current >= amount, "MockReentrantERC20: insufficient allowance");
        if (current != type(uint256).max) {
            allowance[from][msg.sender] = current - amount;
        }
        _transfer(from, to, amount);
        _maybeReenter();
        return true;
    }

    function _transfer(address from, address to, uint256 amount) internal {
        require(balanceOf[from] >= amount, "MockReentrantERC20: insufficient balance");
        balanceOf[from] -= amount;
        balanceOf[to] += amount;
        emit Transfer(from, to, amount);
    }

    function _maybeReenter() internal {
        if (!reentryArmed) return;
        // Disarm before reentry to avoid infinite recursion on error paths.
        reentryArmed = false;
        // solhint-disable-next-line avoid-low-level-calls
        (bool ok, bytes memory ret) = reentryTarget.call(reentryCalldata);
        lastReentryOk = ok;
        lastReentryRet = ret;
        lastReentryRecorded = true;
        emit ReentryAttempted(ok, ret);
        // Deliberately swallow the inner revert: the outer caller (the vault
        // via TransferHelper.safeTransfer) would otherwise rewrap any
        // bubbled-up token error as `TransferFailed()`, masking the exact
        // selector (e.g. {ReentrancyGuardReentrantCall}) the guard produced.
        // Tests inspect `lastReentryOk`/`lastReentryRet` on this mock to
        // verify the guard fired with the expected error.
    }
}
