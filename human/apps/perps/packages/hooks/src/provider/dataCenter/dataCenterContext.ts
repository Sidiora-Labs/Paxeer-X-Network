import { createContext, useContext } from "react";

export type getKeyFunction = (index: number, prevData: any) => string | null;

export type DataCenterContextState = {
  registerKeyHandler: (key: string, handler: getKeyFunction) => void;
  unregisterKeyHandler: (key: string) => void;
};

export const DataCenterContext = createContext({} as DataCenterContextState);

export const useDataCenterContext = () => {
  return useContext(DataCenterContext);
};
