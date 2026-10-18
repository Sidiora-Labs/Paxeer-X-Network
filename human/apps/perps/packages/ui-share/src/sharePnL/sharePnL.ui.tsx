import { FC } from "react";
import { DesktopSharePnLContent } from "./desktop/content";
import { MobileSharePnLContent } from "./mobile/content";
import { SharePnLState } from "./sharePnL.script";

export const DesktopSharePnL: FC<SharePnLState> = (props) => {
  const {
    entity,
    baseDp,
    quoteDp,
    referralInfo,
    shareOptions,
    hide,
    brokerName,
  } = props;

  if (!shareOptions || !entity) {
    return null;
  }
  return (
    <DesktopSharePnLContent
      entity={entity}
      hide={hide}
      baseDp={baseDp}
      quoteDp={quoteDp}
      referral={referralInfo}
      shareOptions={shareOptions}
      brokerName={brokerName}
    />
  );
};

export const MobileSharePnL: FC<SharePnLState> = (props) => {
  const {
    entity,
    baseDp,
    quoteDp,
    referralInfo,
    shareOptions,
    hide,
    brokerName,
  } = props;
  if (!shareOptions || !entity) {
    return null;
  }
  return (
    <MobileSharePnLContent
      entity={entity}
      hide={hide}
      baseDp={baseDp}
      quoteDp={quoteDp}
      referral={referralInfo}
      shareOptions={shareOptions}
      brokerName={brokerName}
    />
  );
};
