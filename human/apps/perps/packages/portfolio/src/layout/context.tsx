import React, { createContext, useContext, useState, useMemo } from "react";
import type { RouterAdapter } from "@paxeer-network/types";

type LayoutContextValue = {
  sideOpen: boolean;
  onSideOpenChange: (open: boolean) => void;
  routerAdapter?: RouterAdapter;
};

const LayoutContext = createContext<LayoutContextValue>({
  sideOpen: true,
} as LayoutContextValue);

export const useLayoutContext = () => {
  return useContext(LayoutContext);
};

export const LayoutProvider: React.FC<
  React.PropsWithChildren<{ routerAdapter?: RouterAdapter }>
> = (props) => {
  const { routerAdapter, children } = props;
  const [sideOpen, setSideOpen] = useState(true);
  const memoizedValue = useMemo<LayoutContextValue>(
    () => ({
      sideOpen: sideOpen,
      onSideOpenChange: setSideOpen,
      routerAdapter: routerAdapter,
    }),
    [sideOpen, setSideOpen, routerAdapter],
  );
  return (
    <LayoutContext.Provider value={memoizedValue}>
      {children}
    </LayoutContext.Provider>
  );
};
