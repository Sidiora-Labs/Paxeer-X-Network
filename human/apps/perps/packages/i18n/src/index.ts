import "./types";

export * from "react-i18next";
export { default as i18next, createInstance } from "i18next";
export { default as i18n, createI18nInstance } from "./i18n";
export * from "./provider";
export {
  useTranslation,
  useLocaleCode,
  useRegisterExternalResources,
} from "./hooks";
export * from "./types";
export * from "./constant";
export * from "./locale/en";
export * from "./utils";
export * from "./resourceBundles";
export * from "./external/utils";
