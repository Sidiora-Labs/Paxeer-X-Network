import { PropsWithChildren, useMemo } from "react";
import { createPaxeerNetworkStore, PaxeerNetworkStoreContext } from "./storeContext";

export const PaxeerNetworkStoreProvider = ({ children }: PropsWithChildren) => {
  const store = useMemo(() => createPaxeerNetworkStore({}), []);
  return (
    <PaxeerNetworkStoreContext.Provider value={store}>
      {children}
    </PaxeerNetworkStoreContext.Provider>
  );
};
