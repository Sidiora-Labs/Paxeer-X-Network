import React, { FC } from "react";
import { cn, useScreen } from "@paxeer-network/ui";
import { VaultsProvider } from "../components/provider/vaults-provider";
import type { VaultsPageConfig } from "../types/vault";

const LazyVaultsHeaderWidget = React.lazy(() =>
  import("../components/vaults-header").then((mod) => {
    return { default: mod.VaultsHeaderWidget };
  }),
);

const LazyVaultsIntroductionWidget = React.lazy(() =>
  import("../components/vaults-introduction").then((mod) => {
    return { default: mod.VaultsIntroductionWidget };
  }),
);

const LazyAllVaultsWidget = React.lazy(() =>
  import("../components/all-vaults").then((mod) => {
    return { default: mod.AllVaultsWidget };
  }),
);

export type VaultsPageProps = {
  className?: string;
  config?: VaultsPageConfig;
};

export const VaultsPage: FC<VaultsPageProps> = (props) => {
  const { isMobile } = useScreen();
  return (
    <VaultsProvider {...props}>
      <div
        className={cn(
          "oui-relative oui-min-h-screen ",
          "oui-bg-base-10",
          isMobile ? "oui-px-3 oui-py-6" : "oui-px-6 oui-py-12",
          props?.className,
        )}
      >
        <div
          id="vaults-content"
          className={cn(
            "oui-mx-auto oui-flex oui-max-w-[1200px] oui-flex-col",
            isMobile ? "oui-gap-6" : "oui-gap-12",
          )}
        >
          <React.Suspense fallback={null}>
            <LazyVaultsHeaderWidget />
          </React.Suspense>
          <React.Suspense fallback={null}>
            <LazyVaultsIntroductionWidget />
          </React.Suspense>
          <React.Suspense fallback={null}>
            <LazyAllVaultsWidget />
          </React.Suspense>
        </div>
      </div>
    </VaultsProvider>
  );
};
