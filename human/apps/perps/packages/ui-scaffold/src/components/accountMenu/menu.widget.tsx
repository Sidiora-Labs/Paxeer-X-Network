import { injectable } from "@paxeer-network/ui";
import { AccountMenu } from "./menu.ui";
import { useAccountMenu } from "./useWidgetBuilder.script";

/** Default account menu - can be intercepted by plugins via Account.AccountMenu path */
const InjectableAccountMenu = injectable(AccountMenu, "Account.AccountMenu");

export const AccountMenuWidget = () => {
  const state = useAccountMenu();
  return <AccountMenu {...state} />;
};

/**
 * Extension slot for account menu (connect wallet button). Uses injectable pattern -
 * plugins can register interceptors for 'Account.AccountMenu' via PaxeerNetworkPluginProvider.
 */
export const WalletConnectButtonExtension = () => {
  const state = useAccountMenu();
  return <InjectableAccountMenu {...state} />;
};
