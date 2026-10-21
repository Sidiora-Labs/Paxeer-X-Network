// SPDX-License-Identifier: LicenseRef-Paxlabs-HyperPax-OS-Protocol
pragma solidity ^0.8.27;

import {IPECORVault} from "../interfaces/IPECORVault.sol";
import {IERC20Minimal} from "../interfaces/IERC20Minimal.sol";
import {IWETH} from "../interfaces/IWETH.sol";
import {Initializable} from "../../base/Initializable.sol";
import {UUPSUpgradeable} from "../../base/UUPSUpgradeable.sol";
import {AccessControl} from "../../base/AccessControl.sol";
import {ReentrancyGuard} from "../../base/ReentrancyGuard.sol";
import {TransferHelper} from "../../libraries/TransferHelper.sol";

/// @title PECORVault (v2)
/// @notice Single multi-asset inventory vault for the Sidiora Meta-AG stack.
///         Holds every non-Sidiora liquidity token, operator-gated pull/push,
///         Timelock-admin upgrades. REPLACES live PECORVault
///         (`0x6500B1B3F8067772041C68b2c51D8E7A84e20C31`) via migration
///         Path M1 at Phase 10.3.
/// @dev Spec reference: `docs/architecture/pecor-sidiora-aggregator-spec.md` §7.5
///      (FROZEN 2026-04-24). Regression tests: `test/meta-ag/vault/PECORVault.test.js`.
///
/// Inheritance (spec §7.5 — Pausable deliberately excluded):
///   IPECORVault, Initializable, UUPSUpgradeable, AccessControl, ReentrancyGuard
///
/// Roles:
///   - DEFAULT_ADMIN_ROLE → Timelock at deploy (invariant S1)
///   - OPERATOR_ROLE      → PECOR, PECOROrders, VaultAdapter (granted later)
///
/// Storage layout (append-only per S12):
///   slot 0:  AccessControl._roles
///   slot 1:  weth                       (address)
///   slot 2:  transactionTracker         (address)
///   slot 3:  authorizedOperators        (mapping)
///   slot 4:  _tokens                    (mapping)
///   slot 5:  _registeredTokens          (address[])
///   slot 6:  _registeredStablecoins     (address[])
///   slot 7:  _tokenIndex                (mapping, 1-indexed)
///   slot 8:  _stablecoinIndex           (mapping, 1-indexed)
///   slot 9..58: __gap[50]
contract PECORVault is
    IPECORVault,
    Initializable,
    UUPSUpgradeable,
    AccessControl,
    ReentrancyGuard
{
    bytes32 public constant OPERATOR_ROLE = keccak256("OPERATOR_ROLE");

    error ZeroAddress();
    error ZeroAmount();
    error InvalidArrayLength();
    error TokenAlreadyRegistered();
    error TokenNotRegistered();
    error ReservesMismatch();
    error NativeTransferFailed();
    error WethOnly();
    error UnsupportedToken();
    error UnsupportedTransfer();
    error TokenQuarantined();
    error ReservedLiabilities();
    error DeprecatedBookMutation();
    error LiabilityMigrationRequired();
    error MigrationAlreadyComplete();

    event TokensReserved(address indexed operator, address indexed token, uint256 amount);
    event TokensReleased(address indexed operator, address indexed token, uint256 amount);
    event ReserveDeficitReconciled(address indexed token, uint256 oldReserve, uint256 newReserve);
    event TokenQuarantineUpdated(address indexed token, bool quarantined);
    event NativeSurplusRecovered(address indexed recipient, uint256 amount);

    /// @notice Canonical wrapped-native token. Semantically immutable —
    ///         assigned exactly once in {initialize}, never mutated afterwards.
    address public weth;

    /// @notice Transaction analytics hub (Phase 7 emitter). May be zero at
    ///         bootstrap; Timelock rotates via {setTransactionTracker}.
    address public transactionTracker;

    /// @notice Mirror of `OPERATOR_ROLE` membership. Kept for O(1) view by
    ///         integrations that prefer a boolean over `hasRole` staticcalls.
    mapping(address => bool) public authorizedOperators;

    mapping(address => TokenInfo) private _tokens;
    address[] private _registeredTokens;
    address[] private _registeredStablecoins;
    mapping(address => uint256) private _tokenIndex;
    mapping(address => uint256) private _stablecoinIndex;

    /// @dev Reserved storage for future upgrades (S12: append-only, 50 * 32 bytes).
    mapping(address => uint256) private _reservedBalance;
    mapping(address => mapping(address => uint256)) public operatorReservations;
    mapping(address => bool) public quarantined;
    bool public liabilitiesInitialized;
    uint256[46] private __gap;

    /// @custom:oz-upgrades-unsafe-allow constructor
    constructor() {
        _disableInitializers();
    }

    /// @inheritdoc IPECORVault
    function initialize(
        address weth_,
        address tracker_,
        address admin_
    ) external override initializer {
        if (weth_ == address(0)) revert ZeroAddress();
        if (weth_.code.length == 0) revert UnsupportedToken();
        if (admin_ == address(0)) revert ZeroAddress();

        _initReentrancyGuard();
        liabilitiesInitialized = true;
        weth = weth_;
        transactionTracker = tracker_;
        _grantRole(DEFAULT_ADMIN_ROLE, admin_);
    }

    function _authorizeUpgrade(address) internal override onlyRole(DEFAULT_ADMIN_ROLE) {}

    /// @inheritdoc IPECORVault
    function registerToken(
        address token,
        bool isStablecoin_
    ) external override onlyRole(DEFAULT_ADMIN_ROLE) {
        if (token == address(0)) revert ZeroAddress();
        TokenInfo storage info = _tokens[token];
        if (info.isRegistered) revert TokenAlreadyRegistered();

        uint8 decimals_ = _fetchDecimals(token);
        info.isRegistered = true;
        info.isStablecoin = isStablecoin_;
        info.decimals = decimals_;

        _registeredTokens.push(token);
        // Legacy _tokenIndex slot retained for storage compatibility; no unused writes.

        if (isStablecoin_) {
            _registeredStablecoins.push(token);
            _stablecoinIndex[token] = _registeredStablecoins.length;
        }

        emit TokenRegistered(token, decimals_, isStablecoin_);
    }

    /// @inheritdoc IPECORVault
    function setStablecoinStatus(
        address token,
        bool isStablecoin_
    ) external override onlyRole(DEFAULT_ADMIN_ROLE) {
        TokenInfo storage info = _tokens[token];
        if (!info.isRegistered) revert TokenNotRegistered();
        if (info.isStablecoin == isStablecoin_) {
            emit StablecoinStatusUpdated(token, isStablecoin_);
            return;
        }

        info.isStablecoin = isStablecoin_;
        if (isStablecoin_) {
            _registeredStablecoins.push(token);
            _stablecoinIndex[token] = _registeredStablecoins.length;
        } else {
            _removeStablecoin(token);
        }
        emit StablecoinStatusUpdated(token, isStablecoin_);
    }

    /// @inheritdoc IPECORVault
    function setOperator(
        address operator,
        bool authorized
    ) external override onlyRole(DEFAULT_ADMIN_ROLE) {
        if (operator == address(0)) revert ZeroAddress();
        authorizedOperators[operator] = authorized;
        if (authorized) {
            _grantRole(OPERATOR_ROLE, operator);
        } else {
            _revokeRole(OPERATOR_ROLE, operator);
        }
        emit OperatorUpdated(operator, authorized);
    }

    /// @inheritdoc IPECORVault
    function setTransactionTracker(
        address tracker
    ) external override onlyRole(DEFAULT_ADMIN_ROLE) {
        transactionTracker = tracker;
        emit TransactionTrackerUpdated(tracker);
    }

    /// @inheritdoc IPECORVault
    function emergencyWithdraw(
        address token,
        uint256 amount,
        address recipient
    ) external override onlyRole(DEFAULT_ADMIN_ROLE) nonReentrant {
        if (!liabilitiesInitialized) revert LiabilityMigrationRequired();
        if (recipient == address(0)) revert ZeroAddress();
        if (amount == 0) revert ZeroAmount();
        TokenInfo storage info = _tokens[token];
        if (!info.isRegistered) revert TokenNotRegistered();
        if (_spendable(token) < amount) revert ReservesMismatch();

        info.reserves -= amount;
        info.totalWithdrawn += amount;
        _transferExact(token, recipient, amount);

        emit EmergencyWithdraw(token, recipient, amount, info.reserves);
    }

    /// @inheritdoc IPECORVault
    function syncReserves(
        address token
    ) external override onlyRole(DEFAULT_ADMIN_ROLE) nonReentrant {
        _syncOne(token);
    }

    /// @inheritdoc IPECORVault
    function syncAllReserves()
        external
        override
        onlyRole(DEFAULT_ADMIN_ROLE)
        nonReentrant
    {
        uint256 n = _registeredTokens.length;
        for (uint256 i = 0; i < n; ++i) {
            _syncOne(_registeredTokens[i]);
        }
    }

    /// @inheritdoc IPECORVault
    /// @notice Donate inventory; no shares or depositor redemption entitlement is created.
    function deposit(address token, uint256 amount) external override nonReentrant {
        if (amount == 0) revert ZeroAmount();
        TokenInfo storage info = _tokens[token];
        if (!info.isRegistered) revert TokenNotRegistered();
        if (quarantined[token]) revert TokenQuarantined();

        uint256 received = _pullAndMeasure(token, msg.sender, amount);
        info.reserves += received;
        info.totalDeposited += received;
        emit Deposit(token, msg.sender, received, info.reserves);
    }

    /// @inheritdoc IPECORVault
    /// @notice Donate inventory for multiple assets; no depositor balances are created.
    function depositBatch(
        address[] calldata tokenList,
        uint256[] calldata amounts
    ) external override nonReentrant {
        uint256 n = tokenList.length;
        if (n == 0 || n != amounts.length) revert InvalidArrayLength();

        for (uint256 i = 0; i < n; ++i) {
            uint256 amount = amounts[i];
            if (amount == 0) revert ZeroAmount();
            address token = tokenList[i];
            TokenInfo storage info = _tokens[token];
            if (!info.isRegistered) revert TokenNotRegistered();
            if (quarantined[token]) revert TokenQuarantined();

            uint256 received = _pullAndMeasure(token, msg.sender, amount);
            info.reserves += received;
            info.totalDeposited += received;
            emit Deposit(token, msg.sender, received, info.reserves);
        }
    }

    /// @inheritdoc IPECORVault
    /// @notice Donate native inventory; no public withdrawal entitlement is created.
    function depositNative() external payable override nonReentrant {
        if (msg.value == 0) revert ZeroAmount();
        address weth_ = weth;
        TokenInfo storage info = _tokens[weth_];
        if (!info.isRegistered) revert TokenNotRegistered();

        if (quarantined[weth_]) revert TokenQuarantined();
        IWETH(weth_).deposit{value: msg.value}();
        info.reserves += msg.value;
        info.totalDeposited += msg.value;
        emit NativeDeposit(msg.sender, msg.value, info.reserves);
    }

    /// @inheritdoc IPECORVault
    function pullTokens(
        address token,
        address from,
        uint256 amount
    ) external override onlyRole(OPERATOR_ROLE) nonReentrant returns (uint256 actualAmount) {
        if (amount == 0) revert ZeroAmount();
        TokenInfo storage info = _tokens[token];
        if (!info.isRegistered) revert TokenNotRegistered();

        if (quarantined[token]) revert TokenQuarantined();
        actualAmount = _pullAndMeasure(token, from, amount);
        info.reserves += actualAmount;
        info.totalDeposited += actualAmount;
        emit Deposit(token, from, actualAmount, info.reserves);
    }

    /// @inheritdoc IPECORVault
    function pushTokens(
        address token,
        address to,
        uint256 amount
    ) external override onlyRole(OPERATOR_ROLE) nonReentrant {
        if (!liabilitiesInitialized) revert LiabilityMigrationRequired();
        if (to == address(0)) revert ZeroAddress();
        if (amount == 0) revert ZeroAmount();
        TokenInfo storage info = _tokens[token];
        if (!info.isRegistered) revert TokenNotRegistered();
        if (_spendable(token) < amount) revert ReservesMismatch();

        info.reserves -= amount;
        info.totalWithdrawn += amount;
        _transferExact(token, to, amount);
        emit Withdrawal(token, to, amount, info.reserves);
    }

    /// @inheritdoc IPECORVault
    function withdrawNative(
        uint256 amount,
        address to
    ) external override onlyRole(OPERATOR_ROLE) nonReentrant {
        if (!liabilitiesInitialized) revert LiabilityMigrationRequired();
        if (to == address(0)) revert ZeroAddress();
        if (amount == 0) revert ZeroAmount();
        address weth_ = weth;
        TokenInfo storage info = _tokens[weth_];
        if (!info.isRegistered) revert TokenNotRegistered();
        if (_spendable(weth_) < amount) revert ReservesMismatch();

        info.reserves -= amount;
        info.totalWithdrawn += amount;
        IWETH(weth_).withdraw(amount);
        // solhint-disable-next-line avoid-low-level-calls
        (bool ok, ) = to.call{value: amount}("");
        if (!ok) revert NativeTransferFailed();
        emit NativeWithdrawal(to, amount, info.reserves);
    }

    /// @inheritdoc IPECORVault
    function updateReserves(
        address tokenIn,
        uint256 amountIn,
        address tokenOut,
        uint256 amountOut
    ) external override onlyRole(OPERATOR_ROLE) nonReentrant {
        // Deprecated: pull/push already reconcile custody and counters. Book-only
        // updates cannot be authenticated and must never manufacture liquidity.
        tokenIn; amountIn; tokenOut; amountOut;
        revert DeprecatedBookMutation();
    }

    /// @inheritdoc IPECORVault
    function getReserves(address token) external view override returns (uint256) {
        return _tokens[token].reserves;
    }

    /// @inheritdoc IPECORVault
    /// @dev Named return parameters are omitted here on purpose: the interface
    ///      names one of them `isStablecoin`, but re-declaring that identifier
    ///      in this contract's scope would shadow the public view function
    ///      {isStablecoin(address)} and trip Solidity's shadow warning.
    ///      Callers should rely on positional tuple order — identical to the
    ///      interface declaration — e.g. via array destructuring.
    function getTokenInfo(
        address token
    )
        external
        view
        override
        returns (bool, bool, uint8, uint256, uint256, uint256)
    {
        TokenInfo storage info = _tokens[token];
        return (
            info.isRegistered,
            info.isStablecoin,
            info.decimals,
            info.reserves,
            info.totalDeposited,
            info.totalWithdrawn
        );
    }

    /// @inheritdoc IPECORVault
    function isStablecoin(address token) external view override returns (bool) {
        return _tokens[token].isStablecoin;
    }

    /// @inheritdoc IPECORVault
    function getTokenDecimals(address token) external view override returns (uint8) {
        return _tokens[token].decimals;
    }

    /// @inheritdoc IPECORVault
    function hasLiquidity(address token, uint256 amount) external view override returns (bool) {
        return liabilitiesInitialized && !quarantined[token] && _tokens[token].isRegistered && _spendable(token) >= amount;
    }

    /// @inheritdoc IPECORVault
    function getRegisteredTokens() external view override returns (address[] memory) {
        return _registeredTokens;
    }

    /// @inheritdoc IPECORVault
    function getRegisteredStablecoins() external view override returns (address[] memory) {
        return _registeredStablecoins;
    }

    /// @inheritdoc IPECORVault
    function getRegisteredTokenCount() external view override returns (uint256) {
        return _registeredTokens.length;
    }

    /// @inheritdoc IPECORVault
    function getAllReserves()
        external
        view
        override
        returns (address[] memory tokens, uint256[] memory reserves)
    {
        uint256 n = _registeredTokens.length;
        tokens = new address[](n);
        reserves = new uint256[](n);
        for (uint256 i = 0; i < n; ++i) {
            address t = _registeredTokens[i];
            tokens[i] = t;
            reserves[i] = _tokens[t].reserves;
        }
    }

    /// @inheritdoc IPECORVault
    function getUntrackedFunds(address token) external view override returns (uint256) {
        uint256 onBook = _tokens[token].reserves;
        uint256 balance = IERC20Minimal(token).balanceOf(address(this));
        return balance > onBook ? balance - onBook : 0;
    }

    /// @notice Accept native transfers only from the wrapped-native contract
    ///         during {withdrawNative} unwraps. Every other sender reverts
    ///         with {WethOnly}.
    receive() external payable {
        if (msg.sender != weth) revert WethOnly();
    }

    /// @dev Pull tokens via `transferFrom` and return the actually credited amount.
    ///      Handles fee-on-transfer tokens by comparing the vault's balance
    ///      delta across the call.
    function _pullAndMeasure(
        address token,
        address from,
        uint256 amount
    ) internal returns (uint256 received) {
        uint256 balanceBefore = IERC20Minimal(token).balanceOf(address(this));
        TransferHelper.safeTransferFrom(token, from, address(this), amount);
        uint256 balanceAfter = IERC20Minimal(token).balanceOf(address(this));
        received = balanceAfter - balanceBefore;
        if (received == 0) revert ZeroAmount();
    }

    /// @dev Reconcile a single registered token's reserves with the actual
    ///      balance held by the vault. Adds any positive delta to `reserves`
    ///      and `totalDeposited`. No-op on parity.
    function _syncOne(address token) internal {
        TokenInfo storage info = _tokens[token];
        if (!info.isRegistered) revert TokenNotRegistered();

        uint256 balance = IERC20Minimal(token).balanceOf(address(this));
        uint256 onBook = info.reserves;
        if (balance < onBook) {
            if (balance < _reservedBalance[token]) revert ReservedLiabilities();
            info.reserves = balance;
            emit ReserveDeficitReconciled(token, onBook, balance);
            return;
        }
        if (balance == onBook) return;

        uint256 delta = balance - onBook;
        info.reserves = balance;
        info.totalDeposited += delta;
        emit ReservesSync(token, onBook, balance, delta);
    }

    /// @dev Compact 1-indexed removal from `_registeredStablecoins`.
    function _removeStablecoin(address token) internal {
        uint256 index1 = _stablecoinIndex[token];
        if (index1 == 0) return;
        uint256 lastIndex = _registeredStablecoins.length - 1;
        uint256 idx = index1 - 1;
        if (idx != lastIndex) {
            address moved = _registeredStablecoins[lastIndex];
            _registeredStablecoins[idx] = moved;
            _stablecoinIndex[moved] = index1;
        }
        _registeredStablecoins.pop();
        _stablecoinIndex[token] = 0;
    }

    /// @dev Metadata must be explicit and safe for all supported price scales.
    function _fetchDecimals(address token) internal view returns (uint8) {
        if (token.code.length == 0) revert UnsupportedToken();
        (bool ok, bytes memory data) = token.staticcall(
            abi.encodeWithSelector(IERC20Minimal.decimals.selector)
        );
        if (!ok || data.length != 32) revert UnsupportedToken();
        uint256 value = abi.decode(data, (uint256));
        if (value > 36) revert UnsupportedToken();
        return uint8(value);
    }

    function reserveTokens(address token, uint256 amount) external override onlyRole(OPERATOR_ROLE) {
        if (amount == 0) revert ZeroAmount();
        if (quarantined[token]) revert TokenQuarantined();
        if (!_tokens[token].isRegistered) revert TokenNotRegistered();
        if (_spendable(token) < amount) revert ReservesMismatch();
        operatorReservations[msg.sender][token] += amount;
        _reservedBalance[token] += amount;
        emit TokensReserved(msg.sender, token, amount);
    }

    function releaseTokens(address token, uint256 amount) external override onlyRole(OPERATOR_ROLE) {
        if (!liabilitiesInitialized) revert LiabilityMigrationRequired();
        if (operatorReservations[msg.sender][token] < amount) revert ReservedLiabilities();
        if (IERC20Minimal(token).balanceOf(address(this)) < _reservedBalance[token]) revert ReservedLiabilities();
        operatorReservations[msg.sender][token] -= amount;
        _reservedBalance[token] -= amount;
        emit TokensReleased(msg.sender, token, amount);
    }

    function getAvailableReserves(address token) external view override returns (uint256) {
        if (!liabilitiesInitialized || quarantined[token]) return 0;
        return _spendable(token);
    }

    function getReservedBalance(address token) external view override returns (uint256) {
        return _reservedBalance[token];
    }

    /// @dev Conservative live custody check makes a deficit immediately visible to
    /// quotes, without granting an administrator authority to erase liabilities.
    function _spendable(address token) internal view returns (uint256) {
        uint256 custody;
        try IERC20Minimal(token).balanceOf(address(this)) returns (uint256 balance) {
            custody = balance;
        } catch { return 0; }
        uint256 book = _tokens[token].reserves;
        uint256 backed = custody < book ? custody : book;
        uint256 reserved = _reservedBalance[token];
        return backed > reserved ? backed - reserved : 0;
    }

    /// @dev Reject both sender surcharges and recipient taxes atomically. Input
    /// taxes are measured, but outbound delivery promises are exact.
    function _transferExact(address token, address to, uint256 amount) internal {
        if (to == address(this)) revert UnsupportedTransfer();
        uint256 beforeVault = IERC20Minimal(token).balanceOf(address(this));
        uint256 beforeRecipient = IERC20Minimal(token).balanceOf(to);
        TransferHelper.safeTransfer(token, to, amount);
        uint256 afterVault = IERC20Minimal(token).balanceOf(address(this));
        uint256 afterRecipient = IERC20Minimal(token).balanceOf(to);
        if (afterVault > beforeVault || beforeVault - afterVault != amount ||
            afterRecipient < beforeRecipient || afterRecipient - beforeRecipient != amount) {
            revert UnsupportedTransfer();
        }
    }

    function setTokenQuarantine(address token, bool value) external onlyRole(DEFAULT_ADMIN_ROLE) {
        if (!_tokens[token].isRegistered) revert TokenNotRegistered();
        quarantined[token] = value;
        emit TokenQuarantineUpdated(token, value);
    }

    /// @notice Bounded administrative reconciliation; a broken asset can be
    /// omitted from this subset without blocking healthy assets.
    function syncReservesSubset(address[] calldata tokens) external onlyRole(DEFAULT_ADMIN_ROLE) nonReentrant {
        if (tokens.length > 100) revert InvalidArrayLength();
        for (uint256 i; i < tokens.length; ++i) _syncOne(tokens[i]);
    }

    function getRegisteredTokensPage(uint256 cursor, uint256 count) external view returns (address[] memory page, uint256 next) {
        uint256 length = _registeredTokens.length;
        if (cursor > length) cursor = length;
        if (count > 100) count = 100;
        next = cursor + (count < length - cursor ? count : length - cursor);
        page = new address[](next - cursor);
        for (uint256 i; i < page.length; ++i) page[i] = _registeredTokens[cursor + i];
    }

    /// @notice Recover forced native value. Ordinary WETH unwraps and their
    /// transfer occur atomically, so no native liability is held between calls.
    function recoverNativeSurplus(address payable recipient) external onlyRole(DEFAULT_ADMIN_ROLE) nonReentrant {
        if (recipient == address(0)) revert ZeroAddress();
        uint256 amount = address(this).balance;
        if (amount == 0) revert ZeroAmount();
        (bool ok,) = recipient.call{value: amount}("");
        if (!ok) revert NativeTransferFailed();
        emit NativeSurplusRecovered(recipient, amount);
    }

    /// @notice Finalize only after every liability-bearing operator has migrated.
    /// Deployment tooling must supply the complete engine/Orders operator set;
    /// immutable custody cannot infer undisclosed liabilities from old mappings.
    function finalizeLiabilityMigration(address[] calldata liabilityOperators) external onlyRole(DEFAULT_ADMIN_ROLE) {
        if (liabilitiesInitialized) revert MigrationAlreadyComplete();
        for (uint256 i; i < liabilityOperators.length; ++i) {
            address operator = liabilityOperators[i];
            if (!hasRole(OPERATOR_ROLE, operator)) revert LiabilityMigrationRequired();
            (bool ok, bytes memory result) = operator.staticcall(abi.encodeWithSignature("liabilitiesReady()"));
            if (!ok || result.length != 32 || !abi.decode(result, (bool))) revert LiabilityMigrationRequired();
        }
        liabilitiesInitialized = true;
    }

    function _grantRole(bytes32 role, address account) internal override returns (bool changed) {
        changed = super._grantRole(role, account);
        if (role == OPERATOR_ROLE) authorizedOperators[account] = true;
    }

    function _revokeRole(bytes32 role, address account) internal override returns (bool changed) {
        changed = super._revokeRole(role, account);
        if (role == OPERATOR_ROLE) authorizedOperators[account] = false;
    }

}

/*
 * ══════════════════════════════════════════════════════════════════════════════════
 *
 *              Paxlabs HyperPax-OS-Protocol LICENSE (HyperPax-OS-Protocol)
 *                 Copyright © 2026 Paxlabs Inc. All rights reserved.
 *           
 *
 * ══════════════════════════════════════════════════════════════════════════════════
 *
 *   HyperPax-OS-Protocol License — Summary (non-binding): You may read, use, deploy, 
 *   and integrate HyperPax-OS-Protocol. If you Modify and distribute/deploy 
 *   the Modified version, you must release your changes under this same license.
 *   NO Commercial License is required until you cross a Commercial Trigger 
 *   (e.g., Charged Fees > US$100,000 in any rolling 12-month period or in any 
 *   single calendar month, or Liquidity Under Control > US$10,000,000).
 *   This summary is for convenience only.
 *
 * ───────────────────────────────────────────────────────────────────────────────────────────────
 *   1) DEFINITION
 * ───────────────────────────────────────────────────────────────────────────────────────────────
 *
 *   1.1  "Licensed Work" means the HyperPax-OS-Protocol stack as released in this repository,
 *        including: (a) the core HyperPax-OS-Protocol execution engine, instruction/handler
 *        interfaces, example instruction libraries published by Paxlabs, SDK stubs, schemas,
 *        configuration, tests, tooling, bytecode/ABIs, and deployment scripts; (b) all
 *        documentation and technical specifications published by Paxlabs; and (c) all updates,
 *        patches, and new versions of the foregoing that Paxlabs publishes under this license.
 *
 *   1.2  "Charged Fees" means all monetary or in-kind value (fiat, crypto, tokens, credits,
 *        rebates, or other consideration) that You or Your Affiliates directly or indirectly
 *        receive or accrue in connection with operating, offering, or providing access to any
 *        product or service that is powered by, routed through, or materially enabled by the
 *        Licensed Work, including without limitation: (a) swap/trade/execution fees, positive-
 *        slippage capture, spreads, mark-ups, retained priority/tips; (b) maker/taker rebates,
 *        order-flow payments, routing/referral/affiliate fees, MEV/builder/bundle payments and
 *        other extractable-value shares; (c) subscription, seat, usage, API, or platform fees
 *        attributable to the Licensed Work; (d) performance/incentive/carried-interest fees,
 *        revenue shares, or similar participation; and (e) token grants, rewards, airdrops,
 *        distributions, or rebates received by or for You or Your Affiliates in consideration
 *        of or tied to such operations. Charged Fees are measured on a gross-receipts basis at
 *        fair-market value in USD when received or accrued, include amounts received by
 *        agents/designees or wallets You control, and must be reasonably allocated for bundles.
 *        Anti-avoidance: relabeling, splitting, routing through Affiliates/Related Parties,
 *        offsetting, or deferring does not exclude amounts; Affiliates/common control are
 *        aggregated; the Control or Benefit Principle applies.
 *
 *   1.3  "Commercial License" means a separate written agreement between Paxlabs and You
 *        (and/or Your Affiliates), that grants You the right to engage in Commercial Use of
 *        the Licensed Work subject to negotiated terms, conditions, and fees.
 *
 *   1.4  "Control or Benefit Principle." Triggers and obligations apply where you or your
 *        Affiliates control the relevant activity or benefit economically from it (directly
 *        or through agents/DAOs under your direction).
 *
 *   1.5  "Rolling Year" means any period of twelve (12) consecutive months measured on a
 *        rolling basis.
 *
 *   1.6  "Liquidity Under Control (LUC)" means the aggregate fair-market USD value of real,
 *        non-synthetic, non-levered, withdrawable assets that Your (or Your Affiliates'/
 *        agents') products, services, or code can instruct or cause to be moved or committed
 *        via the Licensed Work (e.g., wallet balances under automated control, committed
 *        liquidity, or programmatic authorization) at the time assessed.
 *
 *   1.7  "Modify" (and "Modified Work") means to change, fork, translate, extend, or create a
 *        derivative work of the Licensed Work, including: (a) altering source or bytecode;
 *        (b) creating plug-ins/modules/instruction programs that run in the same program/
 *        runtime or EVM address space (e.g., static/dynamic linking, delegatecall/proxy
 *        patterns); or (c) bundling the Licensed Work and additions as a single product.
 *
 *   1.8  "You" (and "Your") means the individual or legal entity exercising rights under this
 *        license, and its Affiliates. "Affiliates" are entities controlling, controlled by, or
 *        under common control with a party, directly or indirectly.
 *
 * ───────────────────────────────────────────────────────────────────────────────────────────────
 *   2) GRANT
 * ───────────────────────────────────────────────────────────────────────────────────────────────
 *
 *   2.1  Source and Object Use. Subject to Sections 3–11, Paxlabs grants You a worldwide,
 *        non-exclusive license to use, copy, distribute unmodified source/object forms of the
 *        Licensed Work.
 *
 *   2.2  Pure Caller Use (Integration-Only / Non-Modifying). Pure Caller Use means building or
 *        operating products or services that interact with the Licensed Work solely by forming
 *        calldata, submitting transactions, or reading state through published ABIs, APIs, or
 *        RPC endpoints, without distributing any Modified Work. Pure Caller Use is permitted
 *        under this License and does not, by itself, trigger any payment obligations or
 *        Commercial License. However, if in connection with Pure Caller Use You or Your
 *        Affiliates (a) charge or retain any fees, spreads, rebates, incentives, or other
 *        consideration; (b) meet any Trigger in Section 5.2; such use constitutes Commercial
 *        Use and requires obtaining a Commercial License from Paxlabs. Even where Pure Caller
 *        Use is not met, see §5.3 for the current enforcement waiver applicable to Volume
 *        Activities.
 *
 *   2.3  Audit/Research Safe Harbor. Security auditors and researchers may compile, test, and
 *        report on the Licensed Work in the course of good-faith security research.
 *
 *   2.4  For any distribution, public display, public performance, publication, reporting,
 *        disclosure, or other public communication of any portion of the Licensed Work or any
 *        analysis, results, or outputs derived from the Licensed Work, You must preserve all
 *        existing copyright, license, and attribution notices included in the Licensed Work
 *        and must include a reasonable attribution identifying the source as
 *        "HyperPaxeer — © Paxlabs Inc 2026" (or any successor notice included in the
 *        Licensed Work).
 *
 * ───────────────────────────────────────────────────────────────────────────────────────────────
 *   3) COPYLEFT FOR MODIFICATIONS & EXTENSIONS
 * ───────────────────────────────────────────────────────────────────────────────────────────────
 *
 *   3.1  If you Modify or distribute any portion of the Licensed Work, you must:
 *        A. Publish under this same license (LicenseRef-Paxlabs-HyperPax-OS-Protocol license),
 *           at no charge, complete corresponding source of any portions of Your work that
 *           modify, extend, incorporate, or otherwise rely on the Licensed Work;
 *        B. Preserve existing copyright, license, and third-party notices;
 *        C. Add prominent attribution: "Powered by HyperPax-OS-Protocol — © Paxlabs Inc 2026"
 *           in repository README and UI where applicable;
 *        D. Clearly mark changes and date of change;
 *        E. Provide build and deployment instructions sufficient for reproducibility.
 *
 *   3.2  This copyleft covers all forms of Modification, combination, or use of the Licensed
 *        Work in or with other code, products, or systems.
 *
 *   3.3  The obligations in §§3.1 A-B apply only to components that are derivative of the
 *        Licensed Work. Independent code that simply calls, interfaces with, or is distributed
 *        alongside the Licensed Work is not subject to this requirement.
 *
 * ───────────────────────────────────────────────────────────────────────────────────────────────
 *   4) NON-COMMERCIAL FREE USE
 * ───────────────────────────────────────────────────────────────────────────────────────────────
 *
 *   Non-commercial use (including experimentation, prototyping, hackathons, research, community
 *   pilots) is free of charge, subject to Section 3 for any Modifications; and provided that
 *   such use does not constitute or involve any activity described in Section 5.
 *
 * ───────────────────────────────────────────────────────────────────────────────────────────────
 *   5) COMMERCIAL TRIGGER
 * ───────────────────────────────────────────────────────────────────────────────────────────────
 *
 *   5.1  Commercial Use: Any Commercial Use of the Licensed Work requires a Commercial License
 *        from Paxlabs, unless otherwise expressly stated. Commercial Use means any use of the
 *        Licensed Work that provides, enables, or is integrated into any product, service,
 *        system, workflow, or operation from which You or Your Affiliates derive, or reasonably
 *        expect to derive, monetary or in-kind commercial value, directly or indirectly,
 *        including through Charged Fees or other consideration.
 *
 *   5.2  Without limiting §5.1, You (and Your Affiliates) must obtain a Commercial License from
 *        Paxlabs if any of the following occur:
 *        A. Aggregated Fees Trigger: Your aggregated Charged Fees attributable to usage of the
 *           Licensed Work exceed USD 100,000 in any Rolling Year.
 *        B. LUC Trigger: Your LUC exceeds USD 10,000,000 at any time.
 *        C. Operator/Liquidity Provider Direct-Use. You (or Your Affiliate) operate instruction
 *           programs or services (e.g., deploying, offering, or running products or services
 *           powered by, routed through, or materially enabled by the Licensed Work) or acting
 *           as a Liquidity Provider that directly exercise the Licensed Work (bypassing Paxeer
 *           Network/Paxlabs or other permitted/licensed interfaces) to capture fees or value
 *           and, in doing so, satisfy Triggers A or B above.
 *        You must aggregate commonly-controlled/Affiliated entities; no disaggregation, white-
 *        labeling, or similar structuring to avoid a Trigger is permitted. The Control or
 *        Benefit Principle applies.
 *
 *   5.3  Notwithstanding §§ 5.1 - 5.2, Paxlabs presently waives enforcement of the Commercial
 *        Triggers for parties whose activities consist primarily of routing order flow,
 *        aggregation, arbitrage, or market-making through the Licensed Work ("Volume
 *        Activities"), including where such parties (i) trade with their own or third-party
 *        capital and/or (ii) charge or retain fees, spreads, rebates, or other compensation.
 *        This waiver is not a license, creates no reliance rights, and is revocable by Paxlabs
 *        at any time in its sole discretion, including with respect to existing users, by
 *        (a) public notice in the project repository or (b) direct notice. Upon notice of
 *        revocation, you must within ten (10) days cease the Volume Activities or obtain a
 *        Commercial License; continued use thereafter constitutes unauthorized Commercial Use.
 *        This waiver does not excuse past breaches unrelated to this subsection.
 *
 *   5.4  Crossing any Trigger or any other Commercial Use requires you to contact Paxlabs
 *        within 15 days at license@Paxlabs.com to execute Commercial License. Commercial
 *        License terms are confidential and may change.
 *
 * ───────────────────────────────────────────────────────────────────────────────────────────────
 *   6) AUDIT
 * ───────────────────────────────────────────────────────────────────────────────────────────────
 *
 *   Once per year, Paxlabs may request an independent revenue/LUC audit (under NDA) at
 *   Paxlabs's expense; if under-reporting exceeds 5%, You reimburse reasonable audit costs in
 *   addition to other remedies. Paxlabs may also request an additional attestation "for cause"
 *   (objective indications of a Trigger). You must reasonably cooperate with any such
 *   attestation, including providing accurate records, logs, and other information reasonably
 *   necessary.
 *
 * ───────────────────────────────────────────────────────────────────────────────────────────────
 *   7) ADDITIONAL INTELLECTUAL PROPERTY TERMS
 * ───────────────────────────────────────────────────────────────────────────────────────────────
 *
 *   7.1  Patents. Paxlabs grants a limited, non-exclusive, worldwide license under Paxlabs's
 *        patent claims that read on the Licensed Work solely to the extent necessary to
 *        exercise the rights expressly granted to You under this License. Nothing in this
 *        License grants You any right to patent, claim, or seek protection for (a) the
 *        Licensed Work (whether modified or unmodified), (b) any Modification of the Licensed
 *        Work, or (c) any work or system that incorporates, combines with, or depends on the
 *        Licensed Work. This patent license terminates if you (or your Affiliates) stop using
 *        the Licensed Work or assert any patent claim against Paxlabs or compliant users of
 *        the Licensed Work. No implied patent license is granted beyond this clause.
 *
 *   7.2  Trademarks & Branding. Trademarks. No rights are granted to use any Paxlabs/
 *        HyperPax-OS-Protocol/Paxeer Network names, logos, or trademarks, or any "Powered by
 *        HyperPaxeer" or similar designation, except solely to make truthful statements of
 *        compatibility or integration. Any use must comply with Paxlabs's brand guidelines and
 *        may require separate written permission.
 *
 *   7.3  Except as expressly granted, no other rights (by implication, estoppel, or otherwise)
 *        are granted, copyrights, patents, trade secrets, trademarks, or other IP.
 *
 *   7.4  You must not suggest Paxlabs endorses or certifies Your product absent a written
 *        agreement.
 *
 * ───────────────────────────────────────────────────────────────────────────────────────────────
 *   8) WARRANTY & LIABILITY
 * ───────────────────────────────────────────────────────────────────────────────────────────────
 *
 *   THE LICENSED WORK IS PROVIDED BY PAXLABS "AS IS" AND "AS AVAILABLE," WITHOUT WARRANTIES OF
 *   ANY KIND, EXPRESS OR IMPLIED, INCLUDING MERCHANTABILITY, FITNESS FOR A PARTICULAR PURPOSE,
 *   NON-INFRINGEMENT, AND THAT OPERATION WILL BE UNINTERRUPTED OR ERROR-FREE. TO THE MAXIMUM
 *   EXTENT PERMITTED BY LAW, PAXLABS, ITS AFFILIATES AND CONTRIBUTORS ARE NOT LIABLE FOR
 *   INDIRECT, INCIDENTAL, SPECIAL, CONSEQUENTIAL, EXEMPLARY, OR PUNITIVE DAMAGES, OR LOST
 *   PROFITS/REVENUE/GOODWILL, ARISING FROM OR RELATED TO THIS LICENSE OR THE LICENSED WORK,
 *   EVEN IF ADVISED OF THE POSSIBILITY.
 *
 *   Nothing in this Section limits Paxlabs's ability to seek injunctive relief without bond in
 *   addition to other remedies or to enforce Your obligations under this License.
 *
 * ───────────────────────────────────────────────────────────────────────────────────────────────
 *   9) TERMINATION
 * ───────────────────────────────────────────────────────────────────────────────────────────────
 *
 *   Material breach, including any breach of §§ 2-9, not cured within 15 days of notice
 *   terminates this License. Prior compliant distributions survive. Sections 2.4, 3, 8, 10,
 *   11.3 survive termination.
 *
 * ───────────────────────────────────────────────────────────────────────────────────────────────
 *   10) GOVERNING LAW; VENUE; INJUNCTIONS
 * ───────────────────────────────────────────────────────────────────────────────────────────────
 *
 *   This License is governed by the laws of the State of New York, excluding conflict rules.
 *   The parties submit to the exclusive jurisdiction and venue of the state and federal courts
 *   located in New York County, New York (SDNY). Each party consents to injunctive relief
 *   (including specific performance) for actual or threatened breach.
 *
 * ───────────────────────────────────────────────────────────────────────────────────────────────
 *   11) NOTICES; ASSIGNMENT; ENTIRE AGREEMENT
 * ───────────────────────────────────────────────────────────────────────────────────────────────
 *
 *   11.1 Notices. Legal or any other notices to Paxlabs: legal@Paxlabs.com (with subject
 *        "HyperPaxeer Notice").
 *
 *   11.2 Third-Party Components. Portions of the Licensed Work may incorporate, bundle, or
 *        reference third-party components governed by their own licenses. You must comply with
 *        those third-party terms; nothing in this License limits rights granted by those
 *        licenses. Preserve all third-party copyright and license notices. A list of such
 *        components and licenses is provided in THIRD_PARTY_NOTICES (and/or in file headers)
 *        and may be updated from time to time.
 *
 *   11.3 Assignment. You may not assign this License (by law or otherwise) without Paxlabs's
 *        prior written consent; any unauthorized assignment is void. Paxlabs may assign freely.
 *
 *   11.4 Entire Agreement. This License is the entire agreement for the Licensed Work and
 *        supersedes prior understandings. If any provision is unenforceable, it will be
 *        modified to the minimum extent necessary to be enforceable; the remainder stays in
 *        effect. No waiver is effective unless in writing.
 *
 * ───────────────────────────────────────────────────────────────────────────────────────────────
 *   12) VERSIONING
 * ───────────────────────────────────────────────────────────────────────────────────────────────
 *
 *   Paxlabs may publish new or updated versions of this License from time to time. Each
 *   release of the Licensed Work is governed by the license version identified in the
 *   repository for that release. Paxlabs may also re-release the Licensed Work, or any
 *   portion of it, under different license terms in future releases.
 *
 * ═══════════════════════════════════════════════════════════════════════════════════════════════
 *   END OF LICENSE — Contact: license@Paxlabs.com  |  legal@Paxlabs.com
 * ═══════════════════════════════════════════════════════════════════════════════════════════════
 */
