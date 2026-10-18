import { JSXElementConstructor, ReactElement } from "react";

export interface ModalState {
  id: string;
  args?: Record<string, unknown>;
  states?: Record<string, unknown>;
  visible?: boolean;
  delayVisible?: boolean;
  keepMounted?: boolean;
}

export interface ModalStore {
  [key: string]: ModalState;
}

export interface ModalAction {
  type: string;
  payload: {
    id: string;
    args?: Record<string, unknown>;
    states?: Record<string, unknown>;
  };
}

export interface ModalCallbacks {
  [modal: string]: {
    resolve: (value: unknown) => void;
    reject: (reason: unknown) => void;
    promise: Promise<unknown>;
  };
}

export type ModalArgs<T> = T extends
  | keyof JSX.IntrinsicElements
  | JSXElementConstructor<any>
  ? Omit<React.ComponentProps<T>, "id">
  : Record<string, unknown>;

export interface ModalHandler<Props = Record<string, unknown>>
  extends ModalState {
  visible: boolean;
  keepMounted: boolean;
  show: (args?: Props) => Promise<unknown>;
  hide: () => Promise<unknown>;
  onOpenChange: (visible: boolean) => void;
  resolve: (value?: unknown) => void;
  reject: (reason?: unknown) => void;

  remove: () => void;

  setStates: (states: Record<string, unknown>) => void;
  updateArgs: (states: Record<string, unknown>) => void;

  resolveHide: (args?: unknown) => void;
}

export interface ModalHocProps {
  id: string;
  defaultVisible?: boolean;
  keepMounted?: boolean;
}
