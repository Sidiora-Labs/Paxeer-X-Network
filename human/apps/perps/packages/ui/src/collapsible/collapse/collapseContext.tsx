import { createContext, useContext } from "react";

export interface CollapseContextState {
  activeKey: string;
  setActiveKey: (key: string) => void;
}

export const CollapseContext = createContext<CollapseContextState>(
  {} as CollapseContextState,
);

export const useCollapseContext = () => {
  return useContext(CollapseContext);
};
