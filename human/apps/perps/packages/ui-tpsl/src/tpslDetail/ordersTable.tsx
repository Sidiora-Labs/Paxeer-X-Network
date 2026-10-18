import { API } from "@paxeer-network/types";
import { cn, ScrollArea } from "@paxeer-network/ui";
import { AuthGuardDataTable } from "@paxeer-network/ui-connector";
import { useColumn } from "./useColum";

export const OrdersTable = (props: {
  className?: string;
  orders: API.AlgoOrder[];
  editTPSLOrder: (order: API.AlgoOrder) => void;
  onCancelOrder: (order: API.AlgoOrder) => Promise<void>;
}) => {
  const { orders } = props;
  const columns = useColumn({ onCancelOrder: props.onCancelOrder });
  return (
    // <ScrollArea className={cn( props.className)}>

    <AuthGuardDataTable
      columns={columns}
      dataSource={orders}
      className="oui-bg-transparent oui-text-2xs"
      bordered
      classNames={{
        root: cn(props.className),
        header: "!oui-bg-base-8",
        scroll: cn(
          !orders || orders.length === 0
            ? "!oui-min-h-[170px]"
            : "!oui-min-h-[100px]",
        ),
      }}
      onRow={(record) => {
        return {
          className: cn(
            "oui-h-[53px] oui-cursor-svg-edit !oui-border-none !oui-p-0",
          ),
          onClick: () => {
            props.editTPSLOrder(record);
          },
        };
      }}
    />
    // </ScrollArea>
  );
};
