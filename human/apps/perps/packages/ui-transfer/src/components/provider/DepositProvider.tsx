import React from "react";
import { PropsWithChildren, createContext, useMemo } from "react";

export interface DepositContextState {}

export const DepositContext = createContext<DepositContextState>(
  {} as DepositContextState
);

export const DepositProvider: React.FC<
  PropsWithChildren<DepositContextState>
> = (props) => {
  const value = useMemo(() => ({}), []);

  return (
    <DepositContext.Provider value={value}>
      {props.children}
    </DepositContext.Provider>
  );
};
