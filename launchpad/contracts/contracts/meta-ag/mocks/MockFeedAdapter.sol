// SPDX-License-Identifier: LicenseRef-Paxlabs-HyperPax-OS-Protocol
pragma solidity ^0.8.27;

import {IDataFeedAdapter} from "../interfaces/IDataFeedAdapter.sol";

/// @title MockFeedAdapterBase
/// @notice Shared test-only scaffolding for OracleHub/aggregator tests.
/// @dev IDataFeedAdapter declares {sourceId} and {adapterName} as `pure`, so per-instance
///      identifiers cannot live in storage. Concrete mocks (A/B/C) hardcode their ids and
///      inherit the stateful feed-price plumbing below.
///
///      Note on file location: the Phase 2 plan calls out `test/meta-ag/mocks/`, but Hardhat
///      2.28's `paths.sources` is typed `string | undefined`, so mocks must live under the
///      default `contracts/` root. Functional parity is preserved — see commit log.
abstract contract MockFeedAdapterBase is IDataFeedAdapter {
    uint256 internal _maxStaleness;
    address[] internal _supportedTokens;
    mapping(address => FeedPrice) internal _feeds;
    mapping(address => bool) internal _supports;
    bool public revertOnGet;

    constructor(uint256 maxStaleness_) {
        _maxStaleness = maxStaleness_;
    }

    // Abstract metadata (overridden pure in concrete mocks)
    function sourceId() external pure virtual override returns (bytes32);

    function adapterName() external pure virtual override returns (string memory);

    // Test hooks

    function setPrice(
        address token,
        uint256 price,
        uint256 timestamp,
        uint256 confidence
    ) external {
        _feeds[token] = FeedPrice({
            price: price,
            timestamp: timestamp,
            confidence: confidence,
            sourceId: _sourceIdInternal()
        });
        if (!_supports[token]) {
            _supports[token] = true;
            _supportedTokens.push(token);
        }
    }

    function setSupported(address token, bool supported) external {
        _supports[token] = supported;
        if (supported) {
            for (uint256 i = 0; i < _supportedTokens.length; ++i) {
                if (_supportedTokens[i] == token) return;
            }
            _supportedTokens.push(token);
        }
    }

    function setMaxStaleness(uint256 s) external {
        _maxStaleness = s;
    }

    function setRevertOnGet(bool v) external {
        revertOnGet = v;
    }

    // IDataFeedAdapter views

    function getFeedPrice(address token) external view override returns (FeedPrice memory feed) {
        if (revertOnGet) revert("MockFeedAdapter: forced revert");
        return _feeds[token];
    }

    function supportsToken(address token) external view override returns (bool) {
        return _supports[token];
    }

    function getFeedPrices(
        address[] calldata tokens
    ) external view override returns (FeedPrice[] memory feeds) {
        feeds = new FeedPrice[](tokens.length);
        for (uint256 i = 0; i < tokens.length; ++i) {
            feeds[i] = _feeds[tokens[i]];
        }
    }

    function maxStaleness() external view override returns (uint256) {
        return _maxStaleness;
    }

    function getSupportedTokens() external view override returns (address[] memory) {
        return _supportedTokens;
    }

    // @dev internal stub so concrete mocks can tag stored feeds with their own sourceId
    function _sourceIdInternal() internal pure virtual returns (bytes32);
}

/// @notice Mock feed adapter keyed to `keccak256("MockFeedAdapter.A.v1")`.
contract MockFeedAdapterA is MockFeedAdapterBase {
    bytes32 private constant _SID = keccak256("MockFeedAdapter.A.v1");

    constructor(uint256 maxStaleness_) MockFeedAdapterBase(maxStaleness_) {}

    function sourceId() external pure override returns (bytes32) {
        return _SID;
    }

    function adapterName() external pure override returns (string memory) {
        return "MockFeedAdapter.A.v1";
    }

    function _sourceIdInternal() internal pure override returns (bytes32) {
        return _SID;
    }
}

/// @notice Mock feed adapter keyed to `keccak256("MockFeedAdapter.B.v1")`.
contract MockFeedAdapterB is MockFeedAdapterBase {
    bytes32 private constant _SID = keccak256("MockFeedAdapter.B.v1");

    constructor(uint256 maxStaleness_) MockFeedAdapterBase(maxStaleness_) {}

    function sourceId() external pure override returns (bytes32) {
        return _SID;
    }

    function adapterName() external pure override returns (string memory) {
        return "MockFeedAdapter.B.v1";
    }

    function _sourceIdInternal() internal pure override returns (bytes32) {
        return _SID;
    }
}

/// @notice Mock feed adapter keyed to `keccak256("MockFeedAdapter.C.v1")`.
contract MockFeedAdapterC is MockFeedAdapterBase {
    bytes32 private constant _SID = keccak256("MockFeedAdapter.C.v1");

    constructor(uint256 maxStaleness_) MockFeedAdapterBase(maxStaleness_) {}

    function sourceId() external pure override returns (bytes32) {
        return _SID;
    }

    function adapterName() external pure override returns (string memory) {
        return "MockFeedAdapter.C.v1";
    }

    function _sourceIdInternal() internal pure override returns (bytes32) {
        return _SID;
    }
}
