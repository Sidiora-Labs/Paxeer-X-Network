import { useMemo, useState } from "react";
import { useTranslation } from "@paxeer-network/i18n";
import { toast } from "@paxeer-network/ui";
import { Decimal } from "@paxeer-network/utils";
import { useReferralCode } from "../../../../hooks/useReferralCode";
import type { TradingFeeRebateWidgetProps } from "./tradingFeeRebate.widget";

const PERCENT_INPUT_PATTERN = /^\d+$/;
const PERCENT_EDIT_PATTERN = /^\d*$/;

export const useTradingFeeRebateScript = (
  options: TradingFeeRebateWidgetProps,
) => {
  const { t } = useTranslation();
  const initialPercentage = useMemo(
    () => new Decimal(options.defaultRefereeKickbackRatio).mul(100),
    [options.defaultRefereeKickbackRatio],
  );
  const maxPercentage = useMemo(
    () => new Decimal(options.maxRefereeKickbackRatio).mul(100),
    [options.maxRefereeKickbackRatio],
  );
  const [percentage, setPercentage] = useState(() =>
    initialPercentage.toString(),
  );
  const [isRefreshing, setIsRefreshing] = useState(false);

  const { updateKickbackRatio, isMutating } = useReferralCode();

  const isValid = useMemo(() => {
    if (!PERCENT_INPUT_PATTERN.test(percentage)) return false;

    const value = new Decimal(percentage);
    return value.gte(0) && value.lte(maxPercentage);
  }, [percentage, maxPercentage]);

  const isChanged = isValid && !new Decimal(percentage).eq(initialPercentage);
  const buttonDisabled = !isChanged || isMutating || isRefreshing;
  const showError = percentage !== "" && !isValid;
  const tradingFeeRebateRate = useMemo(() => {
    if (!PERCENT_INPUT_PATTERN.test(percentage)) return undefined;

    const value = new Decimal(percentage);
    const cappedPercentage = value.gt(maxPercentage) ? maxPercentage : value;
    return new Decimal(options.directTradesRate)
      .mul(cappedPercentage)
      .div(100)
      .toNumber();
  }, [percentage, maxPercentage, options.directTradesRate]);

  const onChange = (value: string) => {
    if (PERCENT_EDIT_PATTERN.test(value)) {
      setPercentage(value);
    }
  };

  const onSave = async () => {
    if (buttonDisabled) return;

    try {
      const response = await updateKickbackRatio({
        referee_kickback_ratio: new Decimal(percentage).div(100).toNumber(),
      });

      if (!response.success) {
        toast.error(response.message || t("common.somethingWentWrong"));
        return;
      }

      setIsRefreshing(true);
      await options.onSuccess?.();
      toast.success(t("affiliate.tradingFeeRebate.save.success"));
      options.close?.();
    } catch (error) {
      const message =
        error instanceof Error ? error.message : t("common.somethingWentWrong");
      toast.error(message);
    } finally {
      setIsRefreshing(false);
    }
  };

  return {
    percentage,
    tradingFeeRebateRate,
    maxPercentage: maxPercentage.toString(),
    showError,
    buttonDisabled,
    confirmButtonLoading: isMutating || isRefreshing,
    onChange,
    onSave,
  };
};

export type TradingFeeRebateReturns = ReturnType<
  typeof useTradingFeeRebateScript
>;
