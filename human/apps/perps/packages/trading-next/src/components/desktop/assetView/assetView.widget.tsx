import React from "react";
import { useAssetViewScript } from "./assetView.script";
import { AssetView } from "./assetView.ui";

type AssetViewWidgetProps = {
  isFirstTimeDeposit?: boolean;
};

export const AssetViewWidget: React.FC<AssetViewWidgetProps> = (props) => {
  const state = useAssetViewScript();
  return <AssetView {...state} isFirstTimeDeposit={props.isFirstTimeDeposit} />;
};
