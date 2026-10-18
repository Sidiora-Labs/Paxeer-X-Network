import { FC } from "react";
import { useTranslation } from "@paxeer-network/i18n";
import { Badge } from "@paxeer-network/ui";
import { SymbolLeverageSheetId } from "@paxeer-network/ui-leverage";
import { LeverageBadge } from "../../desktop/components";
import { PositionCellState } from "./positionCell.script";

export const PositionTags: FC<{ item: PositionCellState["item"] }> = ({
  item,
}) => {
  const { t } = useTranslation();
  const isBuy = item.position_qty > 0;

  return (
    <div className="oui-flex oui-items-center oui-gap-1">
      <Badge color={isBuy ? "success" : "danger"} size="xs">
        {isBuy ? t("common.long") : t("common.short")}
      </Badge>
      <LeverageBadge
        symbol={item.symbol}
        leverage={item.leverage}
        modalId={SymbolLeverageSheetId}
        marginMode={item.margin_mode}
      />
    </div>
  );
};
