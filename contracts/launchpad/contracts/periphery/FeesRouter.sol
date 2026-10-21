// SPDX-License-Identifier: LicenseRef-Paxlabs-HyperPax-OS-Protocol
pragma solidity ^0.8.24;

import "../base/Initializable.sol";
import "../base/UUPSUpgradeable.sol";
import "../base/AccessControl.sol";
import "../base/ReentrancyGuard.sol";
import "../interfaces/IFeesRouter.sol";
import "../interfaces/IFeeAccumulator.sol";
import "../interfaces/ISidioraNFT.sol";
import "../interfaces/ISidioraPool.sol";
import "../interfaces/IPoolRegistry.sol";
import "../interfaces/IEventEmitter.sol";

/// @title FeesRouter
/// @notice NFT-holder interface for fee management.
/// @dev UUPS proxy. Validates NFT ownership, reads strategy, delegates to FeeAccumulator.
contract FeesRouter is IFeesRouter, Initializable, UUPSUpgradeable, AccessControl, ReentrancyGuard {
    event MirrorFailed(bytes4 indexed selector);
    address public nftContract;
    address public feeAccumulator;
    address public poolRegistry;

    /// @custom:oz-upgrades-unsafe-allow constructor
    constructor() {
        _disableInitializers();
    }

    function initialize(
        address _nftContract,
        address _feeAccumulator,
        address _poolRegistry,
        address _admin
    ) external initializer {
        if (_admin == address(0)) revert ZeroAddress();
        if (_nftContract == address(0)) revert ZeroAddress();
        if (_feeAccumulator == address(0)) revert ZeroAddress();

        nftContract = _nftContract;
        feeAccumulator = _feeAccumulator;
        poolRegistry = _poolRegistry;

        _grantRole(DEFAULT_ADMIN_ROLE, _admin);
        _initReentrancyGuard();
    }

    /// @inheritdoc IFeesRouter
    function setFeeStrategy(uint256 nftId, uint8 newStrategy) external override {
        _requireNftOwner(nftId);
        if (newStrategy == 2 && !IFeeAccumulator(feeAccumulator).supportsAirdrop(ISidioraNFT(nftContract).getPoolAddress(nftId))) revert UnsupportedSnapshotToken();
        uint8 oldStrategy = ISidioraNFT(nftContract).getFeeStrategy(nftId);
        ISidioraNFT(nftContract).setFeeStrategy(nftId, newStrategy);
        emit FeeStrategyChanged(nftId, oldStrategy, newStrategy);

    }

    /// @inheritdoc IFeesRouter
    function claimFees(uint256 nftId) external override nonReentrant returns (uint256 amount) {
        _requireNftOwner(nftId);
        _requireStrategy(nftId, 0); // CLAIM = 0

        address pool = ISidioraNFT(nftContract).getPoolAddress(nftId);
        amount = IFeeAccumulator(feeAccumulator).claim(pool, msg.sender);

        emit FeesClaimed(nftId, msg.sender, amount);
        _mirrorDistribution(nftId, pool, 0, amount, msg.sender);
    }

    /// @inheritdoc IFeesRouter
    function executeBurn(uint256 nftId) external override nonReentrant returns (uint256 amount) {
        _requireNftOwner(nftId);
        _requireStrategy(nftId, 1); // BURN = 1

        address pool = ISidioraNFT(nftContract).getPoolAddress(nftId);
        amount = IFeeAccumulator(feeAccumulator).burn(pool);

        emit FeesBurned(nftId, amount);
        _mirrorDistribution(nftId, pool, 1, amount, 0x000000000000000000000000000000000000dEaD);
    }

    /// @inheritdoc IFeesRouter
    function executeAirdrop(uint256 nftId) external override nonReentrant returns (uint256 amount) {
        _requireNftOwner(nftId);
        _requireStrategy(nftId, 2); // AIRDROP = 2

        address pool = ISidioraNFT(nftContract).getPoolAddress(nftId);
        amount = IFeeAccumulator(feeAccumulator).triggerAirdrop(pool);

        emit AirdropExecuted(nftId, amount);
        _mirrorDistribution(nftId, pool, 2, amount, address(0));
    }

    /// @inheritdoc IFeesRouter
    function claimAirdrop(uint256 nftId) external override nonReentrant returns (uint256 amount) {
        // Any token holder can claim — no NFT ownership check
        address pool = ISidioraNFT(nftContract).getPoolAddress(nftId);
        amount = IFeeAccumulator(feeAccumulator).claimAirdrop(pool, msg.sender);

        emit AirdropClaimed(nftId, msg.sender, amount);
    }

    function claimAirdropEpoch(uint256 nftId, uint256 epoch) external override nonReentrant returns (uint256 amount) {
        address pool = ISidioraNFT(nftContract).getPoolAddress(nftId);
        amount = IFeeAccumulator(feeAccumulator).claimAirdropEpoch(pool, msg.sender, epoch);
        emit AirdropClaimed(nftId, msg.sender, amount);
    }

    /// @inheritdoc IFeesRouter
    function executeLpRewards(uint256 nftId) external override nonReentrant returns (uint256 amount) {
        _requireNftOwner(nftId);
        _requireStrategy(nftId, 3); // LP_REWARDS = 3

        address pool = ISidioraNFT(nftContract).getPoolAddress(nftId);
        amount = IFeeAccumulator(feeAccumulator).sendLpRewards(pool);

        // Trigger syncReserves on pool so it picks up the new USDL
        ISidioraPool(pool).syncReserves();

        emit LpRewardsExecuted(nftId, amount);
        _mirrorDistribution(nftId, pool, 3, amount, pool);
    }

    function _mirrorDistribution(uint256 nftId, address pool, uint8 strategy, uint256 amount, address recipient) internal {
        _mirror(abi.encodeWithSelector(IEventEmitter.emitFeeDistributed.selector, bytes32(uint256(uint160(pool))), nftId, strategy, amount, recipient));
    }

    function _mirror(bytes memory payload) internal {
        (bool ok, bytes memory data) = feeAccumulator.staticcall(abi.encodeWithSignature("eventEmitter()"));
        if (!ok || data.length != 32) return;
        address hub = abi.decode(data, (address));
        if (hub == address(0)) return;
        (ok,) = hub.call(payload);
        if (!ok) {
            bytes4 selector;
            assembly { selector := mload(add(payload, 32)) }
            emit MirrorFailed(selector);
        }
    }

    // --- Internal ---

    function _requireNftOwner(uint256 nftId) internal view {
        (bool success, bytes memory data) = nftContract.staticcall(
            abi.encodeWithSignature("ownerOf(uint256)", nftId)
        );
        if (!success || data.length < 32) revert NotNftOwner();
        address owner = abi.decode(data, (address));
        if (owner != msg.sender) revert NotNftOwner();
    }

    function _requireStrategy(uint256 nftId, uint8 expectedStrategy) internal view {
        uint8 currentStrategy = ISidioraNFT(nftContract).getFeeStrategy(nftId);
        if (currentStrategy != expectedStrategy) revert WrongStrategy();
    }

    function _authorizeUpgrade(address) internal override onlyRole(DEFAULT_ADMIN_ROLE) {}
}
