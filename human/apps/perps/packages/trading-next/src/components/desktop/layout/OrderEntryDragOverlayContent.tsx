/**
 * Drag overlay content for order-entry sortable panels.
 * Used by layout-split chrome inside DragOverlay; Trading owns widget definitions.
 */
import React, { useMemo } from "react";
import { OrderEntryWidget } from "@paxeer-network/ui-order-entry";
import { DepositStatusWidget } from "@paxeer-network/ui-transfer";
import { SortablePanel } from "./sortablePanel";

const LazyRiskRateWidget = React.lazy(() =>
  import("../riskRate").then((mod) => ({ default: mod.RiskRateWidget })),
);
const LazyAssetViewWidget = React.lazy(() =>
  import("../assetView").then((mod) => ({ default: mod.AssetViewWidget })),
);

/** Props required to render the overlay for one order-entry item */
export interface OrderEntryDragOverlayContentProps {
  /** Id of the panel being dragged (e.g. "margin" | "assets" | "orderEntry") */
  activeId: string | null;
  showPositionIcon: boolean;
  symbol: string;
  disableFeatures?: unknown;
  navigateToPortfolio?: () => void;
  isFirstTimeDeposit?: boolean;
}

/**
 * Renders the drag overlay content for the active order-entry sortable item.
 * Used by split layout chrome; does not render when activeId is null.
 */
export function OrderEntryDragOverlayContent(
  props: OrderEntryDragOverlayContentProps,
): React.ReactElement | null {
  const {
    activeId,
    showPositionIcon,
    symbol,
    disableFeatures,
    navigateToPortfolio,
    isFirstTimeDeposit,
  } = props;

  const orderInteractionWidgets = useMemo(
    () => ({
      margin: {
        className: "",
        element: (
          <React.Suspense fallback={null}>
            <LazyRiskRateWidget />
          </React.Suspense>
        ),
      },
      assets: {
        className: "oui-border oui-border-line-12",
        element: (
          <>
            <React.Suspense fallback={null}>
              <LazyAssetViewWidget
                isFirstTimeDeposit={isFirstTimeDeposit ?? false}
              />
            </React.Suspense>
            <DepositStatusWidget
              className="oui-mt-3 oui-gap-y-2"
              onClick={navigateToPortfolio}
            />
          </>
        ),
      },
      orderEntry: {
        className: "",
        element: (
          <OrderEntryWidget
            symbol={symbol}
            disableFeatures={
              disableFeatures as unknown as ("slippageSetting" | "feesInfo")[]
            }
          />
        ),
      },
    }),
    [isFirstTimeDeposit, disableFeatures, navigateToPortfolio, symbol],
  );

  if (!activeId) return null;
  const item =
    orderInteractionWidgets[activeId as keyof typeof orderInteractionWidgets];
  if (!item) return null;

  return (
    <SortablePanel
      id={activeId}
      showIndicator={showPositionIcon}
      dragOverlay
      className={`${item.className} oui-shadow-lg oui-shadow-base-9`}
    >
      {item.element}
    </SortablePanel>
  );
}
