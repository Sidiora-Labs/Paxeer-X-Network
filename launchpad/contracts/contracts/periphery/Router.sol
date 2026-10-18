// SPDX-License-Identifier: LicenseRef-Paxlabs-HyperPax-OS-Protocol
pragma solidity ^0.8.24;

import "../base/Initializable.sol";
import "../base/UUPSUpgradeable.sol";
import "../base/AccessControl.sol";
import "../base/ReentrancyGuard.sol";
import "../base/Multicall.sol";
import "../interfaces/IRouter.sol";
import "../interfaces/ISidioraPool.sol";
import "../interfaces/ISidioraFactory.sol";
import "../interfaces/IPoolRegistry.sol";
import "../interfaces/IProtocolConfig.sol";
import "../libraries/TransferHelper.sol";
import "../interfaces/ITreasury.sol";
import "../interfaces/IEventEmitter.sol";

/// @title Router
/// @notice User-facing entry point for ALL protocol interactions.
/// @dev UUPS proxy. Handles validation, token transfers, and delegates to core contracts.
///      Inherits Multicall for batching (e.g., create + buy in one tx).
///      Supports EIP-2612 permit for gasless approvals and multihop Token→USDL→Token swaps.
contract Router is IRouter, Initializable, UUPSUpgradeable, AccessControl, ReentrancyGuard, Multicall {
    address public factory;
    address public poolRegistry;
    address public protocolConfig;
    address public usdlAddress;

    /// @custom:oz-upgrades-unsafe-allow constructor
    constructor() {
        _disableInitializers();
    }

    function initialize(
        address _factory,
        address _poolRegistry,
        address _protocolConfig,
        address _usdlAddress,
        address _admin
    ) external initializer {
        if (_admin == address(0)) revert ZeroAddress();
        if (_factory == address(0)) revert ZeroAddress();
        if (_poolRegistry == address(0)) revert ZeroAddress();
        if (_usdlAddress.code.length == 0 || _protocolConfig.code.length == 0 || _factory.code.length == 0 || _poolRegistry.code.length == 0) revert ZeroAddress();

        factory = _factory;
        poolRegistry = _poolRegistry;
        protocolConfig = _protocolConfig;
        usdlAddress = _usdlAddress;

        _grantRole(DEFAULT_ADMIN_ROLE, _admin);
        _initReentrancyGuard();
    }

    // ============ CORE ============

    /// @inheritdoc IRouter
    function createMarket(
        string calldata name,
        string calldata symbol,
        uint8 feeStrategy,
        address optical
    ) external override nonReentrant returns (address tokenAddr, address poolAddr, uint256 nftId) {
        return _createMarket(name, symbol, feeStrategy, optical);
    }

    /// @inheritdoc IRouter
    function buy(
        address pool,
        uint256 usdlAmountIn,
        uint256 minTokensOut,
        uint256 deadline
    ) external override nonReentrant returns (uint256 amountOut) {
        return _buy(pool, usdlAmountIn, minTokensOut, deadline);
    }

    /// @inheritdoc IRouter
    function sell(
        address pool,
        uint256 tokenAmountIn,
        uint256 minUsdlOut,
        uint256 deadline
    ) external override nonReentrant returns (uint256 amountOut) {
        return _sell(pool, tokenAmountIn, minUsdlOut, deadline);
    }

    // ============ MULTIHOP ============

    /// @inheritdoc IRouter
    function swapTokenForToken(
        address tokenIn,
        address tokenOut,
        uint256 amountIn,
        uint256 minAmountOut,
        uint256 deadline
    ) external override nonReentrant returns (uint256 amountOut, uint256 intermediateUsdl) {
        return _swapTokenForToken(tokenIn, tokenOut, amountIn, minAmountOut, deadline);
    }

    // ============ PERMIT VARIANTS ============

    /// @inheritdoc IRouter
    function buyWithPermit(
        address pool,
        uint256 usdlAmountIn,
        uint256 minTokensOut,
        uint256 deadline,
        PermitParams calldata permit
    ) external override nonReentrant returns (uint256 amountOut) {
        _executePermit(usdlAddress, msg.sender, address(this), permit);
        return _buy(pool, usdlAmountIn, minTokensOut, deadline);
    }

    /// @inheritdoc IRouter
    function sellWithPermit(
        address pool,
        uint256 tokenAmountIn,
        uint256 minUsdlOut,
        uint256 deadline,
        PermitParams calldata permit
    ) external override nonReentrant returns (uint256 amountOut) {
        if (!_isRegisteredPool(pool)) revert PoolNotFound();
        address tokenAddr = ISidioraPool(pool).tokenAddress();
        _executePermit(tokenAddr, msg.sender, address(this), permit);
        return _sell(pool, tokenAmountIn, minUsdlOut, deadline);
    }

    /// @inheritdoc IRouter
    function swapTokenForTokenWithPermit(
        address tokenIn,
        address tokenOut,
        uint256 amountIn,
        uint256 minAmountOut,
        uint256 deadline,
        PermitParams calldata permit
    ) external override nonReentrant returns (uint256 amountOut, uint256 intermediateUsdl) {
        _executePermit(tokenIn, msg.sender, address(this), permit);
        return _swapTokenForToken(tokenIn, tokenOut, amountIn, minAmountOut, deadline);
    }

    /// @inheritdoc IRouter
    function createMarketWithPermit(
        string calldata name,
        string calldata symbol,
        uint8 feeStrategy,
        address optical,
        PermitParams calldata permit
    ) external override nonReentrant returns (address tokenAddr, address poolAddr, uint256 nftId) {
        _executePermit(usdlAddress, msg.sender, address(this), permit);
        return _createMarket(name, symbol, feeStrategy, optical);
    }

    // ============ INTERNAL: CORE LOGIC ============

    function _createMarket(
        string calldata name,
        string calldata symbol,
        uint8 feeStrategy,
        address optical
    ) internal returns (address tokenAddr, address poolAddr, uint256 nftId) {
        // Transfer creation fee from user directly to treasury
        uint256 creationFee = IProtocolConfig(protocolConfig).creationFee();
        if (creationFee > 0) {
            address treasuryAddr = ISidioraFactory(factory).treasury();
            TransferHelper.safeTransferFrom(usdlAddress, msg.sender, treasuryAddr, creationFee);
            ITreasury(treasuryAddr).recordDeposit(usdlAddress, creationFee);
        }

        // Delegate to factory with explicit creator
        (tokenAddr, poolAddr, nftId) = ISidioraFactory(factory).createMarketFor(
            msg.sender, name, symbol, feeStrategy, optical
        );

        emit MarketCreated(tokenAddr, poolAddr, msg.sender, nftId);
    }

    function _buy(
        address pool,
        uint256 usdlAmountIn,
        uint256 minTokensOut,
        uint256 deadline
    ) internal returns (uint256 amountOut) {
        if (block.timestamp > deadline) revert DeadlineExpired();
        if (usdlAmountIn == 0) revert ZeroAmount();
        if (pool == address(0)) revert ZeroAddress();
        if (!_isRegisteredPool(pool)) revert PoolNotFound();

        TransferHelper.safeTransferFrom(usdlAddress, msg.sender, pool, usdlAmountIn);

        amountOut = ISidioraPool(pool).swapFor(
            usdlAmountIn,
            minTokensOut,
            true,
            msg.sender,
            msg.sender,
            deadline
        );

        emit Buy(pool, msg.sender, usdlAmountIn, amountOut);
        _mirrorTrade(0, pool, usdlAddress, ISidioraPool(pool).tokenAddress(), usdlAmountIn, amountOut, 0);
    }

    function _sell(
        address pool,
        uint256 tokenAmountIn,
        uint256 minUsdlOut,
        uint256 deadline
    ) internal returns (uint256 amountOut) {
        if (block.timestamp > deadline) revert DeadlineExpired();
        if (tokenAmountIn == 0) revert ZeroAmount();
        if (pool == address(0)) revert ZeroAddress();
        if (!_isRegisteredPool(pool)) revert PoolNotFound();

        address tokenAddr = ISidioraPool(pool).tokenAddress();
        TransferHelper.safeTransferFrom(tokenAddr, msg.sender, pool, tokenAmountIn);

        amountOut = ISidioraPool(pool).swapFor(
            tokenAmountIn,
            minUsdlOut,
            false,
            msg.sender,
            msg.sender,
            deadline
        );

        emit Sell(pool, msg.sender, tokenAmountIn, amountOut);
        _mirrorTrade(1, pool, tokenAddr, usdlAddress, tokenAmountIn, amountOut, 0);
    }

    /// @dev Executes Token A → USDL → Token B atomically.
    ///      Leg 1: Sell tokenIn on poolA, Router receives USDL.
    ///      Leg 2: Buy tokenOut on poolB with intermediate USDL, user receives tokenOut.
    function _swapTokenForToken(
        address tokenIn,
        address tokenOut,
        uint256 amountIn,
        uint256 minAmountOut,
        uint256 deadline
    ) internal returns (uint256 amountOut, uint256 intermediateUsdl) {
        if (block.timestamp > deadline) revert DeadlineExpired();
        if (amountIn == 0) revert ZeroAmount();
        if (tokenIn == tokenOut) revert SameToken();
        if (tokenIn == address(0) || tokenOut == address(0)) revert ZeroAddress();

        // Resolve pools via PoolRegistry
        address poolA = IPoolRegistry(poolRegistry).getPoolByToken(tokenIn);
        address poolB = IPoolRegistry(poolRegistry).getPoolByToken(tokenOut);
        if (poolA == address(0) || poolB == address(0)) revert PoolNotFound();

        // ── LEG 1: Sell tokenIn → USDL (Router is the recipient) ──
        TransferHelper.safeTransferFrom(tokenIn, msg.sender, poolA, amountIn);

        intermediateUsdl = ISidioraPool(poolA).swapFor(
            amountIn,
            0, // No intermediate slippage — end-to-end check on leg 2
            false, // isSell
            msg.sender, // Actual trader
            address(this), // Router receives USDL
            deadline
        );

        // ── LEG 2: Buy tokenOut with USDL (user is the recipient) ──
        TransferHelper.safeTransfer(usdlAddress, poolB, intermediateUsdl);

        amountOut = ISidioraPool(poolB).swapFor(
            intermediateUsdl,
            minAmountOut, // End-to-end slippage protection
            true, // isBuy
            msg.sender, // Actual trader
            msg.sender, // User receives tokenOut
            deadline
        );

        emit MultihopSwap(msg.sender, tokenIn, tokenOut, amountIn, intermediateUsdl, amountOut);
        _mirrorTrade(2, poolA, tokenIn, tokenOut, amountIn, amountOut, intermediateUsdl);
    }

    // ============ INTERNAL: HELPERS ============

    function _mirrorTrade(uint8 kind, address pool, address tokenIn, address tokenOut, uint256 amountIn, uint256 amountOut, uint256 intermediate) internal {
        address emitter = ISidioraFactory(factory).eventEmitter();
        if (emitter != address(0)) IEventEmitter(emitter).emitRouterTrade(kind, pool, msg.sender, tokenIn, tokenOut, amountIn, amountOut, intermediate);
    }

    function _isRegisteredPool(address pool) internal view returns (bool) {
        return IPoolRegistry(poolRegistry).isRegisteredPool(pool);
    }

    /// @dev Calls EIP-2612 permit on a token. Fails silently if permit reverts
    ///      (e.g., nonce already used, token doesn't support permit) to avoid
    ///      griefing via front-run permit.
    function _executePermit(
        address token,
        address owner_,
        address spender,
        PermitParams calldata permit
    ) internal {
        (bool success, ) = token.call(
            abi.encodeWithSignature(
                "permit(address,address,uint256,uint256,uint8,bytes32,bytes32)",
                owner_,
                spender,
                permit.value,
                permit.deadline,
                permit.v,
                permit.r,
                permit.s
            )
        );
        // Intentionally not reverting on failure — permit may have been
        // front-run or already executed, and the subsequent transferFrom
        // will revert if allowance is insufficient.
        (success); // Silence unused variable warning
    }

    function _authorizeUpgrade(address) internal override onlyRole(DEFAULT_ADMIN_ROLE) {}
}
