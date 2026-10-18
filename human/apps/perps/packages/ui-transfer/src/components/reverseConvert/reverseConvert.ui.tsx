import { useTranslation } from "@paxeer-network/i18n";
import { Box, Flex, Text } from "@paxeer-network/ui";
import { AvailableQuantity } from "../availableQuantity";
import { ConvertAction } from "../convertAction";
import { ConvertProgressStatus } from "../convertForm/convertForm.ui";
import { shouldDisplayConvertProgress } from "../convertForm/convertHistory";
import { ExchangeDivider } from "../exchangeDivider";
import { QuantityInput } from "../quantityInput";
import { SwapCoin } from "../swapCoin";
import { useReverseConvertScript } from "./reverseConvert.script";

export const ReverseConvertUI = (
  props: ReturnType<typeof useReverseConvertScript>,
) => {
  const { t } = useTranslation();
  if (
    props.activeConvertRequest &&
    shouldDisplayConvertProgress(
      props.activeConvertRequest.status,
      props.isRecoveredConvertRequest,
    )
  ) {
    return (
      <ConvertProgressStatus
        request={props.activeConvertRequest}
        isRefreshing={props.isConvertHistoryRefreshing}
      />
    );
  }
  return (
    <Box>
      <QuantityInput
        token={props.token}
        value={props.quantity}
        onValueChange={props.onQuantityChange}
        disabled={props.loading}
      />
      <AvailableQuantity
        token={props.token}
        quantity={props.quantity}
        maxQuantity={props.maxQuantity.toString()}
        onClick={() => props.onQuantityChange(props.maxQuantity.toString())}
      />
      <ExchangeDivider />
      <QuantityInput
        token={props.targetToken}
        readOnly
        value={props.outAmounts === "-" ? "" : props.outAmounts}
        loading={props.isQuoteLoading}
      />
      <SwapCoin
        className="oui-mt-2"
        indexPrice={
          props.isQuoteLoading ||
          !props.quantity ||
          Number.isNaN(Number(props.convertRate))
            ? "-"
            : props.convertRate
        }
        sourceSymbol={props.token?.token}
        targetSymbol={props.targetToken?.token}
        precision={props.targetToken?.precision ?? 6}
      />
      <Text as="div" size="xs" intensity={54} className="oui-my-3">
        {t("transfer.reverseConvert.estimate")}
      </Text>
      {!props.reverseAllowed && (
        <Text as="div" size="xs">
          {t("transfer.reverseConvert.unavailable")}
        </Text>
      )}
      <Flex justify="center" mt={4}>
        <ConvertAction
          networkId={props.networkId}
          disabled={props.disabled}
          loading={props.loading}
          onConvert={props.onConvert}
        />
      </Flex>
    </Box>
  );
};
