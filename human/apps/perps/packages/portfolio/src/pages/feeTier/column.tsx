import React, { useMemo } from "react";
import { useTranslation } from "@paxeer-network/i18n";
import type { Column } from "@paxeer-network/ui";
import { numberToHumanStyle } from "@paxeer-network/utils";
import type { FeeDataType } from "./feeTier.script";

const textStyle: React.CSSProperties = {
  fontVariantLigatures: "none",
};

export const useFeeTierColumns = () => {
  const { t } = useTranslation();
  return useMemo<Column<FeeDataType>[]>(() => {
    return [
      {
        title: t("portfolio.feeTier.column.tier"),
        dataIndex: "tier",
        align: "left",
        width: 100,
      },
      {
        title: `${t("portfolio.feeTier.column.30dVolume")} (USDC)`,
        dataIndex: "volume",
        align: "center",
        width: 180,
        render: (val, record) => {
          const { volume_min, volume_max, volume_node } = record;
          if (volume_node) {
            return volume_node;
          }
          if (!volume_min && !volume_max) {
            return <div style={textStyle}>--</div>;
          }
          if (volume_min && !volume_max) {
            return t("portfolio.feeTier.column.30dVolume.above", {
              volume: numberToHumanStyle(
                volume_min,
                volume_min === 2500000 ? 1 : 0,
              ),
            });
          }
          return (
            <div>
              {volume_min &&
                numberToHumanStyle(volume_min, volume_min === 2500000 ? 1 : 0)}
              {` - `}
              {volume_max &&
                numberToHumanStyle(volume_max, volume_max === 2500000 ? 1 : 0)}
            </div>
          );
        },
      },
      {
        title: t("portfolio.feeTier.column.maker"),
        dataIndex: "maker_fee",
        align: "right",
        width: 150,
      },
      {
        title: t("portfolio.feeTier.column.taker"),
        dataIndex: "taker_fee",
        align: "right",
        width: 150,
      },
    ];
  }, [t]);
};
