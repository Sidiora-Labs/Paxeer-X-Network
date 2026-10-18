import React, { FC, useMemo } from "react";
import { Locale, LocaleContext } from "./context";

export type LocaleProviderProps = {
  locale: Locale;
  children?: React.ReactNode;
};

export const LocaleProvider: FC<LocaleProviderProps> = (props) => {
  const value = useMemo<Locale>(
    () => ({ ...props.locale, exist: true }),
    [props.locale]
  );

  return (
    <LocaleContext.Provider value={value}>
      {props.children}
    </LocaleContext.Provider>
  );
};
