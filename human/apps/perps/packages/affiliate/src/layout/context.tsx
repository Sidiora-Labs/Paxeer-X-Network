import React, {
  PropsWithChildren,
  createContext,
  useMemo,
  useState,
} from "react";

type LayoutContextValue = {
  sideOpen: boolean;
  onSideOpenChange: (open: boolean) => void;
};

const LayoutContext = createContext<LayoutContextValue>({
  sideOpen: true,
} as LayoutContextValue);

export const LayoutProvider: React.FC<React.PropsWithChildren> = (props) => {
  const [sideOpen, setSideOpen] = useState(true);
  const memoizedValue = useMemo<LayoutContextValue>(
    () => ({ sideOpen: sideOpen, onSideOpenChange: setSideOpen }),
    [sideOpen, setSideOpen],
  );
  return (
    <LayoutContext.Provider value={memoizedValue}>
      {props.children}
    </LayoutContext.Provider>
  );
};
