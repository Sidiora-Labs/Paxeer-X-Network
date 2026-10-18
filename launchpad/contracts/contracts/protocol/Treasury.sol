// SPDX-License-Identifier: LicenseRef-Paxlabs-HyperPax-OS-Protocol
pragma solidity ^0.8.24;

import "../base/Initializable.sol";
import "../base/UUPSUpgradeable.sol";
import "../base/AccessControl.sol";
import "../interfaces/ITreasury.sol";
import "../libraries/TransferHelper.sol";

/// @title Treasury
/// @notice Receives and holds protocol fee revenue. Disbursements via governance.
/// @dev UUPS proxy. Only DEPOSITOR_ROLE can deposit. Only admin can withdraw.
contract Treasury is ITreasury, Initializable, UUPSUpgradeable, AccessControl {
    bytes32 public constant DEPOSITOR_ROLE = keccak256("DEPOSITOR_ROLE");

    mapping(address => uint256) private _balances;

    address public eventEmitter;
    bytes32 private constant REENTRANCY_SLOT = keccak256("hyperpax.treasury.reentrancy");
    error InvalidToken();
    error ReentrantCall();
    event SurplusRecovered(address indexed token, address indexed to, uint256 amount);
    modifier nonReentrant() {
        bytes32 slot = REENTRANCY_SLOT;
        uint256 entered;
        assembly { entered := sload(slot) }
        if (entered != 0) revert ReentrantCall();
        assembly { sstore(slot, 1) }
        _;
        assembly { sstore(slot, 0) }
    }

    /// @custom:oz-upgrades-unsafe-allow constructor
    constructor() {
        _disableInitializers();
    }

    function initialize(address _eventEmitter, address _admin) external initializer {
        if (_admin == address(0)) revert ZeroAddress();
        eventEmitter = _eventEmitter;
        _grantRole(DEFAULT_ADMIN_ROLE, _admin);
    }

    /// @notice Deposit tokens into the treasury
    /// @dev Caller must have DEPOSITOR_ROLE. Tokens must be pre-approved.
    function deposit(address token, uint256 amount) external override onlyRole(DEPOSITOR_ROLE) nonReentrant {
        if (amount == 0) revert ZeroAmount();
        uint256 beforeBalance = _actualBalance(token);
        TransferHelper.safeTransferFrom(token, msg.sender, address(this), amount);
        uint256 received = _actualBalance(token) - beforeBalance;
        if (received == 0) revert ZeroAmount();
        _balances[token] += received;
        emit Deposited(token, msg.sender, received);
    }

    /// @notice Withdraw tokens from the treasury
    /// @dev Only admin (governance/timelock) can withdraw.
    function withdraw(
        address token,
        address to,
        uint256 amount
    ) external override onlyRole(DEFAULT_ADMIN_ROLE) nonReentrant {
        if (to == address(0)) revert ZeroAddress();
        if (amount == 0) revert ZeroAmount();
        if (_balances[token] < amount) revert InsufficientBalance();

        _balances[token] -= amount;
        TransferHelper.safeTransfer(token, to, amount);
        emit Withdrawn(token, to, amount);
    }

    /// @notice Returns the treasury balance for a given token
    function getBalance(address token) external view override returns (uint256) {
        return _balances[token];
    }

    /// @notice Account a prior transfer by an authorized protocol fee producer.
    /// @dev The ledger may never exceed the token balance. Call atomically after transferring.
    function recordDeposit(address token, uint256 amount) external override onlyRole(DEPOSITOR_ROLE) nonReentrant {
        if (amount == 0) revert ZeroAmount();
        uint256 actual = _actualBalance(token);
        if (actual < _balances[token] || amount > actual - _balances[token]) revert InsufficientBalance();
        _balances[token] += amount;
        emit Deposited(token, msg.sender, amount);
    }

    function recoverSurplus(address token, address to, uint256 amount) external onlyRole(DEFAULT_ADMIN_ROLE) nonReentrant {
        if (to == address(0)) revert ZeroAddress();
        if (amount == 0) revert ZeroAmount();
        uint256 actual = _actualBalance(token);
        if (actual < _balances[token] || amount > actual - _balances[token]) revert InsufficientBalance();
        TransferHelper.safeTransfer(token, to, amount);
        emit SurplusRecovered(token, to, amount);
    }

    function _actualBalance(address token) private view returns (uint256) {
        if (token.code.length == 0) revert InvalidToken();
        (bool ok, bytes memory data) = token.staticcall(abi.encodeWithSignature("balanceOf(address)", address(this)));
        if (!ok || data.length != 32) revert InvalidToken();
        return abi.decode(data, (uint256));
    }

    function _authorizeUpgrade(address) internal override onlyRole(DEFAULT_ADMIN_ROLE) {}
}
