import { createContext, useContext } from "react";
import { createStore, StateCreator, useStore } from "zustand";
import { devtools } from "zustand/middleware";
import { immer } from "zustand/middleware/immer";
import { SDKError } from "@paxeer-network/types";

type PaxeerNetworkStoreState = {
  // positions: PositionSlice;
};

export type ImmerStateCreator<T> = StateCreator<
  PaxeerNetworkStoreState,
  [["zustand/immer", never], never],
  [],
  T
>;

export const createPaxeerNetworkStore = (initialState: any) => {
  return createStore<PaxeerNetworkStoreState>()(
    immer(
      devtools((...args) => ({
        // positions: positionSlice(...args),
      })),
    ),
  );
};

type PaxeerNetworkStore = ReturnType<typeof createPaxeerNetworkStore>;

export const PaxeerNetworkStoreContext = createContext<PaxeerNetworkStore | null>(null);

export function usePaxeerNetworkStoreContext<T>(
  selector: (state: PaxeerNetworkStoreState) => T,
  equalityFn?: (left: T, right: T) => boolean,
): T {
  const store = useContext(PaxeerNetworkStoreContext);
  if (!store)
    throw new SDKError("Missing PaxeerNetworkStoreProvider.Provider in the tree");
  return useStore(store, selector);
  // return useStoreWithEqualityFn(store, selector, equalityFn)
}
