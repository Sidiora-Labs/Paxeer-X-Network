import mockReact from "react";
import { act, create } from "react-test-renderer";
import {
  AlgoOrderRootType,
  OrderType,
  PositionType,
} from "@paxeer-network/types";
import { TPSLEditModal } from "./tpslEditModal";

jest.mock("@paxeer-network/ui", () => ({
  SimpleDialog: (props: any) =>
    mockReact.createElement("simple-dialog", props, props.children),
  SimpleSheet: (props: any) =>
    mockReact.createElement("simple-sheet", props, props.children),
}));

jest.mock("./tpsl.widget", () => ({
  TPSLWidget: (props: any) => mockReact.createElement("tpsl-widget", props),
}));

const createOrder = (algoType: AlgoOrderRootType) =>
  ({
    algo_order_id: 1,
    algo_type: algoType,
    symbol: "PERP_ETH_USDC",
    type: OrderType.MARKET,
    child_orders: [],
  }) as any;

describe("TPSLEditModal", () => {
  it.each([
    [AlgoOrderRootType.POSITIONAL_TP_SL, PositionType.FULL],
    [AlgoOrderRootType.TP_SL, PositionType.PARTIAL],
  ])("derives position type from %s", (algoType, positionType) => {
    const onOpenChange = jest.fn();
    const tree = create(
      <TPSLEditModal
        mode="dialog"
        open
        onOpenChange={onOpenChange}
        order={createOrder(algoType)}
      />,
    );
    const widget = tree.root.findByType("tpsl-widget" as any);

    expect(widget.props.positionType).toBe(positionType);
    expect(widget.props.isEditing).toBe(true);

    act(() => widget.props.close());
    expect(onOpenChange).toHaveBeenCalledWith(false);
    tree.unmount();
  });

  it("uses a sheet and unmounts editor content while closed", () => {
    const tree = create(
      <TPSLEditModal
        mode="sheet"
        open={false}
        onOpenChange={jest.fn()}
        order={createOrder(AlgoOrderRootType.TP_SL)}
      />,
    );

    expect(tree.root.findAllByType("simple-sheet" as any)).toHaveLength(1);
    expect(tree.root.findAllByType("tpsl-widget" as any)).toHaveLength(0);
    tree.unmount();
  });
});
