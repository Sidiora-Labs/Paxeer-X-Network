import { registerSimpleDialog, registerSimpleSheet } from "@paxeer-network/ui";
import {
  useChainSelectorScript,
  UseChainSelectorScriptOptions,
} from "./chainSelector.script";
import { ChainSelector, ChainSelectorProps } from "./chainSelector.ui";
import { i18n } from "@paxeer-network/i18n";

export type ChainSelectorWidgetProps = UseChainSelectorScriptOptions &
  Pick<ChainSelectorProps, "isWrongNetwork" | "variant" | "className">;

export const ChainSelectorWidget = (props: ChainSelectorWidgetProps) => {
  const state = useChainSelectorScript(props);

  return (
    <ChainSelector
      {...state}
      variant={props.variant}
      isWrongNetwork={props.isWrongNetwork}
    />
  );
};

export const ChainSelectorDialogId = "ChainSelectorDialogId";
export const ChainSelectorSheetId = "ChainSelectorSheetId";

registerSimpleDialog(ChainSelectorDialogId, ChainSelectorWidget, {
  size: "lg",
  title: () => i18n.t("connector.switchNetwork"),
  variant: "wide",
  isWrongNetwork: true,
});

registerSimpleSheet(ChainSelectorSheetId, ChainSelectorWidget, {
  title: () => i18n.t("connector.switchNetwork"),
  classNames: {
    content: "!oui-bg-base-8",
    body: "!oui-bg-base-8",
  },
  variant: "compact",
  isWrongNetwork: true,
});
