// SPDX-License-Identifier: LicenseRef-Paxlabs-HyperPax-OS-Protocol
pragma solidity ^0.8.24;

import "../base/Initializable.sol";
import "../base/UUPSUpgradeable.sol";
import "../base/AccessControl.sol";
import "../interfaces/IPoolRegistry.sol";
import "../interfaces/IEventEmitter.sol";

/// @title PoolRegistry
/// @notice On-chain pool discovery and metadata storage
/// @dev UUPS proxy. Only FACTORY_ROLE can register pools.
contract PoolRegistry is IPoolRegistry, Initializable, UUPSUpgradeable, AccessControl {
    event MirrorFailed(bytes4 indexed selector);
    bytes32 public constant FACTORY_ROLE = keccak256("FACTORY_ROLE");

    mapping(address => address) private _tokenToPool;
    mapping(address => uint256) private _poolToNftId;
    mapping(address => address[]) private _creatorToPools;
    mapping(address => PoolMetadata) private _poolMetadata;
    mapping(address => bool) private _registeredPools;

    address[] private _allPools;

    address public eventEmitter;

    /// @custom:oz-upgrades-unsafe-allow constructor
    constructor() {
        _disableInitializers();
    }

    function initialize(address _eventEmitter, address _admin) external initializer {
        if (_admin == address(0)) revert ZeroAddress();
        eventEmitter = _eventEmitter;
        _grantRole(DEFAULT_ADMIN_ROLE, _admin);
    }

    /// @inheritdoc IPoolRegistry
    function register(
        address pool,
        address token,
        address creator,
        address optical,
        uint256 nftId
    ) external override onlyRole(FACTORY_ROLE) {
        if (pool == address(0) || token == address(0) || creator == address(0)) revert ZeroAddress();
        if (_tokenToPool[token] != address(0)) revert DuplicateToken();
        if (_registeredPools[pool]) revert DuplicatePool();

        _tokenToPool[token] = pool;
        _poolToNftId[pool] = nftId;
        _creatorToPools[creator].push(pool);
        _registeredPools[pool] = true;

        _poolMetadata[pool] = PoolMetadata({
            creator: creator,
            token: token,
            optical: optical,
            nftId: nftId,
            createdAt: block.timestamp,
            createdBlock: block.number
        });

        _allPools.push(pool);

        emit PoolRegistered(pool, token, creator, optical, nftId, block.timestamp);
        if (eventEmitter != address(0)) {
            try IEventEmitter(eventEmitter).emitPoolRegistered(pool, token, creator, optical, nftId) {}
            catch { emit MirrorFailed(IEventEmitter.emitPoolRegistered.selector); }
        }
    }

    /// @inheritdoc IPoolRegistry
    function getPoolByToken(address token) external view override returns (address) {
        return _tokenToPool[token];
    }

    /// @inheritdoc IPoolRegistry
    function getPoolsByCreator(address creator) external view override returns (address[] memory) {
        return _creatorToPools[creator];
    }

    /// @inheritdoc IPoolRegistry
    function getNftIdByPool(address pool) external view override returns (uint256) {
        return _poolToNftId[pool];
    }

    /// @inheritdoc IPoolRegistry
    function getPoolMetadata(address pool) external view override returns (PoolMetadata memory) {
        return _poolMetadata[pool];
    }

    /// @inheritdoc IPoolRegistry
    function getAllPools(uint256 offset, uint256 limit) external view override returns (address[] memory) {
        uint256 total = _allPools.length;
        if (offset >= total) {
            return new address[](0);
        }

        uint256 end = offset + (limit > total - offset ? total - offset : limit);

        uint256 resultLength = end - offset;
        address[] memory result = new address[](resultLength);
        for (uint256 i = 0; i < resultLength; i++) {
            result[i] = _allPools[offset + i];
        }
        return result;
    }

    /// @inheritdoc IPoolRegistry
    function getPoolCount() external view override returns (uint256) {
        return _allPools.length;
    }

    /// @inheritdoc IPoolRegistry
    function isRegisteredPool(address pool) external view override returns (bool) {
        return _registeredPools[pool];
    }

    function getPoolCountByCreator(address creator) external view override returns (uint256) {
        return _creatorToPools[creator].length;
    }

    function getPoolsByCreatorPage(address creator, uint256 offset, uint256 limit)
        external view override returns (address[] memory result)
    {
        address[] storage pools = _creatorToPools[creator];
        if (offset >= pools.length) return new address[](0);
        uint256 count = limit > pools.length - offset ? pools.length - offset : limit;
        result = new address[](count);
        for (uint256 i; i < count; ++i) result[i] = pools[offset + i];
    }

    function _authorizeUpgrade(address) internal override onlyRole(DEFAULT_ADMIN_ROLE) {}
}
