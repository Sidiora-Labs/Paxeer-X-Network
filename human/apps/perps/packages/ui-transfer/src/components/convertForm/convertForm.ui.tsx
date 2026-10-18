import React from "react";
import { useTranslation } from "@paxeer-network/i18n";
import {
  Box,
  Button,
  CheckedCircleFillIcon,
  ExclamationFillIcon,
  Flex,
  Spinner,
  Text,
  textVariants,
} from "@paxeer-network/ui";
import { LtvWidget } from "../LTV";
import { AvailableQuantity } from "../availableQuantity";
import { ConvertAction } from "../convertAction";
import { ExchangeDivider } from "../exchangeDivider";
import { MinimumReceived } from "../minimumReceived";
import { QuantityInput } from "../quantityInput";
import { Slippage } from "../slippage";
import { SwapCoin } from "../swapCoin";
import type { ConvertFormScriptReturn } from "./convertForm.script";
import {
  getConvertProgressStatus,
  isValidConvertAmount,
  shouldDisplayConvertProgress,
} from "./convertHistory";

export type ConvertFormProps = ConvertFormScriptReturn;

export const ConvertFormUI: React.FC<ConvertFormProps> = (props) => {
  const { t } = useTranslation();
  const {
    loading,
    disabled,
    quantity,
    onQuantityChange,
    maxQuantity,
    token,
    targetToken,
    sourceTokens,
    onSourceTokenChange,
    onConvert,
    slippage,
    onSlippageChange,
    convertRate,
    outAmounts,
    minimumReceived,
    isCeFiQuote,
    isQuoteLoading,
    currentLTV,
    nextLTV,
    networkId,
    balanceRevalidating,
    activeConvertRequest,
    isCheckingConvertStatus,
    hasConvertStatusError,
    isConvertHistoryRefreshing,
    onRefreshConvertStatus,
    isRecoveredConvertRequest,
  } = props;

  if (
    activeConvertRequest &&
    shouldDisplayConvertProgress(
      activeConvertRequest.status,
      isRecoveredConvertRequest,
    )
  ) {
    return (
      <ConvertProgressStatus
        request={activeConvertRequest}
        isRefreshing={isConvertHistoryRefreshing}
      />
    );
  }

  return (
    <Box className={textVariants({ weight: "semibold" })}>
      <Box className="oui-mb-6 lg:oui-mb-8">
        <Box mt={3} mb={1}>
          <QuantityInput
            value={quantity}
            onValueChange={onQuantityChange}
            token={token}
            tokens={sourceTokens}
            onTokenChange={onSourceTokenChange}
          />
        </Box>
        <AvailableQuantity
          token={token}
          quantity={quantity}
          maxQuantity={maxQuantity.toString()}
          loading={balanceRevalidating}
          onClick={() => {
            onQuantityChange(maxQuantity.toString());
          }}
        />
        <ExchangeDivider />
        <QuantityInput
          readOnly
          loading={isQuoteLoading}
          token={targetToken}
          value={
            isQuoteLoading || !quantity || Number.isNaN(Number(outAmounts))
              ? ""
              : outAmounts
          }
        />
        <Flex direction="column" itemAlign="start" mt={2} gap={1}>
          <SwapCoin
            indexPrice={
              isQuoteLoading || !quantity || Number.isNaN(Number(convertRate))
                ? "-"
                : convertRate
            }
            sourceSymbol={token?.token}
            targetSymbol={targetToken?.token}
          />
          {/* CeFi execution ignores slippage, so the input is hidden and the
              request silently carries the default value. */}
          {!isCeFiQuote && (
            <Slippage
              value={slippage}
              onValueChange={onSlippageChange}
              riskGuidance={{
                recommendedValue: 0.5,
                tooltip: t("transfer.convert.slippage.tooltip"),
                helperText: t("transfer.convert.slippage.helper"),
                minimumError: t("transfer.convert.slippage.minimumError"),
                lowWarning: t("transfer.convert.slippage.lowWarning"),
                highWarning: t("transfer.convert.slippage.highWarning"),
              }}
            />
          )}
          {/* CeFi manual swaps carry no slippage guarantee, so the estimated
              amount must not be presented as a guaranteed minimum. */}
          {!isCeFiQuote && (
            <MinimumReceived
              symbol={targetToken?.token || ""}
              precision={targetToken?.precision ?? 6}
              value={
                isQuoteLoading ||
                !quantity ||
                Number.isNaN(Number(minimumReceived))
                  ? "-"
                  : minimumReceived.toString()
              }
            />
          )}
          <LtvWidget
            showDiff={typeof quantity !== "undefined" && Number(quantity) > 0}
            currentLtv={currentLTV}
            nextLTV={nextLTV}
          />
        </Flex>
      </Box>
      {hasConvertStatusError && (
        <Flex
          itemAlign="center"
          justify="between"
          gap={3}
          className="oui-mb-4 oui-w-full"
        >
          <Flex itemAlign="center" gap={2}>
            <ExclamationFillIcon className="oui-size-4 oui-shrink-0 oui-text-warning-darken" />
            <Text size="xs" intensity={54}>
              {t("transfer.convert.statusCheckFailed")}
            </Text>
          </Flex>
          <Button
            size="sm"
            variant="outlined"
            loading={isConvertHistoryRefreshing}
            onClick={onRefreshConvertStatus}
          >
            {t("common.refresh")}
          </Button>
        </Flex>
      )}
      <Flex itemAlign={"center"} justify="center">
        <ConvertAction
          networkId={networkId}
          disabled={disabled}
          loading={loading || isCheckingConvertStatus}
          onConvert={onConvert}
        />
      </Flex>
    </Box>
  );
};

export const ConvertProgressStatus: React.FC<{
  request: NonNullable<ConvertFormScriptReturn["activeConvertRequest"]>;
  isRefreshing: boolean;
}> = ({ request, isRefreshing }) => {
  const { t } = useTranslation();
  const isPending = request.status === "pending";
  const isCompleted =
    request.status === "completed" || request.status === "succeeded";
  const isFailed = !isPending && !isCompleted;
  const progressStatus = getConvertProgressStatus(
    request.status,
    isRefreshing,
    request.isDelayed,
  );
  const isChecking = progressStatus === "checking";
  const isDelayed = progressStatus === "delayed";
  const hasTargetAmount = isValidConvertAmount(request.targetAmount);
  const hasReceivedAmount = isValidConvertAmount(request.receivedAmount);
  const interpolation = {
    fromAmount: request.sourceAmount,
    fromToken: request.sourceToken,
    toAmount: request.targetAmount,
    toToken: request.targetToken,
    receivedAmount: request.receivedAmount,
  };
  const statusLabel = isChecking
    ? t("transfer.convert.statusChecking")
    : isDelayed
      ? t("transfer.convert.delayed", interpolation)
      : isPending
        ? t("transfer.convert.pending")
        : isCompleted
          ? t("transfer.convert.completed")
          : t("transfer.convert.failed");
  const description = isDelayed
    ? t("transfer.convert.delayed.description")
    : isPending
      ? t(
          hasTargetAmount
            ? "transfer.convert.pending.description"
            : "transfer.convert.pending.descriptionWithoutAmount",
          interpolation,
        )
      : isCompleted
        ? t(
            hasReceivedAmount
              ? "transfer.convert.completed.description"
              : "transfer.convert.completed.descriptionWithoutAmount",
            interpolation,
          )
        : t("transfer.convert.failed.description");

  return (
    <Flex
      direction="column"
      itemAlign="center"
      gap={6}
      className="oui-w-full oui-px-4 oui-py-6"
    >
      <Flex itemAlign="center" justify="center" gap={3} className="oui-w-full">
        {isCompleted && (
          <CheckedCircleFillIcon className="oui-size-4 oui-text-success" />
        )}
        {!isPending && !isCompleted && (
          <ExclamationFillIcon className="oui-size-4 oui-text-danger" />
        )}
        <Text
          size="sm"
          weight="semibold"
          intensity={isFailed ? undefined : 80}
          color={isFailed ? "danger" : undefined}
          className="oui-text-center"
        >
          {statusLabel}
        </Text>
        {isPending && !isDelayed && (
          <Flex
            itemAlign="center"
            justify="center"
            className="oui-size-4 oui-shrink-0"
          >
            <Spinner size="sm" className="oui-block" />
          </Flex>
        )}
      </Flex>
      <Text
        as="div"
        size="xs"
        intensity={54}
        className="oui-w-full oui-text-center"
      >
        {description}
      </Text>
    </Flex>
  );
};
