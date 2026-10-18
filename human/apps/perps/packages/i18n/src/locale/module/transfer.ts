export const transfer = {
  "transfer.reverseConvert.available":
    "You have <0>{{amount}}</0> USDC available.",
  "transfer.reverseConvert.action": "Convert to USDG",
  "transfer.reverseConvert.estimate":
    "Estimated amount. Final received amount depends on execution.",
  "transfer.reverseConvert.unavailable":
    "USDC to USDG conversion is unavailable on this network or token data is not ready.",

  "transfer.network": "Network",
  "transfer.lowestFee": "lowest fee",
  "transfer.web3Wallet.your": "Your Web3 Wallet",
  "transfer.web3Wallet.my": "My Web3 wallet",

  "transfer.brokerAccount": "Your {{brokerName}} account",
  "transfer.quantity.invalid": "Please input a valid number",

  "transfer.insufficientBalance": "Insufficient balance",
  "transfer.insufficientAllowance": "Insufficient allowance",
  "transfer.rejectTransaction": "Rejected transaction",

  "transfer.deposit.approve": "Approve",
  "transfer.deposit.approve.success": "Approve success",
  "transfer.deposit.approve.failed": "Approve failed",
  "transfer.deposit.requested": "Deposit requested",
  "transfer.deposit.completed": "Deposit completed",
  "transfer.deposit.failed": "Deposit failed",
  "transfer.exclusiveDeposit.warning":
    "Only send {{token}} on {{network}} to this address. Sending other tokens, using other chains, or amounts below the minimum deposit will result in permanent loss of funds.",
  "transfer.exclusiveDeposit.selectFirst":
    "Select your network and token first",
  "transfer.exclusiveDeposit.selectNetwork": "Select network",
  "transfer.exclusiveDeposit.selectToken": "Select token",
  "transfer.exclusiveDeposit.minDeposit": "Min. deposit",
  "transfer.exclusiveDeposit.estimatedTime": "Estimated time",
  "transfer.exclusiveDeposit.estimatedTime.default": "~ 5 min",
  "transfer.exclusiveDeposit.depositPending":
    "Deposit pending: {{amount}} {{symbol}}",
  "transfer.deposit.feeUnavailable":
    "Fee data is currently unavailable. Please try again later.",
  "transfer.deposit.notEnoughGas":
    "Not enough gas. Add some {{token}} to your wallet to continue.",
  "transfer.deposit.exceedCap": "Amount exceeds user deposit cap",
  "transfer.deposit.closeToMaxLimit":
    "Deposit amount too close to maximum limit. To ensure your transaction succeeds, please reduce the amount",
  "transfer.deposit.tab.connectedWallet": "From connected wallet",
  "transfer.deposit.tab.exchangeOrOtherWallet": "From exchange / other wallet",

  "transfer.deposit.estGasFee": "Est. gas fee",
  "transfer.deposit.estGasFee.tooltip":
    "Estimated gas fee includes: transaction execution cost on this chain, and destination chain operation fee for paxeer-network deposit. Please prepare extra to ensure sufficient coverage.",
  "transfer.deposit.destinationGasFee": "Destination gas fee",
  "transfer.deposit.destinationGasFee.description":
    "Additional gas tokens are required to cover operations on the destination chain. The amount shown is an estimate - please prepare extra to ensure sufficient coverage.",

  "transfer.withdraw.unsupported.chain":
    "Withdrawals are not supported on this chain. Please switch to any of the bridgeless networks.",
  "transfer.withdraw.unsupported.networkName":
    "Withdrawals are not supported on {{networkName}}. Please switch to any of the bridgeless networks.",
  "transfer.withdraw.unsupported.token":
    "{{token}} withdrawals are unavailable on this network",

  "transfer.withdraw.crossChain.confirmWithdraw": "Confirm to withdraw",
  "transfer.withdraw.crossChain.recipientAddress": "Recipient address",
  "transfer.withdraw.crossChain.recipientNetwork": "Recipient network",
  "transfer.withdraw.crossChain.withdrawAmount": "Withdraw amount",

  "transfer.withdraw.crossChain.process":
    "Your cross-chain withdrawal is being processed...",
  "transfer.withdraw.crossChain.warning":
    "Withdrawals that require cross-chain rebalancing can't be cancelled or followed up with more withdrawals until they've been processed.",
  "transfer.withdraw.crossChain.vaultWarning":
    "Withdrawal exceeds the balance of the {{networkName}} vault ( {{chainVaultBalance}} USDC ). Cross-chain rebalancing fee will be charged for withdrawal to {{networkName}}.",

  "transfer.withdraw.vaultWarning":
    "The balance of {{tokenName}} on the {{chainName}} is {{balance}}, which is insufficient to meet your withdrawal request. Please try again later or switch to another chain for withdrawal.",

  "transfer.withdraw.minAmount.error":
    "You can't withdraw less than {{minAmount}} {{currency}}. Please adjust your amount.",
  "transfer.withdraw.LTV.error":
    "This withdrawal may push your LTV over {{threshold}}%, immediately triggering an auto-convert.",

  "transfer.withdraw.requested": "Withdraw requested",
  "transfer.withdraw.completed": "Withdraw completed",
  "transfer.withdraw.failed": "Withdraw failed",

  // "transfer.withdraw.unsettled.tooltip":
  //   "Unsettled balance can not be withdrawn. In order to withdraw, please settle your balance first.",
  // "transfer.withdraw.settlePnl.description":
  //   "Are you sure you want to settle your PnL? Settlement will take up to 1 minute before you can withdraw your available balance.",
  "transfer.withdraw.otherAccount": "Other {{brokerName}} account",
  "transfer.withdraw.accountId.tips":
    "Please enter an Account ID instead of a wallet address.",
  "transfer.withdraw.accountId.invalid": "No account found",
  "transfer.withdraw.available.tooltip":
    "Available to withdraw equals your free collateral. If you have unsettled PnL, please settle first in the withdraw modal to increase available amount. Current maximum withdrawal per request is {{amount}} USDC. To withdraw more, please submit multiple requests.",
  "transfer.withdraw.accountIdOrAddress.placeholder": "Account ID or address",
  "transfer.withdraw.accountIdOrAddress.hint":
    "Enter an account ID or address to find the account",
  "transfer.withdraw.addExternalWallet": "Add external wallet",
  "transfer.withdraw.addExternalWallet.addressDescription":
    "Fill in and confirm the address to receive the funds on the destination chain.",
  "transfer.withdraw.addExternalWallet.addressWarning":
    "Please do not enter any address other than the {{networkLabel}} address.",
  "transfer.withdraw.addExternalWallet.label": "Enter external wallet",
  "transfer.withdraw.addExternalWallet.networkMismatch":
    "This address doesn't match the selected network. Please enter a valid {{networkLabel}} address.",

  "transfer.internalTransfer.from": "From",
  "transfer.internalTransfer.to": "To",
  "transfer.internalTransfer.currentAssetValue": "Current asset value",
  "transfer.internalTransfer.success":
    "Success! Funds will be available in 15 seconds.",

  "transfer.internalTransfer.unsettled.tooltip":
    "Unsettled balance can not be transferred. In order to transfer, please settle your balance first.",
  "transfer.internalTransfer.settlePnl.description":
    "Are you sure you want to settle your PnL? <br/> Settlement will take up to 1 minute before you can transfer your available balance.",

  "transfer.internalTransfer.error.default":
    "Unable to complete transfer. Please try again later.",
  "transfer.internalTransfer.error.transferInProgress":
    "An internal transfer is currently in progress.",
  "transfer.internalTransfer.error.withdrawalInProgress":
    "There is a withdrawal in progress.",
  "transfer.internalTransfer.error.transferToSelf":
    "Transfers to your own account are not allowed",
  "transfer.internalTransfer.error.accountIdNotExist":
    "Receiver account ID does not exist.",
  "transfer.internalTransfer.error.transferToSubAccount":
    "Transfers to sub-accounts under different main accounts are not permitted.",
  // "transfer.internalTransfer.error.scopeInsufficient":
  //   "The scope is insufficient",

  // swap deposit
  "transfer.swapDeposit.crossSwap.notice":
    "Cross-chain transaction fees will be charged. To avoid these, use our supported <0>Bridgeless networks</0>",
  "transfer.swapDeposit.swap.notice":
    "Please note that swap fees will be charged.",

  "transfer.swapDeposit.bridging": "Bridging",
  "transfer.swapDeposit.bridge": "Bridge",
  "transfer.swapDeposit.bridge.description": "Bridge to Arbirtum via Stargate",
  "transfer.swapDeposit.depositing": "Depositing",
  "transfer.swapDeposit.deposit": "Deposit",
  "transfer.swapDeposit.deposit.description": "Deposit to {{brokerName}}",

  "transfer.swapDeposit.viewStatus": "View status",
  "transfer.swapDeposit.depositFailed":
    "Deposit failed, please try again later.",

  "transfer.slippage": "Slippage",
  "transfer.slippage.slippageTolerance": "Slippage tolerance",
  "transfer.slippage.slippageTolerance.description":
    "Your transaction will revert if the price changes unfavorably by more than this percentage.",
  "transfer.slippage.error.high":
    "High slippage may result in significant loss",

  "transfer.swapDeposit.swapFee": "Swap fee",
  "transfer.swapDeposit.swapFee.description":
    "WOOFi charges a 0.025% on each swap.",
  "transfer.swapDeposit.bridgeFee": "Bridge fee",
  "transfer.swapDeposit.bridgeFee.description":
    "Stargate charges a fee to bridge your assets.",

  "transfer.swapDeposit.minimumReceived": "Minimum received",
  "transfer.swapDeposit.confirmSwap": "Confirm to swap",
  "transfer.swapDeposit.averageSwapTime":
    "Average swap time <0>~ {{time}} mins</0>",

  "transfer.swapDeposit.swapDialog.title": "Review swap details",
  "transfer.swapDeposit.viewFAQs": "Need help? <0>View FAQs</0>",
  "transfer.swapDeposit.notEnoughLiquidity":
    "Not enough liquidity. Please try again later or use another chain to deposit.",

  "transfer.convert.completed": "Conversion complete",
  "transfer.convert.succeeded": "Succeeded",
  "transfer.convert.request": "Conversion request submitted",
  "transfer.convert.request.description":
    "Your request to convert {{fromAmount}} {{fromToken}} to approximately {{toAmount}} {{toToken}} has been submitted. You can track its status below.",
  "transfer.convert.pending": "Conversion in progress",
  "transfer.convert.pending.description":
    "Converting {{fromAmount}} {{fromToken}} to approximately {{toAmount}} {{toToken}}.",
  "transfer.convert.pending.descriptionWithoutAmount":
    "Converting {{fromAmount}} {{fromToken}} to {{toToken}}.",
  "transfer.convert.statusChecking": "Checking status...",
  "transfer.convert.delayed":
    "Your conversion ({{fromToken}} → {{toToken}}) is still in progress.",
  "transfer.convert.delayed.description":
    "Network activity may cause delays. If the market moves beyond your slippage tolerance, the conversion may fail.",
  "transfer.convert.completed.description":
    "You converted {{fromAmount}} {{fromToken}} to {{receivedAmount}} {{toToken}}.",
  "transfer.convert.completed.descriptionWithoutAmount":
    "You converted {{fromAmount}} {{fromToken}} to {{toToken}}.",
  "transfer.convert.failed.description":
    "Your conversion could not be completed. Please try again.",
  "transfer.convert.statusCheckFailed":
    "Unable to check your latest convert status.",
  "transfer.convert.slippage.tooltip":
    "Slippage tolerance is the maximum price change you are willing to accept while your conversion is processed.",
  "transfer.convert.slippage.helper": "Minimum 0.2%. Recommended 0.5%.",
  "transfer.convert.slippage.minimumError":
    "Slippage tolerance must be at least 0.2%.",
  "transfer.convert.slippage.lowWarning":
    "Lower slippage tolerance increases the risk of delays or failure. For larger amounts, split your conversion into smaller transactions.",
  "transfer.convert.slippage.highWarning":
    "A high slippage tolerance may result in a less favorable conversion rate. Consider using the recommended 0.5%.",
  "transfer.convert.failed": "Conversion failed",
  "transfer.depositCap": "Deposit cap",
  "transfer.depositCap.tooltip": "Max deposit amount for ",
  "transfer.deposit.convertRate": "Convert rate",
  "transfer.convert.convertAssets": "Convert assets to USDC",
  "transfer.deposit.collateralContribution": "Collateral contribution",
  "transfer.withdraw.InsufficientVaultBalance": "Insufficient vault balance",

  "transfer.LTV": "LTV",
  "transfer.LTV.description": `LTV (Loan-to-Value) is the ratio between your negative USDC and the current value of your collateral. If your LTV exceeds {{threshold}}, your collateral will be automatically converted to USDC.`,
  "transfer.LTV.tooltip": `If your LTV exceeds {{threshold}}% or your USDC balance plus Unsettled PnL falls below {{usdcThreshold}}, your collateral will be automatically converted with a haircut. To avoid this, you can manually convert assets to USDC.`,
  "transfer.LTV.currentLTV": "Current LTV",
  "transfer.LTV.isolatedModeUsdcOnly":
    "Isolated margin is denominated in USDC. Eligible collateral assets can back the required USDC balance.",
  "transfer.LTV.availableCrossMarginHint":
    "Available balance = USDC balance + Unsettled PnL. All collateral assets contribute to your available margin.",
  "transfer.LTV.isolatedMarginOrderHint":
    "Isolated margin is denominated in USDC. Eligible collateral assets can back the required USDC balance. A negative USDC balance contributes to LTV and may trigger automatic conversion.",
  "transfer.convert": "Convert",
  // "transfer.convert.note": "Please note that convert fees will be charged.",
  "transfer.deposit.globalMaxQty.error":
    "Your deposit couldn't be processed because the global deposit cap for {{token}} on {{chain}} has been reached. This is typically resolved after rebalancing, which takes around 1 hour. Please try again later, or use a smaller amount or a different chain. <0>Learn more</0>",
  "transfer.deposit.gasFee.error":
    "Please ensure you have enough {{token}} for gas fees.",

  "transfer.deposit.status.pending.one": "You have a pending transaction",
  "transfer.deposit.status.pending.multiple": "You have pending transactions",
  "transfer.deposit.status.completed.one": "You have a successful transaction",
  "transfer.deposit.status.completed.multiple":
    "You have successful transactions",

  // Yield-bearing collateral reminder
  "transfer.deposit.yieldReminder.earnAPY":
    "<0>Earn {{apy}}% APY</0> while you trade.",
  "transfer.deposit.yieldReminder.earnRewards": "Earn rewards while you trade.",
  "transfer.deposit.yieldReminder.distribution":
    "The yield will be distributed to your account weekly.",
  "transfer.deposit.yieldReminder.disclaimer":
    "APY is estimated; actual returns may vary.",
};

export type Transfer = typeof transfer;
