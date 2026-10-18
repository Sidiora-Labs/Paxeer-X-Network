import { createContext, useContext } from "react";
import { AppLogos } from "../types";

export type ThemeContextState = {
  appIcons?: AppLogos;
  brokerName: string;
};

export const AppConfigContext = createContext({} as ThemeContextState);

export const useAppConfig = () => {
  return useContext(AppConfigContext);
};
