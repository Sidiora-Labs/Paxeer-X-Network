import React from "react";
import { useTranslation } from "@paxeer-network/i18n";
import { OrderStatus } from "@paxeer-network/types";
import { TabPanel, Tabs } from "@paxeer-network/ui";
import type { DesktopOrderListWidgetProps } from "./orderList/orderList.widget";
import { OrdersBuilderState } from "./orders.script";
import { TabType } from "./orders.widget";

const LazyDesktopOrderListWidget = React.lazy(() =>
  import("./orderList").then((mod) => {
    return { default: mod.DesktopOrderListWidget };
  }),
);

export const Orders: React.FC<OrdersBuilderState> = (props) => {
  const { t } = useTranslation();
  const { pnlNotionalDecimalPrecision, orderListRef } = props;
  const commonProps: Partial<DesktopOrderListWidgetProps> = {
    pnlNotionalDecimalPrecision: pnlNotionalDecimalPrecision,
  };
  return (
    <Tabs
      defaultValue={props.current || TabType.all}
      variant="contained"
      className="oui-h-full"
      classNames={{
        tabsContent: "oui-h-[calc(100%_-_28px)]",
      }}
    >
      <TabPanel value={TabType.all} title={t("common.all")}>
        <React.Suspense fallback={null}>
          <LazyDesktopOrderListWidget
            ref={orderListRef}
            type={TabType.all}
            pnlNotionalDecimalPrecision={pnlNotionalDecimalPrecision}
            sharePnLConfig={props.sharePnLConfig}
            {...commonProps}
          />
        </React.Suspense>
      </TabPanel>
      <TabPanel value={TabType.pending} title={t("orders.status.pending")}>
        <React.Suspense fallback={null}>
          <LazyDesktopOrderListWidget
            ref={orderListRef}
            type={TabType.pending}
            ordersStatus={OrderStatus.INCOMPLETE}
            {...commonProps}
          />
        </React.Suspense>
      </TabPanel>
      <TabPanel value={TabType.tp_sl} title={t("common.tpsl")}>
        <React.Suspense fallback={null}>
          <LazyDesktopOrderListWidget
            ref={orderListRef}
            type={TabType.tp_sl}
            ordersStatus={OrderStatus.INCOMPLETE}
            {...commonProps}
          />
        </React.Suspense>
      </TabPanel>
      <TabPanel value={TabType.filled} title={t("orders.status.filled")}>
        <React.Suspense fallback={null}>
          <LazyDesktopOrderListWidget
            ref={orderListRef}
            type={TabType.filled}
            ordersStatus={OrderStatus.FILLED}
            pnlNotionalDecimalPrecision={pnlNotionalDecimalPrecision}
            sharePnLConfig={props.sharePnLConfig}
            {...commonProps}
          />
        </React.Suspense>
      </TabPanel>
      <TabPanel value={TabType.cancelled} title={t("orders.status.canceled")}>
        <React.Suspense fallback={null}>
          <LazyDesktopOrderListWidget
            ref={orderListRef}
            type={TabType.cancelled}
            ordersStatus={OrderStatus.CANCELLED}
            {...commonProps}
          />
        </React.Suspense>
      </TabPanel>
      <TabPanel value={TabType.rejected} title={t("orders.status.rejected")}>
        <React.Suspense fallback={null}>
          <LazyDesktopOrderListWidget
            ref={orderListRef}
            type={TabType.rejected}
            ordersStatus={OrderStatus.REJECTED}
            {...commonProps}
          />
        </React.Suspense>
      </TabPanel>
      {/* <TabPanel value={TabType.orderHistory} title={t("orders.orderHistory")}>
        <React.Suspense fallback={null}>
          <LazyDesktopOrderListWidget
            type={TabType.orderHistory}
            {...commonProps}
          />
        </React.Suspense>
      </TabPanel> */}
    </Tabs>
  );
};
// ----------------- Orders ui component end -----------------
