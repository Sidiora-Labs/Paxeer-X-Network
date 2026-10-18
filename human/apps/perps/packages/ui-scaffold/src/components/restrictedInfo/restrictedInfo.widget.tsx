import { FC } from "react";
import { useRestrictedInfoScript } from "./restrictedInfo.script";
import { RestrictedInfo, RestrictedInfoProps } from "./restrictedInfo.ui";

export type RestrictedInfoWidgetProps = Pick<RestrictedInfoProps, "className">;

export const RestrictedInfoWidget: FC<RestrictedInfoWidgetProps> = (props) => {
  const state = useRestrictedInfoScript();
  return <RestrictedInfo {...props} {...state} />;
};
