// SPDX-License-Identifier: LicenseRef-Paxlabs-HyperPax-OS-Protocol
pragma solidity ^0.8.27;

import {IWETH} from "../interfaces/IWETH.sol";

/**
 * @title MockWETH9
 * @notice Minimal WETH-compatible mock that mirrors the WPAX9 live contract
 *         on Paxeer Network. Used by {PECORVault} tests to exercise
 *         `depositNative` (wrap) and `withdrawNative` (unwrap) paths and to
 *         assert the {PECORVault.receive} WethOnly guard.
 * @dev Keeps the WETH9 ABI surface minimal enough to test the vault without
 *      pulling in an external WETH9 source.
 */
contract MockWETH9 is IWETH {
    // Non-constant public state vars (vs `constant`) mirror the pattern used
    // by the other meta-ag mocks (see MockReentrantERC20) and keep solhint's
    // const-name-snakecase rule happy while preserving the exact WETH ABI
    // (lowercase name/symbol/decimals, required for wallet compatibility).
    string public name = "Mock Wrapped PAX";
    string public symbol = "mWPAX";
    uint8 public decimals = 18;

    uint256 private _totalSupply;
    mapping(address => uint256) private _balances;
    mapping(address => mapping(address => uint256)) private _allowances;

    error InsufficientBalance();
    error InsufficientAllowance();

    function totalSupply() external view override returns (uint256) {
        return _totalSupply;
    }

    function balanceOf(address owner) external view override returns (uint256) {
        return _balances[owner];
    }

    function allowance(address owner, address spender) external view override returns (uint256) {
        return _allowances[owner][spender];
    }

    function deposit() external payable override {
        _balances[msg.sender] += msg.value;
        _totalSupply += msg.value;
        emit Deposit(msg.sender, msg.value);
    }

    function withdraw(uint256 wad) external override {
        if (_balances[msg.sender] < wad) revert InsufficientBalance();
        _balances[msg.sender] -= wad;
        _totalSupply -= wad;
        emit Withdrawal(msg.sender, wad);
        // solhint-disable-next-line avoid-low-level-calls
        (bool ok, ) = payable(msg.sender).call{value: wad}("");
        require(ok, "MockWETH9: native transfer failed");
    }

    function approve(address spender, uint256 value) external override returns (bool) {
        _allowances[msg.sender][spender] = value;
        return true;
    }

    function transfer(address to, uint256 value) external override returns (bool) {
        return _transfer(msg.sender, to, value);
    }

    function transferFrom(address from, address to, uint256 value) external override returns (bool) {
        uint256 current = _allowances[from][msg.sender];
        if (current < value) revert InsufficientAllowance();
        if (current != type(uint256).max) {
            _allowances[from][msg.sender] = current - value;
        }
        return _transfer(from, to, value);
    }

    function _transfer(address from, address to, uint256 value) internal returns (bool) {
        if (_balances[from] < value) revert InsufficientBalance();
        _balances[from] -= value;
        _balances[to] += value;
        return true;
    }

    /// @dev Accept native to let tests simulate arbitrary funding paths.
    receive() external payable {
        _balances[msg.sender] += msg.value;
        _totalSupply += msg.value;
        emit Deposit(msg.sender, msg.value);
    }
}
