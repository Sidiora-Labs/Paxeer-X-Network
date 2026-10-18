import { useTranslation } from "@paxeer-network/i18n";
import { Button, Flex, Input, Text, Tooltip, cn } from "@paxeer-network/ui";
import type { TradingFeeRebateReturns } from "./tradingFeeRebate.script";
import type { TradingFeeRebateWidgetProps } from "./tradingFeeRebate.widget";

export type TradingFeeRebateProps = TradingFeeRebateReturns &
  TradingFeeRebateWidgetProps;

export const TradingFeeRebate = (props: TradingFeeRebateProps) => {
  const { t } = useTranslation();

  return (
    <Flex
      direction="column"
      itemAlign="start"
      className="oui-affiliate-tradingFeeRebate oui-font-semibold"
    >
      <Flex
        direction="column"
        itemAlign="start"
        gap={6}
        width="100%"
        className="oui-py-5"
      >
        <Text
          size="2xs"
          intensity={54}
          className="oui-w-full oui-leading-[18px]"
        >
          {t("affiliate.tradingFeeRebate.modal.description")}
        </Text>

        <Flex direction="column" itemAlign="start" gap={1} width="100%">
          <Text
            as="label"
            htmlFor="oui-trading-fee-rebate-rate"
            size="2xs"
            intensity={54}
            className="oui-w-full oui-break-words oui-leading-[18px]"
          >
            {t("affiliate.tradingFeeRebate.rate")}
          </Text>
          <Input
            id="oui-trading-fee-rebate-rate"
            type="text"
            inputMode="numeric"
            autoComplete="off"
            fullWidth
            size="lg"
            value={props.percentage}
            onChange={(event) => props.onChange(event.target.value)}
            color={props.showError ? "danger" : undefined}
            className="oui-border oui-border-line"
            suffix={
              <Text intensity={54} className="oui-pe-3">
                %
              </Text>
            }
          />
          <Text
            size="2xs"
            intensity={54}
            className={cn(
              "oui-w-full oui-break-words oui-leading-[18px]",
              props.showError && "oui-text-danger",
            )}
          >
            {t("affiliate.tradingFeeRebate.maximumAllowed", {
              rate: props.maxPercentage,
            })}
          </Text>
          <Flex
            direction="column"
            itemAlign="start"
            className="oui-affiliate-tradingFeeRebate-preview oui-mt-2 oui-w-full oui-gap-[2px] oui-break-words oui-leading-[18px]"
            aria-live="polite"
          >
            <Text size="2xs" intensity={54}>
              {t("affiliate.tradingFeeRebate.preview.receive")}
            </Text>
            <Tooltip
              className="oui-max-w-[280px]"
              content={t("affiliate.tradingFeeRebate.preview.basis", {
                commissionRate: props.directTradesRate,
              })}
            >
              <Text
                size="2xs"
                intensity={98}
                tabIndex={0}
                className="oui-cursor-help oui-underline oui-decoration-dotted oui-underline-offset-[6px]"
              >
                {t("affiliate.tradingFeeRebate.preview.rebate", {
                  rate: props.tradingFeeRebateRate ?? "-",
                })}
              </Text>
            </Tooltip>
          </Flex>
        </Flex>
      </Flex>

      <Flex width="100%" gap={3} className="oui-pb-5 oui-pt-3">
        <Button
          size="md"
          color="gray"
          fullWidth
          className="oui-rounded-md"
          onClick={props.close}
        >
          {t("common.cancel")}
        </Button>
        <Button
          size="md"
          fullWidth
          className="oui-rounded-md"
          disabled={props.buttonDisabled}
          loading={props.confirmButtonLoading}
          onClick={props.onSave}
        >
          {t("common.save")}
        </Button>
      </Flex>
    </Flex>
  );
};
