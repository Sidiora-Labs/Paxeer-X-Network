// SPDX-License-Identifier: LicenseRef-Paxlabs-HyperPax-OS-Protocol
pragma solidity ^0.8.24;

import "../base/Initializable.sol";
import "../base/ReentrancyGuard.sol";
import "../base/Pausable.sol";
import "../interfaces/ISidioraPool.sol";
import "../interfaces/IProtocolConfig.sol";
import "../interfaces/IFeeAccumulator.sol";
import "../interfaces/IEventEmitter.sol";
import "../libraries/ReserveLib.sol";
import "../libraries/FeeLib.sol";
import "../libraries/BitFlag.sol";
import "../libraries/TransferHelper.sol";
import "../libraries/SidioraMath.sol";
import "../interfaces/ISidioraFactory.sol";
import "../interfaces/IPoolRegistry.sol";
import "../base/ERC1967Utils.sol";

/// @title IOpticalMinimal
/// @dev Minimal interface for optical hooks used by pool
interface IOpticalMinimal {
    function beforeSwap(address pool, address sender, bool isBuy, uint256 amountIn)
        external returns (bool proceed, int256 amountDelta);
    function afterSwap(address pool, address sender, bool isBuy, uint256 amountIn, uint256 amountOut)
        external returns (bytes4);
    function getFlags() external view returns (uint8);
}

/// @title SidioraPool
/// @notice Core AMM engine. Constant product with virtual reserves and dynamic fees.
/// @dev Beacon proxy instances. Deliberately focused on AMM math + state transitions only.
///
///      FEE MODEL: Fees are always paid in the INPUT token.
///        BUY  (USDL → Token): fee in USDL → sent to FeeAccumulator for strategy distribution
///        SELL (Token → USDL): fee in Token → stays in pool, deepens token-side liquidity
///
///      VIRTUAL RESERVE FLOOR: virtualUsdlReserve (10,000 USDL) is pricing-only.
///        realUsdlBalance can never go below 0. The pool cannot pay out virtual USDL.
///        On sells, amountOut is bounded by realUsdlBalance (defense-in-depth).
contract SidioraPool is ISidioraPool, Initializable, ReentrancyGuard, Pausable {
    error VirtualFloorBreached();
    error Unauthorized();
    error InvalidConfiguration();
    error InvalidBalance();
    error InvalidHookResponse();
    event GuardianTransferStarted(address indexed guardian, address indexed pendingGuardian);
    event GuardianTransferred(address indexed previousGuardian, address indexed guardian);
    event InputAdjustment(uint256 paidInput, uint256 pricedInput);
    event FactoryMigrated(address indexed factory);


    address public override tokenAddress;
    address public usdlAddress;
    address public override opticalAddress;
    address public feeAccumulator;
    address public eventEmitter;
    address public protocolConfig;
    address public guardian;

    uint256 public virtualUsdlReserve;
    uint256 public realUsdlBalance;
    uint256 public tokenReserve;
    uint256 public override creationTimestamp;
    uint256 public override cumulativeVolume;

    uint256[8] public priceSnapshots;
    uint256 public snapshotIndex;
    uint256 public snapshotCount;

    uint256 public accumulatedUsdlFees;
    uint256 public accumulatedTokenFees;

    // Appended upgrade storage: existing fields retain their slots.
    address public factory;
    address public pendingGuardian;
    uint256 public lastSnapshotTimestamp;
    uint256 public cumulativeBuyUsdlVolume;
    uint256 public cumulativeSellUsdlVolume;
    uint256 public constant SNAPSHOT_INTERVAL = 60;
    uint256 public constant SNAPSHOT_MIN_VOLUME_BPS = 1;

    /// @custom:oz-upgrades-unsafe-allow constructor
    constructor() {
        _disableInitializers();
    }

    function initialize(
        address _tokenAddress,
        address _usdlAddress,
        address _opticalAddress,
        address _feeAccumulator,
        address _eventEmitter,
        address _protocolConfig,
        address _guardian,
        uint256 _virtualUsdlReserve,
        uint256 _tokenReserve
    ) external override initializer {
        if (_tokenAddress == address(0)) revert ZeroAddress();
        if (_usdlAddress == address(0)) revert ZeroAddress();

        if (_tokenAddress.code.length == 0 || _usdlAddress.code.length == 0 ||
            _feeAccumulator.code.length == 0 || _protocolConfig.code.length == 0 || _guardian == address(0) ||
            _virtualUsdlReserve == 0 || _tokenReserve == 0 ||
            _virtualUsdlReserve > type(uint128).max || _tokenReserve > type(uint128).max ||
            (_opticalAddress != address(0) && _opticalAddress.code.length == 0)) revert InvalidConfiguration();
        factory = msg.sender;
        tokenAddress = _tokenAddress;
        usdlAddress = _usdlAddress;
        opticalAddress = _opticalAddress;
        feeAccumulator = _feeAccumulator;
        eventEmitter = _eventEmitter;
        protocolConfig = _protocolConfig;
        guardian = _guardian;

        virtualUsdlReserve = _virtualUsdlReserve;
        tokenReserve = _tokenReserve;
        realUsdlBalance = 0;
        creationTimestamp = block.timestamp;

        _initReentrancyGuard();
    }

    /// @notice One-time router provenance migration for pools initialized before factory storage existed.
    /// @dev Only the protocol-config administrator can authorize migration. The candidate must
    ///      match configured dependencies and the registry must bind this pool to this token.
    function migrateFactory(address factory_) external {
        (bool ok, bytes memory data) = protocolConfig.staticcall(abi.encodeWithSignature(
            "hasRole(bytes32,address)", bytes32(0), msg.sender));
        if (!ok || data.length != 32 || !abi.decode(data, (bool))) revert Unauthorized();
        if (factory != address(0) || factory_.code.length == 0) revert InvalidConfiguration();
        ISidioraFactory candidate = ISidioraFactory(factory_);
        if (candidate.protocolConfig() != protocolConfig || candidate.eventEmitter() != eventEmitter ||
            candidate.poolBeacon() != ERC1967Utils.getBeacon()) revert InvalidConfiguration();
        (ok, data) = factory_.staticcall(abi.encodeWithSignature("feeAccumulator()"));
        if (!ok || data.length != 32 || abi.decode(data, (address)) != feeAccumulator) revert InvalidConfiguration();
        (ok, data) = factory_.staticcall(abi.encodeWithSignature("usdlAddress()"));
        if (!ok || data.length != 32 || abi.decode(data, (address)) != usdlAddress) revert InvalidConfiguration();
        IPoolRegistry registry = IPoolRegistry(candidate.poolRegistry());
        if (!registry.isRegisteredPool(address(this)) || registry.getPoolByToken(tokenAddress) != address(this)) revert InvalidConfiguration();
        factory = factory_;
        emit FactoryMigrated(factory_);
    }

    // ============ SWAP ============

    function swap(
        uint256 amountIn,
        uint256 minAmountOut,
        bool isBuy,
        address recipient,
        uint256 deadline
    ) external override nonReentrant whenNotPaused returns (uint256 amountOut) {
        return _swap(amountIn, minAmountOut, isBuy, msg.sender, recipient, deadline);
    }

    /// @notice Authenticated routers preserve trader identity independently of the output recipient.
    function swapFor(uint256 amountIn, uint256 minAmountOut, bool isBuy, address trader, address recipient, uint256 deadline)
        external override nonReentrant whenNotPaused returns (uint256)
    {
        (bool ok, bytes memory data) = factory.staticcall(abi.encodeWithSignature(
            "hasRole(bytes32,address)", keccak256("ROUTER_ROLE"), msg.sender));
        if (!ok || data.length != 32 || !abi.decode(data, (bool))) revert Unauthorized();
        if (trader == address(0)) revert ZeroAddress();
        return _swap(amountIn, minAmountOut, isBuy, trader, recipient, deadline);
    }

    function _swap(uint256 amountIn, uint256 minAmountOut, bool isBuy, address trader, address recipient, uint256 deadline)
        internal returns (uint256 amountOut)
    {
        if (block.timestamp > deadline) revert DeadlineExpired();
        if (amountIn == 0) revert InsufficientInput();
        if (recipient == address(0)) revert ZeroAddress();

        uint256 paidInput = amountIn;
        uint256 accounted = isBuy ? realUsdlBalance : tokenReserve;
        uint256 balance = _getBalance(isBuy ? usdlAddress : tokenAddress);
        if (balance < accounted || balance - accounted < paidInput) revert InsufficientInput();
        uint8 flags = opticalAddress == address(0) ? 0 : IOpticalMinimal(opticalAddress).getFlags();

        // Calculate dynamic fee
        uint256 feeBps = _calculateFee();

        // Optical: beforeSwap
        if (opticalAddress != address(0)) {
            if (BitFlag.hasFlag(flags, BitFlag.BEFORE_SWAP)) {
                (bool proceed, int256 amountDelta) =
                    IOpticalMinimal(opticalAddress).beforeSwap(address(this), trader, isBuy, amountIn);
                if (!proceed) revert InsufficientInput();
                if (amountDelta > 0) {
                    revert InsufficientInput(); // Hook cannot invent prepaid input.
                } else if (amountDelta < 0) {
                    uint256 reduction = SidioraMath.abs(amountDelta);
                    if (reduction >= amountIn) revert InsufficientInput();
                    amountIn -= reduction;
                }
            }
        }

        // Calculate fee in the INPUT token
        uint256 feeAmount = SidioraMath.mulDiv(amountIn, feeBps, 10000);
        uint256 amountInAfterFee = amountIn - feeAmount;

        uint256 effectiveUsdl = virtualUsdlReserve + realUsdlBalance;

        if (isBuy) {
            // ── BUY: USDL → Token ──
            // Fee is in USDL. Only amountInAfterFee enters reserves.
            // feeAmount USDL is sent to FeeAccumulator for strategy distribution.
            amountOut = ReserveLib.getAmountOut(effectiveUsdl, tokenReserve, amountInAfterFee);
            if (amountOut > tokenReserve) revert InsufficientLiquidity();

            realUsdlBalance += paidInput - feeAmount;
            tokenReserve -= amountOut;

            // Transfer tokens to buyer
            TransferHelper.safeTransfer(tokenAddress, recipient, amountOut);

            // Send USDL fee to FeeAccumulator
            if (feeAmount > 0 && feeAccumulator != address(0)) {
                accumulatedUsdlFees += feeAmount;
                TransferHelper.safeTransfer(usdlAddress, feeAccumulator, feeAmount);
                IFeeAccumulator(feeAccumulator).recordFee(address(this), feeAmount);
            }
        } else {
            // ── SELL: Token → USDL ──
            // Fee is in Token. Fee tokens stay in pool (deepen token-side liquidity).
            // Only amountInAfterFee enters AMM math for pricing.
            amountOut = ReserveLib.getAmountOut(tokenReserve, effectiveUsdl, amountInAfterFee);

            // VIRTUAL FLOOR: Cannot pay out virtual USDL. Only real USDL is withdrawable.
            if (amountOut > realUsdlBalance) revert VirtualFloorBreached();

            // Full amountIn added to tokenReserve (fee tokens deepen liquidity)
            tokenReserve += paidInput;
            realUsdlBalance -= amountOut;

            // Transfer USDL to seller
            TransferHelper.safeTransfer(usdlAddress, recipient, amountOut);

            // Track token fees (they stay in pool, no external transfer)
            if (feeAmount > 0) {
                accumulatedTokenFees += feeAmount;
            }
        }

        if (amountOut == 0) revert InsufficientInput();
        if (amountOut < minAmountOut) revert SlippageExceeded();

        // Update volume and price snapshots
        uint256 usdlVolume = isBuy ? paidInput : amountOut;
        cumulativeVolume += usdlVolume;
        if (isBuy) cumulativeBuyUsdlVolume += usdlVolume;
        else cumulativeSellUsdlVolume += usdlVolume;
        uint256 currentPrice = _currentPrice();
        _updatePriceSnapshot(currentPrice, usdlVolume);
        if (paidInput != amountIn) emit InputAdjustment(paidInput, amountIn);

        // Optical: afterSwap
        if (opticalAddress != address(0)) {
            if (BitFlag.hasFlag(flags, BitFlag.AFTER_SWAP)) {
                if (IOpticalMinimal(opticalAddress).afterSwap(address(this), trader, isBuy, amountIn, amountOut)
                    != IOpticalMinimal.afterSwap.selector) revert InvalidHookResponse();
            }
        }

        // External hooks and transfer callbacks must not remove booked backing.
        if (_getBalance(usdlAddress) < realUsdlBalance || _getBalance(tokenAddress) < tokenReserve) revert InvalidBalance();

        // Emit event
        if (eventEmitter != address(0)) {
            bytes32 poolId = bytes32(uint256(uint160(address(this))));
            IEventEmitter(eventEmitter).emitSwap(poolId, trader, isBuy, paidInput, amountOut, feeAmount, currentPrice);
        }
    }

    // ============ SYNC RESERVES ============

    function syncReserves() external override nonReentrant returns (uint256 usdlBal, uint256 tokenBal) {
        usdlBal = _getBalance(usdlAddress);
        tokenBal = _getBalance(tokenAddress);

        realUsdlBalance = usdlBal;
        tokenReserve = tokenBal;

        if (eventEmitter != address(0)) {
            bytes32 poolId = bytes32(uint256(uint160(address(this))));
            IEventEmitter(eventEmitter).emitPoolStateUpdated(
                poolId, virtualUsdlReserve, realUsdlBalance, tokenReserve, _currentPrice()
            );
        }
    }

    // ============ PAUSE ============

    function pause() external {
        if (msg.sender != guardian) revert Unauthorized();
        _pause();
    }

    function unpause() external {
        if (msg.sender != guardian) revert Unauthorized();
        _unpause();
    }

    /// @notice Guardian authority is independent of fee NFT ownership and transfers by consent.
    function transferGuardian(address nextGuardian) external {
        if (msg.sender != guardian) revert Unauthorized();
        if (nextGuardian == address(0)) revert ZeroAddress();
        pendingGuardian = nextGuardian;
        emit GuardianTransferStarted(guardian, nextGuardian);
    }

    function acceptGuardian() external {
        if (msg.sender != pendingGuardian) revert Unauthorized();
        address previous = guardian;
        guardian = msg.sender;
        pendingGuardian = address(0);
        emit GuardianTransferred(previous, msg.sender);
    }

    function paused() external view returns (bool) {
        return _paused();
    }

    // ============ VIEWS ============

    function getReserves() external view override returns (
        uint256 virtualUsdl,
        uint256 realUsdl,
        uint256 tokenRes
    ) {
        virtualUsdl = virtualUsdlReserve;
        realUsdl = realUsdlBalance;
        tokenRes = tokenReserve;
    }

    function getEffectiveReserves() external view override returns (
        uint256 effectiveUsdl,
        uint256 tokenRes
    ) {
        effectiveUsdl = virtualUsdlReserve + realUsdlBalance;
        tokenRes = tokenReserve;
    }

    function getPrice() external view override returns (uint256) {
        return _currentPrice();
    }

    function getPoolInfo() external view override returns (PoolInfo memory) {
        return PoolInfo({
            tokenAddress: tokenAddress,
            opticalAddress: opticalAddress,
            virtualUsdlReserve: virtualUsdlReserve,
            realUsdlBalance: realUsdlBalance,
            tokenReserve: tokenReserve,
            creationTimestamp: creationTimestamp,
            cumulativeVolume: cumulativeVolume
        });
    }

    function getPriceSnapshots() external view override returns (uint256[8] memory) {
        return _orderedSnapshots();
    }

    function _orderedSnapshots() internal view returns (uint256[8] memory) {
        uint256[8] memory ordered;
        uint256 start = snapshotCount == 8 ? snapshotIndex : 0;
        for (uint256 i; i < snapshotCount; ++i) ordered[i] = priceSnapshots[(start + i) % 8];
        return ordered;
    }

    // ============ INTERNAL ============

    function _calculateFee() internal view returns (uint256) {
        IProtocolConfig config = IProtocolConfig(protocolConfig);
        uint256 poolAge = block.timestamp - creationTimestamp;

        // Calculate volatility from snapshots
        uint256 volatility = FeeLib.calculateVolatility(_orderedSnapshots(), snapshotCount);

        // Concentration is explicitly disabled by ProtocolConfig until an authenticated holder metric exists.
        uint256 topHolderBps = 0;

        return FeeLib.calculateDynamicFee(
            config.baseFeeBps(),
            config.minFeeBps(),
            config.maxFeeBps(),
            config.feeDecayRate(),
            config.volatilityWeight(),
            config.concentrationWeight(),
            poolAge,
            volatility,
            topHolderBps
        );
    }

    function _currentPrice() internal view returns (uint256) {
        if (tokenReserve == 0) return 0;
        uint256 effectiveUsdl = virtualUsdlReserve + realUsdlBalance;
        return ReserveLib.getPrice(effectiveUsdl, tokenReserve);
    }

    function currentFeeBps() external view override returns (uint256) { return _calculateFee(); }

    function _updatePriceSnapshot(uint256 price, uint256 usdlVolume) internal {
        if (usdlVolume < SidioraMath.mulDiv(virtualUsdlReserve + realUsdlBalance, SNAPSHOT_MIN_VOLUME_BPS, 10000)) return;
        if (snapshotCount != 0 && block.timestamp < lastSnapshotTimestamp + SNAPSHOT_INTERVAL) return;
        lastSnapshotTimestamp = block.timestamp;
        priceSnapshots[snapshotIndex] = price;
        snapshotIndex = (snapshotIndex + 1) % 8;
        if (snapshotCount < 8) {
            snapshotCount++;
        }
    }

    function _getBalance(address token) internal view returns (uint256) {
        (bool success, bytes memory data) = token.staticcall(
            abi.encodeWithSignature("balanceOf(address)", address(this))
        );
        if (!success || data.length != 32 || token.code.length == 0) revert InvalidBalance();
        return abi.decode(data, (uint256));
    }
}
