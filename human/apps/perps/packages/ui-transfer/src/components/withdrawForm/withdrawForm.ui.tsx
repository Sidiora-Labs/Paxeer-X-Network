import { FC, useState } from "react";
import { Trans, useTranslation } from "@paxeer-network/i18n";
import {
  Box,
  modal,
  useScreen,
  Flex,
  Text,
  textVariants,
  Tabs,
  TabPanel,
  ArrowLeftRightIcon,
  WalletIcon,
} from "@paxeer-network/ui";
import { WithdrawTo } from "../../types";
import { LtvWidget } from "../LTV";
import { TextAreaInput } from "../accountIdInput";
import { AvailableQuantity } from "../availableQuantity";
import { BrokerWallet } from "../brokerWallet";
import { ChainSelect } from "../chainSelect";
import { ExchangeDivider } from "../exchangeDivider";
import { QuantityInput } from "../quantityInput";
import {
  ReverseConvertDialogId,
  ReverseConvertSheetId,
} from "../reverseConvert/reverseConvert.widget";
import { UnsettlePnlInfo } from "../unsettlePnlInfo";
import { WithdrawAction } from "../withdrawAction";
import { WithdrawWarningMessage } from "../withdrawWarningMessage";
import { AddWalletDialog } from "./addWalletDialog";
import { WalletSelector } from "./walletSelector";
import { WithdrawFormScriptReturn } from "./withdrawForm.script";

export type WithdrawFormProps = WithdrawFormScriptReturn;

export const WithdrawForm: React.FC<WithdrawFormProps> = (props) => {
  const {
    address,
    walletName,
    loading,
    disabled,
    quantity,
    onQuantityChange,
    sourceToken,
    amount,
    maxQuantity,
    tokenChains,
    currentChain,
    fee,
    settingChain,
    crossChainTrans,
    checkIsBridgeless,
    withdrawTo,
    sourceTokens,
    onSourceTokenChange,
    vaultBalanceList,
    qtyGreaterThanMaxAmount,
    isTokenUnsupported,
    onSwitchToSupportedNetwork,
    externalWallets,
    selectedWalletAddress,
    onSelectWallet,
    onAddExternalWallet,
    isEnableTrading,
    enableWithdrawToExternalWallet,
  } = props;

  const { t } = useTranslation();
  const { isMobile } = useScreen();
  const [addWalletOpen, setAddWalletOpen] = useState(false);

  const handleAddExternalWallet = (
    address: string,
    network?: "EVM" | "SOL",
  ) => {
    onAddExternalWallet?.(address, network);
  };

  const internalWithdrawPanel = (
    <TabPanel
      title={t("transfer.withdraw.otherAccount", {
        brokerName: props.brokerName,
      })}
      value={WithdrawTo.Account}
    >
      <TextAreaInput
        label={t("common.accountId")}
        value={props.toAccountId}
        onChange={props.setToAccountId}
        status={props.toAccountIdInputStatus}
        hintMessage={props.toAccountIdHintMessage}
        disabled={!props.isLoggedIn}
        placeholder={t("transfer.withdraw.accountIdOrAddress.placeholder")}
        enableAccountLookup
        accountInfo={props.toAccountInfo}
        accountDropdownOpen={props.toAccountInfoDropdownOpen}
        setAccountDropdownOpen={props.setToAccountInfoDropdownOpen}
      />
      <Box mb={1} px={2}>
        <Text size="xs" intensity={54}>
          {t("transfer.withdraw.accountIdOrAddress.hint")}
        </Text>
      </Box>
    </TabPanel>
  );

  return (
    <Box
      id="oui-withdraw-form"
      className={textVariants({ weight: "semibold" })}
    >
      <Box className="oui-mb-6 lg:oui-mb-8">
        <BrokerWallet />
        <Box mt={3} mb={1}>
          <QuantityInput
            value={quantity}
            onValueChange={onQuantityChange}
            token={sourceToken}
            tokens={sourceTokens}
            onTokenChange={onSourceTokenChange}
            status={props.inputStatus}
            hintMessage={props.hintMessage}
            hintSuffix={
              isTokenUnsupported ? (
                <button
                  type="button"
                  onClick={onSwitchToSupportedNetwork}
                  className="oui-inline-flex oui-items-center oui-gap-1 oui-text-2xs oui-font-semibold oui-text-primary"
                >
                  {t("common.switch")}
                  <ArrowLeftRightIcon
                    size={16}
                    className="oui-text-primary oui-mt-0.5"
                    opacity={1}
                  />
                </button>
              ) : undefined
            }
            vaultBalanceList={vaultBalanceList}
            testId="oui-testid-withdraw-dialog-quantity-input"
            displayType="vaultBalance"
            disabled={!props.isLoggedIn}
          />
        </Box>
        <AvailableQuantity
          token={sourceToken}
          quantity={amount}
          maxQuantity={maxQuantity.toString()}
          loading={props.balanceRevalidating}
          onClick={() => {
            onQuantityChange(maxQuantity.toString());
          }}
          tooltipContent={t("transfer.withdraw.available.tooltip", {
            amount: maxQuantity.toString(),
          })}
        />
        {props.isRobinhoodWithdrawal && (
          <Flex
            justify="end"
            gap={1}
            mt={2}
            className="oui-flex-wrap oui-text-2xs oui-text-base-contrast-36"
          >
            <Trans
              i18nKey="transfer.reverseConvert.available"
              values={{ amount: props.availableUSDC }}
              components={[
                <Text.numeral key="amount" dp={2} padding>
                  {props.availableUSDC}
                </Text.numeral>,
              ]}
            />
            <button
              type="button"
              className="oui-text-primary"
              disabled={!props.isLoggedIn || !sourceToken}
              onClick={() => {
                void modal.show(
                  isMobile ? ReverseConvertSheetId : ReverseConvertDialogId,
                );
              }}
            >
              {t("transfer.reverseConvert.action")}
            </button>
          </Flex>
        )}
        <Box mx={2} mt={1}>
          <UnsettlePnlInfo
            unsettledPnl={props.unsettledPnL}
            hasPositions={props.hasPositions}
            onSettlePnl={props.onSettlePnl}
            tooltipContent={t("settle.unsettled.tooltip")}
            dialogContent={<Trans i18nKey="settle.settlePnl.description" />}
          />
        </Box>
        <ExchangeDivider />
        <Tabs
          value={withdrawTo}
          onValueChange={props.setWithdrawTo as (tab: string) => void}
          variant="contained"
          size="lg"
          classNames={{
            tabsList: "oui-px-0",
            tabsContent: "oui-pt-3",
          }}
        >
          <TabPanel
            title={t("transfer.web3Wallet.my")}
            icon={
              props.walletName ? (
                <WalletIcon size={"xs"} name={props.walletName ?? ""} />
              ) : undefined
            }
            value={WithdrawTo.Wallet}
          >
            {isEnableTrading && enableWithdrawToExternalWallet && (
              <WalletSelector
                connectedWallet={
                  address
                    ? {
                        name: walletName || t("common.wallet"),
                        address,
                        namespace: currentChain?.namespace,
                      }
                    : undefined
                }
                externalWallets={externalWallets || []}
                selectedAddress={selectedWalletAddress ?? ""}
                onSelect={onSelectWallet}
                onAddExternalWallet={() => setAddWalletOpen(true)}
              />
            )}
            <Box mb={1}>
              <ChainSelect
                chains={tokenChains}
                value={currentChain!}
                onValueChange={props.onChainChange}
                wrongNetwork={props.wrongNetwork}
                loading={settingChain}
                disabled={!props.isLoggedIn}
              />
            </Box>
            <QuantityInput
              classNames={{
                root: "oui-mt-[2px] oui-rounded-t-sm oui-rounded-b-xl",
              }}
              token={sourceToken}
              value={props.showQty}
              readOnly
              showDisplayName
            />
          </TabPanel>
          {internalWithdrawPanel}
        </Tabs>
        <Box mt={2} px={2}>
          <LtvWidget
            showDiff={typeof quantity !== "undefined" && Number(quantity) > 0}
            currentLtv={props.currentLTV}
            nextLTV={props.nextLTV}
          />
        </Box>
        <Flex direction="column" mt={1} gapY={1} px={2} itemAlign="start">
          <Text size="xs" intensity={36}>
            {t("common.fee")}
            {withdrawTo === WithdrawTo.Wallet ? " ≈ " : " = "}
            <Text size="xs" intensity={80}>
              {fee}
            </Text>
          </Text>
        </Flex>
      </Box>
      <WithdrawWarningMessage
        checkIsBridgeless={checkIsBridgeless}
        crossChainTrans={crossChainTrans}
        qtyGreaterThanMaxAmount={qtyGreaterThanMaxAmount}
        message={props.warningMessage}
      />
      <Flex justify="center">
        <WithdrawAction
          checkIsBridgeless={checkIsBridgeless}
          networkId={props.networkId}
          disabled={disabled}
          loading={loading}
          onWithdraw={props.onWithdraw}
          crossChainWithdraw={props.crossChainWithdraw}
          currentChain={currentChain}
          address={address}
          quantity={quantity}
          fee={fee}
          withdrawTo={withdrawTo}
          onTransfer={props.onTransfer}
        />
      </Flex>
      {enableWithdrawToExternalWallet && (
        <AddWalletDialog
          open={addWalletOpen}
          onOpenChange={setAddWalletOpen}
          onConfirm={handleAddExternalWallet}
          chain={currentChain}
        />
      )}
    </Box>
  );
};
