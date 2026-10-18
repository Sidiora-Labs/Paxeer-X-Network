import { FC, PropsWithChildren } from "react";
import { AppLogos } from "../types";
import { AppConfigContext } from "./appConfigContext";

export type ThemeContextState = {
  appIcons?: AppLogos;
  brokerName: string;
};

export const AppConfigProvider: FC<
  PropsWithChildren<{ appIcons?: AppLogos; brokerName: string }>
> = (props) => {
  return (
    <AppConfigContext.Provider value={props}>
      {props.children}
    </AppConfigContext.Provider>
  );
};
