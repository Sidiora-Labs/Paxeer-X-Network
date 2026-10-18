import { ExtensionPositionEnum, installExtension } from "@paxeer-network/ui";
import { DepositFormWidget } from "../depositForm";
import { DepositFormWidgetProps } from "../depositForm/depositForm.widget";

/** no need to use this function, because swap deposit is merged into deposit form */
export function installDeposit() {
  installExtension<DepositFormWidgetProps>({
    name: "deposit-form",
    scope: ["*"],
    positions: [ExtensionPositionEnum.DepositForm],
    __isInternal: true,
  })((props: DepositFormWidgetProps) => {
    return <DepositFormWidget onClose={props.onClose} />;
  });
}
