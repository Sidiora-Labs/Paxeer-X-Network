// SPDX-License-Identifier: LicenseRef-Paxlabs-HyperPax-OS-Protocol
pragma solidity ^0.8.24;

/// @title IEventEmitter
/// @notice Interface for the central event hub contract
interface IEventEmitter {
    // --- Errors ---
    error Unauthorized();

    // --- Events ---
    event MarketCreated(
        bytes32 indexed poolId,
        address indexed token,
        address indexed creator,
        address pool,
        address optical,
        uint256 timestamp,
        uint256 blockNumber
    );

    event Swap(
        bytes32 indexed poolId,
        address indexed sender,
        bool isBuy,
        uint256 amountIn,
        uint256 amountOut,
        uint256 fee,
        uint256 price,
        uint256 timestamp,
        uint256 blockNumber
    );

    event FeeRecorded(
        bytes32 indexed poolId,
        uint256 feeAmount,
        uint256 protocolCut,
        uint256 poolCut,
        uint256 timestamp,
        uint256 blockNumber
    );

    event FeeDistributed(
        bytes32 indexed poolId,
        uint256 nftId,
        uint8 strategy,
        uint256 amount,
        address recipient,
        uint256 timestamp,
        uint256 blockNumber
    );

    event FeeStrategyChanged(
        bytes32 indexed poolId,
        uint256 nftId,
        uint8 oldStrategy,
        uint8 newStrategy,
        uint256 timestamp,
        uint256 blockNumber
    );

    event OpticalExecuted(
        bytes32 indexed poolId,
        address indexed optical,
        string hookName,
        bytes data,
        uint256 timestamp,
        uint256 blockNumber
    );

    event PoolStateUpdated(
        bytes32 indexed poolId,
        uint256 virtualReserve,
        uint256 realReserve,
        uint256 tokenReserve,
        uint256 price,
        uint256 timestamp,
        uint256 blockNumber
    );

    event ConfigUpdated(
        bytes32 indexed key,
        uint256 oldValue,
        uint256 newValue,
        uint256 timestamp,
        uint256 blockNumber
    );

    // --- Functions ---
    function emitMarketCreated(
        bytes32 poolId, address token, address creator, address pool, address optical
    ) external;

    function emitSwap(
        bytes32 poolId, address sender, bool isBuy,
        uint256 amountIn, uint256 amountOut, uint256 fee, uint256 price
    ) external;

    function emitFeeRecorded(
        bytes32 poolId, uint256 feeAmount, uint256 protocolCut, uint256 poolCut
    ) external;

    function emitFeeDistributed(
        bytes32 poolId, uint256 nftId, uint8 strategy, uint256 amount, address recipient
    ) external;

    function emitFeeStrategyChanged(
        bytes32 poolId, uint256 nftId, uint8 oldStrategy, uint8 newStrategy
    ) external;

    function emitPoolStateUpdated(
        bytes32 poolId, uint256 virtualReserve, uint256 realReserve, uint256 tokenReserve, uint256 price
    ) external;

    function emitOpticalExecuted(
        bytes32 poolId, address optical, string calldata hookName, bytes calldata data
    ) external;

    function emitConfigUpdated(bytes32 key, uint256 oldValue, uint256 newValue) external;

    function isAuthorizedEmitter(address emitter) external view returns (bool);
    function setAuthorizedEmitter(address emitter, bool authorized) external;

    // ════════════════════════════════════════════════════════════════════════
    //                                  v2
    //
    // Backward-compatible expansion of the v1 surface. All v1 events and
    // functions above remain identical (same selectors, same topic0, same
    // storage layout). v2 adds:
    //   - Generic schemaless emission (EventLog / EventLog1 / EventLog2)
    //     with indexed keccak256(eventName) for cheap topic1 filtering.
    //   - Typed fast-path emitters covering Meta-AG/PECOR, oracle,
    //     vault, governance, treasury, optical, NFT, token, role,
    //     upgrade and pause domains.
    //   - VERSION() accessor.
    //   - EVENT_EMITTER_ROLE — distinct from DEFAULT_ADMIN_ROLE — for
    //     emission authorization without granting upgrade rights.
    //
    // Reference: contracts/data/EVENT-EMITTER-V2-PLAN.md
    // ════════════════════════════════════════════════════════════════════════

    // --- Generic schemaless payload (GMX V2 pattern) ----------------------

    struct AddressKeyValue       { string key; address value; }
    struct AddressArrayKeyValue  { string key; address[] value; }
    struct UintKeyValue          { string key; uint256 value; }
    struct UintArrayKeyValue     { string key; uint256[] value; }
    struct IntKeyValue           { string key; int256 value; }
    struct IntArrayKeyValue      { string key; int256[] value; }
    struct BoolKeyValue          { string key; bool value; }
    struct BoolArrayKeyValue     { string key; bool[] value; }
    struct Bytes32KeyValue       { string key; bytes32 value; }
    struct Bytes32ArrayKeyValue  { string key; bytes32[] value; }
    struct BytesKeyValue         { string key; bytes value; }
    struct BytesArrayKeyValue    { string key; bytes[] value; }
    struct StringKeyValue        { string key; string value; }
    struct StringArrayKeyValue   { string key; string[] value; }

    struct AddressItems  { AddressKeyValue[]  items; AddressArrayKeyValue[]  arrayItems; }
    struct UintItems     { UintKeyValue[]     items; UintArrayKeyValue[]     arrayItems; }
    struct IntItems      { IntKeyValue[]      items; IntArrayKeyValue[]      arrayItems; }
    struct BoolItems     { BoolKeyValue[]     items; BoolArrayKeyValue[]     arrayItems; }
    struct Bytes32Items  { Bytes32KeyValue[]  items; Bytes32ArrayKeyValue[]  arrayItems; }
    struct BytesItems    { BytesKeyValue[]    items; BytesArrayKeyValue[]    arrayItems; }
    struct StringItems   { StringKeyValue[]   items; StringArrayKeyValue[]   arrayItems; }

    struct EventData {
        AddressItems addressItems;
        UintItems    uintItems;
        IntItems     intItems;
        BoolItems    boolItems;
        Bytes32Items bytes32Items;
        BytesItems   bytesItems;
        StringItems  stringItems;
    }

    /// @notice Generic schemaless event with no extra topics beyond eventNameHash.
    /// @dev `msgSender` records the actual caller (real provenance).
    ///      `eventNameHash = keccak256(bytes(eventName))` — indexed so off-chain
    ///      consumers filter on topic1 without decoding the string body.
    event EventLog(
        address indexed msgSender,
        bytes32 indexed eventNameHash,
        string eventName,
        EventData eventData,
        uint256 timestamp,
        uint256 blockNumber
    );

    /// @notice Generic schemaless event with one extra topic (eg. user / pool / token).
    event EventLog1(
        address indexed msgSender,
        bytes32 indexed eventNameHash,
        bytes32 indexed topic1,
        string eventName,
        EventData eventData,
        uint256 timestamp,
        uint256 blockNumber
    );

    /// @notice Generic schemaless event with two extra topics (eg. user + token, pool + nft).
    event EventLog2(
        address indexed msgSender,
        bytes32 indexed eventNameHash,
        bytes32 indexed topic1,
        bytes32 topic2,
        string eventName,
        EventData eventData,
        uint256 timestamp,
        uint256 blockNumber
    );

    function emitEventLog(
        string calldata eventName,
        EventData calldata data
    ) external;

    function emitEventLog1(
        string calldata eventName,
        bytes32 topic1,
        EventData calldata data
    ) external;

    function emitEventLog2(
        string calldata eventName,
        bytes32 topic1,
        bytes32 topic2,
        EventData calldata data
    ) external;

    // --- Typed fast-path: Meta-AG / PECOR ---------------------------------

    event PecorSwap(
        address indexed user,
        address indexed tokenIn,
        address indexed tokenOut,
        uint256 amountIn,
        uint256 amountOut,
        uint256 priceIn,
        uint256 priceOut,
        uint256 volumeUSD,
        uint256 feeBps,
        uint256 feeAmount,
        uint256 impactBps,
        uint8 swapKind,        // 0=ExactIn 1=ExactOut 2=Market 3=Native 4=BestRoute
        uint256 timestamp,
        uint256 blockNumber
    );

    event PecorOrderCreated(
        uint256 indexed orderId,
        address indexed user,
        uint8 orderKind,       // 0=Limit 1=StopLimit 2=StopLoss
        uint8 orderType,       // 0=Buy 1=Sell
        address tokenIn,
        address tokenOut,
        uint256 amount,
        uint256 targetPrice,
        uint256 stopPrice,
        uint256 limitPrice,
        uint256 timestamp,
        uint256 blockNumber
    );

    event PecorOrderLifecycle(
        uint256 indexed orderId,
        address indexed user,
        uint8 orderKind,       // 0=Limit 1=StopLimit 2=StopLoss
        uint8 phase,           // 0=Activated 1=Executed 2=Cancelled 3=KeeperUpdated
        uint256 price,
        bytes payload,
        uint256 timestamp,
        uint256 blockNumber
    );

    event BestRouteSwap(
        address indexed user,
        address indexed tokenIn,
        address indexed tokenOut,
        bytes32 routeId,
        uint256 amountIn,
        uint256 amountOut,
        address[] hops,
        uint256 protocolFeeBps,
        uint256 timestamp,
        uint256 blockNumber
    );

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
    ) external;

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
    ) external;

    function emitPecorOrderLifecycle(
        uint256 orderId,
        address user,
        uint8 orderKind,
        uint8 phase,
        uint256 price,
        bytes calldata payload
    ) external;

    function emitBestRouteSwap(
        address user,
        address tokenIn,
        address tokenOut,
        bytes32 routeId,
        uint256 amountIn,
        uint256 amountOut,
        address[] calldata hops,
        uint256 protocolFeeBps
    ) external;

    // --- Typed fast-path: Oracle ------------------------------------------

    event PriceUpdated(
        address indexed token,
        uint256 indexed roundId,
        address indexed relayer,
        uint256 price,
        uint256 confidence,
        bytes32 sourceId,
        uint256 timestamp,
        uint256 blockNumber
    );

    event CircuitBreakerTriggered(
        address indexed token,
        bytes32 indexed sourceId,
        uint256 reportedPrice,
        uint256 referencePrice,
        uint256 deviationBps,
        uint256 timestamp,
        uint256 blockNumber
    );

    event OracleAdapterLifecycle(
        bytes32 indexed sourceId,
        address indexed adapter,
        uint8 phase,           // 0=Registered 1=Activated 2=Deactivated 3=PriorityUpdated
        uint256 priority,
        uint256 timestamp,
        uint256 blockNumber
    );

    function emitPriceUpdated(
        address token,
        uint256 roundId,
        address relayer,
        uint256 price,
        uint256 confidence,
        bytes32 sourceId
    ) external;

    function emitCircuitBreaker(
        address token,
        bytes32 sourceId,
        uint256 reportedPrice,
        uint256 referencePrice,
        uint256 deviationBps
    ) external;

    function emitOracleAdapterLifecycle(
        bytes32 sourceId,
        address adapter,
        uint8 phase,
        uint256 priority
    ) external;

    // --- Typed fast-path: Vault -------------------------------------------

    event VaultFlow(
        uint8 indexed flowType,    // 0=Deposit 1=Withdrawal 2=NativeDeposit 3=NativeWithdrawal 4=Emergency 5=ReservesSync
        address indexed token,
        address indexed party,
        uint256 amount,
        uint256 newReserve,
        uint256 timestamp,
        uint256 blockNumber
    );

    function emitVaultFlow(
        uint8 flowType,
        address token,
        address party,
        uint256 amount,
        uint256 newReserve
    ) external;

    // --- Typed fast-path: Governance / Timelock ---------------------------

    event Governance(
        uint8 indexed action,      // 0=TxQueued 1=TxExecuted 2=TxCancelled
                                   // 3=ProposalCreated 4=VoteCast 5=ProposalExecuted 6=ProposalCancelled
                                   // 7=ProposerChanged 8=GuardianChanged 9=AdminModeDeactivated
        bytes32 indexed id,
        address indexed actor,
        bytes payload,
        uint256 timestamp,
        uint256 blockNumber
    );

    function emitGovernance(
        uint8 action,
        bytes32 id,
        address actor,
        bytes calldata payload
    ) external;

    // --- Typed fast-path: Treasury ----------------------------------------

    event TreasuryFlow(
        uint8 indexed direction,   // 0=Deposit 1=Withdraw
        address indexed token,
        address indexed party,
        uint256 amount,
        uint256 timestamp,
        uint256 blockNumber
    );

    function emitTreasuryFlow(
        uint8 direction,
        address token,
        address party,
        uint256 amount
    ) external;

    // --- Typed fast-path: Optical -----------------------------------------

    event OpticalLifecycle(
        uint8 indexed action,      // 0=Registered 1=Deregistered 2=MetadataUpdated
                                   // 3=Deployed 4=Hooked 5=Triggered
        address indexed optical,
        address indexed pool,
        bytes32 name,
        bytes payload,
        uint256 timestamp,
        uint256 blockNumber
    );

    function emitOpticalLifecycle(
        uint8 action,
        address optical,
        address pool,
        bytes32 name,
        bytes calldata payload
    ) external;

    // --- Typed fast-path: Token / NFT mirrors -----------------------------

    event TokenTransfer(
        address indexed token,
        address indexed from,
        address indexed to,
        uint256 value,
        uint256 timestamp,
        uint256 blockNumber
    );

    event NftTransfer(
        address indexed nft,
        address indexed from,
        address indexed to,
        uint256 tokenId,
        uint256 timestamp,
        uint256 blockNumber
    );

    event AssetApproval(
        address indexed asset,
        address indexed owner,
        address indexed spender,
        uint256 valueOrTokenId,
        bool isNft,
        uint256 timestamp,
        uint256 blockNumber
    );

    function emitTokenTransfer(
        address token,
        address from,
        address to,
        uint256 value
    ) external;

    function emitNftTransfer(
        address nft,
        address from,
        address to,
        uint256 tokenId
    ) external;

    function emitAssetApproval(
        address asset,
        address owner,
        address spender,
        uint256 valueOrTokenId,
        bool isNft
    ) external;

    // --- Typed fast-path: Access-control & lifecycle ----------------------

    event RoleChange(
        uint8 indexed action,      // 0=Granted 1=Revoked 2=AdminChanged
        bytes32 indexed role,
        address indexed account,
        address sender,
        bytes32 previousAdminRole,
        uint256 timestamp,
        uint256 blockNumber
    );

    event ContractUpgraded(
        address indexed proxy,
        address indexed newImplementation,
        uint8 kind,                // 0=UUPS 1=Beacon 2=AdminChanged
        uint256 timestamp,
        uint256 blockNumber
    );

    event PauseToggle(
        address indexed pausedContract,
        bool paused,
        uint256 timestamp,
        uint256 blockNumber
    );

    function emitRoleChange(
        uint8 action,
        bytes32 role,
        address account,
        address sender,
        bytes32 previousAdminRole
    ) external;

    function emitUpgraded(
        address proxy,
        address newImplementation,
        uint8 kind
    ) external;

    function emitPauseToggle(
        address pausedContract,
        bool paused
    ) external;

    // --- Typed fast-path: Launchpad mirrors (additive to v1) --------------

    /// @notice Mirror of FeeAccumulator's full event surface, collapsed
    ///         into a single typed channel for indexer simplicity.
    event FeeFlow(
        uint8 indexed kind,        // 0=Recorded 1=Claimed 2=Burned
                                   // 3=AirdropTriggered 4=AirdropClaimed 5=LpRewardsSent
                                   // 6=ProtocolFeeSwept 7=OpticalSurplusRecorded 8=OpticalSurplusClaimed
        address indexed pool,
        address indexed party,
        uint256 amount,
        uint256 protocolCut,
        uint256 poolCut,
        uint256 epoch,
        uint256 timestamp,
        uint256 blockNumber
    );

    event RouterTrade(
        uint8 indexed kind,        // 0=Buy 1=Sell 2=MultihopSwap
        address indexed pool,
        address indexed sender,
        address tokenIn,
        address tokenOut,
        uint256 amountIn,
        uint256 amountOut,
        uint256 intermediateUsdl,
        uint256 timestamp,
        uint256 blockNumber
    );

    event NftMint(
        uint256 indexed tokenId,
        address indexed creator,
        address indexed pool,
        uint8 strategy,
        uint256 timestamp,
        uint256 blockNumber
    );

    event PoolRegistered(
        address indexed pool,
        address indexed token,
        address indexed creator,
        address optical,
        uint256 nftId,
        uint256 timestamp,
        uint256 blockNumber
    );

    event TokenDeployed(
        address indexed token,
        address indexed pool,
        address indexed creator,
        bytes32 salt,
        string name,
        string symbol,
        uint8 decimals,
        uint256 totalSupply,
        uint256 timestamp,
        uint256 blockNumber
    );

    function emitFeeFlow(
        uint8 kind,
        address pool,
        address party,
        uint256 amount,
        uint256 protocolCut,
        uint256 poolCut,
        uint256 epoch
    ) external;

    function emitRouterTrade(
        uint8 kind,
        address pool,
        address sender,
        address tokenIn,
        address tokenOut,
        uint256 amountIn,
        uint256 amountOut,
        uint256 intermediateUsdl
    ) external;

    function emitNftMint(
        uint256 tokenId,
        address creator,
        address pool,
        uint8 strategy
    ) external;

    function emitPoolRegistered(
        address pool,
        address token,
        address creator,
        address optical,
        uint256 nftId
    ) external;

    function emitTokenDeployed(
        address token,
        address pool,
        address creator,
        bytes32 salt,
        string calldata name,
        string calldata symbol,
        uint8 decimals,
        uint256 totalSupply
    ) external;

    // --- v2 administrative surface ----------------------------------------

    event TypedEmitterPermission(address indexed emitter, bytes4 indexed selector, bool allowed);
    function setTypedEmitter(address emitter, bytes4 selector, bool allowed) external;
    function isTypedEmitter(address emitter, bytes4 selector) external view returns (bool);

    /// @notice Semantic version string of the deployed implementation.
    function VERSION() external pure returns (string memory);

    /// @notice The role that can authorize emitters and wire registries.
    ///         Distinct from DEFAULT_ADMIN_ROLE (which retains upgrade authority).
    function EVENT_EMITTER_ROLE() external view returns (bytes32);

    /// @notice Wire dynamic auth registries (one-time admin call after v2 upgrade).
    function setOpticalRegistry(address registry) external;
    function setMetaAGRouter(address router) external;
    function setSidioraFactory(address factory) external;
    function setTokenRegistry(address registry) external;

    /// @notice Per-token registration for the dynamic ERC20 auto-auth path.
    ///         Allows pool-token Transfer mirrors without per-token admin tx.
    function registerToken(address token, address pool) external;
    function deregisterToken(address token) external;
    function isRegisteredToken(address token) external view returns (bool);

    /// @notice One-time storage migration entrypoint for v1 → v2 proxy upgrade.
    ///         Idempotent — safe to call once.
    function reinitializeV2(address adminWithEmitterRole) external;
}
