// SPDX-License-Identifier: LicenseRef-Paxlabs-HyperPax-OS-Protocol
pragma solidity ^0.8.24;

import "../base/ERC721Enumerable.sol";
import "../base/Initializable.sol";
import "../base/UUPSUpgradeable.sol";
import "../base/AccessControl.sol";
import "../interfaces/ISidioraNFT.sol";
import "../interfaces/IEventEmitter.sol";

/// @title SidioraNFT
/// @notice ERC721 representing fee rights for a pool. One NFT per pool.
/// @dev UUPS proxy singleton. Factory mints via MINTER_ROLE.
contract SidioraNFT is ISidioraNFT, ERC721Enumerable, Initializable, UUPSUpgradeable, AccessControl {
    bytes32 public constant MINTER_ROLE = keccak256("MINTER_ROLE");
    bytes32 public constant STRATEGY_SETTER_ROLE = keccak256("STRATEGY_SETTER_ROLE");

    uint256 public override nextTokenId;

    mapping(uint256 => uint8) private _feeStrategy;
    mapping(uint256 => address) private _poolAddress;

    address public eventEmitter;
    // Appended storage: one fee-right NFT per pool.
    mapping(address => uint256) public poolTokenId;
    error DuplicatePool();
    error UnsupportedSnapshotToken();
    error PoolIndexMigrationRequired();
    uint256 public poolIndexMigrationCursor;
    bool public poolIndexReady;

    /// @custom:oz-upgrades-unsafe-allow constructor
    constructor() ERC721Base("", "") {
        _disableInitializers();
    }

    function initialize(
        string calldata _name,
        string calldata _symbol,
        address _eventEmitter,
        address _admin
    ) external initializer {
        if (_admin == address(0)) revert ZeroAddress();
        name = _name;
        symbol = _symbol;
        eventEmitter = _eventEmitter;
        nextTokenId = 1; // Start from 1
        poolIndexReady = true;
        _grantRole(DEFAULT_ADMIN_ROLE, _admin);
    }

    /// @inheritdoc ISidioraNFT
    function mint(
        address to,
        address pool,
        uint8 strategy
    ) external override onlyRole(MINTER_ROLE) returns (uint256 tokenId) {
        if (to == address(0)) revert ZeroAddress();
        if (pool == address(0)) revert ZeroAddress();
        if (strategy > 3) revert InvalidStrategy();
        if (strategy == 2) _requireAirdropSupported(pool);

        if (!poolIndexReady) revert PoolIndexMigrationRequired();
        if (poolTokenId[pool] != 0) revert DuplicatePool();
        tokenId = nextTokenId++;
        poolTokenId[pool] = tokenId;

        _feeStrategy[tokenId] = strategy;
        _poolAddress[tokenId] = pool;
        _mint(to, tokenId);
        if (to.code.length > 0) {
            try IERC721Receiver(to).onERC721Received(msg.sender, address(0), tokenId, "") returns (bytes4 result) {
                if (result != IERC721Receiver.onERC721Received.selector) revert NonERC721Receiver();
            } catch (bytes memory reason) {
                if (reason.length == 0) revert NonERC721Receiver();
                assembly { revert(add(reason, 32), mload(reason)) }
            }
        }

        emit PoolNFTMinted(tokenId, to, pool);
        if (eventEmitter != address(0)) IEventEmitter(eventEmitter).emitNftMint(tokenId, to, pool, strategy);
    }

    /// @notice Populate the appended uniqueness index after an upgrade, in bounded batches.
    /// @dev Duplicate legacy rights fail closed and require an explicit reconciliation decision.
    function migratePoolIndex(uint256 maxItems) external onlyRole(DEFAULT_ADMIN_ROLE) {
        if (poolIndexReady) return;
        if (maxItems == 0 || maxItems > 1000) revert PoolIndexMigrationRequired();
        uint256 cursor = poolIndexMigrationCursor;
        if (cursor == 0) cursor = 1;
        uint256 remaining = nextTokenId - cursor;
        uint256 end = cursor + (maxItems < remaining ? maxItems : remaining);
        for (; cursor < end; ++cursor) {
            address pool = _poolAddress[cursor];
            if (poolTokenId[pool] != 0 && poolTokenId[pool] != cursor) revert DuplicatePool();
            poolTokenId[pool] = cursor;
        }
        poolIndexMigrationCursor = cursor;
        if (cursor == nextTokenId) poolIndexReady = true;
    }

    /// @inheritdoc ISidioraNFT
    function getFeeStrategy(uint256 tokenId) external view override returns (uint8) {
        if (!_exists(tokenId)) revert TokenNotFound();
        return _feeStrategy[tokenId];
    }

    /// @inheritdoc ISidioraNFT
    function setFeeStrategy(uint256 tokenId, uint8 newStrategy) external override {
        if (!_exists(tokenId)) revert TokenNotFound();
        if (newStrategy > 3) revert InvalidStrategy();

        // Only token owner, approved, or STRATEGY_SETTER_ROLE can change
        if (!_isApprovedOrOwner(msg.sender, tokenId) && !hasRole(STRATEGY_SETTER_ROLE, msg.sender)) {
            revert NotApproved();
        }

        if (newStrategy == 2) _requireAirdropSupported(_poolAddress[tokenId]);
        uint8 oldStrategy = _feeStrategy[tokenId];
        _feeStrategy[tokenId] = newStrategy;

        emit FeeStrategyChanged(tokenId, oldStrategy, newStrategy);
        if (eventEmitter != address(0)) IEventEmitter(eventEmitter).emitFeeStrategyChanged(
            bytes32(uint256(uint160(_poolAddress[tokenId]))), tokenId, oldStrategy, newStrategy);
    }

    /// @inheritdoc ISidioraNFT
    function getPoolAddress(uint256 tokenId) external view override returns (address) {
        if (!_exists(tokenId)) revert TokenNotFound();
        return _poolAddress[tokenId];
    }

    function _requireAirdropSupported(address pool) internal view {
        (bool ok, bytes memory data) = pool.staticcall(abi.encodeWithSignature("feeAccumulator()"));
        if (!ok || data.length != 32) revert UnsupportedSnapshotToken();
        address accumulator = abi.decode(data, (address));
        if (accumulator.code.length == 0) revert UnsupportedSnapshotToken();
        (ok, data) = accumulator.staticcall(abi.encodeWithSignature("supportsAirdrop(address)", pool));
        if (!ok || data.length != 32 || !abi.decode(data, (bool))) revert UnsupportedSnapshotToken();
    }

    function _transfer(address from, address to, uint256 tokenId) internal override {
        super._transfer(from, to, tokenId);
        if (eventEmitter != address(0)) IEventEmitter(eventEmitter).emitNftTransfer(address(this), from, to, tokenId);
    }

    /// @notice ERC165 override
    function supportsInterface(bytes4 interfaceId) public view virtual override returns (bool) {
        return ERC721Enumerable.supportsInterface(interfaceId);
    }

    function _authorizeUpgrade(address) internal override onlyRole(DEFAULT_ADMIN_ROLE) {}
}
