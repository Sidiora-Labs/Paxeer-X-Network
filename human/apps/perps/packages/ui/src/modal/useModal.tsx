import { FC, useCallback, useContext, useEffect } from "react";
import { ModalArgs, ModalHandler } from "./types";
import {
  ModalContext,
  hideModalCallbacks,
  modalActions,
  modalCallbacks,
  ModalIdContext,
} from "./modalContext";
import { getModalId } from "./utils";
import { register } from "./modalHelper";

export function useModal(): ModalHandler;
export function useModal(
  modal: string,
  args?: Record<string, unknown>
): ModalHandler;
export function useModal<
  T extends React.FC<any>,
  ComponentProps extends ModalArgs<T>,
  PreparedProps extends Partial<ComponentProps> = {} | ComponentProps,
  RemainingProps = Omit<ComponentProps, keyof PreparedProps> &
    Partial<ComponentProps>,
  ResolveType = unknown
>(
  modal: T,
  args?: PreparedProps
): Omit<ModalHandler, "show"> & {
  show: Partial<RemainingProps> extends RemainingProps
    ? (args?: RemainingProps) => Promise<ResolveType>
    : (args: RemainingProps) => Promise<ResolveType>;
};
export function useModal(modal?: any, args?: any): ModalHandler {
  const modals = useContext(ModalContext);
  const modalIdFromContext = useContext(ModalIdContext);

  let modalId: string | null;
  let isComponent = modal && typeof modal !== "string";

  if (!modal) {
    modalId = modalIdFromContext;
  } else {
    modalId = getModalId(modal);
  }

  if (!modalId) {
    throw new Error("modalId is required");
  }

  const id = modalId as string;

  useEffect(() => {
    if (isComponent) {
      register(id, modal, args);
    }
  }, [isComponent, modalId, modal, args]);

  const modalInfo = modals[id];

  // console.log("modalInfo", modalInfo);

  const show = useCallback(
    (args?: Record<string, unknown>) => modalActions.show(id, args),
    [id]
  );

  const hide = useCallback(() => modalActions.hide(id), [id]);

  const remove = useCallback(() => modalActions.remove(id), [id]);

  const onOpenChange = useCallback(
    (isOpen: boolean) => {
      // isOpen ? () => {} : hide();
      if (!isOpen) {
        // hide();
        reject("cancel");
        hide();
      }
    },
    [id]
  );

  const setStates = useCallback(
    (states: Record<string, unknown>) => {
      modalActions.setStates(id, states);
    },
    [id]
  );

  const updateArgs = useCallback(
    (args: Record<string, unknown>) => {
      modalActions.updateArgs(id, args);
    },
    [id]
  );

  const resolve = useCallback(
    (args?: unknown) => {
      modalCallbacks[id]?.resolve(args);
      delete modalCallbacks[id];
    },
    [id]
  );

  const reject = useCallback(
    (args?: unknown) => {
      modalCallbacks[id]?.reject(args);
      delete modalCallbacks[id];
    },
    [id]
  );

  const resolveHide = useCallback((args?: unknown) => {
    hideModalCallbacks[id]?.resolve(args);
    delete hideModalCallbacks[id];
  }, []);

  return {
    id,
    args: modalInfo?.args,
    states: modalInfo?.states,
    visible: !!modalInfo?.visible,
    keepMounted: !!modalInfo?.keepMounted,

    show,
    hide,
    onOpenChange,
    setStates,
    updateArgs,
    remove,
    /**
     * resolve the show Promise
     */
    resolve,
    /**
     * reject the show Promise
     */
    reject,
    /**
     * emit when modal is hidden
     */
    resolveHide,
  };
}
