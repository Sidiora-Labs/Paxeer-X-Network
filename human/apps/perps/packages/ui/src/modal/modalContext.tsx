import React, {
  FC,
  PropsWithChildren,
  ReactElement,
  ElementType,
  createContext,
  useContext,
  useReducer,
  ReactNode,
} from "react";
import { ModalAction, ModalArgs, ModalCallbacks, ModalStore } from "./types";
import { getModalId } from "./utils";
import { MODAL_REGISTRY, register } from "./modalHelper";

export type ModalContextType = {};

const initialState: ModalStore = {};

// const MODAL_STACK: string[] = [];

export const ALREADY_MOUNTED: Record<string, boolean> = {};

export const ModalContext = createContext<ModalStore>(initialState);

export const ModalIdContext = createContext<string | null>(null);

export const modalCallbacks: ModalCallbacks = {};
export const hideModalCallbacks: ModalCallbacks = {};

let dispatch: React.Dispatch<ModalAction> = () => {
  throw new Error(
    "No dispatch method detected, did you embed your app with Modal.Provider?"
  );
};

//------- reducer actions --------
/**
 * show modal
 * @param id string modalId
 * @param args Record<string, unknown> modalArgs
 * @returns ModalAction
 */
function showModal(id: string, args?: any): ModalAction {
  return {
    type: "SHOW_MODAL",
    payload: {
      id,
      args,
    },
  };
}

/**
 * close modal
 * @param id string modalId
 * @returns ModalAction
 */
function hideModal(id: string): ModalAction {
  return {
    type: "HIDE_MODAL",
    payload: {
      id,
    },
  };
}

function removeModal(id: string): ModalAction {
  return {
    type: "DESTROY_MODAL",
    payload: {
      id,
    },
  };
}

function updateArgsAction(
  id: string,
  args: Record<string, unknown>
): ModalAction {
  return {
    type: "UPDATE_ARGS",
    payload: {
      id,
      args,
    },
  };
}

function setModalStates(
  id: string,
  states: Record<string, unknown>
): ModalAction {
  return {
    type: "SET_MODAL_STATES",
    payload: {
      id,
      states,
    },
  };
}

//------- reducer actions --------

const reducer = (state: ModalStore, action: ModalAction) => {
  const { id, args } = action.payload;
  // console.log("reducer action", action);
  switch (action.type) {
    case "SHOW_MODAL": {
      return {
        ...state,
        [id]: {
          ...state[id],
          id,
          args,
          visible: !!ALREADY_MOUNTED[id],
          delayVisible: !ALREADY_MOUNTED[id],
        },
      };
    }
    case "HIDE_MODAL": {
      return {
        ...state,
        [id]: {
          ...state[id],
          visible: false,
        },
      };
    }
    case "DESTROY_MODAL": {
      const newState = { ...state };
      delete newState[id];
      return newState;
    }
    case "UPDATE_ARGS": {
      return {
        ...state,
        [id]: {
          ...state[id],
          args: {
            ...state[id].args,
            ...args,
          },
        },
      };
    }
    case "SET_MODAL_STATES": {
      return {
        ...state,
        [id]: {
          ...state[id],
          states: {
            ...state.states,
            ...action.payload.states,
          },
        },
      };
    }
    default:
      return state;
  }
};

const ModalContainer: FC = () => {
  const modals = useContext(ModalContext);
  const visibleModalIds = Object.keys(modals).filter((id) => !!modals[id]);

  visibleModalIds.forEach((id) => {
    if (!MODAL_REGISTRY.has(id) && !ALREADY_MOUNTED[id]) {
      console.warn(
        `No modal found for id: ${id}. Please check the id or if it is registered or declared via JSX.`
      );
      return;
    }
  });

  const components = visibleModalIds
    .filter((id) => MODAL_REGISTRY.has(id))
    .map((id) => {
      return {
        id,
        ...MODAL_REGISTRY.get(id)!,
      };
    });

  // console.log("components", components, modals);

  return (
    <>
      {components.map((component) => {
        // console.log("component", component);

        const Comp: ElementType = component.comp;
        return (
          <Comp key={component.id} id={component.id} {...component.props} />
        );
      })}
    </>
  );
};

export const ModalProvider: FC<
  PropsWithChildren<{
    dialogBuilder?: (children: ReactNode) => ReactElement;
  }>
> = (props) => {
  const [state, dispatchOrigin] = useReducer(reducer, initialState);
  dispatch = dispatchOrigin;
  return (
    <ModalContext.Provider value={state}>
      {props.children}
      <ModalContainer />
    </ModalContext.Provider>
  );
};

function show<T extends any, C extends any>(
  modal: ElementType<C>,
  args?: ModalArgs<ElementType<C>>
): Promise<T>;
function show<T extends any>(
  modal: string,
  args?: Record<string, unknown>
): Promise<T>;
function show<T extends any, P extends any>(
  modal: string,
  args?: P
): Promise<T>;
function show<T extends any, P extends any>(
  modal: ElementType<any> | string,
  args?: any
): Promise<unknown> {
  const modalId = getModalId(modal);
  if (typeof modal !== "string" && !MODAL_REGISTRY.has(modalId)) {
    register(modalId, modal as ElementType);
  }

  dispatch(showModal(modalId, args));
  if (!modalCallbacks[modalId]) {
    let theResolve!: (args?: unknown) => void;
    // `!` tell ts that theResolve will be written before it is used
    let theReject!: (args?: unknown) => void;
    const promise = new Promise((resolve, reject) => {
      theResolve = resolve;
      theReject = reject;
    });
    modalCallbacks[modalId] = {
      resolve: theResolve,
      reject: theReject,
      promise,
    };
  }

  return modalCallbacks[modalId].promise as Promise<T>;
}

function hide<T>(modal: string | FC<any>): Promise<T>;
function hide(modal: string | FC<any>) {
  const modalId = getModalId(modal);
  dispatch(hideModal(modalId));

  delete modalCallbacks[modalId];

  if (!hideModalCallbacks[modalId]) {
    let theResolve!: (args?: unknown) => void;
    let theReject!: (args?: unknown) => void;
    const promise = new Promise((resolve, reject) => {
      theResolve = resolve;
      theReject = reject;
    });
    hideModalCallbacks[modalId] = {
      resolve: theResolve,
      reject: theReject,
      promise,
    };
  }

  return hideModalCallbacks[modalId].promise;
}

function remove(id: string) {
  dispatch(removeModal(id));
  delete modalCallbacks[id];
  delete hideModalCallbacks[id];
}

function setStates(id: string, states: Record<string, unknown>) {
  dispatch(setModalStates(id, states));
}

function updateArgs(id: string, args: Record<string, unknown>) {
  dispatch(updateArgsAction(id, args));
}

export const modalActions = {
  show,
  hide,
  remove,
  setStates,
  updateArgs,
};
