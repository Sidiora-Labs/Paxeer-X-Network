import { FlagKeys, useFeatureFlag } from "@paxeer-network/hooks";
import { useTranslation } from "@paxeer-network/i18n";
import { OrderSide } from "@paxeer-network/types";
import { MarginMode } from "@paxeer-network/types";
import { cn, modal, Text, useScreen } from "@paxeer-network/ui";
import {
  SymbolLeverageDialogId,
  SymbolLeverageSheetId,
} from "@paxeer-network/ui-leverage";
import { Decimal } from "@paxeer-network/utils";
import {
  MarginModeSwitchDialogId,
  MarginModeSwitchSheetId,
} from "../marginModeSwitch";

type LeverageBadgeProps = {
  symbol: string;
  side: OrderSide;
  symbolLeverage?: number;
  marginMode?: MarginMode;
  disabled?: boolean;
};

export const LeverageBadge = (props: LeverageBadgeProps) => {
  const { symbol, side, symbolLeverage, disabled } = props;
  const { isMobile } = useScreen();
  const { t } = useTranslation();
  const { enabled } = useFeatureFlag(FlagKeys.IsolatedMargin);

  const marginMode = props.marginMode;

  const isDisabled = !!disabled;
  const isMarginModeDisabled = isDisabled || !enabled;
  const curLeverage = symbolLeverage ?? 1;

  const showLeverageModal = () => {
    if (isDisabled) return;

    const modalId = isMobile ? SymbolLeverageSheetId : SymbolLeverageDialogId;
    modal.show(modalId, {
      symbol,
      side,
      curLeverage,
      marginMode,
    });
  };

  const showMarginModeModal = () => {
    if (isMarginModeDisabled) {
      return;
    }

    const modalId = isMobile
      ? MarginModeSwitchSheetId
      : MarginModeSwitchDialogId;
    modal.show(modalId, {
      symbol,
    });
  };

  return (
    <div
      className={cn(
        "oui-flex oui-w-full oui-items-center oui-rounded-md oui-bg-base-6",
        "oui-orderEntry-leverage-btn",
        "oui-h-8",
        "oui-select-none",
      )}
      data-testid="oui-testid-orderEntry-margin-leverage"
    >
      <button
        type="button"
        className={cn(
          "oui-flex oui-flex-1 oui-items-center oui-justify-center oui-gap-x-1",
          "oui-px-3 oui-py-1.5",
          "oui-text-xs oui-font-semibold oui-text-base-contrast-54",
          isMarginModeDisabled
            ? "oui-cursor-not-allowed"
            : "oui-cursor-pointer",
        )}
        data-testid="oui-testid-orderEntry-margin-mode"
        aria-label={t("marginMode.switchMarginMode")}
        disabled={isMarginModeDisabled}
        onClick={showMarginModeModal}
      >
        <Text>
          {marginMode === undefined
            ? "--"
            : marginMode === MarginMode.ISOLATED
              ? t("marginMode.isolated")
              : t("marginMode.cross")}
        </Text>
      </button>
      <div className="oui-h-5 oui-w-px oui-bg-line" aria-hidden="true" />
      <button
        type="button"
        className={cn(
          "oui-flex oui-flex-1 oui-items-center oui-justify-center oui-gap-x-1",
          "oui-px-3 oui-py-1.5",
          "oui-text-xs oui-font-semibold oui-text-base-contrast-54",
          isDisabled ? "oui-cursor-not-allowed" : "oui-cursor-pointer",
        )}
        aria-label="Adjust leverage"
        disabled={isDisabled}
        onClick={showLeverageModal}
        data-testid="oui-testid-orderEntry-leverage"
      >
        <Text.numeral
          dp={0}
          rm={Decimal.ROUND_DOWN}
          unit="x"
          unitClassName="oui-ms-0"
        >
          {curLeverage}
        </Text.numeral>
      </button>
    </div>
  );
};
