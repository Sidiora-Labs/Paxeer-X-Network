import { injectable } from "@paxeer-network/plugin-core";
import Comp from "./comp";

/** Props for Deposit.DepositForm injectable; used by plugins for typed interceptor */
export interface DepositFormProps {
  onOk?: () => void;
  position?: string;
  [k: string]: unknown;
}

/**
 * Wrapper that strips `position` from slot props before passing to Comp.
 * Plugins can intercept via 'Deposit.DepositForm' path.
 */
const DepositFormWithProps = (props: DepositFormProps) => {
  const { position: _position, ...rest } = props;
  return <Comp {...(rest as Record<string, never>)} />;
};

/** Injectable default for Deposit.DepositForm slot - plugins can intercept via PaxeerNetworkPluginProvider */
export const InjectableDepositForm = injectable(
  DepositFormWithProps,
  "Deposit.DepositForm",
);
