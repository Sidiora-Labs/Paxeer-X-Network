import React from "react";
import { useSettingScript } from "./setting.script";
import { Setting } from "./setting.ui";

export const SettingWidget: React.FC = () => {
  const state = useSettingScript();
  return <Setting {...state} />;
};
