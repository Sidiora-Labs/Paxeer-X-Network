import React from "react";
import {
  modal,
  registerSimpleDialog,
  registerSimpleSheet,
  useModal,
} from "@paxeer-network/ui";
import {
  MarginModeSettingsDialogId,
  MarginModeSettingsSheetId,
} from "../marginModeSettings";
import {
  useMarginModeSwitchScript,
  type MarginModeSwitchScriptOptions,
} from "./marginModeSwitch.script";
import {
  MarginModeSwitch,
  type MarginModeSwitchProps,
} from "./marginModeSwitch.ui";

export type MarginModeSwitchWidgetProps = Pick<
  MarginModeSwitchProps,
  "onOpenSettings"
> &
  MarginModeSwitchScriptOptions;

export const MarginModeSwitchWidget: React.FC<MarginModeSwitchWidgetProps> = (
  props,
) => {
  const state = useMarginModeSwitchScript(props);
  const { id: currentModalId } = useModal();

  const onOpenSettings = () => {
    const modalId =
      currentModalId === MarginModeSwitchSheetId
        ? MarginModeSettingsSheetId
        : MarginModeSettingsDialogId;
    modal.show(modalId, {});
  };

  return (
    <MarginModeSwitch
      {...state}
      onOpenSettings={props.onOpenSettings ?? onOpenSettings}
    />
  );
};

export const MarginModeSwitchSheetId = "MarginModeSwitchSheetId";
export const MarginModeSwitchDialogId = "MarginModeSwitchDialogId";

registerSimpleSheet(MarginModeSwitchSheetId, MarginModeSwitchWidget, {
  // Use custom header in widget UI for both web and mweb.
  title: undefined,
  closable: false,
  classNames: {
    content: "oui-bg-transparent !oui-px-0",
    body: "oui-p-0",
  },
});

registerSimpleDialog(MarginModeSwitchDialogId, MarginModeSwitchWidget, {
  // Use custom header in widget UI for both web and mweb.
  title: undefined,
  closable: false,
  classNames: {
    content: "oui-w-[360px] oui-bg-transparent !oui-px-0",
    body: "oui-p-0",
  },
});
