import { FC } from "react";
import { useTranslation } from "@paxeer-network/i18n";
import { Box, Flex, textVariants, Tabs, TabPanel } from "@paxeer-network/ui";
import { LtvWidget } from "../LTV";
import { ActionButton } from "../actionButton";
import { AvailableQuantity } from "../availableQuantity";
import { BrokerWallet } from "../brokerWallet";
import { ChainSelect } from "../chainSelect";
import { CollateralContribution } from "../collateralContribution";
import { CollateralRatioWidget } from "../collateralRatio";
import { ExchangeDivider } from "../exchangeDivider";
import { ExclusiveDeposit } from "../exclusiveDeposit";
import { Fee } from "../fee";
import { MinimumReceived } from "../minimumReceived";
import { QuantityInput } from "../quantityInput";
import { Slippage } from "../slippage";
import { Web3Wallet } from "../web3Wallet";
import { YieldBearingReminder } from "../yieldBearingReminder";
import { ConvertRate } from "./components/convertRate";
import { DepositTokenValueFormatter } from "./components/depositTokenValueFormatter";
import { Notice } from "./components/notice";
import { type DepositFormScriptReturn } from "./depositForm.script";

export const DepositForm: FC<DepositFormScriptReturn> = (props) => {
  const {
    sourceToken,
    targetToken,
    sourceTokens,
    targetTokens,
    onSourceTokenChange,
    onTargetTokenChange,
    quantity,
    collateralContributionQuantity,
    maxQuantity,
    maxDepositAmount,
    onQuantityChange,
    hintMessage,
    inputStatus,
    targetInputStatus,
    targetHintMessage,
    chains,
    currentChain,
    settingChain,
    onChainChange,
    actionType,
    onDeposit,
    onApprove,
    onApproveAndDeposit,
    wrongNetwork,
    balanceRevalidating,
    loading,
    disabled,
    networkId,
    fee,
    collateralRatio,
    currentLTV,
    nextLTV,
    slippage,
    onSlippageChange,
    swapMinReceived,
    needSwap,
    swapPrice,
    swapPriceInUSD,
    warningMessage,
    usdcToken,
    targetQuantity,
    targetQuantityLoading,
    batchBalancesRevalidating,
    showSourceDepositCap,
    showTargetDepositCap,
    slippageValidate,
    quantityNotional,
    activeSubTab,
    setActiveSubTab,
    showExclusiveDeposit,
  } = props;

  const { t } = useTranslation();

  const tokenValueFormatter = (value: string) => (
    <DepositTokenValueFormatter
      value={value}
      userMaxQty={targetToken?.user_max_qty}
    />
  );

  const renderContent = () => {
    return (
      <Flex direction="column" itemAlign="start" mt={2} gap={1}>
        {needSwap && (
          <ConvertRate
            sourceSymbol={sourceToken?.display_name || sourceToken?.symbol}
            targetSymbol={targetToken?.symbol}
            precision={targetToken?.precision}
            swapPrice={swapPrice!}
            swapPriceInUSD={swapPriceInUSD}
          />
        )}
        <CollateralRatioWidget value={collateralRatio} />
        <CollateralContribution
          // it need to use USDC precision
          precision={usdcToken?.precision}
          value={collateralContributionQuantity}
        />
        <LtvWidget
          showDiff={typeof quantity !== "undefined" && Number(quantity) > 0}
          currentLtv={currentLTV}
          nextLTV={nextLTV}
        />

        {needSwap && (
          <>
            <Slippage
              value={slippage}
              onValueChange={onSlippageChange}
              min={0.01}
              max={50}
              validate={slippageValidate}
            />
            <MinimumReceived
              value={swapMinReceived!}
              symbol={targetToken?.symbol ?? ""}
              precision={targetToken?.precision}
            />
          </>
        )}

        <Fee {...fee} nativeSymbol={props.nativeSymbol} />
      </Flex>
    );
  };

  return (
    <Box id="oui-deposit-form" className={textVariants({ weight: "semibold" })}>
      <Tabs
        value={activeSubTab}
        onValueChange={(value) =>
          setActiveSubTab(value as "web3" | "exclusive_deposit")
        }
        variant="contained"
        classNames={{
          tabsList: "oui-w-fit",
        }}
      >
        <TabPanel
          title={t("transfer.deposit.tab.connectedWallet")}
          value="web3"
        >
          <Box className="oui-mt-3">
            <Web3Wallet />
            <Box mt={3} mb={1}>
              <ChainSelect
                chains={chains}
                value={currentChain!}
                onValueChange={onChainChange}
                wrongNetwork={wrongNetwork}
                loading={settingChain}
                disabled={!props.isLoggedIn}
              />
              <QuantityInput
                data-testId="oui-testid-deposit-dialog-quantity-input"
                classNames={{
                  root: "oui-mt-[2px] oui-rounded-t-sm oui-rounded-b-xl",
                }}
                value={quantity}
                onValueChange={onQuantityChange}
                token={sourceToken}
                tokens={sourceTokens}
                onTokenChange={onSourceTokenChange}
                status={inputStatus}
                hintMessage={hintMessage}
                // when show deposit cap, hide select caret
                tokenShowCaret={
                  !showSourceDepositCap && sourceTokens?.length > 1
                }
                tokenValueFormatter={
                  showSourceDepositCap ? tokenValueFormatter : undefined
                }
                disabled={!props.isLoggedIn}
                balancesRevalidating={batchBalancesRevalidating}
                showBalance
                showDisplayName
              />
            </Box>

            <AvailableQuantity
              token={sourceToken}
              quantity={quantity}
              maxQuantity={maxQuantity}
              notional={quantityNotional}
              loading={balanceRevalidating}
              onClick={() => {
                onQuantityChange(maxDepositAmount);
              }}
              showDisplayName
            />

            {/* Yield-bearing collateral reminder */}
            <YieldBearingReminder
              symbol={targetToken?.symbol}
              className="oui-mt-3"
            />

            <ExchangeDivider />

            <BrokerWallet />

            <QuantityInput
              classNames={{
                root: "oui-mt-3 oui-border-transparent focus-within:oui-outline-transparent",
              }}
              token={targetToken}
              tokens={targetTokens}
              onTokenChange={onTargetTokenChange}
              value={targetQuantity}
              loading={targetQuantityLoading}
              disabled={!props.isLoggedIn}
              readOnly
              status={targetInputStatus}
              hintMessage={targetHintMessage}
              // when show deposit cap, hide select caret
              tokenShowCaret={!showTargetDepositCap && targetTokens?.length > 1}
              tokenValueFormatter={
                showTargetDepositCap ? tokenValueFormatter : undefined
              }
            />
            {renderContent()}
          </Box>

          <Box className="oui-mb-6 lg:oui-mb-8">
            <Notice message={warningMessage} wrongNetwork={wrongNetwork} />
          </Box>

          <Flex justify="center">
            <ActionButton
              actionType={actionType}
              symbol={sourceToken?.symbol}
              disabled={disabled}
              loading={loading}
              onDeposit={onDeposit}
              onApprove={onApprove}
              onApproveAndDeposit={onApproveAndDeposit}
              networkId={networkId}
            />
          </Flex>
        </TabPanel>
        {showExclusiveDeposit && (
          <TabPanel
            title={t("transfer.deposit.tab.exchangeOrOtherWallet")}
            value="exclusive_deposit"
          >
            <ExclusiveDeposit active={activeSubTab === "exclusive_deposit"} />
          </TabPanel>
        )}
      </Tabs>
    </Box>
  );
};
