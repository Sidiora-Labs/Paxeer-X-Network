import "i18next";
import { defaultNS, LocaleEnum } from "./constant";
import { en } from "./locale/en";

export type LocaleCode = keyof typeof LocaleEnum | (string & {});

type ExtendLocaleMessages = Record<`extend.${string}`, string>;

type EnType = typeof en;

// need to use interface to support extending the LocaleMessages type in other packages
export interface LocaleMessages extends EnType, ExtendLocaleMessages {}

export type Resources<T extends {} = {}> = {
  [key in LocaleCode]?: Partial<LocaleMessages & T>;
};

/** Async loader for a single locale/namespace message table (see `useRegisterExternalResources`, ExternalLocaleProvider, LocaleProvider `resources`). */
export type AsyncResources = (
  lang: LocaleCode,
  ns: string,
) => Promise<Record<string, string>>;

// https://www.i18next.com/overview/typescript#create-a-declaration-file
// Enhance the input parameter intelliSense for the t function.
declare module "i18next" {
  interface CustomTypeOptions {
    // custom namespace type, if you changed it
    // defaultNS: "translation";

    // custom resources type
    resources: {
      [defaultNS]: LocaleMessages;
    };
  }
}
