// SPDX-License-Identifier: LicenseRef-Paxlabs-HyperPax-OS-Protocol
pragma solidity ^0.8.24;

import "../base/Initializable.sol";
import "../base/UUPSUpgradeable.sol";
import "../base/AccessControl.sol";
import "../interfaces/IEventEmitter.sol";
import "../interfaces/IPoolRegistry.sol";

/// @notice Minimal interface for the optional optical registry auth path.
interface IOpticalRegistryMinimal {
    function isApproved(address optical) external view returns (bool);
}

/// @notice Minimal interface for the optional Meta-AG router auth path.
///         Adapter authorization is keyed by `bytes32 adapterId` rather than
///         address; v2 keeps the lookup as `isAdapterActive` for parity with
///         the existing MetaAGRouter surface, expecting the router to have
///         been registered via `setAuthorizedEmitter` directly.
interface IMetaAGRouterMinimal {
    function isAdapterActive(bytes32 adapterId) external view returns (bool);
}

/// @title EventEmitter v2
/// @notice Optional protocol event mirror. Native source logs remain authoritative across
///         Sidiora Launchpad, Meta-AG/PECOR, opticals, oracle, treasury,
///         governance, and base-layer lifecycle (roles, upgrades, pause).
/// @dev    UUPS-upgradeable. Storage layout is **append-only** vs v1.
///         Typed authorization is selector scoped; generic logs include caller provenance.
///         v1 functions and events retain their ABI — same selectors,
///         same topic0 — for backward compatibility with existing call sites
///         in `core/SidioraFactory`, `core/SidioraPool`, `protocol/ProtocolConfig`.
///
///         v2 additions (per `contracts/data/EVENT-EMITTER-V2-PLAN.md`):
///           - Generic schemaless `EventLog{,1,2}` with indexed eventNameHash.
///           - ~30 typed fast-path emitters spanning every protocol domain.
///           - `EVENT_EMITTER_ROLE` separate from `DEFAULT_ADMIN_ROLE`.
///           - `VERSION()` accessor returning `"2.0.0"`.
///           - 5-path authorization mesh:
///               1) static `_authorizedEmitters` mapping
///               2) dynamic `IPoolRegistry.isRegisteredPool`        (Launchpad pools)
///               3) dynamic `IOpticalRegistry.isApproved`           (opticals)
///               4) `sidioraFactory == msg.sender`                  (factory itself)
///               5) `_registeredTokens[msg.sender]`                 (pool tokens)
///
///         Reference: GMX V2 EventEmitter pattern, Synthetix V3 EventLog schema.
contract EventEmitter is IEventEmitter, Initializable, UUPSUpgradeable, AccessControl {

    // ════════════════════════════════════════════════════════════════════════
    //                                Constants
    // ════════════════════════════════════════════════════════════════════════

    /// @notice v1 — retained for ABI compatibility. Not used as a gate in v2.
    bytes32 public constant EMITTER_ADMIN_ROLE = keccak256("EMITTER_ADMIN_ROLE");

    /// @notice v2 — distinct from DEFAULT_ADMIN_ROLE. Holds permission to
    ///         authorize emitters and wire registries. Does NOT grant
    ///         upgrade authority (DEFAULT_ADMIN_ROLE retains that).
    bytes32 public constant override EVENT_EMITTER_ROLE = keccak256("EVENT_EMITTER_ROLE");

    /// @notice v2 implementation semantic version.
    string private constant _VERSION = "2.0.0";

    // ════════════════════════════════════════════════════════════════════════
    //                                Storage
    //
    // Layout is APPEND-ONLY relative to v1. Existing slots are immutable.
    // Slot 0 is consumed by AccessControl._roles via inheritance.
    // ════════════════════════════════════════════════════════════════════════

    // --- v1 (do not reorder, do not remove) -------------------------------
    /// @dev slot 1 (after AccessControl._roles at slot 0).
    mapping(address => bool) private _authorizedEmitters;
    /// @dev slot 2.
    address public poolRegistry;

    // --- v2 (appended) ----------------------------------------------------
    /// @dev slot 3.
    address public opticalRegistry;
    /// @dev slot 4.
    address public metaAGRouter;
    /// @dev slot 5.
    address public sidioraFactory;
    /// @dev slot 6 — local pool-token registration. SidioraFactory pushes
    ///      every `SidioraERC20` it deploys here so token Transfer mirrors
    ///      can pass the auth gate without per-token admin tx.
    mapping(address => bool) private _registeredTokens;
    /// @dev slot 7 — guards `reinitializeV2` against repeat invocation.
    bool private _v2Initialized;

    /// @dev Storage gap reserved for v3+ append-only growth (50 slots).
    mapping(bytes4 => mapping(address => bool)) private _typedEmitters;
    uint256[49] private __gap;

    // ════════════════════════════════════════════════════════════════════════
    //                              Initializers
    // ════════════════════════════════════════════════════════════════════════

    /// @custom:oz-upgrades-unsafe-allow constructor
    constructor() {
        _disableInitializers();
    }

    /// @notice v1 initializer — preserved for fresh-deploy compatibility.
    ///         New deploys SHOULD call this once, then `reinitializeV2`.
    function initialize(address _admin) external initializer {
        if (_admin == address(0)) revert Unauthorized();
        _grantRole(DEFAULT_ADMIN_ROLE, _admin);
    }

    /// @notice v1 → v2 storage migration. Idempotent.
    /// @param adminWithEmitterRole Address granted EVENT_EMITTER_ROLE.
    ///        Pass `address(0)` to skip role grant (admin can do it later
    ///        via `grantRole(EVENT_EMITTER_ROLE, ...)`).
    /// @dev Callable only by DEFAULT_ADMIN_ROLE. Safe to call once on the
    ///      existing deployed proxy `0x6679aF411d534de222C32ed0AF94C3BD67090672`
    ///      after the impl swap.
    function reinitializeV2(address adminWithEmitterRole)
        external
        override
        onlyRole(DEFAULT_ADMIN_ROLE)
    {
        if (_v2Initialized) revert Unauthorized();
        _v2Initialized = true;

        // Set EVENT_EMITTER_ROLE admin to DEFAULT_ADMIN_ROLE (default behavior
        // already, but explicit for clarity and audit).
        _setRoleAdmin(EVENT_EMITTER_ROLE, DEFAULT_ADMIN_ROLE);

        if (adminWithEmitterRole != address(0)) {
            _grantRole(EVENT_EMITTER_ROLE, adminWithEmitterRole);
        }
    }

    function _authorizeUpgrade(address) internal override onlyRole(DEFAULT_ADMIN_ROLE) {}

    // ════════════════════════════════════════════════════════════════════════
    //                          Authorization mesh
    // ════════════════════════════════════════════════════════════════════════

    modifier onlyAuthorized() {
        if (!_isAuthorized(msg.sender)) revert Unauthorized();
        _;
    }

    /// @notice Public auth-check for off-chain probes / dependent contracts.
    function isAuthorizedEmitter(address emitter)
        external
        view
        override
        returns (bool)
    {
        return _isAuthorized(emitter);
    }

    function isRegisteredToken(address token)
        external
        view
        override
        returns (bool)
    {
        return _registeredTokens[token];
    }

    function _isAuthorized(address sender) internal view returns (bool) {
        // 1. Static whitelist (the canonical fast path).
        if (_authorizedEmitters[sender]) return true;
        if (sender == poolRegistry && sender != address(0)) return true;

        // 2. Launchpad pool dynamic auth (v1 path, retained).
        address pr = poolRegistry;
        if (pr != address(0)) {
            // Defensive try/catch — never trust a misconfigured registry to
            // brick the entire emitter surface.
            try IPoolRegistry(pr).isRegisteredPool(sender) returns (bool ok) {
                if (ok) return true;
            } catch {
                // fall through
            }
        }

        // 3. Optical registry dynamic auth (v2).
        address or_ = opticalRegistry;
        if (or_ != address(0)) {
            try IOpticalRegistryMinimal(or_).isApproved(sender) returns (bool ok) {
                if (ok) return true;
            } catch {
                // fall through
            }
        }

        // 4. Factory itself can emit (v2).
        if (sender == sidioraFactory && sender != address(0)) return true;

        // 5. Pool-token registration (v2 — pool tokens emit Transfer mirrors).
        if (_registeredTokens[sender]) return true;

        return false;
    }

    /// @notice Typed permissions are independent of the generic log whitelist.
    /// @dev Assign selectors only to their canonical source contract. Optical
    /// approval never grants authority over oracle, treasury or governance logs.
    function setTypedEmitter(address emitter, bytes4 selector, bool allowed)
        external override onlyRole(DEFAULT_ADMIN_ROLE)
    {
        if (emitter == address(0)) revert Unauthorized();
        _typedEmitters[selector][emitter] = allowed;
        emit TypedEmitterPermission(emitter, selector, allowed);
    }

    function isTypedEmitter(address emitter, bytes4 selector) external view override returns (bool) {
        return _typedEmitters[selector][emitter];
    }

    function _requireTyped() private view {
        if (!_typedEmitters[msg.sig][msg.sender]) revert Unauthorized();
    }

    function _requirePool(bytes32 id) private view {
        if (_typedEmitters[msg.sig][msg.sender]) return;
        if (poolRegistry == address(0)) revert Unauthorized();
        try IPoolRegistry(poolRegistry).isRegisteredPool(msg.sender) returns (bool ok) {
            if (!ok) revert Unauthorized();
        } catch { revert Unauthorized(); }
        if (msg.sender.code.length == 0 || bytes32(uint256(uint160(msg.sender))) != id) revert Unauthorized();
    }

    function _requireFactory() private view {
        if (_typedEmitters[msg.sig][msg.sender]) return;
        if (msg.sender != sidioraFactory || msg.sender == address(0)) revert Unauthorized();
    }

    // ════════════════════════════════════════════════════════════════════════
    //                         Admin / wiring surface
    // ════════════════════════════════════════════════════════════════════════

    /// @notice v1 — wire the Launchpad pool registry. Retained.
    function setPoolRegistry(address _poolRegistry)
        external
        onlyRole(DEFAULT_ADMIN_ROLE)
    {
        poolRegistry = _poolRegistry;
    }

    /// @notice v1 — preserved for compatibility. v2 prefers EVENT_EMITTER_ROLE
    ///         (see `setAuthorizedEmitterByRole` below) but DEFAULT_ADMIN_ROLE
    ///         remains a valid caller.
    function setAuthorizedEmitter(address emitter, bool authorized)
        external
        override
    {
        if (!hasRole(DEFAULT_ADMIN_ROLE, msg.sender)
            && !hasRole(EVENT_EMITTER_ROLE, msg.sender)) {
            revert MissingRole(msg.sender, EVENT_EMITTER_ROLE);
        }
        if (emitter == address(0)) revert Unauthorized();
        _authorizedEmitters[emitter] = authorized;
    }

    function setOpticalRegistry(address registry)
        external
        override
    {
        if (!hasRole(DEFAULT_ADMIN_ROLE, msg.sender)
            && !hasRole(EVENT_EMITTER_ROLE, msg.sender)) {
            revert MissingRole(msg.sender, EVENT_EMITTER_ROLE);
        }
        opticalRegistry = registry;
    }

    function setMetaAGRouter(address router)
        external
        override
    {
        if (!hasRole(DEFAULT_ADMIN_ROLE, msg.sender)
            && !hasRole(EVENT_EMITTER_ROLE, msg.sender)) {
            revert MissingRole(msg.sender, EVENT_EMITTER_ROLE);
        }
        metaAGRouter = router;
    }

    function setSidioraFactory(address factory)
        external
        override
    {
        if (!hasRole(DEFAULT_ADMIN_ROLE, msg.sender)
            && !hasRole(EVENT_EMITTER_ROLE, msg.sender)) {
            revert MissingRole(msg.sender, EVENT_EMITTER_ROLE);
        }
        sidioraFactory = factory;
    }

    /// @notice Compatibility shim — the interface declares `setTokenRegistry`
    ///         to allow a future external token registry. v2 uses the local
    ///         `_registeredTokens` mapping, so this currently no-ops the
    ///         external pointer; future versions may delegate.
    function setTokenRegistry(address /*registry*/ )
        external
        override
    {
        if (!hasRole(DEFAULT_ADMIN_ROLE, msg.sender)
            && !hasRole(EVENT_EMITTER_ROLE, msg.sender)) {
            revert MissingRole(msg.sender, EVENT_EMITTER_ROLE);
        }
        // Reserved for future delegation; v2 uses the in-contract mapping.
    }

    /// @notice Register a pool-token deployed by the SidioraFactory so it
    ///         can mirror Transfer / Approval through this emitter.
    /// @dev Callable by the wired factory itself (auto-registration on
    ///      market creation) or by EVENT_EMITTER_ROLE / DEFAULT_ADMIN_ROLE.
    function registerToken(address token, address /*pool*/ )
        external
        override
    {
        if (token == address(0)) revert Unauthorized();
        bool authorized =
            (sidioraFactory != address(0) && msg.sender == sidioraFactory)
            || hasRole(EVENT_EMITTER_ROLE, msg.sender)
            || hasRole(DEFAULT_ADMIN_ROLE, msg.sender);
        if (!authorized) revert Unauthorized();
        _registeredTokens[token] = true;
    }

    function deregisterToken(address token)
        external
        override
    {
        if (!hasRole(DEFAULT_ADMIN_ROLE, msg.sender)
            && !hasRole(EVENT_EMITTER_ROLE, msg.sender)) {
            revert MissingRole(msg.sender, EVENT_EMITTER_ROLE);
        }
        _registeredTokens[token] = false;
    }

    // ════════════════════════════════════════════════════════════════════════
    //                            Version accessor
    // ════════════════════════════════════════════════════════════════════════

    function VERSION() external pure override returns (string memory) {
        return _VERSION;
    }

    // ════════════════════════════════════════════════════════════════════════
    //                          v1 emit functions
    //
    //   Preserved verbatim for backward compatibility. SidioraFactory,
    //   SidioraPool, ProtocolConfig and downstream test mocks rely on
    //   these signatures. Do NOT change selectors, parameter order, or
    //   topic0 of the emitted events.
    // ════════════════════════════════════════════════════════════════════════

    function emitMarketCreated(
        bytes32 poolId,
        address token,
        address creator,
        address pool,
        address optical
    ) external override onlyAuthorized {
        _requireFactory();
        emit MarketCreated(poolId, token, creator, pool, optical, block.timestamp, block.number);
    }

    function emitSwap(
        bytes32 poolId,
        address sender,
        bool isBuy,
        uint256 amountIn,
        uint256 amountOut,
        uint256 fee,
        uint256 price
    ) external override onlyAuthorized {
        _requirePool(poolId);
        emit Swap(poolId, sender, isBuy, amountIn, amountOut, fee, price, block.timestamp, block.number);
    }

    function emitFeeRecorded(
        bytes32 poolId,
        uint256 feeAmount,
        uint256 protocolCut,
        uint256 poolCut
    ) external override onlyAuthorized {
        _requireTyped();
        emit FeeRecorded(poolId, feeAmount, protocolCut, poolCut, block.timestamp, block.number);
    }

    function emitFeeDistributed(
        bytes32 poolId,
        uint256 nftId,
        uint8 strategy,
        uint256 amount,
        address recipient
    ) external override onlyAuthorized {
        _requireTyped();
        emit FeeDistributed(poolId, nftId, strategy, amount, recipient, block.timestamp, block.number);
    }

    function emitFeeStrategyChanged(
        bytes32 poolId,
        uint256 nftId,
        uint8 oldStrategy,
        uint8 newStrategy
    ) external override onlyAuthorized {
        _requireTyped();
        emit FeeStrategyChanged(poolId, nftId, oldStrategy, newStrategy, block.timestamp, block.number);
    }

    function emitPoolStateUpdated(
        bytes32 poolId,
        uint256 virtualReserve,
        uint256 realReserve,
        uint256 tokenReserve,
        uint256 price
    ) external override onlyAuthorized {
        _requirePool(poolId);
        emit PoolStateUpdated(poolId, virtualReserve, realReserve, tokenReserve, price, block.timestamp, block.number);
    }

    function emitOpticalExecuted(
        bytes32 poolId,
        address optical,
        string calldata hookName,
        bytes calldata data
    ) external override onlyAuthorized {
        _requirePool(poolId);
        if (!_typedEmitters[msg.sig][msg.sender] && IPoolRegistry(poolRegistry).getPoolMetadata(msg.sender).optical != optical) revert Unauthorized();
        emit OpticalExecuted(poolId, optical, hookName, data, block.timestamp, block.number);
    }

    function emitConfigUpdated(
        bytes32 key,
        uint256 oldValue,
        uint256 newValue
    ) external override onlyAuthorized {
        _requireTyped();
        emit ConfigUpdated(key, oldValue, newValue, block.timestamp, block.number);
    }

    // ════════════════════════════════════════════════════════════════════════
    //                       v2 — Generic schemaless emit
    // ════════════════════════════════════════════════════════════════════════

    function emitEventLog(
        string calldata eventName,
        EventData calldata data
    ) external override onlyAuthorized {
        emit EventLog(
            msg.sender,
            keccak256(bytes(eventName)),
            eventName,
            data,
            block.timestamp,
            block.number
        );
    }

    function emitEventLog1(
        string calldata eventName,
        bytes32 topic1,
        EventData calldata data
    ) external override onlyAuthorized {
        emit EventLog1(
            msg.sender,
            keccak256(bytes(eventName)),
            topic1,
            eventName,
            data,
            block.timestamp,
            block.number
        );
    }

    function emitEventLog2(
        string calldata eventName,
        bytes32 topic1,
        bytes32 topic2,
        EventData calldata data
    ) external override onlyAuthorized {
        emit EventLog2(
            msg.sender,
            keccak256(bytes(eventName)),
            topic1,
            topic2,
            eventName,
            data,
            block.timestamp,
            block.number
        );
    }

    // ════════════════════════════════════════════════════════════════════════
    //                       v2 — Typed fast-paths: PECOR
    // ════════════════════════════════════════════════════════════════════════

    function emitPecorSwap(
        address user,
        address tokenIn,
        address tokenOut,
        uint256 amountIn,
        uint256 amountOut,
        uint256 priceIn,
        uint256 priceOut,
        uint256 volumeUSD,
        uint256 feeBps,
        uint256 feeAmount,
        uint256 impactBps,
        uint8 swapKind
    ) external override onlyAuthorized {
        _requireTyped();
        emit PecorSwap(
            user, tokenIn, tokenOut,
            amountIn, amountOut, priceIn, priceOut,
            volumeUSD, feeBps, feeAmount, impactBps, swapKind,
            block.timestamp, block.number
        );
    }

    function emitPecorOrderCreated(
        uint256 orderId,
        address user,
        uint8 orderKind,
        uint8 orderType,
        address tokenIn,
        address tokenOut,
        uint256 amount,
        uint256 targetPrice,
        uint256 stopPrice,
        uint256 limitPrice
    ) external override onlyAuthorized {
        _requireTyped();
        emit PecorOrderCreated(
            orderId, user, orderKind, orderType,
            tokenIn, tokenOut, amount,
            targetPrice, stopPrice, limitPrice,
            block.timestamp, block.number
        );
    }

    function emitPecorOrderLifecycle(
        uint256 orderId,
        address user,
        uint8 orderKind,
        uint8 phase,
        uint256 price,
        bytes calldata payload
    ) external override onlyAuthorized {
        _requireTyped();
        emit PecorOrderLifecycle(
            orderId, user, orderKind, phase, price, payload,
            block.timestamp, block.number
        );
    }

    function emitBestRouteSwap(
        address user,
        address tokenIn,
        address tokenOut,
        bytes32 routeId,
        uint256 amountIn,
        uint256 amountOut,
        address[] calldata hops,
        uint256 protocolFeeBps
    ) external override onlyAuthorized {
        _requireTyped();
        emit BestRouteSwap(
            user, tokenIn, tokenOut, routeId,
            amountIn, amountOut, hops, protocolFeeBps,
            block.timestamp, block.number
        );
    }

    // ════════════════════════════════════════════════════════════════════════
    //                       v2 — Typed fast-paths: Oracle
    // ════════════════════════════════════════════════════════════════════════

    function emitPriceUpdated(
        address token,
        uint256 roundId,
        address relayer,
        uint256 price,
        uint256 confidence,
        bytes32 sourceId
    ) external override onlyAuthorized {
        _requireTyped();
        emit PriceUpdated(
            token, roundId, relayer, price, confidence, sourceId,
            block.timestamp, block.number
        );
    }

    function emitCircuitBreaker(
        address token,
        bytes32 sourceId,
        uint256 reportedPrice,
        uint256 referencePrice,
        uint256 deviationBps
    ) external override onlyAuthorized {
        _requireTyped();
        emit CircuitBreakerTriggered(
            token, sourceId, reportedPrice, referencePrice, deviationBps,
            block.timestamp, block.number
        );
    }

    function emitOracleAdapterLifecycle(
        bytes32 sourceId,
        address adapter,
        uint8 phase,
        uint256 priority
    ) external override onlyAuthorized {
        _requireTyped();
        emit OracleAdapterLifecycle(
            sourceId, adapter, phase, priority,
            block.timestamp, block.number
        );
    }

    // ════════════════════════════════════════════════════════════════════════
    //                       v2 — Typed fast-paths: Vault
    // ════════════════════════════════════════════════════════════════════════

    function emitVaultFlow(
        uint8 flowType,
        address token,
        address party,
        uint256 amount,
        uint256 newReserve
    ) external override onlyAuthorized {
        _requireTyped();
        emit VaultFlow(
            flowType, token, party, amount, newReserve,
            block.timestamp, block.number
        );
    }

    // ════════════════════════════════════════════════════════════════════════
    //                       v2 — Typed fast-paths: Governance
    // ════════════════════════════════════════════════════════════════════════

    function emitGovernance(
        uint8 action,
        bytes32 id,
        address actor,
        bytes calldata payload
    ) external override onlyAuthorized {
        _requireTyped();
        emit Governance(action, id, actor, payload, block.timestamp, block.number);
    }

    // ════════════════════════════════════════════════════════════════════════
    //                       v2 — Typed fast-paths: Treasury
    // ════════════════════════════════════════════════════════════════════════

    function emitTreasuryFlow(
        uint8 direction,
        address token,
        address party,
        uint256 amount
    ) external override onlyAuthorized {
        _requireTyped();
        emit TreasuryFlow(
            direction, token, party, amount,
            block.timestamp, block.number
        );
    }

    // ════════════════════════════════════════════════════════════════════════
    //                       v2 — Typed fast-paths: Optical
    // ════════════════════════════════════════════════════════════════════════

    function emitOpticalLifecycle(
        uint8 action,
        address optical,
        address pool,
        bytes32 name,
        bytes calldata payload
    ) external override onlyAuthorized {
        if (msg.sender != optical) _requireTyped();
        emit OpticalLifecycle(
            action, optical, pool, name, payload,
            block.timestamp, block.number
        );
    }

    // ════════════════════════════════════════════════════════════════════════
    //                  v2 — Typed fast-paths: Token / NFT mirrors
    // ════════════════════════════════════════════════════════════════════════

    function emitTokenTransfer(
        address token,
        address from,
        address to,
        uint256 value
    ) external override onlyAuthorized {
        if (msg.sender != token) revert Unauthorized();
        emit TokenTransfer(token, from, to, value, block.timestamp, block.number);
    }

    function emitNftTransfer(
        address nft,
        address from,
        address to,
        uint256 tokenId
    ) external override onlyAuthorized {
        if (msg.sender != nft) revert Unauthorized();
        emit NftTransfer(nft, from, to, tokenId, block.timestamp, block.number);
    }

    function emitAssetApproval(
        address asset,
        address owner,
        address spender,
        uint256 valueOrTokenId,
        bool isNft
    ) external override onlyAuthorized {
        if (msg.sender != asset) revert Unauthorized();
        emit AssetApproval(
            asset, owner, spender, valueOrTokenId, isNft,
            block.timestamp, block.number
        );
    }

    // ════════════════════════════════════════════════════════════════════════
    //                v2 — Typed fast-paths: Access-control & lifecycle
    // ════════════════════════════════════════════════════════════════════════

    function emitRoleChange(
        uint8 action,
        bytes32 role,
        address account,
        address sender,
        bytes32 previousAdminRole
    ) external override onlyAuthorized {
        _requireTyped();
        emit RoleChange(
            action, role, account, sender, previousAdminRole,
            block.timestamp, block.number
        );
    }

    function emitUpgraded(
        address proxy,
        address newImplementation,
        uint8 kind
    ) external override onlyAuthorized {
        if (msg.sender != proxy) revert Unauthorized();
        emit ContractUpgraded(
            proxy, newImplementation, kind,
            block.timestamp, block.number
        );
    }

    function emitPauseToggle(
        address pausedContract,
        bool paused
    ) external override onlyAuthorized {
        if (msg.sender != pausedContract) revert Unauthorized();
        emit PauseToggle(pausedContract, paused, block.timestamp, block.number);
    }

    // ════════════════════════════════════════════════════════════════════════
    //                v2 — Typed fast-paths: Launchpad mirrors
    // ════════════════════════════════════════════════════════════════════════

    function emitFeeFlow(
        uint8 kind,
        address pool,
        address party,
        uint256 amount,
        uint256 protocolCut,
        uint256 poolCut,
        uint256 epoch
    ) external override onlyAuthorized {
        _requireTyped();
        emit FeeFlow(
            kind, pool, party, amount, protocolCut, poolCut, epoch,
            block.timestamp, block.number
        );
    }

    function emitRouterTrade(
        uint8 kind,
        address pool,
        address sender,
        address tokenIn,
        address tokenOut,
        uint256 amountIn,
        uint256 amountOut,
        uint256 intermediateUsdl
    ) external override onlyAuthorized {
        _requireTyped();
        emit RouterTrade(
            kind, pool, sender, tokenIn, tokenOut,
            amountIn, amountOut, intermediateUsdl,
            block.timestamp, block.number
        );
    }

    function emitNftMint(
        uint256 tokenId,
        address creator,
        address pool,
        uint8 strategy
    ) external override onlyAuthorized {
        _requireTyped();
        emit NftMint(
            tokenId, creator, pool, strategy,
            block.timestamp, block.number
        );
    }

    function emitPoolRegistered(
        address pool,
        address token,
        address creator,
        address optical,
        uint256 nftId
    ) external override onlyAuthorized {
        if (!_typedEmitters[msg.sig][msg.sender] && (msg.sender != poolRegistry || msg.sender == address(0))) revert Unauthorized();
        if (!_typedEmitters[msg.sig][msg.sender]) {
            IPoolRegistry.PoolMetadata memory meta = IPoolRegistry(poolRegistry).getPoolMetadata(pool);
            if (meta.token != token || meta.creator != creator || meta.optical != optical || meta.nftId != nftId) revert Unauthorized();
        }
        emit PoolRegistered(
            pool, token, creator, optical, nftId,
            block.timestamp, block.number
        );
    }

    function emitTokenDeployed(
        address token,
        address pool,
        address creator,
        bytes32 salt,
        string calldata name,
        string calldata symbol,
        uint8 decimals,
        uint256 totalSupply
    ) external override onlyAuthorized {
        _requireFactory();
        emit TokenDeployed(
            token, pool, creator, salt, name, symbol, decimals, totalSupply,
            block.timestamp, block.number
        );
    }
}
