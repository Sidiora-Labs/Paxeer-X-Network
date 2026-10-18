import { useMemo } from "react";
import { useTranslation } from "@paxeer-network/i18n";
import { MarginMode } from "@paxeer-network/types";
import {
  Flex,
  Text,
  Tips,
  InfoCircleIcon,
  AddCircleIcon,
  Button,
  cn,
  modal,
  useScreen,
} from "@paxeer-network/ui";
import { LTVRiskTooltipWidget } from "../LTVRiskTooltip";

type AvailableProps = {
  canTrade: boolean;
  currentLtv: number;
  freeCollateral: number;
  quote?: string;
  marginMode?: MarginMode;
};

export const Available = (props: AvailableProps) => {
  const { canTrade, currentLtv, quote, freeCollateral, marginMode } = props;
  const { t } = useTranslation();
  const { isMobile } = useScreen();

  const showLTV = useMemo(() => {
    return (
      typeof currentLtv === "number" &&
      !Number.isNaN(currentLtv) &&
      currentLtv > 0
    );
  }, [currentLtv]);

  const availableTooltip = useMemo(() => {
    return marginMode === MarginMode.ISOLATED
      ? t("transfer.LTV.isolatedModeUsdcOnly")
      : t("transfer.LTV.availableCrossMarginHint");
  }, [marginMode, t]);

  const availableTipsContent = useMemo(
    () => (
      <div className="oui-text-pretty oui-text-2xs oui-leading-normal oui-text-base-contrast-80">
        {availableTooltip}
      </div>
    ),
    [availableTooltip],
  );

  return (
    <Flex
      itemAlign={"center"}
      justify={"between"}
      className="oui-orderEntry-available"
    >
      <Tips
        title={t("common.tips")}
        content={availableTipsContent}
        trigger={
          <Text
            size="2xs"
            className={cn(
              "oui-available-label oui-cursor-pointer",
              "oui-border-b oui-border-dashed oui-border-line-12",
            )}
          >
            {t("common.available")}
          </Text>
        }
      />
      <Flex itemAlign={"center"} justify={"center"} gap={1}>
        {showLTV && (
          <Tips
            title={t("common.tips")}
            content={<LTVRiskTooltipWidget marginMode={marginMode} />}
            trigger={
              <InfoCircleIcon
                className={"oui-cursor-pointer oui-text-warning oui-opacity-80"}
              />
            }
          />
        )}
        <Text.numeral
          unit={quote}
          size={"2xs"}
          className={"oui-available-value oui-text-base-contrast-80"}
          unitClassName={"oui-ms-1 oui-text-base-contrast-54"}
          dp={2}
          padding={false}
        >
          {canTrade ? freeCollateral : 0}
        </Text.numeral>
        <Button
          variant="text"
          size="xs"
          color="secondary"
          className="oui-available-deposit-icon oui-p-0 hover:oui-text-base-contrast-80"
          onClick={() => {
            // TODO: when we plan to move modal IDs to a public package, we need to use the ID from the public package
            const handleDomId = isMobile
              ? "DepositAndWithdrawWithSheetId"
              : "DepositAndWithdrawWithDialogId";
            modal.show(handleDomId, {
              activeTab: "deposit",
            });
          }}
        >
          <AddCircleIcon opacity={1} />
        </Button>
      </Flex>
    </Flex>
  );
};
