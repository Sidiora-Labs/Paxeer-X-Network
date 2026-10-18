import { i18n } from "@paxeer-network/i18n";
import {
  registerSimpleDialog,
  registerSimpleSheet,
  useModal,
} from "@paxeer-network/ui";
import { useReverseConvertScript } from "./reverseConvert.script";
import { ReverseConvertUI } from "./reverseConvert.ui";

export const ReverseConvertDialogId = "ReverseConvertDialogId";
export const ReverseConvertSheetId = "ReverseConvertSheetId";
export const ReverseConvertWidget = () => (
  <ReverseConvertUI {...useReverseConvertScript()} />
);
const ReverseConvertModal = () => {
  const { visible } = useModal();
  return visible ? <ReverseConvertWidget /> : null;
};
registerSimpleDialog(ReverseConvertDialogId, ReverseConvertModal, {
  size: "md",
  title: () => i18n.t("transfer.reverseConvert.action"),
  classNames: { content: "oui-border oui-border-line-6" },
});
registerSimpleSheet(ReverseConvertSheetId, ReverseConvertModal, {
  title: () => i18n.t("transfer.reverseConvert.action"),
});
