import { FC } from "react";
import { useAppConfig } from "@paxeer-network/react-app";

export const SecondaryLogo: FC = () => {
  const { appIcons } = useAppConfig();

  const { secondary } = appIcons || {};

  if (secondary?.img) {
    return <img src={secondary?.img} className="oui-w-5 oui-h-5" />;
  }

  if (secondary?.component) {
    return secondary.component;
  }

  return null;
};
