import React from "react";
import { AssetsWidget } from "./assetsPage/assets.widget";
import type { AssetsWidgetProps } from "./assetsPage/assets.widget";

export type AssetsPageProps = AssetsWidgetProps;

export const AssetsPage: React.FC = () => {
  return <AssetsWidget />;
};
