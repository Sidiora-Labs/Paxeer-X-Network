// SPDX-License-Identifier: LicenseRef-Paxlabs-HyperPax-OS-Protocol
pragma solidity ^0.8.27;
import {IDataFeedAdapter} from "../meta-ag/interfaces/IDataFeedAdapter.sol";

/// @dev Test-only controllable USD reference. Production adapters require a freshness-enforcing oracle.
contract MockUsdReference {
    uint256 public price = 1e18;
    bool public stale;
    function setPrice(uint256 value) external { price = value; }
    function setStale(bool value) external { stale = value; }
    function getPrice(address) external view returns (uint256) { require(!stale, "stale"); return price; }
}

contract MockOracleMetadataAdapter is IDataFeedAdapter {
    bool public metadataReverts;
    bytes32 public immutable override sourceId;
    FeedPrice private _feed;
    constructor(bytes32 sid) { sourceId = sid; }
    function setFeed(uint256 price, uint256 timestamp, uint256 confidence) external { _feed = FeedPrice(price, timestamp, confidence, sourceId); }
    function setMetadataReverts(bool value) external { metadataReverts = value; }
    function adapterName() external pure returns (string memory) { return "audit"; }
    function maxStaleness() external view returns (uint256) { require(!metadataReverts, "metadata"); return 3600; }
    function supportsToken(address) external pure returns (bool) { return true; }
    function getSupportedTokens() external pure returns (address[] memory tokens) { return new address[](0); }
    function getFeedPrice(address) external view returns (FeedPrice memory) { return _feed; }
    function getFeedPrices(address[] calldata tokens) external view returns (FeedPrice[] memory feeds) { feeds = new FeedPrice[](tokens.length); for(uint256 i; i < tokens.length; ++i) feeds[i] = _feed; }
}
