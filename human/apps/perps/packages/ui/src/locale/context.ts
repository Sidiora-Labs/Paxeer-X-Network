import { createContext } from "react";
import { localeValues } from "./en";

type WidenLocale<T> = T extends string ? string : T extends (...args:any[])=>any ? T : T extends object ? {[K in keyof T]:WidenLocale<T[K]>} : T;
export type Locale = WidenLocale<typeof localeValues>;

export const LocaleContext = createContext<Locale | undefined>(undefined);
