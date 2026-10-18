import { FC, PropsWithChildren } from "react";
import { useRegisterExternalResources } from "../hooks/useRegisterExternalResources";
import type { AsyncResources, Resources } from "../types";

export type { AsyncResources };

export type ExternalLocaleProviderProps = PropsWithChildren<{
  resources?: Resources | AsyncResources;
}>;

/**
 * ExternalLocaleProvider lets host applications inject i18n resources that live
 * outside of this package (e.g. from another bundle, backend, or runtime loader).
 *
 * - When `resources` is a function, it will be invoked whenever the locale changes
 *   to lazily fetch the latest bundle for that locale.
 * - When `resources` is a static map, all provided locale bundles are registered
 *   synchronously on mount.
 *
 * This component renders no UI; it only manages i18n side effects and simply
 * returns its children. Same behavior as calling `useRegisterExternalResources(resources)`
 * in your own component under `LocaleProvider`.
 */
export const ExternalLocaleProvider: FC<ExternalLocaleProviderProps> = (
  props,
) => {
  useRegisterExternalResources(props.resources);

  return <>{props.children}</>;
};
