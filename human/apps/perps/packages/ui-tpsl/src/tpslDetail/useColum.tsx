import { FC, SVGProps, useMemo, useState } from "react";
import { useTranslation } from "@paxeer-network/i18n";
import { API } from "@paxeer-network/types";
import {
  cn,
  Flex,
  Text,
  ThrottledButton,
  toast,
  Tooltip,
  useScreen,
} from "@paxeer-network/ui";
import { EstPnlRender } from "./components/estPnl";
import { OrderPriceRender } from "./components/orderPrice";
import { QtyRender } from "./components/qty";
import { TriggerPrice } from "./components/triggerPrice";
import { TypeRender } from "./components/type";

export const useColumn = (props: {
  onCancelOrder?: (order: API.AlgoOrder) => Promise<void>;
}) => {
  const { t } = useTranslation();
  const { isMobile } = useScreen();
  const { onCancelOrder } = props;

  const columns = useMemo(() => {
    const moblieColumns = [
      {
        title: t("tpsl.tpslDetail.qty"),
        dataIndex: "quantity",
        width: 70,
        className: "oui-py-2",
        render: (_: string, record: API.AlgoOrder) => (
          <QtyRender order={record} />
        ),
      },
      {
        title: t("common.type"),
        dataIndex: "type",
        width: 35,
        className: "oui-ps-1 oui-py-2",
        render: (_: string, record: API.AlgoOrder) => (
          <TypeRender order={record} />
        ),
      },
      {
        title: t("common.trigger"),
        dataIndex: "trigger",
        width: 70,
        className: "oui-ps-1 oui-py-2",
        render: (_: string, record: API.AlgoOrder) => {
          return <TriggerPrice order={record} />;
        },
      },
      {
        title: t("common.price"),
        dataIndex: "price",
        width: 70,
        className: "oui-py-2",
        render: (_: string, record: API.AlgoOrder) => (
          <OrderPriceRender order={record} />
        ),
      },

      {
        title: (
          <Tooltip
            className="oui-max-w-[280px] oui-bg-base-8 oui-p-3 oui-text-2xs oui-text-base-contrast"
            content={t("tpsl.tpslDetail.estPnl.tooltip")}
          >
            <Text className="oui-underline oui-decoration-dashed oui-underline-offset-2">
              {t("tpsl.tpslDetail.estPnl")}
            </Text>
          </Tooltip>
        ),
        dataIndex: "estpnl",
        width: 70,
        className: "!oui-pe-0 oui-py-2",
        render: (_: string, record: API.AlgoOrder) => (
          <EstPnlRender order={record} />
        ),
      },
    ];
    const desktopColums = [
      {
        title: t("tpsl.tpslDetail.qty"),
        dataIndex: "quantity",
        width: 70,
        className: cn(" oui-py-2 !oui-ps-5"),
        render: (_: string, record: API.AlgoOrder) => (
          <QtyRender order={record} />
        ),
      },
      {
        title: t("common.type"),
        dataIndex: "type",
        width: 35,
        className: "oui-ps-1 oui-py-2",
        render: (_: string, record: API.AlgoOrder) => (
          <TypeRender order={record} />
        ),
      },
      {
        title: t("common.trigger"),
        dataIndex: "trigger",
        width: 70,
        className: "oui-ps-1 oui-py-2",
        render: (_: string, record: API.AlgoOrder) => {
          return <TriggerPrice order={record} />;
        },
      },
      {
        title: t("common.price"),
        dataIndex: "price",
        width: 70,
        className: "oui-ps-1 oui-py-2",
        render: (_: string, record: API.AlgoOrder) => (
          <OrderPriceRender order={record} />
        ),
      },

      {
        title: (
          <Tooltip
            className="oui-max-w-[280px] oui-bg-base-8 oui-p-3 oui-text-2xs oui-text-base-contrast"
            content={t("tpsl.tpslDetail.estPnl.tooltip")}
          >
            <Text className="oui-underline oui-decoration-dashed oui-underline-offset-2">
              {t("tpsl.tpslDetail.estPnl")}
            </Text>
          </Tooltip>
        ),
        dataIndex: "estpnl",
        width: 70,
        className: "oui-ps-1 oui-py-2",
        render: (_: string, record: API.AlgoOrder) => (
          <EstPnlRender order={record} />
        ),
      },
      {
        title: "",
        dataIndex: "delete",
        width: 50,
        className: cn("oui-py-2 !oui-pe-5"),
        render: (_: any, record: API.AlgoOrder) => {
          return <CancelAllBtn order={record} onCancelOrder={onCancelOrder} />;
        },
      },
    ];
    if (isMobile) {
      return moblieColumns;
    }
    return desktopColums;
  }, [t, isMobile]);
  return columns;
};

export const FlexCell = (props: { children: React.ReactNode }) => {
  return (
    <Flex
      direction={"column"}
      justify={"center"}
      itemAlign={"start"}
      className="oui-text-2xs oui-h-[36px]"
    >
      {props.children}
    </Flex>
  );
};

export interface IconProps extends SVGProps<SVGSVGElement> {
  size?: number;
}
const DeleteIcon: FC<IconProps> = (props) => {
  const { size = 18 } = props;
  return (
    <svg
      xmlns="http://www.w3.org/2000/svg"
      width={size}
      height={size}
      viewBox="0 0 18 18"
      fill="currentColor"
      {...props}
    >
      <path d="M5.48081 15.375C5.10681 15.375 4.78731 15.2426 4.52231 14.9777C4.25744 14.7127 4.125 14.3932 4.125 14.0192V4.50004H3.375V3.37505H6.75V2.71167H11.25V3.37505H14.625V4.50004H13.875V14.0192C13.875 14.3981 13.7438 14.7188 13.4813 14.9813C13.2188 15.2438 12.8981 15.375 12.5192 15.375H5.48081ZM12.75 4.50004H5.25V14.0192C5.25 14.0866 5.27162 14.1419 5.31487 14.1852C5.35812 14.2284 5.41344 14.25 5.48081 14.25H12.5192C12.5769 14.25 12.6298 14.226 12.6778 14.1779C12.7259 14.1299 12.75 14.077 12.75 14.0192V4.50004ZM7.053 12.75H8.17781V6.00004H7.053V12.75ZM9.82219 12.75H10.947V6.00004H9.82219V12.75Z" />
    </svg>
  );
};

export const CancelAllBtn = (props: {
  order: API.AlgoOrder;
  onCancelOrder?: (order: API.AlgoOrder) => Promise<void>;
}) => {
  const [loading, setLoading] = useState(false);
  return (
    <ThrottledButton
      size="sm"
      loading={loading}
      variant="text"
      color="gray"
      onClick={(e) => {
        e.stopPropagation();
        setLoading(true);
        props
          .onCancelOrder?.(props.order)
          .then(
            () => {},
            (error) => {
              toast.error(error.message);
            },
          )
          .finally(() => {
            setLoading(false);
          });
      }}
    >
      <DeleteIcon className="oui-text-base-contrast-54 hover:oui-text-base-contrast oui-cursor-pointer" />
    </ThrottledButton>
  );
};
