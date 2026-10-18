import React from "react";
import { useAccount } from "@paxeer-network/hooks";
import { DataTableClassNames, useScreen } from "@paxeer-network/ui";
import { useAssetsScript } from "./assets.script";
import type { useAssetsScriptReturn } from "./assets.script";
import { AssetsDataTable, AssetsTable } from "./assets.ui.desktop";
import { AssetsTableMobile } from "./assets.ui.mobile";

export type AssetsWidgetProps = useAssetsScriptReturn;

export type AssetsDataTableWidgetProps = {
  classNames?: {
    root?: string;
    desc?: string;
    scrollRoot?: string;
  };
  tbClassName?: string;
  dataTableClassNames?: DataTableClassNames;
};

export const AssetsDataTableWidget: React.FC<AssetsDataTableWidgetProps> = (
  props,
) => {
  const assetsState = useAssetsScript();
  const accountState = useAccount();
  return <AssetsDataTable {...assetsState} {...accountState} {...props} />;
};

export const AssetsWidget: React.FC = () => {
  const assetsState = useAssetsScript();
  const accountState = useAccount();
  const { isMobile } = useScreen();
  if (isMobile) {
    return <AssetsTableMobile {...assetsState} {...accountState} />;
  }
  return <AssetsTable {...assetsState} {...accountState} />;
};
