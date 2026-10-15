// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

address constant PERPS_PRECOMPILE_ADDRESS = 0x0000000000000000000000000000000000001015;

IPerps constant PERPS_CONTRACT = IPerps(PERPS_PRECOMPILE_ADDRESS);

/// Perpetuals venue. Collateral is the USDL ERC20 (6 decimals); prices, USD
/// notionals, PnL and fees are 18-decimal fixed point; argument leverage is
/// 18-decimal, maxLeverage a plain integer. Margin is cross only. Every failure
/// reverts with one of the parameterless custom errors below.
interface IPerps {
    struct OrderParams {
        uint256 marketId;
        bool isLong;
        uint8 orderType;
        uint8 timeInForce;
        uint256 sizeUsd;
        uint256 leverage;
        uint256 limitPrice;
        uint256 triggerPrice;
        uint256 acceptablePrice;
        uint256 collateralAmount;
        bool reduceOnly;
        uint256 takeProfitPrice;
        uint256 stopLossPrice;
        uint64 deadline;
        bytes16 cloid;
    }

    struct AccountSummary {
        uint256 idle;
        uint256 totalLocked;
        uint256 positionCount;
    }

    struct AccountState {
        uint256 collateral;
        int256 equityUsd;
        uint256 initialMarginUsd;
        uint256 maintenanceMarginUsd;
        uint256 withdrawable;
        uint8 feeTier;
        uint256 volume14dUsd;
        uint256 positionCount;
        uint256 openOrderCount;
    }

    struct PositionMargin {
        address token;
        uint256 lockedAmount;
        uint256 marketId;
        bool isLong;
    }

    struct MarginTier {
        uint256 lowerBoundUsd;
        uint256 maxLeverage;
        uint256 maintenanceBps;
        uint256 deductionUsd;
    }

    struct OrderDetails {
        uint256 orderId;
        address user;
        uint256 marketId;
        bool isLong;
        uint8 orderType;
        uint8 timeInForce;
        uint8 status;
        bool reduceOnly;
        uint256 limitPrice;
        uint256 triggerPrice;
        uint256 acceptablePrice;
        uint256 sizeUsd;
        uint256 sizeBase;
        uint256 filledBase;
        uint256 leverage;
        uint64 deadline;
        bytes16 cloid;
        uint64 sequence;
    }

    struct PositionDetails {
        uint256 positionId;
        address user;
        uint256 marketId;
        bool isLong;
        uint256 sizeBase;
        uint256 sizeUsdAtMark;
        uint256 entryPrice;
        uint256 leverage;
        int256 unrealizedPnlUsd;
        int256 fundingOwedUsd;
        uint256 liquidationPrice;
        uint256 takeProfitPrice;
        uint256 stopLossPrice;
        uint256 openedAt;
    }

    struct MarketConfig {
        uint256 marketId;
        string symbol;
        string name;
        string oracleSymbol;
        uint8 szDecimals;
        uint8 pxDecimals;
        uint256 tickSize;
        uint256 lotSize;
        uint256 maxLeverage;
        uint8 tierClass;
        uint256 impactNotionalUsd;
        uint256 oiCapUsd;
        uint256 fundingCapPpmPerHour;
        bool enabled;
        uint64 listedAt;
    }

    struct MarketQuote {
        uint256 indexPrice;
        uint256 markPrice;
        uint256 oracleTWAP;
        int256 fundingRatePerSecond;
        int256 fundingRate24h;
        uint256 longOI;
        uint256 shortOI;
        uint256 maxLeverage;
        uint256 maintenanceMarginBps;
        bool enabled;
        bool priceStale;
    }

    struct OpenPositionQuote {
        uint256 entryPrice;
        uint256 sizeUsd;
        uint256 collateralUsd;
        uint256 leverage;
        uint256 tradingFee;
        uint256 tradingFeeUsd;
        uint256 priceImpact;
        uint256 liquidationPrice;
        int256 estimatedFunding24h;
        uint256 maintenanceMarginBps;
    }

    struct ClosePositionQuote {
        uint256 exitPrice;
        int256 unrealizedPnl;
        uint256 tradingFee;
        uint256 tradingFeeUsd;
        int256 fundingOwed;
        int256 netPnl;
        uint256 estimatedPayout;
    }

    event VaultCreated(address indexed user, address indexed vault);
    event CollateralDeposited(address indexed account, uint256 amount, uint256 newBalance);
    event CollateralWithdrawn(address indexed account, uint256 amount, uint256 newBalance);
    event CollateralTransferred(address indexed from, address indexed to, uint256 amount);
    event OrderPlaced(
        uint256 indexed orderId,
        address indexed user,
        uint256 indexed marketId,
        uint8 orderType,
        bool isLong,
        uint256 triggerPrice,
        uint256 sizeUsd
    );
    event OrderOptions(
        uint256 indexed orderId,
        uint8 timeInForce,
        bool reduceOnly,
        uint256 limitPrice,
        uint256 acceptablePrice,
        uint64 deadline,
        bytes16 cloid
    );
    event OrderCancelled(uint256 indexed orderId, address indexed user);
    event OrderExpired(uint256 indexed orderId, address indexed user, uint256 indexed marketId);
    event OrderTriggered(uint256 indexed orderId, uint256 indexed marketId, uint256 markPrice);
    event OrderExecuted(uint256 indexed orderId, uint256 indexed positionId, uint256 executionPrice);
    event Fill(
        uint256 indexed marketId,
        uint256 indexed makerOrderId,
        uint256 indexed takerOrderId,
        address maker,
        address taker,
        bool takerIsLong,
        uint256 price,
        uint256 sizeUsd,
        int256 makerFeeUsd,
        uint256 takerFeeUsd
    );
    event PositionOpened(
        uint256 indexed positionId,
        address indexed user,
        uint256 indexed marketId,
        bool isLong,
        uint256 sizeUsd,
        uint256 leverage,
        uint256 entryPrice,
        address collateralToken,
        uint256 collateralAmount
    );
    event PositionModified(
        uint256 indexed positionId, uint256 newSizeUsd, uint256 newCollateralUsd, uint256 newCollateralAmount
    );
    event PositionEntryPriceChanged(uint256 indexed positionId, uint256 oldEntryPrice, uint256 newEntryPrice);
    event PositionClosed(
        uint256 indexed positionId,
        address indexed user,
        uint256 indexed marketId,
        uint256 closedSizeUsd,
        uint256 exitPrice,
        int256 realizedPnl,
        bool isFullClose
    );
    event PositionTpSlSet(uint256 indexed positionId, uint256 takeProfitPrice, uint256 stopLossPrice);
    event TradeSettled(
        uint256 indexed positionId,
        address indexed user,
        uint256 indexed marketId,
        uint8 tradeType,
        uint256 sizeUsd,
        uint256 executionPrice,
        int256 grossPnl,
        uint256 totalFeesUsd,
        int256 fundingPaidUsd,
        uint256 netPayoutTokens
    );
    event FeeCollected(
        uint256 indexed positionId,
        address indexed user,
        uint256 indexed marketId,
        uint8 feeType,
        uint256 feeUsd,
        uint256 feeTokens,
        address token
    );
    event MarkPriceUpdated(uint256 indexed marketId, uint256 markPrice, uint256 indexPrice, uint256 midPrice);
    event FundingRateUpdated(uint256 indexed marketId, int256 newRatePerSecond, int256 fundingRate24h);
    event FundingSettled(uint256 indexed marketId, int256 fundingRate, int256 longPayment, int256 shortPayment);
    event PositionFundingApplied(
        uint256 indexed positionId,
        address indexed user,
        uint256 indexed marketId,
        int256 fundingPaymentUsd,
        uint256 newCollateralUsd,
        uint256 newCollateralAmount
    );
    event OpenInterestChanged(
        uint256 indexed marketId, uint256 longOI, uint256 shortOI, uint256 deltaUsd, bool isIncrease
    );
    event Liquidation(
        uint256 indexed positionId,
        address indexed user,
        uint256 indexed marketId,
        uint256 liquidationPrice,
        uint256 penalty,
        address keeper
    );
    event BackstopLiquidation(
        address indexed account, uint256 indexed marketId, uint256 sizeUsd, uint256 markPrice, uint256 marginTakenUsd
    );
    event InsuranceContribution(address indexed token, uint256 amount, uint8 source);
    event InsuranceDraw(uint256 indexed marketId, uint256 amount, uint256 newBalance);
    event VaultDeficit(address indexed token, uint256 deficit);
    event ADLExecuted(uint256 indexed positionId, uint256 deleveragedSizeUsd);
    event ADLMatched(
        uint256 indexed marketId,
        uint256 indexed positionId,
        address indexed account,
        uint256 sizeUsd,
        uint256 price,
        uint256 rankScore
    );

    function createVault() external returns (address vault);
    function getVault(address user) external view returns (address vault);
    function deposit(address token, uint256 amount) external;
    function withdraw(address token, uint256 amount) external;
    function emergencyWithdraw(address token) external;
    function transferCollateral(address to, uint256 amount) external;
    function getBalance(address token) external view returns (uint256 balance);
    function getBalance(address account, address token) external view returns (uint256 balance);
    function getLockedBalance(address token) external view returns (uint256 locked);
    function getLockedBalance(address account, address token) external view returns (uint256 locked);
    function getAccountSummary(address token) external view returns (AccountSummary memory summary);
    function getAccountSummary(address account, address token) external view returns (AccountSummary memory summary);
    function getAccount(address account) external view returns (AccountState memory state);
    function setMarginMode(uint8 mode) external;
    function getMarginMode() external view returns (uint8 mode);
    function getMarginMode(address account) external view returns (uint8 mode);
    function topUpPosition(uint256 positionId, uint256 amount) external;
    function transferMargin(uint256 fromPositionId, uint256 toPositionId, uint256 amount) external;
    function getPositionMargin(uint256 positionId) external view returns (PositionMargin memory margin);
    function getMarginTiers(uint256 marketId) external view returns (MarginTier[] memory tiers);
    function checkLiquidatable(uint256 positionId) external view returns (bool liquidatable, uint256 marginBps);
    function placeOrder(OrderParams calldata p) external returns (uint256 orderId);
    function placeLimitOrder(
        uint256 marketId,
        bool isLong,
        uint256 triggerPrice,
        uint256 sizeUsd,
        uint256 leverage,
        address collateralToken,
        uint256 collateralAmount
    ) external returns (uint256 orderId);
    function placeStopLimitOrder(
        uint256 marketId,
        bool isLong,
        uint256 triggerPrice,
        uint256 limitPrice,
        uint256 sizeUsd,
        uint256 leverage,
        address collateralToken,
        uint256 collateralAmount
    ) external returns (uint256 orderId);
    function cancelOrder(uint256 orderId) external;
    function cancelByCloid(uint256 marketId, bytes16 cloid) external;
    function getOrder(uint256 orderId)
        external
        view
        returns (
            address user,
            uint256 marketId,
            bool isLong,
            uint8 orderType,
            uint256 triggerPrice,
            uint256 limitPrice,
            uint256 sizeUsd,
            uint256 leverage,
            address collateralToken,
            uint256 collateralAmount,
            bool active
        );
    function getOrderDetails(uint256 orderId) external view returns (OrderDetails memory details);
    function getUserOrderIds(address user) external view returns (uint256[] memory orderIds);
    function placeTakeProfitOrder(uint256 positionId, uint256 triggerPrice) external returns (uint256 orderId);
    function placeStopLossOrder(uint256 positionId, uint256 triggerPrice) external returns (uint256 orderId);
    function setPositionTpSl(uint256 positionId, uint256 takeProfitPrice, uint256 stopLossPrice) external;
    function executeOrder(uint256 orderId) external returns (uint256 positionId);
    function openPosition(
        uint256 marketId,
        address collateralToken,
        uint256 collateralAmount,
        uint256 leverage,
        bool isLong
    ) external returns (uint256 positionId);
    function addSize(uint256 positionId, uint256 additionalCollateral, uint256 leverage) external;
    function addCollateral(uint256 positionId, uint256 amount) external;
    function removeCollateral(uint256 positionId, uint256 amount) external;
    function closePosition(uint256 positionId) external;
    function partialClose(uint256 positionId, uint256 closeSizeUsd) external;
    function closePositionWithParams(uint256 positionId, uint256 closeSizeUsd, uint256 acceptablePrice, uint64 deadline)
        external;
    function getPosition(uint256 positionId)
        external
        view
        returns (
            address user,
            uint256 marketId,
            bool isLong,
            uint256 sizeUsd,
            uint256 collateralUsd,
            address collateralToken,
            uint256 collateralAmount,
            uint256 entryPrice,
            uint256 timestamp,
            bool active
        );
    function getPositionDetails(uint256 positionId) external view returns (PositionDetails memory details);
    function getUserPositionIds(address user) external view returns (uint256[] memory positionIds);
    function getUserMarketPosition(address user, uint256 marketId) external view returns (uint256 positionId);
    function getMarket(uint256 marketId)
        external
        view
        returns (
            string memory name,
            string memory symbol,
            uint256 maxLeverage,
            uint256 maintenanceMarginBps,
            uint256 maxOpenInterest,
            bool enabled
        );
    function getMarketConfig(uint256 marketId) external view returns (MarketConfig memory config);
    function getActiveMarketIds() external view returns (uint256[] memory marketIds);
    function totalMarkets() external view returns (uint256 count);
    function isMarketActive(uint256 marketId) external view returns (bool active);
    function getFees()
        external
        view
        returns (uint256 takerFeePpm, uint256 makerFeePpm, uint256 liquidationFeePpm, uint256 insuranceSharePpm);
    function getFeeTier(address account)
        external
        view
        returns (uint8 tier, uint256 takerFeePpm, int256 makerFeePpm, uint256 volume14dUsd);
    function getProtocolAccounts() external view returns (address feeRecipient, address insuranceFund);
    function getMarkPrice(uint256 marketId) external view returns (uint256 markPrice);
    function getMarkPriceDetails(uint256 marketId)
        external
        view
        returns (uint256 markPrice, uint256 indexPrice, uint256 midPrice, bool fresh);
    function quoteMarket(uint256 marketId) external view returns (MarketQuote memory quote);
    function getCurrentFundingRate(uint256 marketId) external view returns (int256 ratePerSecond);
    function getFundingState(uint256 marketId)
        external
        view
        returns (int256 cumulativeLong, int256 cumulativeShort, uint256 lastUpdate, int256 ratePerSecond);
    function getFundingDetails(uint256 marketId)
        external
        view
        returns (
            int256 cumulativeFundingPerUnit,
            uint256 lastSettleTime,
            int256 lastHourlyRate,
            int256 premiumSampleSum,
            uint256 premiumSampleCount
        );
    function getPositionFunding(uint256 positionId) external view returns (int256 fundingOwedUsd);
    function getOpenInterest(uint256 marketId) external view returns (uint256 longOI, uint256 shortOI);
    function liquidate(uint256 positionId) external;
    function getLiquidationPrice(uint256 positionId) external view returns (uint256 liquidationPrice);
    function getInsuranceBalance(address token) external view returns (uint256 balance);
    function getADLRank(uint256 positionId) external view returns (uint256 rankScore, bool profitable);
    function quoteOpenPosition(
        uint256 marketId,
        address collateralToken,
        uint256 collateralAmount,
        uint256 leverage,
        bool isLong
    ) external view returns (OpenPositionQuote memory quote);
    function quoteClosePosition(uint256 positionId) external view returns (ClosePositionQuote memory quote);
    function quotePartialClose(uint256 positionId, uint256 closeSizeUsd)
        external
        view
        returns (uint256 exitPrice, int256 closedPnl, uint256 fee, uint256 estimatedPayout);

    error PerpsInvalidParams();
    error PerpsUnknownMarket();
    error PerpsMarketDisabled();
    error PerpsPriceStale();
    error PerpsZeroAmount();
    error PerpsUnsupportedCollateral();
    error PerpsInsufficientCollateral();
    error PerpsInsufficientMargin();
    error PerpsLeverageTooHigh();
    error PerpsTickSize();
    error PerpsLotSize();
    error PerpsMinOrderValue();
    error PerpsMaxOrderValue();
    error PerpsOpenInterestCap();
    error PerpsInvalidTimeInForce();
    error PerpsInvalidOrderType();
    error PerpsPostOnlyWouldCross();
    error PerpsFokNotFilled();
    error PerpsNoLiquidity();
    error PerpsSlippageExceeded();
    error PerpsDeadlineExpired();
    error PerpsReduceOnlyViolation();
    error PerpsDuplicateCloid();
    error PerpsUnknownOrder();
    error PerpsNotOrderHolder();
    error PerpsUnknownPosition();
    error PerpsNotPositionHolder();
    error PerpsInvalidTriggerPrice();
    error PerpsTriggerNotReached();
    error PerpsTooManyOpenOrders();
    error PerpsCrossMarginOnly();
    error PerpsNotLiquidatable();
    error PerpsLiquidationCooldown();
    error PerpsUnauthorized();
    error PerpsStaticCall();
    error PerpsDelegatecall();
    error PerpsOverflow();
    error PerpsInvalidGenesis();
    error PerpsCloseSizeExceedsPosition();
    error PerpsSelfTransfer();
    error PerpsZeroAddress();
    error PerpsNoAccount();
    error PerpsTokenTransferFailed();
    error PerpsPaused();
}
