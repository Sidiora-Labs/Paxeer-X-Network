import { useContext } from "react";
import {
  FallbackNs,
  useTranslation as _useTranslation,
  UseTranslationOptions,
  I18nContext,
} from "react-i18next";
type $Tuple<T> = readonly [T?, ...T[]];
import { FlatNamespace, KeyPrefix } from "i18next";
import i18n from "../i18n";

export function useTranslation<
  Ns extends FlatNamespace | $Tuple<FlatNamespace> | undefined,
  KPrefix extends KeyPrefix<FallbackNs<Ns>> = undefined,
>(ns?: Ns, options?: UseTranslationOptions<KPrefix>) {
  // @ts-ignore
  const context = useContext(I18nContext);
  return _useTranslation(ns, {
    i18n: context?.i18n || i18n,
    // @ts-ignore
    // when the language resource is loaded, notify the translation component to re-render
    bindI18nStore: "added",
    ...options,
  }) as unknown as {t:(key:string|string[],options?:any)=>string;i18n:typeof i18n;ready:boolean};
}
