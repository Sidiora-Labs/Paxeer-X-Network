export const widget = {
  "assetHistory.status.pending": "Pending",
  "assetHistory.status.confirm": "Confirm",
  "assetHistory.status.processing": "Processing",
  "assetHistory.status.completed": "Completed",
  "assetHistory.status.failed": "Failed",
  "assetHistory.status.pendingRebalance": "Pending rebalance",

  /** linkDevice */
  "linkDevice.createQRCode.loading.description":
    "Approve QR code with wallet...",
  "linkDevice.createQRCode.linkMobileDevice": "Link Mobile Device",
  "linkDevice.createQRCode.linkMobileDevice.description":
    "Open {{hostname}} on your mobile device and scan the QR code to link this wallet. For security, the QR code will expire in 60 seconds. <br/> The QR code allows mobile trading but does not enable withdrawals. Ensure you are not sharing your screen or any screenshots of the QR code.",

  "linkDevice.createQRCode.success.description":
    "Scan the QR code or paste the URL into another browser/<br/>device to continue.",
  "linkDevice.createQRCode.success.copyUrl": "Copy URL",

  "linkDevice.scanQRCode": "Scan QR Code",
  "linkDevice.scanQRCode.description":
    "Click the <0/> icon in the top right corner on desktop to generate a QR code to scan.",
  "linkDevice.scanQRCode.tooltip": "Link to Desktop via QR Code",
  "linkDevice.scanQRCode.connected.description":
    "You are connected via another device. This mode is for trading only. To switch networks, deposit or withdraw assets, please disconnect and reconnect your wallet on this device.",
  "linkDevice.tooltip": "Link mobile device",

  /** settle */
  "settle.settlePnl": "Settle PnL",
  "settle.settlePnl.warning": "Please settle your balance",
  "settle.settlePnl.description":
    "Are you sure you want to settle your PnL? <br/> Settlement will take up to 1 minute before you can withdraw your available balance.",

  "settle.unsettled": "Unsettled",
  "settle.unsettled.tooltip":
    "Unsettled balance can not be withdrawn. In order to withdraw, please settle your balance first.",

  "settle.settlement.requested": "Settlement requested",
  "settle.settlement.completed": "Settlement completed",
  "settle.settlement.failed": "Settlement failed",
  "settle.settlement.error":
    "Settlement is only allowed once every 10 minutes. Please try again later.",

  "languageSwitcher.language": "Language",
  "languageSwitcher.tooltip": "Switch language",
  "languageSwitcher.tips":
    "AI-generated translations may not be fully accurate.",

  "announcement.type.listing": "Listing",
  "announcement.type.maintenance": "Maintenance",
  "announcement.type.delisting": "Delisting",

  "maintenance.dialog.title": "System upgrade in progress",
  "maintenance.dialog.description":
    "Sorry, {{brokerName}} is temporarily unavailable due to a scheduled upgrade. The service is expected to be back by {{endDate}}.",
  "maintenance.tips.description":
    "{{brokerName}} will be temporarily unavailable for a scheduled upgrade from {{startDate}} to {{endDate}}.",

  "restrictedInfo.description.default":
    " You are accessing {{brokerName}} from an IP address ({{ip}}) associated with a restricted country.",
  "restrictedInfo.accessRestricted": "Access Restricted",
  "restrictedInfo.accessRestricted.description":
    "Due to laws and regulations, we currently do not operate in the United States. By continuing to use our platform, you represent and warrant at all times that you are not a resident of the United States throughout the period of such use.",
  "restrictedInfo.accessRestricted.agree": "I understand and agree",

  "subAccount.modal.title": "Switch account",
  "subAccount.modal.switch.success.description": "Account switched",
  "subAccount.modal.mainAccount.title": "Main account",
  "subAccount.modal.subAccounts.title": "Sub-accounts",
  "subAccount.modal.current": "Current",
  "subAccount.modal.noAccount.description":
    "Create a sub-account now to explore different trading strategies.",
  "subAccount.modal.create.max.description":
    "You have reached the maximum limit of 10 sub-accounts.",
  "subAccount.modal.create.title": "Create sub-account",
  "subAccount.modal.create.description":
    "You have {{subAccountCount}} sub-accounts. {{remainingCount}} more can be created.",
  "subAccount.modal.create.nickname.role":
    "1-20 characters. Only letters, numbers, and @ , _ - (space) allowed.",
  "subAccount.modal.create.success.description":
    "Sub-account created successfully.",
  "subAccount.modal.create.failed.description": "Failed to create sub-account.",
  "subAccount.modal.edit.title": "Edit nickname",
  "subAccount.modal.nickName.label": "Sub-account nickname",
  "subAccount.modal.edit.success.description": "Nickname updated successfully.",
  "subAccount.modal.edit.failed.description": "Failed to update nickname.",

  "funding.fundingFee": "Funding fee",
  "funding.fundingRate": "Funding rate",
  "funding.annualRate": "Annual rate",
  "funding.paymentType": "Payment type",
  "funding.paymentType.paid": "Paid",
  "funding.paymentType.received": "Received",

  "leftNav.feedback": "Share your feedback",

  "errorBoundary.title": "Page Crashed",
  "errorBoundary.description":
    "The page is currently unavailable. Please refresh and try again.",
  "errorBoundary.refresh": "Refresh",
  "dmm.maker": "Maker",
  "dmm.taker": "Taker",
};
