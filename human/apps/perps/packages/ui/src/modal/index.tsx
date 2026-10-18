import { modalActions } from "./modalContext";
import { register, unregister, create } from "./modalHelper";

export { ModalProvider } from "./modalContext";

import { sheet } from "./preset/sheet";
import { alert } from "./preset/alert";
import { dialog } from "./preset/dialog";

import { confirm } from "./preset/confirm";
export type { ConfirmProps } from "./preset/confirm";
// import { actionSheet } from "@/modal/preset/actionSheet";
export { ConfirmDialog } from "./preset/confirm";

export type { ModalHocProps } from "./types";

export { ModalContext, ModalIdContext } from "./modalContext";

// @ebay/nice-modal-react
export { useModal } from "./useModal";

export const modal = {
  create,
  register,
  unregister,

  ...modalActions,
  // actionSheet,
  confirm,
  alert,
  sheet,
  dialog,
  // alert
  //actionSheet
  /// toast
};
