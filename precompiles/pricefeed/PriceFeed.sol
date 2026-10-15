// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

address constant PRICEFEED_PRECOMPILE_ADDRESS = 0x0000000000000000000000000000000000001020;

IPriceFeed constant PRICEFEED_CONTRACT = IPriceFeed(PRICEFEED_PRECOMPILE_ADDRESS);

/// Validator-aggregated market prices on Paxeer X Network.
///
/// Each bonded validator operator registers one 33-byte compressed secp256k1
/// feed key with registerFeedKey, called directly from the operator's
/// associated EVM address; the key's Cosmos account is the feed account.
/// Prices are submitted by the validators' sidecars as Cosmos MsgSubmitPrices
/// transactions, not through this precompile. After each block's transactions
/// the module aggregates, per market, the latest fresh observations of the
/// bonded set into one index price (median) and one mark price.
///
/// Every price is an unsigned integer with 18 decimals. `validator` is the 20
/// bytes of the operator address and `feedAccount` the 20 bytes of the feed
/// account. A price is fresh when its aggregate exists, the market is enabled
/// and the aggregate is no older than maxPriceAgeMs. getPrices takes at most 64
/// ids and returns the prices in input order. Refusals revert with
/// Error(string) whose text starts with an LXP_PRICEFEED_ code. Gas = base +
/// 16 * len(input), where base is 3000 for views and 30000 for registerFeedKey
/// and revokeFeedKey.
interface IPriceFeed {
    struct Price {
        uint256 indexPrice;
        uint256 markPrice;
        uint64 timestampMs;
        int64 height;
        uint32 sources;
        bool fresh;
    }

    struct Market {
        uint64 marketId;
        string symbol;
        string chainflowSymbol;
        uint256 minPrice;
        uint256 maxPrice;
        bool enabled;
    }

    struct Observation {
        uint64 sequence;
        uint256 indexPrice;
        uint256 markPrice;
        uint64 sourceTimestampMs;
        int64 height;
    }

    struct Params {
        uint64 stalenessWindowMs;
        uint64 maxClockSkewMs;
        uint32 minSources;
        uint32 maxDivergenceBps;
        uint32 maxDeviationBps;
        uint32 maxObservationsPerSubmission;
        uint32 maxSubmissionBytes;
        uint64 markEmaTauMs;
        uint32 maxMarkBasisBps;
        uint64 maxPriceAgeMs;
    }

    event FeedKeyRegistered(
        address indexed validator, address indexed feedAccount, bytes pubKey, address previousFeedAccount
    );
    event FeedKeyRevoked(address indexed validator, address indexed feedAccount);

    function registerFeedKey(bytes calldata pubKey) external returns (address feedAccount);

    function revokeFeedKey() external;

    function getPrice(uint256 marketId) external view returns (Price memory);

    function getPriceBySymbol(string calldata symbol) external view returns (Price memory);

    function getPrices(uint256[] calldata marketIds) external view returns (Price[] memory);

    function getMarket(uint256 marketId) external view returns (Market memory);

    function getMarketCount() external view returns (uint256);

    function marketIdOf(string calldata symbol) external view returns (uint256);

    function getObservation(uint256 marketId, address validator) external view returns (Observation memory);

    function getFeedKey(address validator) external view returns (bytes memory pubKey, address feedAccount);

    function getValidatorByFeedAccount(address feedAccount) external view returns (address validator);

    function getParams() external view returns (Params memory);
}
