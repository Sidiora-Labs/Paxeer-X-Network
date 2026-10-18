import { FC } from "react";
import { useTranslation } from "@paxeer-network/i18n";
import { Button, cn, Flex } from "@paxeer-network/ui";
import type { AdjustMarginTab } from "../adjustMargin.script";

export interface MarginActionsProps {
  isAdd: boolean;
  onTabChange: (tab: AdjustMarginTab) => void;
}

export const MarginActions: FC<MarginActionsProps> = ({
  isAdd,
  onTabChange,
}) => {
  const { t } = useTranslation();

  const tabBase =
    "oui-h-7 oui-rounded-[4px] oui-text-xs oui-font-semibold oui-transition-colors";

  return (
    <Flex className="oui-w-full oui-gap-[6px]">
      <Button
        size="md"
        fullWidth
        variant="contained"
        color="secondary"
        className={cn(
          tabBase,
          isAdd && "oui-bg-base-5 oui-text-base-contrast-98",
          !isAdd &&
            "oui-bg-base-7 oui-text-base-contrast-54 hover:oui-text-base-contrast-80",
        )}
        onClick={() => onTabChange("add")}
      >
        {t("positions.adjustMargin.add")}
      </Button>
      <Button
        size="sm"
        fullWidth
        variant="contained"
        color="secondary"
        className={cn(
          tabBase,
          !isAdd &&
            "oui-bg-base-5 oui-text-base-contrast-98 hover:oui-text-base-contrast-80",
          isAdd &&
            "oui-bg-base-7 oui-text-base-contrast-54 hover:oui-text-base-contrast-80",
        )}
        onClick={() => onTabChange("reduce")}
      >
        {t("positions.adjustMargin.reduce")}
      </Button>
    </Flex>
  );
};
