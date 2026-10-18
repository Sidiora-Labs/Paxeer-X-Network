import { injectable } from "@paxeer-network/ui";
import { useAccountScript } from "./account.script";
import { Account } from "./account.ui";

/** Default mobile account menu - can be intercepted by plugins via Account.MobileAccountMenu path */
const InjectableAccount = injectable(Account, "Account.MobileAccountMenu");

export const AccountWidget = () => {
  const state = useAccountScript();
  return <Account {...state} />;
};

/**
 * Extension entry for mobile account menu. Uses injectable pattern so plugins can
 * register interceptors for `Account.MobileAccountMenu` via PaxeerNetworkPluginProvider.
 */
export const MobileAccountMenuExtension = () => {
  const state = useAccountScript();
  return <InjectableAccount {...state} />;
};
