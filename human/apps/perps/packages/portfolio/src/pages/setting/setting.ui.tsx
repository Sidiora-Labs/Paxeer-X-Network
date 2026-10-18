import { FC } from "react";
import { useScreen } from "@paxeer-network/ui";
import { SettingScriptReturns } from "./setting.script";
import { SettingDesktop } from "./setting.ui.desktop";
import { SettingMobile } from "./setting.ui.mobile";

export const Setting: FC<SettingScriptReturns> = (props) => {
  const { isMobile } = useScreen();
  if (isMobile) {
    return (
      <div className="oui-px-3">
        <SettingMobile {...props} />
      </div>
    );
  }
  return <SettingDesktop {...props} />;
};
