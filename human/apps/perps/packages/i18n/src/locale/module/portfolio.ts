export const portfolio = {
  "portfolio.feeTier": "Fee tier",
  "portfolio.apiKeys": "API Keys",
  "portfolio.setting": "Settings",

  "portfolio.overview.availableWithdraw": "Available to withdraw",
  "portfolio.overview.handle.title": "Portfolio value",

  "portfolio.overview.performance": "Performance",
  "portfolio.overview.performance.roi": "{{period}} ROI",
  "portfolio.overview.performance.roi.tooltip":
    "ROI = PnL / (Start account value + Total deposits + Total transfer-in)",
  "portfolio.overview.performance.pnl": "{{period}} PnL",
  "portfolio.overview.performance.pnl.tooltip":
    "PnL = Current account value − Start account value − Total deposits + Total withdrawals − Total transfer-in + Total transfer-out",
  "portfolio.overview.performance.volume": "{{period}} Volume (USDC)",
  "portfolio.overview.performance.volume.tooltip":
    "Trading volume over the selected period.",
  "portfolio.overview.performance.dailyPnl": "Daily PnL",
  "portfolio.overview.performance.cumulativePnl": "Cumulative PnL",
  "portfolio.overview.performance.dailyVolume": "Daily Volume",
  "portfolio.overview.performance.cumulativeVolume": "Cumulative Volume",

  "portfolio.overview.todaysPnl": "Today's PnL",
  "portfolio.overview.todaysVol": "Today's Vol",

  "portfolio.overview.distribution": "Distribution",
  "portfolio.overview.transferHistory": "Transfer history",
  "portfolio.overview.vaults": "Vaults",
  "portfolio.overview.vaultName": "Vault name",
  "portfolio.overview.column.txId": "TxID",

  "portfolio.overview.column.token": "Token",
  "portfolio.overview.column.qty": "Qty.",
  "portfolio.overview.column.indexPrice": "Index price",
  "portfolio.overview.column.assetValue": "Asset value",
  "portfolio.overview.column.collateralRatio": "Collateral ratio",
  "portfolio.overview.column.collateralRatio.explain":
    "Collateral ratio shows how much of an asset’s value can be used as margin",
  "portfolio.overview.column.assetContribution": "Asset contribution",

  "portfolio.overview.column.form": "Form",
  "portfolio.overview.column.to": "To",

  "portfolio.overview.column.convert.convertedAsset": "Converted asset",
  "portfolio.overview.column.convert.receivedAmount": "Received amount",
  "portfolio.overview.column.convert.convertId": "Convert ID",
  "portfolio.overview.column.convert.details": "Details",
  "portfolio.overview.tab.convert.history": "Convert history",
  "portfolio.overview.convert.dialog.title.details": "Convert details",

  "portfolio.overview.distribution.type.referralCommission":
    "Referral commission",
  "portfolio.overview.distribution.type.refereeRebate": "Referee rebate",
  "portfolio.overview.distribution.type.brokerFee": "Broker fee",

  "portfolio.feeTier.updatedDailyBy": "Updated daily by",
  "portfolio.feeTier.header.yourTier": "Your tier",
  "portfolio.feeTier.header.30dVolume": "30D trading volume",
  "portfolio.feeTier.header.takerFeeRate": "Taker fee rate",
  "portfolio.feeTier.header.makerFeeRate": "Maker fee rate",
  "portfolio.feeTier.header.myStake": "My stake",
  "portfolio.feeTier.column.tier": "Tier",
  "portfolio.feeTier.column.30dVolume": "30 day volume",
  "portfolio.feeTier.column.30dVolume.above": "Above {{volume}}",
  "portfolio.feeTier.column.stakingLevel": "Staking Level",
  "portfolio.feeTier.column.or": "or",
  "portfolio.feeTier.column.maker": "Maker / RWA Maker",
  "portfolio.feeTier.column.taker": "Taker / RWA Taker",
  "portfolio.apiKey.accountId.copied": "Account id copied",
  "portfolio.apiKey.uid": "UID",
  "portfolio.apiKey.secretKey": "Secret key",
  "portfolio.apiKey.secretKey.copied": "Secret key copied",

  "portfolio.apiKey.ip": "IP",
  "portfolio.apiKey.permissions": "Permissions",
  "portfolio.apiKey.permissions.read": "Read",
  "portfolio.apiKey.permissions.trading": "Trading",

  "portfolio.apiKey.description":
    "Create API keys to suit your trading needs. For your security, don't share your API keys with anyone.",
  "portfolio.apiKey.readApiGuide": "Read API guide",

  "portfolio.apiKey.column.apiKey": "API key",
  "portfolio.apiKey.column.apiKey.copy": "API key copied",
  "portfolio.apiKey.column.permissionType": "Permission type",
  "portfolio.apiKey.column.restrictedIP": "Restricted IP",
  "portfolio.apiKey.column.restrictedIP.copy": "Restricted IP copied",
  "portfolio.apiKey.column.expirationDate": "Expiration date",

  "portfolio.apiKey.create.dialog.title": "Create API key",

  "portfolio.apiKey.create.connectWallet.tooltip":
    "Please connect wallet before create API key",
  // "portfolio.apiKey.create.signIn.tooltip":
  //   "Please sign in before create API key",
  "portfolio.apiKey.create.createAccount.tooltip":
    "Please create account before create API key",
  "portfolio.apiKey.create.enableTrading.tooltip":
    "Please enable trading before create API key",
  "portfolio.apiKey.create.wrongNetwork.tooltip":
    "Please switch to a supported network to create API key",

  "portfolio.apiKey.create.ipRestriction": "IP restriction (optional)",
  "portfolio.apiKey.create.ipRestriction.placeholder":
    "Add IP addresses, separated by commas.",

  "portfolio.apiKey.created.warning":
    "Please copy the API secret. Once you close this pop-up, the API secret will be encrypted.",
  "portfolio.apiKey.created.button.copyApiInfo": "Copy API info",
  "portfolio.apiKey.apiInfo.copied": "API info copied",

  "portfolio.apiKey.created": "API key created",
  "portfolio.apiKey.deleted": "API key deleted",
  "portfolio.apiKey.updated": "API key updated",

  "portfolio.apiKey.edit.dialog.title": "Edit API key",
  "portfolio.apiKey.delete.dialog.title": "Delete API key",
  "portfolio.apiKey.delete.dialog.description":
    "Delete your API key <0>{{apiKey}}</0>?",

  "portfolio.setting.systemUpgrade": "System upgrade",
  "portfolio.setting.cancelOpenOrders":
    "Cancel open orders during system upgrade",
  "portfolio.setting.cancelOpenOrders.description":
    "During the upgrade period, all open orders will be cancelled to manage your risk in case of high market volatility.",
  "portfolio.setting.soundAlerts": "Sound notification",
  "portfolio.setting.soundAlerts.description":
    "Play a notification sound when an order is filled or canceled.",
  "portfolio.setting.theme": "Theme",
};

export type Portfolio = typeof portfolio;
